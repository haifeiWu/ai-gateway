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

## 项目结构

```
ai-gateway/
├── cmd/
│   ├── gateway/          # Gateway 入口
│   └── mock-provider/    # Mock Provider 入口（模拟 OpenAI 接口）
├── internal/
│   ├── config/           # 配置加载（环境变量）
│   ├── handler/          # HTTP 处理器
│   │   ├── admin.go      # 租户/Key 管理
│   │   ├── proxy.go      # AI 模型代理（Chat/Embed/Models）
│   │   └── usage.go      # 用量查询/CSV 导出
│   ├── middleware/        # 中间件
│   │   ├── admin_auth.go # Admin Token 鉴权
│   │   ├── apikey_auth.go# API Key 鉴权 + Scope 校验
│   │   ├── recovery.go   # Panic 恢复
│   │   └── security.go   # 安全头
│   ├── model/            # 数据模型（GORM）
│   │   ├── tenant.go     # 租户
│   │   ├── apikey.go     # API Key（SHA-256 hash）
│   │   └── usage.go      # 用量记录
│   ├── repository/       # 数据访问层（GORM）
│   │   ├── tenant.go
│   │   ├── apikey.go
│   │   └── usage.go
│   ├── router/           # 路由注册
│   │   └── router.go
│   └── service/          # 业务逻辑层
│       ├── tenant.go     # 租户服务
│       ├── apikey.go     # API Key 生成/哈希/校验
│       ├── usage.go      # 用量异步写入/查询
│       └── ratelimit.go  # 内存固定窗口限流
├── pkg/
│   └── apierror/         # 统一错误响应（OpenAI 兼容格式）
├── test/
│   └── e2e/              # E2E 测试（build tag: e2e）
├── web/                  # Dashboard（纯 HTML + JS + CSS）
├── scripts/
│   └── verify.sh         # 端到端验证脚本（curl + jq）
├── docs/
│   └── openapi.yaml      # OpenAPI 3.0 规范
├── dev_doc/              # 设计/评审/安全文档
│   ├── DESIGN.md         # 设计文档
│   ├── CODE_REVIEW.md    # 代码评审报告
│   ├── E2E_TEST_PLAN.md  # E2E 测试计划
│   ├── EVALUATION.md     # 项目评估
│   └── SECURITY_AUDIT.md # 安全审计报告
├── docker-compose.yml    # Docker Compose 编排
├── Dockerfile            # Gateway 镜像
├── Dockerfile.mock       # Mock Provider 镜像
├── Makefile              # 开发命令
└── .env.example          # 环境变量模板
```

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

## 测试与验证

### 单元测试

```bash
# 运行所有单元测试
make test

# 或直接使用 go test
go test ./...
```

单元测试覆盖：
- **API Key 生成与哈希**：`internal/model/apikey_test.go`
- **API Key 服务层**：`internal/service/apikey_test.go`
- **Rate Limiter 计数器**：`internal/service/ratelimit_test.go`
- **Tenant 服务层**：`internal/service/tenant_test.go`
- **Usage 服务层**：`internal/service/usage_test.go`
- **Config 配置加载**：`internal/config/config_test.go`

### E2E 测试

E2E 测试通过 build tag `e2e` 隔离，需要 MySQL 才能运行：

```bash
# 确保 MySQL 运行
docker compose up -d mysql

# 运行 E2E 测试
make test-e2e
```

E2E 测试 (`test/e2e/`) 覆盖场景：

| 测试文件 | 覆盖场景 |
|---------|---------|
| `TestHealth` | 健康检查端点 |
| `TestAdminAuth` | Admin Token 鉴权（缺失、错误、正确） |
| `TestTenantManagement` | 租户 CRUD（创建/列表/获取/更新/删除/404） |
| `TestKeyManagement` | API Key CRUD（创建/Scope/禁用/启用/删除/查询） |
| `TestProxyAuth` | 代理鉴权（无效 Key/禁用 Key/过期 Key/禁用租户） |
| `TestChatCompletions` | Chat 代理（成功/无效请求/X-Request-ID） |
| `TestChatCompletionsStreaming` | SSE 流式代理（SSE 格式/[DONE] 标记/非流式不受影响） |
| `TestEmbeddings` | Embeddings 代理（成功/无效请求） |
| `TestModels` | 模型列表（成功/未鉴权拒绝） |
| `TestScopeEnforcement` | Scope 校验（允许/拒绝 model 和 endpoint/空 Scope 不限制） |
| `TestRateLimit` | Rate Limit（限额内/超额 429） |
| `TestUsageTracking` | 用量追踪（记录/按 model 分组/按天/按租户/时间范围/分页/CSV 导出/按 Key 过滤/空结果） |
| `TestErrorHandling` | 错误处理（上游不可达 502） |
| `TestEndToEndFlow` | 完整 E2E 流程（创建→使用→查询→禁用→删除） |

### 端到端验证脚本

`scripts/verify.sh` 提供一键端到端验证，无需 Go 环境，只需 `curl` 和 `jq`：

```bash
# 使用默认配置（.env 中的 ADMIN_TOKEN，localhost:8080）
./scripts/verify.sh

# 或指定 base URL 和 admin token
./scripts/verify.sh http://localhost:8080 your-admin-token
```

验证脚本执行 8 项检查：

| 步骤 | 检查项 | 说明 |
|------|--------|------|
| 1 | 健康检查 | `/health` 返回 `{"status":"ok"}` |
| 2 | 创建租户 | 创建 e2e-verify-tenant |
| 3 | 创建 API Key | 验证 Key 前缀 `sk-agw-` 和长度 39 |
| 4 | Chat Completions | 代理 `/v1/chat/completions`，验证返回 `chat.completion` |
| 5 | Embeddings | 代理 `/v1/embeddings`，验证返回 `list` |
| 6 | Models | 代理 `/v1/models`，验证返回 `list` |
| 7 | 鉴权拒绝 | 无效 Key → 401 / 过期 Key → 401 / 越权模型 → 403 |
| 8 | 清理 | 删除测试租户 |

### 完整验证流程

开发和提交前的完整验证顺序：

```bash
# 1. 构建检查
make build

# 2. 单元测试
make test

# 3. 代码检查
make lint

# 4. 启动服务
docker compose up -d

# 5. E2E 测试（需 MySQL）
make test-e2e

# 6. 端到端验证脚本
./scripts/verify.sh

# 7. 停止服务
docker compose down
```

> **提示**：`make build && make test && make lint` 三步应全部通过再提交代码。

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
