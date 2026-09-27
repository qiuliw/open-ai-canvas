# NeoLink Seedance

复制自 Volcengine Ark Seedance，面向 NeoLink 中转：`baseUrl=https://neolink.com/api`，路径为 `/v1/contents/generations/tasks`，**不**拼接 `/api/v3`。

完整接口见 [docs/interface.md](docs/interface.md)。

---

# NeoLink Seedance 接口字段

## 协议身份

- 插件 ID：`neolink-seedance`。
- Provider ID：`neolink-seedance-video`。
- 能力：`video`。
- 默认 Base URL：`https://neolink.com/api`。
- 鉴权驱动：`bearer`。
- 创建：`POST /v1/contents/generations/tasks`。
- 查询：`GET /v1/contents/generations/tasks/{{taskId}}`。
- 取消：`DELETE /v1/contents/generations/tasks/{{taskId}}`。

## 配置字段

| 字段 | 类型 | 必填 | 含义 |
| --- | --- | --- | --- |
| `apiKey` | secret | 是 | API Key |

## 兼容边界

Base URL 已含 `/api`；插件路径只使用 `/v1/contents/generations/tasks`，避免再拼 `/api/v3`。
