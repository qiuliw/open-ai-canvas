//! 易支付签名与验签。

use std::collections::BTreeMap;

use base64::{Engine as _, engine::general_purpose::STANDARD};
use rsa::{
    Pkcs1v15Sign, RsaPrivateKey, RsaPublicKey,
    pkcs8::{DecodePrivateKey, DecodePublicKey},
};
use sha2::{Digest, Sha256};

/// 规范化编码。
pub type Params = BTreeMap<String, String>;

/// 生成 MD5 签名。
pub fn generate_signature(data: &Params, key: &str) -> String {
    let content = signing_string(data);
    format!("{:x}", md5::compute(format!("{content}{key}")))
}

/// 生成签名原文。
pub fn signing_string(params: &Params) -> String {
    params
        .iter()
        .filter(|(key, value)| {
            key.as_str() != "sign" && key.as_str() != "sign_type" && !value.is_empty()
        })
        .map(|(key, value)| format!("{key}={value}"))
        .collect::<Vec<_>>()
        .join("&")
}

/// 生成 RSA-SHA256 签名。
pub fn rsa_sign(params: &Params, raw_key: &str) -> Result<String, String> {
    let key = parse_private_key(raw_key).map_err(|_| "签名失败，商户私钥错误".to_owned())?;
    let digest = Sha256::digest(signing_string(params).as_bytes());
    let signature = key
        .sign(Pkcs1v15Sign::new::<Sha256>(), &digest)
        .map_err(|error| error.to_string())?;
    Ok(STANDARD.encode(signature))
}

/// 验证 RSA-SHA256 签名。
pub fn rsa_verify(params: &Params, raw_key: &str) -> bool {
    let Ok(key) = parse_public_key(raw_key) else {
        return false;
    };
    let Ok(signature) = STANDARD.decode(params.get("sign").map(String::as_str).unwrap_or(""))
    else {
        return false;
    };
    let digest = Sha256::digest(signing_string(params).as_bytes());
    key.verify(Pkcs1v15Sign::new::<Sha256>(), &digest, &signature)
        .is_ok()
}

/// 按配置版本验证签名。
pub fn verification(data: &Params, config: &Params) -> bool {
    match config.get("version").map(String::as_str) {
        Some("1") => config
            .get("platform_public_key")
            .is_some_and(|key| rsa_verify(data, key)),
        Some("0") => {
            let Some(received) = data.get("sign") else {
                return false;
            };
            config
                .get("key")
                .is_some_and(|key| received == &generate_signature(data, key))
        }
        _ => false,
    }
}

fn parse_private_key(raw: &str) -> Result<RsaPrivateKey, String> {
    RsaPrivateKey::from_pkcs8_pem(&wrap_pem(raw, "PRIVATE KEY")).map_err(|error| error.to_string())
}

fn parse_public_key(raw: &str) -> Result<RsaPublicKey, String> {
    RsaPublicKey::from_public_key_pem(&wrap_pem(raw, "PUBLIC KEY"))
        .map_err(|error| error.to_string())
}

fn wrap_pem(raw: &str, label: &str) -> String {
    let raw = raw.trim();
    if raw.contains("BEGIN ") {
        return raw.to_owned();
    }
    let compact: String = raw
        .chars()
        .filter(|character| !character.is_whitespace())
        .collect();
    let body = compact
        .as_bytes()
        .chunks(64)
        .map(|chunk| String::from_utf8_lossy(chunk))
        .collect::<Vec<_>>()
        .join("\n");
    format!("-----BEGIN {label}-----\n{body}\n-----END {label}-----")
}

#[cfg(test)]
mod tests {
    use super::*;
    use rsa::{
        pkcs8::{EncodePrivateKey, EncodePublicKey, LineEnding},
        rand_core::OsRng,
    };

    #[test]
    fn md5_signature_sorts_and_filters_fields() {
        let params = Params::from([
            ("type".into(), "alipay".into()),
            ("empty".into(), String::new()),
            ("pid".into(), "1000".into()),
            ("sign".into(), "ignored".into()),
            ("sign_type".into(), "MD5".into()),
        ]);
        assert_eq!(
            generate_signature(&params, "secret"),
            format!("{:x}", md5::compute("pid=1000&type=alipaysecret"))
        );
    }

    #[test]
    fn rsa_signatures_accept_pem_and_bare_keys() {
        let private_key = RsaPrivateKey::new(&mut OsRng, 2048).unwrap();
        let public_key = RsaPublicKey::from(&private_key);
        let private_pem = private_key.to_pkcs8_pem(LineEnding::LF).unwrap();
        let public_pem = public_key.to_public_key_pem(LineEnding::LF).unwrap();
        let bare_private = private_pem
            .lines()
            .filter(|line| !line.starts_with("-----"))
            .collect::<String>();
        let bare_public = public_pem
            .lines()
            .filter(|line| !line.starts_with("-----"))
            .collect::<String>();
        let mut params = Params::from([("out_trade_no".into(), "ORDER-1".into())]);
        params.insert("sign".into(), rsa_sign(&params, &bare_private).unwrap());
        assert!(rsa_verify(&params, &bare_public));
        params.insert("out_trade_no".into(), "ORDER-2".into());
        assert!(!rsa_verify(&params, &bare_public));
    }
}
