//! 易支付下单与通知处理。

use serde::{Deserialize, Serialize};
use serde_json::Value;

use crate::signature::{Params, generate_signature, rsa_sign, verification};

/*
pay api  <-> 易支付：
MerchantOrderNo / merchantOrderNo <-> out_trade_no
AmountFen / amountFen（分） <-> money（元）
NotifyURL -> notify_url
ReturnURL -> return_url
providerTradeNo / eventId <- trade_no
providerStatus / paid <- trade_status
qr_code Checkout <- qrcode
redirect Checkout <- payurl / pay_info
*/

/// 下单请求。
#[derive(Debug, Deserialize)]
#[serde(rename_all = "PascalCase")]
pub struct CreateRequest {
    pub merchant_order_no: String,
    pub amount_fen: i64,
    pub expires_at: String,
    #[serde(rename = "NotifyURL")]
    pub notify_url: String,
    #[serde(rename = "ReturnURL")]
    pub return_url: String,
}

/// 收银台信息。
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Checkout {
    pub mode: String,
    pub value: String,
    pub expires_at: String,
}

/// 支付通知。
#[derive(Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Notification {
    pub event_id: String,
    pub merchant_order_no: String,
    pub provider_trade_no: String,
    pub provider_status: String,
    pub amount_fen: i64,
    pub currency: String,
    pub paid: bool,
}

/// 校验插件配置。
pub fn validate_config(config: &Params) -> Result<(), String> {
    if empty(config, "url")
        || empty(config, "pid")
        || !config.contains_key("version")
        || (config["version"] == "1"
            && (empty(config, "private_key") || empty(config, "platform_public_key")))
        || (config["version"] == "0" && empty(config, "key"))
    {
        return Err("重要参数缺失，请检查插件配置文件！".into());
    }
    match config["version"].as_str() {
        "0" | "1" => Ok(()),
        _ => Err("支付接口出错，下单失败！".into()),
    }
}

/// 创建支付订单。
pub async fn create_order(config: &Params, request: CreateRequest) -> Result<Checkout, String> {
    validate_config(config)?;

    let title = config
        .get("order_title")
        .map(|value| value.replace("${trade_no}", &request.merchant_order_no))
        .unwrap_or_else(|| format!("商品订单号:{}", request.merchant_order_no));
    let mut params = Params::from([
        ("pid".into(), config["pid"].clone()),
        ("name".into(), title),
        (
            "type".into(),
            config
                .get("payType")
                .cloned()
                .unwrap_or_else(|| "alipay".into()),
        ),
        ("money".into(), money_from_fen(request.amount_fen)),
        ("out_trade_no".into(), request.merchant_order_no.clone()),
        ("notify_url".into(), request.notify_url),
        ("return_url".into(), request.return_url),
        ("sitename".into(), request.merchant_order_no),
        ("clientip".into(), "127.0.0.1".into()),
        // 易支付会根据设备类型选择 PC 网页或手机支付；接口暂未透传 UA，先默认 PC。
        ("device".into(), "pc".into()),
    ]);

    let base = config["url"].trim_end_matches('/');
    let (endpoint, expected_code) = match config["version"].as_str() {
        "1" => {
            params.insert("method".into(), "jump".into());
            params.insert(
                "timestamp".into(),
                std::time::SystemTime::now()
                    .duration_since(std::time::UNIX_EPOCH)
                    .map_err(|error| error.to_string())?
                    .as_secs()
                    .to_string(),
            );
            let sign = rsa_sign(&params, &config["private_key"])?;
            params.insert("sign".into(), sign);
            params.insert("sign_type".into(), "RSA".into());
            (
                format!(
                    "{base}{}",
                    if config.get("mapi").is_some_and(|value| value == "1") {
                        "/api/pay/create"
                    } else {
                        "/api/pay/submit"
                    }
                ),
                0,
            )
        }
        "0" => {
            params.insert("sign".into(), generate_signature(&params, &config["key"]));
            params.insert("sign_type".into(), "MD5".into());
            (
                format!(
                    "{base}{}",
                    if config.get("mapi").is_some_and(|value| value == "1") {
                        "/mapi.php"
                    } else {
                        "/submit.php"
                    }
                ),
                1,
            )
        }
        _ => return Err("支付接口出错，下单失败！".into()),
    };

    if config.get("mapi").is_some_and(|value| value == "1") {
        return create_mapi_order(
            &endpoint,
            &params,
            expected_code,
            &config["version"],
            request.expires_at,
        )
        .await;
    }

    let query = form_urlencoded::Serializer::new(String::new())
        .extend_pairs(params.iter())
        .finish();
    Ok(Checkout {
        mode: "redirect".into(),
        value: format!("{endpoint}?{query}"),
        expires_at: request.expires_at,
    })
}

async fn create_mapi_order(
    endpoint: &str,
    params: &Params,
    expected_code: i64,
    version: &str,
    expires_at: String,
) -> Result<Checkout, String> {
    let response = reqwest::Client::builder()
        .timeout(std::time::Duration::from_secs(25))
        .build()
        .map_err(|_| "支付接口出错，请查看插件日志")?
        .post(endpoint)
        .form(params)
        .send()
        .await
        .map_err(|_| "支付接口出错，请查看插件日志")?;
    let body = response
        .bytes()
        .await
        .map_err(|_| "支付接口出错，请查看插件日志")?;
    let json: Value = serde_json::from_slice(&body).map_err(|_| "下单失败#0")?;
    let code = match &json["code"] {
        Value::Number(value) => value.as_i64(),
        Value::String(value) => value.trim().parse().ok(),
        _ => None,
    };
    if code != Some(expected_code) {
        return Err(json["msg"].as_str().unwrap_or("下单失败#1").to_owned());
    }
    let (mode, value) = if version == "1" {
        ("redirect", json["pay_info"].as_str().ok_or("下单失败#2")?)
    } else if let Some(value) = json["qrcode"].as_str().filter(|value| !value.is_empty()) {
        ("qr_code", value)
    } else if let Some(value) = json["payurl"].as_str().filter(|value| !value.is_empty()) {
        ("redirect", value)
    } else {
        return Err("支付接口出错，请查看插件日志".into());
    };
    Ok(Checkout {
        mode: mode.into(),
        value: value.into(),
        expires_at,
    })
}

/// 验证支付通知。
pub fn verify_notification(config: &Params, body: &[u8]) -> Result<Notification, String> {
    validate_config(config)?;
    let params: Params = form_urlencoded::parse(body).into_owned().collect();
    if !verification(&params, config) {
        return Err("签名校验失败".into());
    }
    if params.get("trade_status").map(String::as_str) != Some("TRADE_SUCCESS") {
        return Err("未支付成功".into());
    }
    let merchant_order_no = params
        .get("out_trade_no")
        .filter(|value| !value.is_empty())
        .ok_or("缺少商户订单号")?
        .clone();
    let provider_trade_no = params.get("trade_no").cloned().unwrap_or_default();
    let event_id = if provider_trade_no.is_empty() {
        merchant_order_no.clone()
    } else {
        provider_trade_no.clone()
    };
    Ok(Notification {
        event_id,
        merchant_order_no,
        provider_trade_no,
        provider_status: "TRADE_SUCCESS".into(),
        amount_fen: fen_from_money(params.get("money").map(String::as_str).unwrap_or(""))?,
        currency: "CNY".into(),
        paid: true,
    })
}

fn empty(config: &Params, key: &str) -> bool {
    config.get(key).is_none_or(String::is_empty)
}

fn money_from_fen(fen: i64) -> String {
    format!("{}.{:02}", fen / 100, fen % 100)
}

fn fen_from_money(value: &str) -> Result<i64, String> {
    let (yuan, fraction) = value.trim().split_once('.').unwrap_or((value.trim(), ""));
    if yuan.is_empty()
        || fraction.len() > 2
        || !fraction.chars().all(|character| character.is_ascii_digit())
    {
        return Err("支付金额无效".into());
    }
    let yuan: i64 = yuan.parse().map_err(|_| "支付金额无效")?;
    let fraction: i64 = format!("{fraction:0<2}")
        .parse()
        .map_err(|_| "支付金额无效")?;
    yuan.checked_mul(100)
        .and_then(|value| value.checked_add(fraction))
        .filter(|value| *value >= 0)
        .ok_or_else(|| "支付金额无效".into())
}

#[cfg(test)]
mod tests {
    use super::*;

    fn md5_config() -> Params {
        Params::from([
            ("url".into(), "https://pay.example".into()),
            ("pid".into(), "1000".into()),
            ("version".into(), "0".into()),
            ("key".into(), "secret".into()),
            ("payType".into(), "alipay".into()),
        ])
    }

    #[test]
    fn validates_version_specific_configuration() {
        assert!(validate_config(&md5_config()).is_ok());
        let mut config = md5_config();
        config.remove("key");
        assert!(validate_config(&config).is_err());
        config.insert("version".into(), "1".into());
        assert!(validate_config(&config).is_err());
    }

    #[test]
    fn converts_money_without_floating_point() {
        assert_eq!(money_from_fen(12345), "123.45");
        assert_eq!(fen_from_money("123.45").unwrap(), 12345);
        assert_eq!(fen_from_money("1.2").unwrap(), 120);
        assert!(fen_from_money("1.234").is_err());
    }

    #[test]
    fn verifies_md5_notification() {
        let config = md5_config();
        let mut params = Params::from([
            ("out_trade_no".into(), "ORDER-1".into()),
            ("trade_no".into(), "EPAY-1".into()),
            ("trade_status".into(), "TRADE_SUCCESS".into()),
            ("money".into(), "12.34".into()),
        ]);
        params.insert("sign".into(), generate_signature(&params, &config["key"]));
        params.insert("sign_type".into(), "MD5".into());
        let body = form_urlencoded::Serializer::new(String::new())
            .extend_pairs(params.iter())
            .finish();
        let notification = verify_notification(&config, body.as_bytes()).unwrap();
        assert_eq!(notification.event_id, "EPAY-1");
        assert_eq!(notification.amount_fen, 1234);
    }
}
