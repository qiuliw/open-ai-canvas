# External Model API Implementation Plan

**Goal:** 实现账号级 API Key 管理与受限模型调用。
**Architecture:** Key 鉴权与 Cookie 登录相互独立；HTTP → handler → service 别名 → app → repository。调用走 CreateTask 和 Worker。
**Tech Stack:** Go、Gin、GORM、SQLite/PostgreSQL。
**Spec:** external-model-api.md

## Global Constraints

只保存密钥散列；不绕过模型目录和计费；不修改用户已有工作；不推送部署。

## Review Focus

跨用户/跨 Key 访问、过期/吊销/禁用账号、畸形和超大 JSON、伪造渠道地址/凭证、媒体下载归属。

### Task 1: API Key 持久化与生命周期

- [x] 先写生命周期、过期和归属测试并观察失败。
- [x] 新增 APIKey model/repository/app，以及迁移 90；任务保存服务端 Key ID。
- [x] 运行 `go test ./internal/app -run TestAPIKey`，预期通过。

### Task 2: 受限外部模型调用

- [x] 先写 HTTP 鉴权、参数验证、Key 隔离、真实 Worker 假上游测试并观察失败。
- [x] 新增请求转换、结果投影、Key-only 模型与任务路由，Cookie-only Key 路由；限制请求大小与速率。
- [x] 运行专项测试，预期通过。

### Task 3: 文档与交付验证

- [x] 更新 OpenAPI、数据库文档、代码地图、待确认验收和 curl 专题。
- [x] 运行后端测试、专项 race、构建及差异检查，记录不能覆盖的环境。
- [x] 完成独立代码审查并修正实质问题，提交本地分支并导出补丁与使用说明。

用户要求精简测试后，删除已完成验证的一次性迁移探针及重复的配置结构断言；保留密钥权限、任务隔离、计费、系统指令和协议预算的回归测试。
