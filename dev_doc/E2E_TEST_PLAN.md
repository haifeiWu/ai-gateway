# AI Gateway — 端到端测试方案

> 版本：1.0
> 日期：2026-07-08
> 覆盖范围：Admin API、Proxy API、Scope 权限、限流、用量追踪、错误处理

---

## 1. 测试环境

### 1.1 依赖服务

| 服务 | 端口 | 说明 |
|------|------|------|
| MySQL 8.0 | 3306 | 数据存储 |
| Mock Provider | 8081 | 模拟 AI 上游 |
| Gateway | 8080 | 被测系统 |

### 1.2 启动方式

```bash
# 方式一：Docker Compose（推荐）
make docker-up

# 方式二：本地启动
make run-mock-bg    # 后台启动 Mock Provider
make run-gateway    # 前台启动 Gateway
```

### 1.3 测试数据清理

每个测试套件开始前清空 DB 表数据，保证测试隔离：

```sql
TRUNCATE TABLE usage_records;
TRUNCATE TABLE api_keys;
TRUNCATE TABLE tenants;
```

### 1.4 环境变量

| 变量 | 值 | 说明 |
|------|-----|------|
| `ADMIN_TOKEN` | `test-admin-token` | 测试用 admin token |
| `MOCK_PROVIDER_URL` | `http://localhost:8081` | Mock Provider 地址 |
| `DB_HOST` | `localhost` | MySQL 地址 |

---

## 2. 测试用例总览

| 分类 | 用例数 | 优先级 |
|------|--------|--------|
| TC-HEALTH 健康检查 | 1 | P0 |
| TC-ADMIN-AUTH Admin 鉴权 | 3 | P0 |
| TC-TENANT 租户管理 | 5 | P0 |
| TC-KEY API Key 管理 | 7 | P0 |
| TC-PROXY-AUTH 代理鉴权 | 5 | P0 |
| TC-CHAT Chat Completions | 4 | P0 |
| TC-EMBED Embeddings | 3 | P1 |
| TC-MODELS 模型列表 | 2 | P1 |
| TC-SCOPE 权限校验 | 6 | P0 |
| TC-RATELIMIT 限流 | 2 | P1 |
| TC-USAGE 用量追踪 | 5 | P1 |
| TC-ERROR 错误处理 | 3 | P1 |
| TC-FLOW 端到端流程 | 1 | P0 |
| **合计** | **47** | |

---

## 3. 详细测试用例

### 3.1 TC-HEALTH 健康检查

#### TC-HEALTH-01: 健康检查端点返回 200

| 项目 | 内容 |
|------|------|
| **前置条件** | Gateway 已启动 |
| **请求** | `GET /health` |
| **期望状态码** | 200 |
| **期望响应** | `{"status":"ok"}` |
| **优先级** | P0 |

---

### 3.2 TC-ADMIN-AUTH Admin 鉴权

#### TC-ADMIN-AUTH-01: 无 Authorization 头 → 401

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `GET /admin/v1/tenants`（无 Authorization 头） |
| **期望状态码** | 401 |
| **期望响应** | `{"error":{"message":"missing admin token","type":"auth_error"}}` |
| **优先级** | P0 |

#### TC-ADMIN-AUTH-02: 错误 token → 401

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `GET /admin/v1/tenants`，`Authorization: Bearer wrong-token` |
| **期望状态码** | 401 |
| **期望响应** | `{"error":{"message":"invalid admin token","type":"auth_error"}}` |
| **优先级** | P0 |

#### TC-ADMIN-AUTH-03: 正确 token → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `GET /admin/v1/tenants`，`Authorization: Bearer test-admin-token` |
| **期望状态码** | 200 |
| **期望响应** | JSON 数组（可为空 `[]`） |
| **优先级** | P0 |

---

### 3.3 TC-TENANT 租户管理

#### TC-TENANT-01: 创建租户 → 201

| 项目 | 内容 |
|------|------|
| **前置条件** | Admin token 有效 |
| **请求** | `POST /admin/v1/tenants`，body: `{"name":"Test Tenant"}` |
| **期望状态码** | 201 |
| **期望响应** | `{"id":"<uuid>","name":"Test Tenant","status":"active","created_at":"...","updated_at":"..."}` |
| **校验点** | id 为合法 UUID；status 为 active；created_at 非空 |
| **优先级** | P0 |

#### TC-TENANT-02: 创建租户 — 缺少 name → 400

| 项目 | 内容 |
|------|------|
| **前置条件** | Admin token 有效 |
| **请求** | `POST /admin/v1/tenants`，body: `{}` |
| **期望状态码** | 400 |
| **期望响应** | `{"error":{"message":"name is required","type":"invalid_request_error"}}` |
| **优先级** | P0 |

#### TC-TENANT-03: 列出租户 → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建至少 1 个租户 |
| **请求** | `GET /admin/v1/tenants` |
| **期望状态码** | 200 |
| **期望响应** | JSON 数组，包含已创建的租户 |
| **校验点** | 数组按 created_at 降序排列 |
| **优先级** | P0 |

#### TC-TENANT-04: 查询租户详情 → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建租户，记录其 id |
| **请求** | `GET /admin/v1/tenants/{id}` |
| **期望状态码** | 200 |
| **期望响应** | 租户完整信息，name 和 id 与创建时一致 |
| **优先级** | P0 |

#### TC-TENANT-05: 查询不存在的租户 → 404

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `GET /admin/v1/tenants/nonexistent-id` |
| **期望状态码** | 404 |
| **期望响应** | `{"error":{"message":"tenant not found","type":"not_found"}}` |
| **优先级** | P0 |

---

### 3.4 TC-KEY API Key 管理

#### TC-KEY-01: 创建 Key（无限 scope）→ 201

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建租户，记录 tenant_id |
| **请求** | `POST /admin/v1/tenants/{tenant_id}/keys`，body: `{"name":"Full Access Key","scopes":{}}` |
| **期望状态码** | 201 |
| **期望响应** | `{"id":"<uuid>","key":"sk-agw-xxxx","key_prefix":"sk-agw-xxxx****","name":"Full Access Key","scopes":{"allowed_models":null,"allowed_endpoints":null,"rate_limit_rpm":0},"status":"active","expires_at":null,"created_at":"..."}` |
| **校验点** | key 以 `sk-agw-` 开头，长度 40 字符；key_prefix 以 `****` 结尾；status 为 active |
| **优先级** | P0 |

#### TC-KEY-02: 创建 Key（带 scope 限制）→ 201

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建租户 |
| **请求** | `POST /admin/v1/tenants/{tenant_id}/keys`，body: `{"name":"Limited Key","scopes":{"allowed_models":["gpt-4"],"allowed_endpoints":["/v1/chat/completions"],"rate_limit_rpm":60}}` |
| **期望状态码** | 201 |
| **校验点** | 返回的 scopes 包含正确的 allowed_models、allowed_endpoints、rate_limit_rpm |
| **优先级** | P0 |

#### TC-KEY-03: 创建 Key — 不存在的租户 → 400

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `POST /admin/v1/tenants/nonexistent-id/keys`，body: `{"name":"Key"}` |
| **期望状态码** | 400 |
| **优先级** | P0 |

#### TC-KEY-04: 列出租户的 Key → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 已在租户下创建至少 2 个 Key |
| **请求** | `GET /admin/v1/tenants/{tenant_id}/keys` |
| **期望状态码** | 200 |
| **校验点** | 返回数组包含已创建的 Key；key_hash 字段不返回（json tag 为 `-`） |
| **优先级** | P0 |

#### TC-KEY-05: 禁用 Key → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建 Key，status 为 active |
| **请求** | `PATCH /admin/v1/keys/{key_id}`，body: `{"status":"disabled"}` |
| **期望状态码** | 200 |
| **校验点** | 返回的 status 为 disabled |
| **优先级** | P0 |

#### TC-KEY-06: 重新启用 Key → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 当前状态为 disabled |
| **请求** | `PATCH /admin/v1/keys/{key_id}`，body: `{"status":"active"}` |
| **期望状态码** | 200 |
| **校验点** | 返回的 status 为 active |
| **优先级** | P0 |

#### TC-KEY-07: 删除 Key → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建 Key |
| **请求** | `DELETE /admin/v1/keys/{key_id}` |
| **期望状态码** | 200 |
| **期望响应** | `{"status":"deleted"}` |
| **校验点** | 删除后 `GET /admin/v1/keys/{key_id}` 返回 404 |
| **优先级** | P0 |

---

### 3.5 TC-PROXY-AUTH 代理鉴权

#### TC-PROXY-AUTH-01: 无 Authorization 头 → 401

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `POST /v1/chat/completions`（无 Authorization 头） |
| **期望状态码** | 401 |
| **期望响应** | `{"error":{"message":"missing API key","type":"auth_error"}}` |
| **优先级** | P0 |

#### TC-PROXY-AUTH-02: 无效 API Key → 401

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `POST /v1/chat/completions`，`Authorization: Bearer sk-agw-invalidkey0000000000000000000` |
| **期望状态码** | 401 |
| **期望响应** | `{"error":{"message":"invalid API key","type":"auth_error"}}` |
| **优先级** | P0 |

#### TC-PROXY-AUTH-03: 被禁用的 Key → 403

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建 Key 并将其 status 设为 disabled |
| **请求** | `POST /v1/chat/completions`，使用该 Key |
| **期望状态码** | 403 |
| **期望响应** | `{"error":{"message":"API key disabled","type":"forbidden"}}` |
| **优先级** | P0 |

#### TC-PROXY-AUTH-04: 已过期的 Key → 401

| 项目 | 内容 |
|------|------|
| **前置条件** | 已创建 Key，expires_at 设为过去时间 |
| **请求** | `POST /v1/chat/completions`，使用该 Key |
| **期望状态码** | 401 |
| **期望响应** | `{"error":{"message":"API key expired","type":"auth_error"}}` |
| **优先级** | P0 |

#### TC-PROXY-AUTH-05: 被禁用租户的 Key → 403

| 项目 | 内容 |
|------|------|
| **前置条件** | 租户 status 为 disabled（通过 DB 直接修改），Key 仍为 active |
| **请求** | `POST /v1/chat/completions`，使用该 Key |
| **期望状态码** | 403 |
| **期望响应** | `{"error":{"message":"tenant disabled","type":"forbidden"}}` |
| **优先级** | P0 |

---

### 3.6 TC-CHAT Chat Completions

#### TC-CHAT-01: 正常请求 → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key（无限 scope） |
| **请求** | `POST /v1/chat/completions`，body: `{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}` |
| **期望状态码** | 200 |
| **期望响应** | `{"id":"chatcmpl-...","object":"chat.completion","model":"gpt-4","choices":[{"index":0,"message":{"role":"assistant","content":"..."},"finish_reason":"stop"}],"usage":{"prompt_tokens":15,"completion_tokens":20,"total_tokens":35}}` |
| **校验点** | choices 非空；usage.total_tokens > 0；响应头包含 `X-Request-ID` |
| **优先级** | P0 |

#### TC-CHAT-02: 无效请求体 → 400

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key |
| **请求** | `POST /v1/chat/completions`，body: `{"invalid":"json"}` |
| **期望状态码** | 400 |
| **期望响应** | `{"error":{"message":"invalid request body","type":"invalid_request_error"}}` |
| **优先级** | P0 |

#### TC-CHAT-03: 透传下游错误

| 项目 | 内容 |
|------|------|
| **前置条件** | Mock Provider 返回 500（需配置 mock 行为） |
| **请求** | `POST /v1/chat/completions`，正常 body |
| **期望状态码** | 500 |
| **校验点** | 响应体为下游原始错误 |
| **优先级** | P1 |

#### TC-CHAT-04: 响应头包含 X-Request-ID

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key |
| **请求** | `POST /v1/chat/completions`，正常 body |
| **期望状态码** | 200 |
| **校验点** | 响应头 `X-Request-ID` 存在且为合法 UUID |
| **优先级** | P0 |

---

### 3.7 TC-EMBED Embeddings

#### TC-EMBED-01: 正常请求 → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key |
| **请求** | `POST /v1/embeddings`，body: `{"model":"text-embedding-ada-002","input":"hello world"}` |
| **期望状态码** | 200 |
| **期望响应** | `{"object":"list","data":[{"object":"embedding","index":0,"embedding":[...1536 floats...]}],"model":"text-embedding-ada-002","usage":{"prompt_tokens":8,"total_tokens":8}}` |
| **校验点** | data[0].embedding 长度为 1536；usage.total_tokens > 0 |
| **优先级** | P1 |

#### TC-EMBED-02: 无效请求体 → 400

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key |
| **请求** | `POST /v1/embeddings`，body: `{"invalid":"json"}` |
| **期望状态码** | 400 |
| **优先级** | P1 |

#### TC-EMBED-03: 缺少 model 字段 → 400

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key |
| **请求** | `POST /v1/embeddings`，body: `{"input":"hello"}` |
| **期望状态码** | 400 |
| **优先级** | P1 |

---

### 3.8 TC-MODELS 模型列表

#### TC-MODELS-01: 获取模型列表 → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key |
| **请求** | `GET /v1/models` |
| **期望状态码** | 200 |
| **期望响应** | `{"object":"list","data":[{"id":"gpt-4",...},{"id":"gpt-4-turbo",...},{"id":"gpt-3.5-turbo",...},{"id":"text-embedding-ada-002",...}]}` |
| **校验点** | data 数组包含 4 个模型 |
| **优先级** | P1 |

#### TC-MODELS-02: 无 API Key → 401

| 项目 | 内容 |
|------|------|
| **前置条件** | — |
| **请求** | `GET /v1/models`（无 Authorization 头） |
| **期望状态码** | 401 |
| **优先级** | P1 |

---

### 3.9 TC-SCOPE 权限校验

#### TC-SCOPE-01: Key 限制 model — 允许的 model → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `allowed_models: ["gpt-4"]` |
| **请求** | `POST /v1/chat/completions`，body 中 model 为 `gpt-4` |
| **期望状态码** | 200 |
| **优先级** | P0 |

#### TC-SCOPE-02: Key 限制 model — 不允许的 model → 403

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `allowed_models: ["gpt-4"]` |
| **请求** | `POST /v1/chat/completions`，body 中 model 为 `gpt-3.5-turbo` |
| **期望状态码** | 403 |
| **期望响应** | `{"error":{"message":"model not allowed","type":"forbidden"}}` |
| **优先级** | P0 |

#### TC-SCOPE-03: Key 限制 endpoint — 允许的 endpoint → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `allowed_endpoints: ["/v1/chat/completions"]` |
| **请求** | `POST /v1/chat/completions` |
| **期望状态码** | 200 |
| **优先级** | P0 |

#### TC-SCOPE-04: Key 限制 endpoint — 不允许的 endpoint → 403

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `allowed_endpoints: ["/v1/chat/completions"]` |
| **请求** | `POST /v1/embeddings` |
| **期望状态码** | 403 |
| **期望响应** | `{"error":{"message":"endpoint not allowed","type":"forbidden"}}` |
| **优先级** | P0 |

#### TC-SCOPE-05: Key 无 model 限制 — 任意 model → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `allowed_models` 为空/null |
| **请求** | `POST /v1/chat/completions`，body 中 model 为任意值 |
| **期望状态码** | 200 |
| **优先级** | P0 |

#### TC-SCOPE-06: Key 无 endpoint 限制 — 任意 endpoint → 200

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `allowed_endpoints` 为空/null |
| **请求** | `GET /v1/models` |
| **期望状态码** | 200 |
| **优先级** | P0 |

---

### 3.10 TC-RATELIMIT 限流

#### TC-RATELIMIT-01: 未超限 → 正常响应

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `rate_limit_rpm: 10` |
| **请求** | 连续发送 10 次 `POST /v1/chat/completions` |
| **期望状态码** | 前 10 次均为 200 |
| **优先级** | P1 |

#### TC-RATELIMIT-02: 超限 → 429

| 项目 | 内容 |
|------|------|
| **前置条件** | Key 的 `rate_limit_rpm: 5` |
| **请求** | 连续发送 7 次 `POST /v1/chat/completions` |
| **期望状态码** | 前 5 次为 200，第 6-7 次为 429 |
| **期望响应（429）** | `{"error":{"message":"rate limit exceeded","type":"rate_limit"}}` |
| **优先级** | P1 |

---

### 3.11 TC-USAGE 用量追踪

#### TC-USAGE-01: 请求后用量被记录

| 项目 | 内容 |
|------|------|
| **前置条件** | 拥有有效 API Key，已发送 1 次 chat completions 请求 |
| **请求** | `GET /admin/v1/usage?tenant_id={tenant_id}` |
| **期望状态码** | 200 |
| **期望响应** | `{"summary":{"total_requests":1,"total_tokens":35},"groups":[]}` |
| **校验点** | total_requests = 1；total_tokens = 35（mock 返回的值） |
| **注意** | 用量为异步写入，需等待最多 5s（flushInterval）后查询 |
| **优先级** | P1 |

#### TC-USAGE-02: 按 model 分组查询

| 项目 | 内容 |
|------|------|
| **前置条件** | 已发送多次请求，涉及不同 model |
| **请求** | `GET /admin/v1/usage?tenant_id={tenant_id}&group_by=model` |
| **期望状态码** | 200 |
| **校验点** | groups 数组按 model 分组，每组有 requests、prompt_tokens、completion_tokens、total_tokens |
| **优先级** | P1 |

#### TC-USAGE-03: 按 key_id 过滤

| 项目 | 内容 |
|------|------|
| **前置条件** | 同一租户下有多个 Key，各自发送过请求 |
| **请求** | `GET /admin/v1/usage?key_id={key_id}` |
| **期望状态码** | 200 |
| **校验点** | summary.total_requests 只包含该 Key 的请求 |
| **优先级** | P1 |

#### TC-USAGE-04: 按时间范围过滤

| 项目 | 内容 |
|------|------|
| **前置条件** | 已有用量数据 |
| **请求** | `GET /admin/v1/usage?start=2026-07-08T00:00:00Z&end=2026-07-08T23:59:59Z` |
| **期望状态码** | 200 |
| **校验点** | summary 只包含指定时间范围内的记录 |
| **优先级** | P1 |

#### TC-USAGE-05: 无用量数据 → 空结果

| 项目 | 内容 |
|------|------|
| **前置条件** | 清空 usage_records 表 |
| **请求** | `GET /admin/v1/usage` |
| **期望状态码** | 200 |
| **期望响应** | `{"summary":{"total_requests":0,"total_tokens":0},"groups":[]}` |
| **优先级** | P1 |

---

### 3.12 TC-ERROR 错误处理

#### TC-ERROR-01: 上游不可用 → 502

| 项目 | 内容 |
|------|------|
| **前置条件** | Mock Provider 停止，Gateway 的 MOCK_PROVIDER_URL 指向不可达地址 |
| **请求** | `POST /v1/chat/completions`，正常 body |
| **期望状态码** | 502 |
| **期望响应** | `{"error":{"message":"upstream error","type":"upstream_error"}}` |
| **优先级** | P1 |

#### TC-ERROR-02: 上游超时 → 504

| 项目 | 内容 |
|------|------|
| **前置条件** | Mock Provider 延迟响应（>30s），或 Gateway 超时设为 1s |
| **请求** | `POST /v1/chat/completions`，正常 body |
| **期望状态码** | 504 |
| **期望响应** | `{"error":{"message":"upstream timeout","type":"timeout"}}` |
| **优先级** | P1 |

#### TC-ERROR-03: Panic 恢复 → 500

| 项目 | 内容 |
|------|------|
| **前置条件** | 触发 handler 内 panic（如注入 nil 指针） |
| **请求** | 任意代理请求 |
| **期望状态码** | 500 |
| **期望响应** | `{"error":{"message":"internal server error","type":"server_error"}}` |
| **校验点** | 响应不包含堆栈信息 |
| **优先级** | P1 |

---

### 3.13 TC-FLOW 端到端完整流程

#### TC-FLOW-01: 创建租户 → 创建 Key → 调用代理 → 查询用量

| 步骤 | 操作 | 期望结果 |
|------|------|---------|
| 1 | `POST /admin/v1/tenants` `{"name":"E2E Tenant"}` | 201，返回 tenant_id |
| 2 | `POST /admin/v1/tenants/{tenant_id}/keys` `{"name":"E2E Key","scopes":{}}` | 201，返回 api_key 明文 |
| 3 | `POST /v1/chat/completions` `{"model":"gpt-4","messages":[...]}`，使用 api_key | 200，返回 chat completion + usage |
| 4 | `POST /v1/embeddings` `{"model":"text-embedding-ada-002","input":"test"}`，使用 api_key | 200，返回 embedding |
| 5 | `GET /v1/models`，使用 api_key | 200，返回 4 个模型 |
| 6 | 等待 6s（用量异步刷盘） | — |
| 7 | `GET /admin/v1/usage?tenant_id={tenant_id}&group_by=model` | 200，summary.total_requests = 2 |
| 8 | `PATCH /admin/v1/keys/{key_id}` `{"status":"disabled"}` | 200，status = disabled |
| 9 | `POST /v1/chat/completions`，使用同一 api_key | 403，"API key disabled" |
| 10 | `DELETE /admin/v1/keys/{key_id}` | 200，`{"status":"deleted"}` |
| 11 | `GET /admin/v1/keys/{key_id}` | 404，"key not found" |

**优先级**：P0

---

## 4. 测试执行

### 4.1 自动化测试

```bash
# 启动依赖服务
make docker-up

# 等待服务就绪
sleep 10

# 运行 E2E 测试
make e2e

# 清理
make docker-down
```

### 4.2 手动测试（curl 脚本）

```bash
# 设置变量
export GATEWAY_URL=http://localhost:8080
export ADMIN_TOKEN=test-admin-token

# 1. 健康检查
curl -s $GATEWAY_URL/health

# 2. 创建租户
TENANT_ID=$(curl -s -X POST $GATEWAY_URL/admin/v1/tenants \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Manual Test"}' | jq -r '.id')

# 3. 创建 API Key
API_KEY=$(curl -s -X POST $GATEWAY_URL/admin/v1/tenants/$TENANT_ID/keys \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name":"Test Key","scopes":{}}' | jq -r '.key')

# 4. 调用 Chat Completions
curl -s -X POST $GATEWAY_URL/v1/chat/completions \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-4","messages":[{"role":"user","content":"Hello"}]}' | jq

# 5. 查询用量
sleep 6
curl -s "$GATEWAY_URL/admin/v1/usage?tenant_id=$TENANT_ID&group_by=model" \
  -H "Authorization: Bearer $ADMIN_TOKEN" | jq
```

---

## 5. 测试覆盖率矩阵

| 功能模块 | 用例数 | 覆盖路径 |
|---------|--------|---------|
| 健康检查 | 1 | `/health` |
| Admin 鉴权 | 3 | AdminAuth 中间件（无 token / 错 token / 正确 token） |
| 租户管理 | 5 | Create / List / GetByID / 参数校验 / 404 |
| Key 管理 | 7 | Create(带/不带 scope) / List / Update(禁用/启用) / Delete / 不存在租户 |
| 代理鉴权 | 5 | 无 key / 无效 key / 禁用 key / 过期 key / 禁用租户 |
| Chat Completions | 4 | 正常 / 无效 body / 下游错误 / Request-ID |
| Embeddings | 3 | 正常 / 无效 body / 缺 model |
| Models | 2 | 正常 / 无 key |
| Scope 校验 | 6 | model 允许/拒绝 / endpoint 允许/拒绝 / 无限制 model / 无限制 endpoint |
| 限流 | 2 | 未超限 / 超限 429 |
| 用量追踪 | 5 | 记录验证 / 分组查询 / key 过滤 / 时间过滤 / 空结果 |
| 错误处理 | 3 | 502 / 504 / panic 500 |
| 端到端流程 | 1 | 完整业务闭环 |
| **合计** | **47** | |

---

## 6. 已知限制

1. **用量异步写入延迟**：UsageWriter 的 flushInterval 为 5s，测试中需 `sleep 6` 后才能查到用量
2. **限流窗口固定 1 分钟**：限流测试需在 1 分钟窗口内完成，跨窗口测试需等待窗口重置
3. **Mock Provider 行为固定**：无法模拟延迟、500 等场景，需增强 mock 或使用可配置 mock
4. **无租户禁用 API**：当前无 Admin API 直接禁用租户，需通过 DB 操作（TC-PROXY-AUTH-05）
5. **无 Key 过期创建入口**：Dashboard 支持设置过期时间，但 Admin API 的 `expires_at` 需传入 RFC3339 时间
