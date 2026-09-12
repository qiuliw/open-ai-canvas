//! yingce.payment/v1 RPC 入口。

mod pay;
mod signature;

use base64::{Engine as _, engine::general_purpose::STANDARD};
use serde::Deserialize;
use serde_json::{Value, json};
use signature::Params;
use tokio::io::{self, AsyncReadExt, AsyncWriteExt};

const RPC_VERSION: &str = "yingce.payment/v1"; // RPC API 版本
const MAX_INPUT: u64 = 2 << 20; // RPC 请求大小限制

/*
payment plugin api：

实现

validate_config        -> ()
create_order           -> Checkout
verify_notification    -> Notification

未实现

query_order            -> Result
close_order            -> Result
download_trade_bill    -> BillRecord[]
...
*/

#[derive(Debug, Deserialize)]
#[serde(rename_all = "camelCase")]
struct RpcRequest {
    version: String,
    operation: String,
    #[serde(default)]
    config: Params,
    #[serde(default)]
    request: Value, // 订单请求
    #[serde(default)]
    body_base64: String, // 异步通知请求
}

#[tokio::main]
async fn main() {
    // read stdin
    let mut input = Vec::new();
    let result = io::stdin()
        .take(MAX_INPUT + 1) // +1 判断是溢出截止还是正常截止
        .read_to_end(&mut input)
        .await;

    // handle
    let response = match result {
        Ok(_) if input.len() as u64 <= MAX_INPUT => handle_rpc(&input).await,
        _ => failure("invalid_request", "请求不是有效 JSON"),
    };

    // write stdout
    let mut output = serde_json::to_vec(&response).unwrap_or_default();
    output.push(b'\n');
    let _ = io::stdout().write_all(&output).await;
}

async fn handle_rpc(input: &[u8]) -> Value {
    // Parse
    let request: RpcRequest = match serde_json::from_slice(input) {
        Ok(request) => request,
        Err(_) => return failure("invalid_request", "请求不是有效 JSON"),
    };
    if request.version != RPC_VERSION {
        return failure("unsupported_version", "不支持的支付插件协议版本");
    }

    let result = match request.operation.as_str() {
        // () -> JSON null
        "validate_config" => pay::validate_config(&request.config).map(|_| Value::Null),
        "create_order" => match serde_json::from_value(request.request) {
            Ok(order) => pay::create_order(&request.config, order)
                .await
                .and_then(|checkout| {
                    serde_json::to_value(checkout).map_err(|error| error.to_string())
                }),
            Err(_) => Err("支付订单参数无效".to_owned()),
        },
        "verify_notification" => STANDARD
            .decode(request.body_base64)
            .map_err(|_| "支付通知正文不是有效 Base64".to_owned())
            .and_then(|body| pay::verify_notification(&request.config, &body))
            .and_then(|notification| {
                serde_json::to_value(notification).map_err(|error| error.to_string())
            }),
        "query_order" | "close_order" | "download_trade_bill" => {
            return failure("unsupported", "不支持该操作");
        }
        _ => return failure("unknown_operation", "未知支付插件操作"),
    };

    match result {
        Ok(data) => json!({"ok": true, "data": data}),
        Err(message) => failure("provider_error", &message),
    }
}

fn failure(code: &str, message: &str) -> Value {
    json!({"ok": false, "code": code, "message": message})
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn rejects_invalid_json_and_protocol_versions() {
        assert_eq!(handle_rpc(b"not-json").await["code"], "invalid_request");
        assert_eq!(
            handle_rpc(br#"{"version":"other","operation":"validate_config"}"#).await["code"],
            "unsupported_version"
        );
    }

    #[tokio::test]
    async fn reports_unsupported_operations() {
        let response =
            handle_rpc(br#"{"version":"yingce.payment/v1","operation":"close_order","config":{}}"#)
                .await;
        assert_eq!(response["code"], "unsupported");
    }
}
