## yingce.payment/v1

支持 `validate_config`、`create_order` 和 `verify_notification`，统一返回 JSON 响应。其他操作返回 `unsupported`。

<!-- YINGCE_MANIFEST_CONTRACT_START -->
## Manifest 完整接口定义

以下 JSON 与插件包内实际 `manifest.json` 逐字段一致，覆盖插件身份、权限、配置、鉴权、参数、校验、创建、Agent、查询、取消、结果下载、响应和 Agent 响应映射。`documentation` 字段的值就是当前完整文档；为避免文档在自身内部无限递归，JSON 中仅用等义占位文本表示正文。

```json
{
  "apiVersion": "yingce.plugin/v1",
  "id": "official-payment-epay",
  "name": "易支付",
  "version": "1.0.0",
  "author": "荔枝",
  "description": "支持所有易支付协议。",
  "enabled": true,
  "installable": true,
  "runtime": {
    "backend": "rpc",
    "backendEntry": "backend/provider"
  },
  "surfaces": [
    "wallet",
    "settings"
  ],
  "permissions": [
    "payment.create"
  ],
  "configuration": {
    "fields": [
      {
        "name": "publicBaseUrl",
        "type": "url",
        "label": "服务器公网地址",
        "required": true,
        "description": "用于生成异步通知和同步返回地址，必须可被支付网关访问。"
      },
      {
        "name": "url",
        "type": "url",
        "label": "支付网关",
        "required": true,
        "description": "易支付网关根地址。"
      },
      {
        "name": "pid",
        "type": "string",
        "label": "商户 ID",
        "required": true
      },
      {
        "name": "version",
        "type": "string",
        "label": "接口版本",
        "required": true,
        "default": "0",
        "description": "0=MD5，1=RSA。"
      },
      {
        "name": "key",
        "type": "password",
        "label": "商户密钥",
        "secret": true,
        "description": "V0/MD5 接口必填。"
      },
      {
        "name": "private_key",
        "type": "textarea",
        "label": "商户私钥",
        "secret": true,
        "description": "V1/RSA 接口必填，支持裸 Base64 或 PEM。"
      },
      {
        "name": "platform_public_key",
        "type": "textarea",
        "label": "平台公钥",
        "secret": true,
        "description": "V1/RSA 接口必填，支持裸 Base64 或 PEM。"
      },
      {
        "name": "mapi",
        "type": "string",
        "label": "MAPI 模式",
        "default": "0",
        "description": "填 1 使用 API 下单，填 0 跳转网关。"
      },
      {
        "name": "payType",
        "type": "string",
        "label": "支付方式",
        "default": "alipay",
        "description": "alipay / wxpay / qqpay / usdt。"
      },
      {
        "name": "order_title",
        "type": "string",
        "label": "订单标题",
        "default": "商品订单号:${trade_no}",
        "description": "可使用 ${trade_no} 代表系统订单号。"
      }
    ]
  },
  "contributes": {
    "paymentProviders": [
      {
        "id": "epay",
        "label": "易支付",
        "icon": "brand:alipay",
        "checkoutMode": "redirect",
        "identityFields": [
          "pid",
          "url"
        ],
        "expiryPolicy": {
          "defaultMinutes": 30,
          "minMinutes": 5,
          "maxMinutes": 1440
        },
        "notificationSuccess": {
          "status": 200,
          "contentType": "text/plain; charset=utf-8",
          "body": "success"
        },
        "notificationFailure": {
          "status": 400,
          "contentType": "text/plain; charset=utf-8",
          "body": "failure"
        }
      }
    ]
  },
  "documentation": "<当前插件的完整 documentation，由 README.md 与 docs/interface.md 拼接而成；为避免 JSON 递归，此处不重复展开正文。>"
}
```
<!-- YINGCE_MANIFEST_CONTRACT_END -->
