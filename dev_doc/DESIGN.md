# AI Gateway MVP — 技术方案

## 1. 项目概述

统一管理租户 API Key，代理 AI 模型请求（下游 mock），并记录用量。

核心能力：
- 租户与 Key 管理（scope / 启用禁用 / 过期）
- OpenAI 兼容代理（/v1/chat/completions, /v1/embeddings, /v1/models）
- 用量追踪与查询
- 完整 OpenAPI 3.0 spec
- docker compose 一键启动

---

## 2. 架构设计

```
┌──────────────────────────────────────────────────────────┐
│                     AI Gateway (Go)                      │
│                                                          │
│  ┌─────────────┐   ┌──────────────┐   ┌──────────────┐  │
│  │  Admin API  │   │  Proxy API   │   │ Usage Query  │  │
│  │             │   │ (OpenAI 兼容) │   │    API       │  │
│  │ - Tenant    │   │              │   │              │  │
│  │ - APIKey    │   │  Key 鉴权    │   │  聚合查询    │  │
│  │ - CRUD      │   │  Scope 校验  │   │              │  │
│  └──────┬──────┘   └──────┬───────┘   └──────┬───────┘  │
│         │                 │                  │          │
│         └─────────────────┼──────────────────┘          │
│                           │                             │
│                    ┌──────┴───────┐                     │
│                    │    MySQL 8.0  │                     │
│                    └──────────────┘                     │
└──────────────────────────┬───────────────────────────────┘
                           │ HTTP (OpenAI 格式)
                    ┌──────┴───────┐
                    │ Mock Provider│
                    │ (Go HTTP srv)│
                    └──────────────┘
```

**关键设计**：
- Gateway 与 Mock Provider 是独立进程，通过 HTTP 通信，与真实场景一致
- API Key 仅存 hash + 前缀，创建时返回明文一次
- 用量记录异步写入（channel + batch），不阻塞代理响应
- 管理 API 用 admin token 鉴权（`subtle.ConstantTimeCompare` 常量时间比较），代理 API 用租户 API Key 鉴权
- 请求体上限 10MB（`http.MaxBytesReader`），防止大请求攻击
- 每个代理请求附带 `X-Request-ID` 响应头（UUID），便于追踪
- 优雅关闭：收到 SIGINT/SIGTERM 后，先 flush 用量写入器，再关闭 DB 连接

---

## 3. 技术选型

| 组件 | 选型 | 理由 |
|------|------|------|
| Web 框架 | **Gin** | 生态成熟、中间件丰富、快速开发 |
| ORM | **GORM** | 自动迁移省时、链式查询简洁 |
| 数据库 | **MySQL 8.0** | 通用成熟、JSON 字段支持 scope、团队熟悉度高 |
| 日志 | **slog** (标准库) | 零依赖、结构化日志 |
| 配置 | 环境变量 + config struct | MVP 足够，无需 viper |
| OpenAPI | **手写 YAML** | 精确可控、与实现逐一校验 |
| Mock Provider | 独立 Go HTTP server | 模拟真实下游、可替换为真实 provider |

---

## 4. 数据模型

### 4.1 tenants

| 字段 | 类型 | 说明 |
|------|------|------|
| id | CHAR(36) PK | UUID |
| name | VARCHAR(100) | 租户名 |
| status | VARCHAR(20) | active / disabled |
| created_at | DATETIME | |
| updated_at | DATETIME | |

**实现位置**：`internal/model/tenant.go`

租户支持通过管理 API 更新名称和状态（启用/禁用）。中间件 `apikey_auth.go:58-66` 在每次代理请求时检查 tenant status，若为 `disabled` 则返回 403。

### 4.2 api_keys

| 字段 | 类型 | 说明 |
|------|------|------|
| id | CHAR(36) PK | UUID |
| tenant_id | CHAR(36) FK | 关联 tenants |
| key_hash | CHAR(64) | SHA-256 hash，不存明文 |
| key_prefix | VARCHAR(20) | 显示用，如 `sk-agw-abcd****` |
| name | VARCHAR(100) | Key 名称 |
| scopes | JSON | scope 配置（见 4.4） |
| status | VARCHAR(20) | active / disabled |
| expires_at | DATETIME NULL | NULL = 永不过期 |
| created_at | DATETIME | |
| updated_at | DATETIME | |

**实现位置**：`internal/model/apikey.go`

**安全设计**：
- Key 生成：`crypto/rand` 生成 32 字节随机数，hex 编码取前 32 字符，加 `sk-agw-` 前缀，总计 40 字符
- 存储：仅存 SHA-256 hash（64 字符 hex）和前缀（`sk-agw-` + 前 4 位 + `****`）
- 查询：鉴权时对输入 key 做 SHA-256，按 hash 查 DB，明文永不出现在数据库中
- `key_hash` 字段有 `uniqueIndex`，保证唯一性

### 4.3 usage_records

| 字段 | 类型 | 说明 |
|------|------|------|
| id | CHAR(36) PK | UUID |
| tenant_id | CHAR(36) | 关联租户 |
| key_id | CHAR(36) | 关联 API Key |
| model | VARCHAR(50) | 请求的模型名 |
| prompt_tokens | INT | 输入 token 数 |
| completion_tokens | INT | 输出 token 数 |
| total_tokens | INT | 总 token 数 |
| request_id | VARCHAR(64) | 请求追踪 ID |
| status_code | INT | HTTP 状态码 |
| latency_ms | INT | 请求延迟（毫秒） |
| created_at | DATETIME | 请求时间戳 |

**实现位置**：`internal/model/usage.go`

**索引**（复合索引，支持按时间范围查询）：
- `idx_usage_tenant_time`：`(tenant_id, created_at)` — 按 tenant + 时间查询
- `idx_usage_key_time`：`(key_id, created_at)` — 按 key + 时间查询
- `idx_usage_model_time`：`(model, created_at)` — 按 model + 时间查询

### 4.4 Scope 建模

```json
{
  "allowed_models": ["gpt-4", "gpt-3.5-turbo"],
  "allowed_endpoints": ["/v1/chat/completions", "/v1/embeddings"],
  "rate_limit_rpm": 100
}
```

| 字段 | 类型 | 默认 | 限制 | 说明 |
|------|------|------|------|------|
| allowed_models | []string | [] (全部允许) | max 50 | 允许调用的模型列表 |
| allowed_endpoints | []string | [] (全部允许) | max 20 | 允许调用的端点列表 |
| rate_limit_rpm | int | 0 (不限) | >= 0 | 每分钟请求上限 |

**实现位置**：`internal/model/apikey.go:19-23`（Scopes struct），`internal/handler/proxy.go:230-273`（校验逻辑）

**设计理由**：
- `allowed_models`：最常见的权限控制需求——某些 Key 只能用便宜模型
- `allowed_endpoints`：区分聊天 vs 嵌入权限（如只允许 chat 不允许 embeddings）
- `rate_limit_rpm`：防止滥用（MVP 实现简单计数器，非分布式限流）
- **空数组/零值 = 不限制**，符合"最小权限可按需收紧"原则。创建 Key 时若不设置 scopes，则该 Key 拥有租户内全部权限

**Scopes 在 DB 中的存储**：
- 使用 MySQL JSON 字段（`gorm:"type:json"`）
- 通过 `Scopes.Scan()` / `Scopes.Value()` 实现 `sql.Scanner` / `driver.Valuer` 接口
- `Scan` 处理 nil 和空 bytes 的情况（返回 nil scope = 空配置 = 不限制）

**Scope 校验流程**（`handler/proxy.go:91-126`）：
1. **Endpoint 校验**（`checkEndpoint`）：若 `allowed_endpoints` 为空则放行；否则检查请求路径是否在白名单中
2. **Model 校验**（`checkModel`）：若 `allowed_models` 为空则放行；否则从请求体解析 model，检查是否在白名单中
3. **Rate Limit 校验**（`checkRateLimit`）：若 `rate_limit_rpm` 为 0 则放行；否则检查当前分钟内请求数是否超限

---

## 5. API 设计

### 5.1 管理 API（Admin Token 鉴权）

Header: `Authorization: Bearer <ADMIN_TOKEN>`

Admin Token 使用 `subtle.ConstantTimeCompare` 常量时间比较，防止时序攻击。启动时校验 ADMIN_TOKEN 不为空且不等于默认值 `admin-secret-token`。

Admin API 附加限流：按客户端 IP 限流，20 RPM（`middleware/security.go:22-31`）。

| 方法 | 路径 | 说明 | 实现位置 |
|------|------|------|---------|
| POST | /admin/v1/tenants | 创建租户 | `handler/admin.go:41` |
| GET | /admin/v1/tenants | 列出租户 | `handler/admin.go:66` |
| GET | /admin/v1/tenants/:id | 获取租户详情 | `handler/admin.go:81` |
| PATCH | /admin/v1/tenants/:id | 更新租户（name/status） | `handler/admin.go:97` |
| DELETE | /admin/v1/tenants/:id | 删除租户 | `handler/admin.go:119` |
| POST | /admin/v1/tenants/:id/keys | 创建 API Key | `handler/admin.go:99` |
| GET | /admin/v1/tenants/:id/keys | 列出租户的 Key | `handler/admin.go:124` |
| GET | /admin/v1/keys/:id | 获取 Key 详情 | `handler/admin.go:139` |
| PATCH | /admin/v1/keys/:id | 更新 Key（status/scope/expires_at/name） | `handler/admin.go:157` |
| DELETE | /admin/v1/keys/:id | 删除 Key | `handler/admin.go:180` |
| GET | /admin/v1/usage | 查询用量 | `handler/usage.go:27` |

#### 创建租户

```bash
POST /admin/v1/tenants
{"name": "Acme Corp"}
→ 201 {"id":"t-uuid","name":"Acme Corp","status":"active","created_at":"...","updated_at":"..."}
```

校验：`name` 必填，1-100 字符。

#### 更新租户（名称/状态）

```bash
PATCH /admin/v1/tenants/:id
{"name": "New Name"}              # 更新名称
{"status": "disabled"}            # 禁用租户
{"status": "active"}              # 启用租户
→ 200 {完整 Tenant 对象}
```

校验：
- `name`：1-100 字符
- `status`：仅允许 `active` 或 `disabled`

#### 删除租户

```bash
DELETE /admin/v1/tenants/:id
→ 200 {"status":"deleted"}
```

硬删除，租户及关联数据（api_keys、usage_records）会被级联删除。

#### 创建 API Key

```bash
POST /admin/v1/tenants/:id/keys
{
  "name": "production-key",
  "scopes": {
    "allowed_models": ["gpt-4", "gpt-3.5-turbo"],
    "allowed_endpoints": ["/v1/chat/completions"],
    "rate_limit_rpm": 100
  },
  "expires_at": "2026-12-31T23:59:59Z"   // null = 永不过期
}
→ 201
{
  "id": "k-uuid",
  "key": "sk-agw-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx",  // 明文仅此一次返回
  "key_prefix": "sk-agw-abcd****",
  "name": "production-key",
  "scopes": {...},
  "status": "active",
  "expires_at": "2026-12-31T23:59:59Z",
  "created_at": "..."
}
```

校验：
- `name` 必填，1-100 字符
- `allowed_models` 最多 50 个
- `allowed_endpoints` 最多 20 个
- 租户必须存在

**Key 生成**：`crypto/rand` 生成 32 字节 → hex 编码取前 32 字符 → 加 `sk-agw-` 前缀 → 总计 40 字符。

#### 更新 Key（启用/禁用/scope/过期/名称）

```bash
PATCH /admin/v1/keys/:id
{"status": "disabled"}          // 禁用 Key
{"status": "active"}            // 启用 Key
{"scopes": {...}}               // 更新 scope
{"expires_at": "2026-12-31T23:59:59Z"}  // 设置过期
{"name": "new-name"}            // 更新名称
→ 200 {完整 Key 对象（不含明文 key）}
```

所有字段可选，仅更新提供的字段（`service/apikey.go:108-140`）。

#### 用量查询

```bash
GET /admin/v1/usage?tenant_id=xxx&key_id=xxx&model=gpt-4&start=2026-07-01T00:00:00Z&end=2026-07-08T23:59:59Z&group_by=model
→ 200
{
  "summary": {"total_requests": 150, "total_tokens": 45000},
  "groups": [
    {"model": "gpt-4", "requests": 100, "prompt_tokens": 20000, "completion_tokens": 15000, "total_tokens": 35000},
    {"model": "gpt-3.5-turbo", "requests": 50, "prompt_tokens": 5000, "completion_tokens": 5000, "total_tokens": 10000}
  ]
}
```

查询参数（全部可选）：

| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| tenant_id | UUID | — | 按租户过滤 |
| key_id | UUID | — | 按API Key过滤 |
| model | string | — | 按模型过滤 |
| start | RFC3339 | — | 起始时间 |
| end | RFC3339 | — | 结束时间 |
| group_by | enum | — | 分组维度：`model`、`day`、`hour`、`tenant` |
| page | int | 1 | 页码（≥1） |
| page_size | int | 20 | 每页条数（1-100） |
| format | enum | json | 导出格式：`json` 或 `csv` |

时间格式必须为 RFC3339（如 `2026-07-01T00:00:00Z`），否则返回 400。

`summary` 始终返回（全量聚合，不受分页影响）。`groups` 仅当 `group_by` 指定时非空。分页仅在有分组时生效。`format=csv` 时返回 `text/csv` 文件下载。

### 5.2 代理 API（租户 API Key 鉴权）

Header: `Authorization: Bearer sk-agw-xxxx`

| 方法 | 路径 | 说明 | 实现位置 |
|------|------|------|---------|
| POST | /v1/chat/completions | OpenAI 兼容 | `handler/proxy.go:50` |
| POST | /v1/embeddings | OpenAI 兼容 | `handler/proxy.go:70` |
| GET | /v1/models | 列出可用模型 | `handler/proxy.go:170` |

#### Chat Completions

```bash
POST /v1/chat/completions
Authorization: Bearer sk-agw-xxxx
{
  "model": "gpt-4",
  "messages": [{"role":"user","content":"Hello"}]
}
→ 200 (OpenAI 格式响应，含 usage 字段)
→ 响应头: X-Request-ID: <uuid>
```

请求体支持 `model`、`messages`（必填），以及 `temperature`、`max_tokens`、`stream`。

**SSE 流式响应**：当 `stream: true` 时，Gateway 使用无超时的 HTTP 客户端将请求转发至下游，逐 chunk 透传 SSE 事件（`text/event-stream`）给客户端。从最后一个携带 `usage` 字段的 chunk 中提取 token 用量，流结束后异步记录。Mock Provider 按单词拆分响应内容，每 50ms 发送一个 chunk，模拟真实 AI 服务的流式输出。

#### Embeddings

```bash
POST /v1/embeddings
Authorization: Bearer sk-agw-xxxx
{
  "model": "text-embedding-ada-002",
  "input": "hello world"
}
→ 200 (OpenAI 格式响应，含 usage 字段)
→ 响应头: X-Request-ID: <uuid>
```

#### Models

```bash
GET /v1/models
Authorization: Bearer sk-agw-xxxx
→ 200 {"object":"list","data":[{"id":"gpt-4","object":"model","created":1687882411,"owned_by":"openai"},...]}
```

返回 Mock Provider 支持的模型列表（gpt-4, gpt-4-turbo, gpt-3.5-turbo, text-embedding-ada-002）。

### 5.3 系统端点

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | /health | 健康检查，返回 `{"status":"ok"}` |
| GET | /dashboard/* | Dashboard 静态文件 |

---

## 6. 错误处理策略

所有错误响应统一为 OpenAI 兼容格式：`{"error": {"message": "...", "type": "..."}}`

### 6.1 鉴权错误（401）

| 场景 | HTTP Code | message | type | 实现位置 |
|------|-----------|---------|------|---------|
| 无 Authorization header | 401 | missing API key | auth_error | `middleware/apikey_auth.go:29` |
| Key 格式无效（非 sk-agw- 前缀或长度 < 12） | 401 | invalid API key format | auth_error | `middleware/apikey_auth.go:35` |
| Key 不存在 / hash 不匹配 | 401 | invalid API key | auth_error | `middleware/apikey_auth.go:42` |
| Key 已过期 | 401 | API key expired | auth_error | `middleware/apikey_auth.go:54` |
| 租户不存在（关联异常） | 401 | invalid API key | auth_error | `middleware/apikey_auth.go:61` |

### 6.2 权限错误（403）

| 场景 | HTTP Code | message | type | 实现位置 |
|------|-----------|---------|------|---------|
| Key 已禁用 | 403 | API key disabled | forbidden | `middleware/apikey_auth.go:48` |
| Tenant 已禁用 | 403 | tenant disabled | forbidden | `middleware/apikey_auth.go:65` |
| model 不在 allowed_models | 403 | model not allowed | forbidden | `handler/proxy.go:259` |
| endpoint 不在 allowed_endpoints | 403 | endpoint not allowed | forbidden | `handler/proxy.go:242` |

### 6.3 限流错误（429）

| 场景 | HTTP Code | message | type | 实现位置 |
|------|-----------|---------|------|---------|
| 超出 Key 的 rate_limit_rpm | 429 | rate limit exceeded | rate_limit | `handler/proxy.go:269` |
| Admin API IP 限流（20 RPM） | 429 | rate limit exceeded | rate_limit | `middleware/security.go:25` |

### 6.4 上游错误（502 / 504）

| 场景 | HTTP Code | message | type | 实现位置 |
|------|-----------|---------|------|---------|
| 下游不可达 / 连接失败 | 502 | upstream error | upstream_error | `handler/proxy.go:141` |
| 下游超时（30s） | 504 | upstream timeout | timeout | `handler/proxy.go:139` |

**502 vs 504 区分**：使用 `os.IsTimeout(err)` 判断是否为超时错误。超时 → 504，其他网络错误 → 502。

**用量记录**：即使上游错误（502/504），也会记录用量（token 数为 0），便于排查上游问题。

### 6.5 其他错误

| 场景 | HTTP Code | message | type |
|------|-----------|---------|------|
| 请求体无效 / 超过 10MB | 400 | invalid request body / request body too large | invalid_request_error |
| 资源不存在 | 404 | tenant not found / key not found | not_found |
| Panic 未捕获 | 500 | internal server error | server_error |

### 6.6 错误类型枚举

| type | 含义 |
|------|------|
| auth_error | 认证失败（401） |
| forbidden | 权限不足（403） |
| rate_limit | 限流（429） |
| invalid_request_error | 请求参数错误（400） |
| upstream_error | 下游服务错误（502） |
| timeout | 下游超时（504） |
| server_error | 内部错误（500） |
| not_found | 资源不存在（404） |

---

## 7. 代理流程

```
请求进入 → APIKeyAuth 中间件
  │
  ├─ 1. 提取 Bearer token
  │     ├─ 无 header → 401 auth_error
  │     ├─ 格式无效（非 sk-agw- 前缀/长度不足）→ 401 auth_error
  │     └─ 有效 token → 继续
  │
  ├─ 2. SHA-256 hash → 查 DB
  │     ├─ 不存在 → 401 auth_error
  │     └─ 存在 → 继续
  │
  ├─ 3. Key 状态检查
  │     ├─ status=disabled → 403 forbidden
  │     └─ active → 继续
  │
  ├─ 4. 过期检查
  │     ├─ 已过期 → 401 auth_error
  │     └─ 未过期/null → 继续
  │
  ├─ 5. Tenant 状态检查
  │     ├─ tenant disabled → 403 forbidden
  │     └─ active → 继续，存储 key 到 context
  │
  └─ Handler (proxyModelEndpoint)
       │
       ├─ 6. Scope: Endpoint 校验
       │     ├─ 不在 allowed_endpoints → 403 forbidden
       │     └─ 通过 → 继续
       │
       ├─ 7. 读取请求体（10MB 限制）
       │     └─ 解析 model 名
       │
       ├─ 8. Scope: Model 校验
       │     ├─ 不在 allowed_models → 403 forbidden
       │     └─ 通过 → 继续
       │
       ├─ 9. Scope: Rate Limit 校验
       │     ├─ 超限 → 429 rate_limit
       │     └─ 通过 → 继续
       │
       ├─ 10. 转发到下游（Mock Provider，30s 超时）
       │     ├─ 超时 → 504 timeout
       │     ├─ 连接失败 → 502 upstream_error
       │     └─ 成功 → 继续
       │
       ├─ 11. 异步记录用量
       │     └─ channel → batch writer → DB（100条/5秒刷盘）
       │
       └─ 12. 返回响应 + X-Request-ID header
```

**`/v1/models` 简化流程**：跳过步骤 7-8（不解析请求体和 model），直接执行 endpoint 校验 → rate limit 校验 → 转发。

**SSE 流式流程**（`stream=true`）：步骤 1-9 相同，步骤 10 使用无超时 HTTP 客户端，逐行读取下游 SSE 事件流 → 透传 `data: <json>` 行到客户端 → Flush → 从最后一个 chunk 提取 usage → 步骤 11 记录用量。Mock Provider 按单词拆分内容，50ms 间隔发送 chunk，末尾 chunk 携带 `usage` 和 `finish_reason`。

---

## 8. 用量追踪

### 8.1 记录机制

**异步批量写入**（`service/usage.go`）：

```
代理请求完成
  └─ ProxyHandler.recordUsage()
       └─ UsageWriter.Record()  →  非阻塞写入 channel（容量 1000）
                                   └─ channel 满 → 丢弃 + 日志告警
                                       └─ 后台 goroutine
                                            ├─ 每 100 条 → flush 到 DB
                                            └─ 每 5 秒   → flush 到 DB
```

**设计参数**：
- channel 容量：1000（缓冲峰值流量）
- batch size：100 条
- flush interval：5 秒
- 写入失败：日志告警，不重试（MVP 可接受少量丢失）

**记录字段**：每次代理请求记录 `tenant_id`、`key_id`、`model`、`prompt_tokens`、`completion_tokens`、`total_tokens`、`request_id`、`status_code`、`latency_ms`、`created_at`（时间戳）。

**异常请求也记录**：上游 502/504 错误时，token 数记为 0，但 status_code 和 latency 正常记录，便于排查上游问题。

### 8.2 查询机制

**查询 API**：`GET /admin/v1/usage`（`handler/usage.go:27`）

**过滤维度**（`repository/usage.go:61-107`）：

| 参数 | 字段 | 说明 |
|------|------|------|
| tenant_id | tenant_id | 按租户过滤 |
| key_id | key_id | 按API Key过滤 |
| model | model | 按模型过滤 |
| start | created_at >= | 起始时间 |
| end | created_at <= | 结束时间 |

**分组**：支持 `model`（按模型）、`day`（按天）、`hour`（按小时）、`tenant`（按租户）四种维度分组聚合，返回每组 requests、prompt_tokens、completion_tokens、total_tokens。

**分页**：当指定 `group_by` 时，结果按 `total_tokens` 降序排列，支持 `page` 和 `page_size` 分页。`total` 字段返回分组总数。

**导出**：`format=csv` 时返回 CSV 文件（含 summary + groups + 分页信息），Content-Type 为 `text/csv`。

**返回结构**：
```json
{
  "summary": {"total_requests": 150, "total_tokens": 45000},
  "groups": [
    {"model": "gpt-4", "requests": 100, "prompt_tokens": 20000, "completion_tokens": 15000, "total_tokens": 35000}
  ]
}
```

`summary` 始终返回（全量聚合），`groups` 仅当 `group_by=model` 时非空。

### 8.3 优雅关闭

进程收到 SIGINT/SIGTERM 后：
1. `UsageWriter.Shutdown()` — 标记 closed，关闭 channel，drain 剩余数据并 flush
2. `sqlDB.Close()` — 关闭数据库连接

确保 channel 中未刷盘的用量数据在进程退出前写入 DB。

---

## 9. Rate Limit 机制

**实现位置**：`service/ratelimit.go`

**算法**：固定窗口计数器（非滑动窗口），按 key_id 维度限流。

```
每个 Key 维护一个窗口：
  - counter: 当前分钟内请求数
  - resetAt: 窗口重置时间（now + 1min）

Allow(keyID, rpm):
  if rpm <= 0: return true（不限流）
  if 窗口过期: 重置 counter=1, resetAt=now+1min
  if counter < rpm: counter++, return true
  return false（超限）
```

**内存清理**：后台 goroutine 每 5 分钟清理过期窗口（`CleanupExpired`），防止内存泄漏。

**Admin API 限流**：按客户端 IP 限流，20 RPM（`middleware/security.go:22`）。

---

## 10. 安全设计

### 10.1 API Key 安全

| 措施 | 实现位置 | 说明 |
|------|---------|------|
| SHA-256 hash 存储 | `service/apikey.go:163-166` | 明文永不入库 |
| 明文仅创建时返回 | `service/apikey.go:96-105` | 后续查询只返回 prefix |
| crypto/rand 生成 | `service/apikey.go:169-175` | 密码学安全随机数 |
| 格式校验 | `middleware/apikey_auth.go:34` | 检查 sk-agw- 前缀 + 最小长度 |
| uniqueIndex | `model/apikey.go:49` | key_hash 唯一索引 |

### 10.2 Admin Token 安全

| 措施 | 实现位置 | 说明 |
|------|---------|------|
| 常量时间比较 | `middleware/admin_auth.go:26` | `subtle.ConstantTimeCompare` 防时序攻击 |
| 启动校验 | `config/config.go:23-27` | 拒绝空值和默认值 `admin-secret-token` |

### 10.3 HTTP 安全头

`middleware/security.go:11-18`：
- `X-Frame-Options: DENY` — 防止点击劫持
- `X-Content-Type-Options: nosniff` — 防止 MIME 嗅探
- `X-XSS-Protection: 1; mode=block` — XSS 过滤
- `Referrer-Policy: strict-origin-when-cross-origin` — 限制 Referrer 泄露

### 10.4 请求限制

- 请求体上限：10MB（`handler/proxy.go:101`，`http.MaxBytesReader`）
- 响应体上限：50MB（`handler/proxy.go:212`，`io.LimitReader`）
- 下游超时：30s（`handler/proxy.go:42`，`http.Client.Timeout`）
- Panic recovery：`middleware/recovery.go` 捕获 panic 返回 500

---

## 11. 项目结构

```
ai-gateway/
├── cmd/
│   ├── gateway/main.go          # Gateway 入口
│   └── mock-provider/main.go     # Mock Provider 入口
├── internal/
│   ├── config/                   # 配置加载
│   ├── model/                    # GORM 数据模型
│   ├── repository/               # 数据访问层
│   ├── service/                  # 业务逻辑
│   │   ├── tenant.go
│   │   ├── apikey.go
│   │   ├── usage.go              # 异步批量写入
│   │   └── ratelimit.go          # 内存限流器
│   ├── handler/                  # HTTP handler
│   │   ├── admin.go              # 管理 API
│   │   ├── proxy.go              # 代理 API
│   │   └── usage.go              # 用量查询
│   ├── middleware/
│   │   ├── admin_auth.go         # Admin Token 鉴权
│   │   ├── apikey_auth.go        # API Key 鉴权
│   │   ├── recovery.go           # Panic recovery
│   │   └── security.go           # 安全头 + 通用限流
│   └── router/
│       └── router.go             # 路由注册
├── pkg/
│   └── openai/types.go           # OpenAI 类型定义
├── web/                          # 前端 Dashboard
│   ├── index.html
│   ├── app.js
│   └── style.css
├── docs/
│   └── openapi.yaml              # OpenAPI 3.0 spec
├── test/
│   └── e2e/                      # E2E 测试
├── .env.example
├── docker-compose.yml
├── Dockerfile
├── Makefile
├── go.mod
└── README.md
```

---

## 12. OpenAPI 3.0 Spec

**文件位置**：`docs/openapi.yaml`

### 12.1 覆盖范围

| 端点 | 方法 | 请求体 Schema | 响应体 Schema | 错误响应 |
|------|------|--------------|--------------|---------|
| /health | GET | — | HealthResponse | — |
| /admin/v1/tenants | POST | CreateTenantRequest | Tenant | 400, 401, 429 |
| /admin/v1/tenants | GET | — | Tenant[] | 401, 429 |
| /admin/v1/tenants/{id} | GET | — | Tenant | 401, 404, 429 |
| /admin/v1/tenants/{id} | PATCH | UpdateTenantRequest | Tenant | 400, 401, 404, 429 |
| /admin/v1/tenants/{id} | DELETE | — | {status: deleted} | 401, 404, 429 |
| /admin/v1/tenants/{id}/keys | POST | CreateKeyRequest | CreateKeyResponse | 400, 401, 429 |
| /admin/v1/tenants/{id}/keys | GET | — | APIKey[] | 401, 429 |
| /admin/v1/keys/{id} | GET | — | APIKey | 401, 404, 429 |
| /admin/v1/keys/{id} | PATCH | UpdateKeyRequest | APIKey | 400, 401, 429 |
| /admin/v1/keys/{id} | DELETE | — | {status: deleted} | 401, 429 |
| /admin/v1/usage | GET | — | UsageResult | 400, 401, 429 |
| /v1/chat/completions | POST | ChatCompletionRequest | ChatCompletionResponse / SSE stream | 400, 401, 403, 429, 502, 504 |
| /v1/embeddings | POST | EmbeddingRequest | EmbeddingResponse | 400, 401, 403, 429, 502, 504 |
| /v1/models | GET | — | ModelsResponse | 401, 403, 429, 502 |

### 12.2 Schema 定义

| Schema | 说明 |
|--------|------|
| Error | 统一错误响应 `{"error":{"message":"...","type":"..."}}` |
| Tenant | 租户对象 |
| CreateTenantRequest | 创建租户请求 |
| Scopes | Key 权限范围（allowed_models / allowed_endpoints / rate_limit_rpm） |
| CreateKeyRequest | 创建 Key 请求 |
| CreateKeyResponse | 创建 Key 响应（含明文 key） |
| APIKey | Key 详情（不含明文 key） |
| UpdateKeyRequest | 更新 Key 请求（所有字段可选） |
| UsageSummary | 用量汇总（total_requests / total_tokens） |
| UsageGroup | 按 model 分组的用量 |
| UsageResult | 用量查询结果（summary + groups） |
| UsageRecord | 单条用量记录（内部模型） |
| ChatCompletionRequest | Chat 请求（model / messages / temperature / max_tokens / stream） |
| ChatCompletionResponse | Chat 响应（含 choices + usage） |
| TokenUsage | Token 用量（prompt / completion / total） |
| EmbeddingRequest | Embedding 请求（model / input） |
| EmbeddingResponse | Embedding 响应（含 data + usage） |
| ModelsResponse | 模型列表响应 |
| HealthResponse | 健康检查响应 |

### 12.3 可复用 Responses

| Response | HTTP Code | 说明 |
|----------|-----------|------|
| BadRequest | 400 | 请求参数错误 |
| Unauthorized | 401 | 认证失败 |
| Forbidden | 403 | 权限不足 |
| NotFound | 404 | 资源不存在 |
| RateLimited | 429 | 请求过多 |
| UpstreamError | 502 | 下游服务错误 |
| Timeout | 504 | 下游超时 |

### 12.4 Security Schemes

| Scheme | 类型 | 用途 |
|--------|------|------|
| AdminAuth | http bearer | 管理 API 鉴权 |
| APIKeyAuth | http bearer | 代理 API 鉴权（sk-agw-* 格式） |

---

## 13. Docker Compose 编排

```yaml
services:
  mysql:       # MySQL 8.0
  gateway:     # AI Gateway (depends_on mysql, 内置 Dashboard 静态托管)
  mock-provider: # Mock AI Provider
```

一键启动：`docker compose up -d`

环境变量（.env）：
```
DB_HOST=mysql
DB_PORT=3306
DB_USER=gateway
DB_PASS=gateway
DB_NAME=ai_gateway
ADMIN_TOKEN=<your-secure-token>    # 必填，不能是 admin-secret-token
MOCK_PROVIDER_URL=http://mock-provider:8080
LISTEN_ADDR=:8080
```

---

## 14. Dashboard 前端

极简单页应用，无构建工具，纯 HTML + JS + CSS，由 Gateway 直接静态托管。

**技术选型**：原生 JS，不引入 Node/npm 构建链。

**功能页面**：

| 页面 | 功能 |
|------|------|
| Tenant 管理 | 列出 / 创建租户，查看租户下 Key 列表 |
| Key 管理 | 创建 Key（设置 scope / 过期时间）、启用/禁用、删除 |
| 用量查看 | 按 tenant / key / model / 时间范围查询，展示汇总表格 |

**鉴权**：前端页面输入 Admin Token，存 localStorage，后续请求带 `Authorization: Bearer <token>`。

**路由**：Gateway 通过 `/dashboard` 前缀托管静态文件，不影响 `/admin/v1/*` 和 `/v1/*` API 路由。

---

## 15. 设计决策与 Trade-off

### 15.1 API Key 存储：hash + prefix
**决策**：只存 SHA-256 hash 和前缀，明文仅在创建时返回一次。
**Trade-off**：无法找回明文，但安全性高。符合行业最佳实践（如 OpenAI、Stripe）。

### 15.2 用量记录：异步 batch 写入
**决策**：用量记录通过 channel 异步写入，batch（每 100 条或每 5 秒刷一次）。
**Trade-off**：进程崩溃可能丢失少量用量数据。MVP 可接受。生产环境可用 WAL 或消息队列补充。

### 15.3 Rate Limit：内存计数器
**决策**：单机内存固定窗口计数器，按 key_id 维度。
**Trade-off**：多实例部署时限流不精确。MVP 单实例可接受。生产环境需 Redis。

### 15.4 数据库：MySQL vs SQLite
**决策**：MySQL。
**Trade-off**：docker compose 多一个容器，但获得了 JSON 字段、并发、聚合查询能力，团队熟悉度高。

### 15.5 OpenAPI：手写 vs 代码生成
**决策**：手写 YAML spec。
**Trade-off**：需要手动保持一致，但精确度更高、可控性更强。

### 15.6 Mock Provider：独立进程
**决策**：独立 HTTP server，而非内置 mock。
**Trade-off**：多一个进程管理，但架构更真实，未来替换为真实 provider 零改动。

### 15.7 Dashboard：无构建工具的纯静态页面
**决策**：原生 HTML + JS + CSS，Gateway 直接托管。
**Trade-off**：无组件化、无热更新，但零构建依赖、部署简单。

---

## 16. 已知限制

1. **单实例部署** — 不支持水平扩展（rate limit 用内存、usage writer 用单 channel）
2. **无 TLS** — MVP 在容器内通信，生产需加 TLS 终止层
3. **用量记录可能丢失** — 异步写入，进程崩溃时 channel 中未刷盘的数据会丢失
4. **无 Key 轮换机制** — 需手动创建新 Key + 禁用旧 Key
5. **无多 provider 路由** — MVP 仅对接一个 mock provider，不支持按 model 路由到不同 provider
6. **admin token 无过期** — 固定 token，无 JWT/刷新机制

---

## 17. 自测清单

- [x] `docker compose up -d` 可启动全部服务
- [x] 创建 tenant → 创建 key → 拿到明文 key
- [x] 用 key 调用 `/v1/chat/completions`，返回 OpenAI 格式响应
- [x] 用量记录出现在 DB 且可查询
- [x] 无效 key → 401
- [x] 过期 key → 401
- [x] 禁用 key → 403
- [x] 越权 model → 403
- [x] Dashboard 页面可访问，能管理 tenant/key 并查看用量
- [x] OpenAPI spec 与实现一致
- [x] E2E 测试覆盖完整业务流程（47 个测试用例，14 个 Test 函数）

---

## 18. E2E 测试

**文件位置**：`test/e2e/`

| 文件 | 说明 |
|------|------|
| `main_test.go` | 测试基础设施：TestMain 启动 Mock Provider + Gateway，helper 函数 |
| `e2e_test.go` | 14 个 Test 函数覆盖全部用例 |

**运行方式**：
```bash
make docker-up    # 启动 MySQL
make test-e2e     # 运行 E2E 测试
```

**测试覆盖**：

| Test 函数 | 覆盖场景 |
|-----------|---------|
| TestHealth | 健康检查 |
| TestAdminAuth | Admin Token 鉴权（缺失/错误/正确） |
| TestTenantManagement | 租户 CRUD + 校验 |
| TestKeyManagement | Key CRUD + scope + 启停 + 删除 |
| TestProxyAuth | 代理鉴权（无 auth/无效/禁用/过期/tenant 禁用） |
| TestChatCompletions | Chat 接口（成功/无效 body/request_id） |
| TestEmbeddings | Embedding 接口（成功/无效 body） |
| TestModels | 模型列表（成功/无 auth） |
| TestScopeEnforcement | Scope 校验（model/endpoint 白名单） |
| TestRateLimit | 限流（未超限/超限 429） |
| TestUsageTracking | 用量记录 + 查询 + 分组 + 过滤 |
| TestErrorHandling | 上游不可达 502 |
| TestEndToEndFlow | 完整业务流程：创建→调用→查询→禁用→删除 |
