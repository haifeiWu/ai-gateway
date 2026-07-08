# AI Gateway — Code Review 报告

> 审查日期：2026-07-08
> 审查范围：全部源码（31 个文件）
> 审查基准：DESIGN.md 技术方案 + 面试交付要求

---

## 审查结论

| 级别 | 数量 | 说明 |
|------|------|------|
| 🔴 Critical | 5 | 会导致核心功能故障或构建失败 |
| 🟠 High | 7 | 影响健壮性和交付质量 |
| 🟡 Medium | 5 | 代码质量与可维护性 |
| 🔵 Low | 3 | 建议改进 |

**优先修复顺序**：#1 → #2 → #5 → #4 → #3 → #7 → #6 → #8 → #9 → 其余

---

## 🔴 Critical

### #1 Go 版本不匹配 — 构建必失败

**文件**：`go.mod:3`、`Dockerfile:1`、`Dockerfile.mock:1`

`go.mod` 声明 `go 1.25.0`，但两个 Dockerfile 均使用 `golang:1.23-alpine` 作为构建基础镜像。`docker compose up` 时 Go 1.23 编译器无法处理 `go 1.25` 指令，构建必定失败。

```diff
- FROM golang:1.23-alpine AS builder
+ FROM golang:1.25-alpine AS builder
```

---

### #2 Dashboard Tab 切换失效

**文件**：`web/app.js:43`

```js
document.querySelector(`.tab:nth-${name === 'tenants' ? '1' : name === 'keys' ? '2' : '3'}`)
```

生成的是 `.tab:nth-1` / `.tab:nth-2` / `.tab:nth-3`，**不是合法的 CSS 选择器**。`querySelector` 返回 null，后续 `.classList.add('active')` 抛异常，tab 高亮完全失效。

**修复**：

```js
document.querySelector(`.tab:nth-child(${name === 'tenants' ? 1 : name === 'keys' ? 2 : 3})`)
```

---

### #3 用量查询忽略 DB 错误

**文件**：`internal/repository/usage.go:82-83`、`95`

```go
// 第 82 行：summary 查询
base.Select(
    "COUNT(*) as total_requests, COALESCE(SUM(total_tokens), 0) as total_tokens",
).Scan(&summary)  // ← error 被丢弃

// 第 95 行：groups 查询
base.Select(groupByExpr + ", " + selectCols).Group(groupByExpr).Scan(&groups)  // ← error 被丢弃
```

`Scan()` 返回的 error 未被检查。DB 查询失败时，函数返回空结果 + nil error，调用方误以为查询成功且无数据。

**修复**：

```go
if err := base.Select(...).Scan(&summary).Error; err != nil {
    return nil, fmt.Errorf("query usage summary: %w", err)
}
```

---

### #4 缺少 504 超时处理

**文件**：`internal/handler/proxy.go:77`、`136`

设计文档明确要求下游超时返回 504，但当前实现将所有上游错误统一返回 502：

```go
if err != nil {
    slog.Error("upstream error", ...)
    writeError(c, http.StatusBadGateway, "upstream error", "upstream_error")  // ← 全部 502
    h.recordUsage(key, req.Model, 0, 0, 0, statusCode, int(latency), c)
    return
}
```

`http.Client.Timeout` 为 30s，超时后返回 error，但被当作普通 502 处理。

**修复**：

```go
if err != nil {
    if os.IsTimeout(err) {
        writeError(c, http.StatusGatewayTimeout, "upstream timeout", "timeout")
    } else {
        writeError(c, http.StatusBadGateway, "upstream error", "upstream_error")
    }
    return
}
```

---

### #5 代理鉴权未校验 Tenant 状态

**文件**：`internal/middleware/apikey_auth.go:39-56`

中间件检查了 Key 的 status 和 expiry，但**未检查 Tenant 状态**。设计文档要求 "Tenant disabled → 403"，当前实现下禁用租户的 Key 仍可正常调用代理。

```go
// 当前代码：只检查 Key 状态
if key.Status == model.KeyStatusDisabled {
    writeError(c, http.StatusForbidden, "API key disabled", "forbidden")
    c.Abort()
    return
}
// ← 缺少 Tenant 状态检查
```

**修复**：在中间件中查询 Tenant 状态，或在 `GetByHash` 时 Preload Tenant。

---

## 🟠 High

### #6 API Key 生成忽略 `rand.Read` 错误

**文件**：`internal/service/apikey.go:144-148`

```go
func randomString(n int) string {
    b := make([]byte, n/2+1)
    rand.Read(b)  // ← error 未检查
    return hex.EncodeToString(b)[:n]
}
```

`crypto/rand.Read` 在极端情况（如系统熵池耗尽）会返回 error。忽略后 `b` 为全零字节，生成的 key 可预测。

**修复**：

```go
if _, err := rand.Read(b); err != nil {
    return "", fmt.Errorf("generate random key: %w", err)
}
```

---

### #7 Scopes.Scan 不处理 NULL 值

**文件**：`internal/model/apikey.go:26-32`

```go
func (s *Scopes) Scan(value interface{}) error {
    bytes, ok := value.([]byte)
    if !ok {
        return errors.New("invalid scopes type")
    }
    return json.Unmarshal(bytes, s)
}
```

DB 列为 NULL 时 `value` 为 `nil`，类型断言 `[]byte` 失败返回 error。虽然 GORM AutoMigrate 会设 `type:json`，但 MySQL JSON 列默认值行为不一定为空 JSON。

**修复**：

```go
func (s *Scopes) Scan(value interface{}) error {
    if value == nil {
        return nil
    }
    bytes, ok := value.([]byte)
    if !ok {
        return errors.New("invalid scopes type")
    }
    if len(bytes) == 0 {
        return nil
    }
    return json.Unmarshal(bytes, s)
}
```

---

### #8 writeError 重复定义且行为不一致

**文件**：`internal/handler/proxy.go:291` vs `internal/middleware/apikey_auth.go:60`

| 位置 | 实现 | 问题 |
|------|------|------|
| handler/proxy.go | `c.AbortWithStatusJSON` | 正确，原子操作 |
| middleware/apikey_auth.go | `c.JSON` + 手动 `c.Abort()` | 脆弱，JSON 失败时 Abort 仍执行但响应可能异常 |

**修复**：统一提取到公共包，统一使用 `AbortWithStatusJSON`。

---

### #9 RateLimiter 无定期清理

**文件**：`internal/service/ratelimit.go:49-59`

`CleanupExpired()` 方法已定义但从未被调用。长期运行后 `windows` map 中过期 key 的窗口不会被回收，内存持续增长。

**修复**：在 `NewRateLimiter` 中启动定时清理 goroutine：

```go
go func() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for range ticker.C {
        rl.CleanupExpired()
    }
}()
```

---

### #10 请求 ID 未写入响应头

**文件**：`internal/handler/proxy.go:271-273`

```go
requestID := c.GetString("request_id")
if requestID == "" {
    requestID = uuid.NewString()
}
```

生成 request_id 但未设到 response header，调用方无法关联请求，排障困难。

**修复**：

```go
c.Header("X-Request-ID", requestID)
```

---

### #11 OpenAPI spec 缺少错误响应

**文件**：`docs/openapi.yaml`

| 端点 | 缺失的响应码 |
|------|-------------|
| `/v1/embeddings` | 401, 403, 429, 502, 504 |
| `/v1/models` | 401, 403, 429, 502 |
| `/admin/v1/tenants` POST | 500 |
| 各 admin 端点 | 500 |

代码实际会返回这些状态码，但 spec 未声明，与实现不一致。

---

### #12 Gateway 无健康检查端点

**文件**：`docker-compose.yml`、`internal/router/router.go`

docker-compose 只对 mysql 做了 healthcheck，Gateway 无 `/health` 端点。Kubernetes/Docker 就绪探针无法使用。

**修复**：在 router 中添加 `/health` 路由：

```go
r.GET("/health", func(c *gin.Context) {
    c.JSON(200, gin.H{"status": "ok"})
})
```

---

## 🟡 Medium

### #13 forward 方法构造请求方式 hacky

**文件**：`internal/handler/proxy.go:186-194`

```go
req, err := http.NewRequest(http.MethodGet, url, bytes.NewReader(body))
if body != nil {
    req.Method = http.MethodPost  // ← 先 GET 再改 POST
}
```

先创建 GET 请求再改 Method，可读性差且容易出错。应根据 endpoint 明确指定 HTTP method。

---

### #14 ChatCompletions 与 Embeddings 大量重复代码

**文件**：`internal/handler/proxy.go:40-101` vs `104-161`

两者逻辑几乎相同：读 body → 解析 → scope 校验 → forward → 记录用量 → 返回。约 60 行重复代码，可提取公共方法。

---

### #15 APIKey.Tenant 关联字段定义了但从未 Preload

**文件**：`internal/model/apikey.go:53`

```go
Tenant *Tenant `json:"tenant,omitempty" gorm:"foreignKey:TenantID;references:ID"`
```

无 repository 方法使用 `Preload("Tenant")`，返回 Key 列表时 tenant 字段永远为 nil。

---

### #16 无 `.gitignore`

项目缺少 `.gitignore`，二进制文件（gateway、mock-provider）、`.env`、IDE 配置等可能被误提交。

---

### #17 无单元测试

整个项目零测试文件。面试交付建议至少覆盖：
- Key 生成与 hash 逻辑
- Scope 校验逻辑
- RateLimiter
- UsageWriter batch 逻辑

---

## 🔵 Low

### #18 import 顺序不规范

**文件**：`internal/service/apikey.go:4-5`

```go
"crypto/sha256"     // ← 应在 crypto/rand 之后
"crypto/rand"
```

标准库 import 应按字母序排列。

---

### #19 Admin token 明文比较

**文件**：`internal/middleware/admin_auth.go:25`

```go
if token != adminToken {
```

存在时序攻击风险。建议使用 `crypto/subtle.ConstantTimeCompare`。MVP 可接受，但应在已知限制中注明。

---

### #20 Proxy 不转发原始请求头

**文件**：`internal/handler/proxy.go:195`

```go
req.Header.Set("Content-Type", "application/json")
```

只设了 `Content-Type`，丢失了 `Accept`、`User-Agent` 等。对于真实 provider，部分 header（如 `Accept-Encoding`）需要特殊处理。

---

## 附录：文件审查清单

| 文件 | 状态 | 涉及问题 |
|------|------|---------|
| `go.mod` | ⚠️ | #1 Go 版本 |
| `Dockerfile` | ⚠️ | #1 Go 版本 |
| `Dockerfile.mock` | ⚠️ | #1 Go 版本 |
| `cmd/gateway/main.go` | ✅ | — |
| `cmd/mock-provider/main.go` | ✅ | — |
| `internal/config/config.go` | ✅ | — |
| `internal/model/tenant.go` | ✅ | — |
| `internal/model/apikey.go` | ⚠️ | #7 Scopes.Scan |
| `internal/model/usage.go` | ✅ | — |
| `internal/repository/tenant.go` | ✅ | — |
| `internal/repository/apikey.go` | ✅ | — |
| `internal/repository/usage.go` | ⚠️ | #3 错误忽略 |
| `internal/service/tenant.go` | ✅ | — |
| `internal/service/apikey.go` | ⚠️ | #6 rand 错误, #18 import |
| `internal/service/ratelimit.go` | ⚠️ | #9 无清理 |
| `internal/service/usage.go` | ✅ | — |
| `internal/handler/admin.go` | ✅ | — |
| `internal/handler/proxy.go` | ⚠️ | #4 504, #8 writeError, #10 request_id, #13 forward, #14 重复, #20 header |
| `internal/handler/usage.go` | ✅ | — |
| `internal/middleware/admin_auth.go` | ⚠️ | #19 时序攻击 |
| `internal/middleware/apikey_auth.go` | ⚠️ | #5 Tenant 校验, #8 writeError |
| `internal/middleware/recovery.go` | ✅ | — |
| `internal/router/router.go` | ⚠️ | #12 无 health |
| `pkg/openai/types.go` | ✅ | — |
| `web/index.html` | ✅ | — |
| `web/app.js` | ⚠️ | #2 Tab 选择器 |
| `web/style.css` | ✅ | — |
| `docs/openapi.yaml` | ⚠️ | #11 缺错误响应 |
| `docker-compose.yml` | ⚠️ | #12 无 gateway healthcheck |
| `README.md` | ✅ | — |
| `Makefile` | ✅ | — |
| `.env.example` | ✅ | — |
