# AI Gateway

统一管理租户 API Key，代理 AI 模型请求（下游 mock），并记录用量。

---

## 架构说明

```
┌─ Client (curl / Dashboard) ─────────────────────────────────────┐
│                                                                  │
│  Admin API                Proxy API              Dashboard       │
│  Bearer <ADMIN_TOKEN>     Bearer sk-agw-xxx      浏览器          │
└──────────┬──────────────────┬──────────────────────┬────────────┘
           │                  │                      │
           ▼                  ▼                      ▼
┌──────────────────────────────────────────────────────────────────┐
│                      AI Gateway (Go / Gin)                       │
│                                                                  │
│  ┌──────────┐   ┌───────────┐   ┌──────────┐   ┌────────────┐  │
│  │ Admin    │   │ Proxy     │   │ Usage    │   │ Middleware │  │
│  │ Handler  │   │ Handler   │   │ Handler  │   │ - AdminAuth│  │
│  │          │   │           │   │          │   │ - APIKeyAuth│ │
│  │ Tenant   │   │ Chat      │   │ Query    │   │ - Recovery │  │
│  │ Key CRUD │   │ Embed     │   │ Aggregate│   │ - Security │  │
│  │          │   │ Models    │   │ Export   │   │ - RateLimit│  │
│  │          │   │ SSE Stream│   │          │   │            │  │
│  └────┬─────┘   └─────┬─────┘   └────┬─────┘   └────────────┘  │
│       │               │              │                          │
│       └───────────────┼──────────────┘                          │
│                       │                                         │
│  ┌────────────┐  ┌────┴──────┐  ┌───────────┐                  │
│  │ RateLimiter│  │UsageWriter│  │ Repository│                  │
│  │ (内存)     │  │ (异步batch)│  │ (GORM)    │                  │
│  └────────────┘  └───────────┘  └─────┬─────┘                  │
│                                       │                         │
└───────────────────────────────────────┼─────────────────────────┘
                                        │
                                 ┌──────┴───────┐
                                 │  MySQL 8.0   │
                                 │  - tenants   │
                                 │  - api_keys  │
                                 │  - usage_records│
                                 └──────────────┘

                 ┌───────────────────────┐
                 │   Mock Provider (Go)  │
                 │                       │
                 │  /v1/chat/completions │
                 │  （含 SSE streaming） │
                 │  /v1/embeddings       │
                 │  /v1/models           │
                 └───────────────────────┘
```

**核心组件**：

| 组件 | 说明 |
|------|------|
| Gateway | Go + Gin，统一入口。管理 API 用 Admin Token 鉴权，代理 API 用租户 API Key 鉴权 |
| Mock Provider | 独立 Go HTTP server，模拟 OpenAI 接口（含 SSE 流式响应），返回 mock 数据 |
| MySQL 8.0 | 存储租户、API Key（SHA-256 hash）、用量记录 |
| Dashboard | 纯 HTML+JS+CSS，Gateway 直接静态托管，无构建工具 |

**数据流**：

1. 管理员通过 Admin API 创建租户和 API Key（明文仅返回一次）
2. 客户端用 API Key 调用代理 API → 中间件鉴权 → Scope 校验 → 转发到 Mock Provider
3. 普通请求：解析响应 → 提取 usage → 异步记录用量
4. 流式请求（`stream=true`）：无超时转发 SSE 事件流 → 从末尾 chunk 提取 usage → 异步记录
5. 管理员通过 Usage API 查询聚合用量（支持分页、分组、CSV 导出）

---

## 运行步骤

### 方式一：Docker Compose（推荐）

#### 1. 准备环境变量

```bash
cp .env.example .env
# 编辑 .env，设置安全的 ADMIN_TOKEN（不能使用默认值 admin-secret-token）
```

`.env` 文件内容：

```bash
DB_HOST=mysql
DB_PORT=3306
DB_USER=gateway
DB_PASS=your-secure-db-password
DB_NAME=ai_gateway
ADMIN_TOKEN=your-secure-random-token     # 必填，不能是 admin-secret-token
MOCK_PROVIDER_URL=http://mock-provider:8080
LISTEN_ADDR=:8080
```

#### 2. 启动服务

```bash
docker compose up -d
```

服务启动后：
- Gateway: `http://localhost:8080`
- Dashboard: `http://localhost:8080/dashboard/index.html`
- OpenAPI Spec: `docs/openapi.yaml`

#### 3. 验证服务

```bash
# 健康检查
curl http://localhost:8080/health
# → {"status":"ok"}

# 查看服务日志
docker compose logs -f gateway
```

### 方式二：本地运行（开发）

#### 前置条件

- Go 1.25+
- MySQL 8.0（本地或 Docker）

```bash
# 1. 启动 MySQL
docker run -d --name mysql \
  -e MYSQL_ROOT_PASSWORD=root \
  -e MYSQL_DATABASE=ai_gateway \
  -e MYSQL_USER=gateway \
  -e MYSQL_PASSWORD=gateway \
  -p 3306:3306 mysql:8.0

# 2. 设置环境变量
export ADMIN_TOKEN="dev-secret-token-123"
export DB_HOST=localhost
export DB_PASS=gateway
export MOCK_PROVIDER_URL=http://localhost:8081

# 3. 启动 Mock Provider（终端 1）
make run-mock

# 4. 启动 Gateway（终端 2）
make run-gateway
```

---

## 使用示例（curl）

以下示例假设 Gateway 运行在 `http://localhost:8080`。

### 1. 租户管理

```bash
ADMIN="Authorization: Bearer your-secure-random-token"
BASE="http://localhost:8080"

# 创建租户
curl -s -X POST "$BASE/admin/v1/tenants" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{"name":"Acme Corp"}' | jq .
# → {"id":"550e8400-...","name":"Acme Corp","status":"active","created_at":"...","updated_at":"..."}

# 列出租户
curl -s "$BASE/admin/v1/tenants" -H "$ADMIN" | jq .

# 获取租户详情
curl -s "$BASE/admin/v1/tenants/<tenant-id>" -H "$ADMIN" | jq .

# 更新租户（名称/状态）
curl -s -X PATCH "$BASE/admin/v1/tenants/<tenant-id>" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{"name":"New Name"}' | jq .

curl -s -X PATCH "$BASE/admin/v1/tenants/<tenant-id>" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{"status":"disabled"}' | jq .

# 删除租户
curl -s -X DELETE "$BASE/admin/v1/tenants/<tenant-id>" -H "$ADMIN" | jq .
# → {"status":"deleted"}

# 保存租户 ID
TENANT_ID=$(curl -s "$BASE/admin/v1/tenants" -H "$ADMIN" | jq -r '.[0].id')
```

### 2. API Key 管理

```bash
# 创建无限制的 API Key
curl -s -X POST "$BASE/admin/v1/tenants/$TENANT_ID/keys" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{"name":"full-access-key"}' | jq .
# → {"id":"...","key":"sk-agw-a1b2c3d4...","key_prefix":"sk-agw-a1b2****","name":"full-access-key","status":"active",...}

# 创建带 scope 限制的 API Key（仅允许 gpt-4 + chat completions，限流 100 RPM）
curl -s -X POST "$BASE/admin/v1/tenants/$TENANT_ID/keys" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{
    "name": "limited-key",
    "scopes": {
      "allowed_models": ["gpt-4"],
      "allowed_endpoints": ["/v1/chat/completions"],
      "rate_limit_rpm": 100
    },
    "expires_at": "2026-12-31T23:59:59Z"
  }' | jq .

# 列出租户的 Key
curl -s "$BASE/admin/v1/tenants/$TENANT_ID/keys" -H "$ADMIN" | jq .

# 获取 Key 详情
curl -s "$BASE/admin/v1/keys/<key-id>" -H "$ADMIN" | jq .

# 禁用/启用 Key
curl -s -X PATCH "$BASE/admin/v1/keys/<key-id>" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{"status":"disabled"}' | jq .

# 更新 Key 的 scope
curl -s -X PATCH "$BASE/admin/v1/keys/<key-id>" \
  -H "$ADMIN" -H "Content-Type: application/json" \
  -d '{"scopes":{"allowed_models":["gpt-4","gpt-4-turbo"],"rate_limit_rpm":200}}' | jq .

# 删除 Key
curl -s -X DELETE "$BASE/admin/v1/keys/<key-id>" -H "$ADMIN" | jq .
# → {"status":"deleted"}
```

> **注意**：`key` 字段（明文）仅在创建时返回一次，后续无法获取。请妥善保存。

### 3. 调用代理 API

```bash
# 用创建时返回的明文 API Key
KEY="Authorization: Bearer sk-agw-a1b2c3d4e5f6789012345678901234ab"

# === Chat Completions（非流式） ===
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "$KEY" -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}' | jq .
# → {"id":"chatcmpl-mock-...","object":"chat.completion","model":"gpt-4",
#    "choices":[{"index":0,"message":{"role":"assistant","content":"Hello! ..."},"finish_reason":"stop"}],
#    "usage":{"prompt_tokens":15,"completion_tokens":20,"total_tokens":35}}

# === Chat Completions（SSE 流式） ===
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "$KEY" -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}],"stream":true}'
# → data: {"id":"chatcmpl-mock-...","object":"chat.completion.chunk","model":"gpt-4","choices":[...]}
# → data: {"id":"chatcmpl-mock-...","object":"chat.completion.chunk","model":"gpt-4","choices":[...], "usage":{...}}
# → data: [DONE]

# === Embeddings ===
curl -s -X POST "$BASE/v1/embeddings" \
  -H "$KEY" -H "Content-Type: application/json" \
  -d '{"model":"text-embedding-ada-002","input":"Hello world"}' | jq .
# → {"object":"list","data":[{"object":"embedding","index":0,"embedding":[...]}],
#    "model":"text-embedding-ada-002","usage":{"prompt_tokens":8,"total_tokens":8}}

# === 模型列表 ===
curl -s "$BASE/v1/models" -H "$KEY" | jq .
# → {"object":"list","data":[{"id":"gpt-4","object":"model","created":1687882411,"owned_by":"openai"},...]}
```

### 4. 查询用量

```bash
# 查询全部用量汇总
curl -s "$BASE/admin/v1/usage" -H "$ADMIN" | jq .

# 按租户 + 时间范围查询
curl -s "$BASE/admin/v1/usage?tenant_id=$TENANT_ID&start=2026-07-01T00:00:00Z&end=2026-07-08T23:59:59Z" \
  -H "$ADMIN" | jq .

# 按 model 分组 + 分页
curl -s "$BASE/admin/v1/usage?tenant_id=$TENANT_ID&group_by=model&page=1&page_size=10" \
  -H "$ADMIN" | jq .
# → {"summary":{"total_requests":3,"total_tokens":78},
#    "groups":[{"model":"gpt-4","requests":2,"total_tokens":70},...],
#    "total":2,"page":1,"page_size":10}

# 按天分组
curl -s "$BASE/admin/v1/usage?tenant_id=$TENANT_ID&group_by=day" -H "$ADMIN" | jq .

# 按小时分组
curl -s "$BASE/admin/v1/usage?tenant_id=$TENANT_ID&group_by=hour" -H "$ADMIN" | jq .

# 按租户分组
curl -s "$BASE/admin/v1/usage?group_by=tenant" -H "$ADMIN" | jq .

# CSV 导出
curl -s "$BASE/admin/v1/usage?tenant_id=$TENANT_ID&group_by=model&format=csv" \
  -H "$ADMIN" -o usage_report.csv
```

### 5. 错误场景验证

```bash
# === 401: 缺少 Authorization ===
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}' | jq .
# → {"error":{"message":"missing API key","type":"auth_error"}}

# === 401: 无效 Key ===
curl -s "$BASE/v1/models" -H "Authorization: Bearer sk-agw-invalid000000000000000000000" | jq .
# → {"error":{"message":"invalid API key","type":"auth_error"}}

# === 401: Key 格式无效 ===
curl -s "$BASE/v1/models" -H "Authorization: Bearer invalid-key" | jq .
# → {"error":{"message":"invalid API key format","type":"auth_error"}}

# === 401: Key 已过期 ===
# 创建 Key 时设置过去的 expires_at，再用该 Key 请求
# → {"error":{"message":"API key expired","type":"auth_error"}}

# === 403: Key 已禁用 ===
# curl -X PATCH .../keys/<key-id> -d '{"status":"disabled"}'
# curl ... -H "Authorization: Bearer <disabled-key>"
# → {"error":{"message":"API key disabled","type":"forbidden"}}

# === 403: Tenant 已禁用 ===
# curl -X PATCH .../tenants/<id> -d '{"status":"disabled"}'
# → {"error":{"message":"tenant disabled","type":"forbidden"}}

# === 403: Model 不在 allowed_models ===
# Key 的 scopes.allowed_models = ["gpt-4"]，但请求 model = "gpt-3.5-turbo"
# → {"error":{"message":"model not allowed","type":"forbidden"}}

# === 403: Endpoint 不在 allowed_endpoints ===
# Key 的 scopes.allowed_endpoints = ["/v1/chat/completions"]，但请求 /v1/embeddings
# → {"error":{"message":"endpoint not allowed","type":"forbidden"}}

# === 429: 超出 rate limit ===
# Key 的 rate_limit_rpm = 5，连续发送 6 次请求
# → {"error":{"message":"rate limit exceeded","type":"rate_limit"}}

# === 400: 请求体无效 ===
curl -s -X POST "$BASE/v1/chat/completions" \
  -H "$KEY" -H "Content-Type: application/json" \
  -d '{"invalid":"json"}' | jq .
# → {"error":{"message":"invalid request body","type":"invalid_request_error"}}

# === 404: 资源不存在 ===
curl -s "$BASE/admin/v1/tenants/nonexistent-id" -H "$ADMIN" | jq .
# → {"error":{"message":"tenant not found","type":"not_found"}}

# === 502 / 504: 上游错误 ===
# 下游不可达 → 502 {"error":{"message":"upstream error","type":"upstream_error"}}
# 下游超时（30s）→ 504 {"error":{"message":"upstream timeout","type":"timeout"}}
```

### 6. Dashboard

浏览器访问 `http://localhost:8080/dashboard/index.html`，输入 Admin Token 后即可：
- 创建、查看、更新、删除租户
- 创建、启用/禁用、删除 API Key（设置 scope 和过期时间）
- 按租户/Key/模型/时间范围查询用量（支持分组和 CSV 导出）

---

## E2E 测试

```bash
# 确保 MySQL 运行
docker compose up -d mysql

# 运行 E2E 测试
make test-e2e
```

测试覆盖：Health / AdminAuth / Tenant CRUD / Key CRUD / ProxyAuth / Chat / SSE Stream / Embed / Models / Scope 校验 / RateLimit / Usage 查询 / 502 错误 / 完整 E2E 流程。

---

## API 端点总览

| 方法 | 路径 | 鉴权 | 说明 |
|------|------|------|------|
| GET | /health | 无 | 健康检查 |
| POST | /admin/v1/tenants | Admin | 创建租户 |
| GET | /admin/v1/tenants | Admin | 列出租户 |
| GET | /admin/v1/tenants/:id | Admin | 获取租户详情 |
| PATCH | /admin/v1/tenants/:id | Admin | 更新租户（名称/状态） |
| DELETE | /admin/v1/tenants/:id | Admin | 删除租户 |
| POST | /admin/v1/tenants/:id/keys | Admin | 创建 API Key |
| GET | /admin/v1/tenants/:id/keys | Admin | 列出租户的 Key |
| GET | /admin/v1/keys/:id | Admin | 获取 Key 详情 |
| PATCH | /admin/v1/keys/:id | Admin | 更新 Key（status/scope/expires_at/name） |
| DELETE | /admin/v1/keys/:id | Admin | 删除 Key |
| GET | /admin/v1/usage | Admin | 查询用量（分页/分组/CSV 导出） |
| POST | /v1/chat/completions | API Key | Chat Completions（含 SSE streaming） |
| POST | /v1/embeddings | API Key | Embeddings（OpenAI 兼容） |
| GET | /v1/models | API Key | 模型列表 |

完整 OpenAPI 3.0 spec 见 `docs/openapi.yaml`（15 个端点，21 个 Schema，7 个可复用 Response）。

---

## 设计决策

### API Key 安全：SHA-256 hash + prefix

只存 SHA-256 hash 和前缀（`sk-agw-abcd****`），明文仅在创建时返回一次。使用 `crypto/rand` 生成 32 字节随机数。鉴权时对输入 key 做 SHA-256，按 hash 查 DB。符合行业最佳实践（OpenAI、Stripe），无法找回明文但安全性高。

### 用量记录：异步 batch 写入

用量记录通过 channel 异步写入（容量 1000），后台 goroutine 每 100 条或每 5 秒 flush 到 DB。不阻塞代理响应。Trade-off：进程崩溃可能丢失 channel 中未刷盘数据，MVP 可接受。优雅关闭时 drain channel 并 flush 剩余数据。

### Rate Limit：内存固定窗口计数器

单机内存计数器，按 key_id 维度，固定窗口（1 分钟）。后台 goroutine 每 5 分钟清理过期窗口。`rate_limit_rpm=0` 表示不限流。Admin API 另有限流：按客户端 IP，20 RPM。

### Scope 建模：三个维度

```json
{
  "allowed_models": ["gpt-4"],
  "allowed_endpoints": ["/v1/chat/completions"],
  "rate_limit_rpm": 100
}
```

空数组/零值 = 不限制，符合"最小权限可按需收紧"原则。存储为 MySQL JSON 字段，通过 `sql.Scanner` / `driver.Valuer` 接口序列化。

### SSE 流式支持

`stream=true` 时，Gateway 使用独立的无超时 HTTP 客户端转发请求，逐行读取下游 SSE 事件流（`text/event-stream`），逐 chunk 透传给客户端。从最后一个携带 `usage` 字段的 chunk 提取 token 用量，流结束后异步记录。Mock Provider 按单词拆分响应内容模拟流式输出。

### Admin Token：常量时间比较

使用 `subtle.ConstantTimeCompare` 防止时序攻击。启动时校验 ADMIN_TOKEN 不为空且不等于默认值 `admin-secret-token`。

### Mock Provider：独立进程

Gateway 与 Mock Provider 是独立 Go HTTP server 进程，通过 HTTP 通信。架构更真实，未来替换为真实 provider 零改动。

### Dashboard：无构建工具

原生 HTML + JS + CSS，Gateway 直接静态托管。零构建依赖、部署简单。

### 错误处理：OpenAI 兼容格式

所有错误响应统一为 `{"error":{"message":"...","type":"..."}}`，type 枚举：`auth_error` / `forbidden` / `rate_limit` / `invalid_request_error` / `upstream_error` / `timeout` / `server_error` / `not_found`。502（下游不可达）和 504（下游超时）通过 `os.IsTimeout` 区分。

---

## 已知限制

| # | 限制 | 说明 |
|---|------|------|
| 1 | 单实例部署 | rate limit 用内存、usage writer 用单 channel，不支持水平扩展 |
| 2 | 无 TLS | 容器内通信，生产需加 TLS 终止层 |
| 3 | 用量记录可能丢失 | 异步写入，进程崩溃时 channel 中未刷盘数据会丢失 |
| 4 | 无 Key 轮换机制 | 需手动创建新 Key + 禁用旧 Key |
| 5 | 无多 provider 路由 | 仅对接一个 mock provider，不支持按 model 路由到不同 provider |
| 6 | admin token 无过期 | 固定 token，无 JWT/刷新机制 |

---

## 开发命令

```bash
make build          # 编译
make test           # 单元测试
make test-e2e       # E2E 测试（需 MySQL）
make lint           # golangci-lint
make docker-up      # docker compose up -d
make docker-down    # docker compose down
make run-gateway    # 本地运行 Gateway
make run-mock       # 本地运行 Mock Provider
```
