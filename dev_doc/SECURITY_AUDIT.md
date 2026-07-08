# AI Gateway — 安全审计报告

> 审查日期：2026-07-08
> 审查范围：全部源码 + 部署配置
> 审查重点：认证破解、注入攻击、拒绝服务、数据泄露、权限提升

---

## 审查结论

| 级别 | 数量 | 说明 |
|------|------|------|
| 🔴 Critical | 4 | 可被直接利用，攻击者可接管网关或导致服务不可用 |
| 🟠 High | 6 | 严重影响安全姿态，特定条件下可被利用 |
| 🟡 Medium | 5 | 特定场景下有风险，需配合其他条件利用 |
| 🔵 Low | 3 | 安全加固建议 |

**最高风险**：S1（Admin Token 硬编码）— 攻击者可直接接管网关，无需任何特殊条件。

---

## 攻击面总览

| 攻击场景 | 可行性 | 涉及问题 |
|---------|--------|---------|
| 用默认 Admin Token 接管网关 | **极高** | S1 |
| 暴力破解 Admin Token | 高 | S4 |
| 发送大请求体导致 OOM 崩溃 | 高 | S3 |
| 直接连接 MySQL 窃取数据 | 高 | S6 |
| 直接调用 Mock Provider 绕过网关 | 中 | S7 |
| 网络嗅探窃取 API Key | 中 | S5 |
| XSS 窃取 Admin Token | 中 | S2 |
| 重放攻击消耗租户配额 | 中 | S15 |
| SSRF 探测内网服务 | 低 | S12 |
| 点击劫持 Dashboard | 低 | S9 |

---

## 🔴 Critical

### S1. Admin Token 硬编码且公开可见

**文件**：`internal/config/config.go:25`、`docker-compose.yml:38`、`.env.example:6`

三处均使用同一个弱默认值 `admin-secret-token`：

```go
// config.go:25
AdminToken: getEnv("ADMIN_TOKEN", "admin-secret-token")
```

```yaml
# docker-compose.yml:38
ADMIN_TOKEN: admin-secret-token
```

**风险**：如果部署时未修改环境变量，攻击者直接用此 token 调用全部 Admin API：创建租户、创建 Key、查看用量、删除 Key。**等于完全控制网关。**

**修复**：

```go
func Load() *Config {
    cfg := &Config{
        AdminToken: os.Getenv("ADMIN_TOKEN"),
        // ... 其他字段保留默认值
    }
    if cfg.AdminToken == "" || cfg.AdminToken == "admin-secret-token" {
        log.Fatal("ADMIN_TOKEN must be set and must not be the default value")
    }
    return cfg
}
```

docker-compose.yml 中使用 `${ADMIN_TOKEN}` 引用环境变量，不硬编码。

---

### S2. Admin Token 明文存储在浏览器 localStorage

**文件**：`web/app.js:6-7,27`

```js
function getToken() {
    return localStorage.getItem('admin_token') || '';
}
function setToken() {
    // ...
    localStorage.setItem('admin_token', t);
}
```

**风险**：

1. `localStorage` 不受 HttpOnly 保护，任何 XSS 攻击可直接读取 token
2. `localStorage` 不会随页面关闭而清除，持久暴露
3. 第三方依赖被污染即可触发 XSS 窃取 token

**修复方案**：

- **短期**：改用 `sessionStorage`（页面关闭即清除），降低暴露窗口
- **长期**：改为后端设置 HttpOnly + Secure + SameSite cookie，前端不持有 token

---

### S3. 无请求体大小限制 — OOM 拒绝服务

**文件**：`internal/handler/proxy.go:59,125,224`

```go
// 请求体读取 — 无 size limit
body, err := io.ReadAll(c.Request.Body)

// 响应体读取 — 无 size limit
respBody, err := io.ReadAll(resp.Body)
```

**风险**：攻击者用有效 API Key 发送一个超大请求体（如 10GB），`io.ReadAll` 全部读入内存导致 OOM 崩溃。Gin 默认不限制请求体大小。下游响应同理。

**修复**：

```go
// 限制请求体 10MB
c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)
body, err := io.ReadAll(c.Request.Body)
```

响应体使用 `io.LimitReader`：

```go
respBody, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50MB max
```

或使用 Gin 中间件全局限制：

```go
r.Use(func(c *gin.Context) {
    c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)
    c.Next()
})
```

---

### S4. Admin API 无限流 — 可暴力破解 Admin Token

**文件**：`internal/router/router.go:42-54`

```go
admin := r.Group("/admin/v1")
admin.Use(middleware.AdminAuth(cfg.AdminToken))
// ← 无 rate limit 中间件
```

**风险**：攻击者可无限速发送不同 token 尝试。虽然 `admin_auth.go:26` 使用了 `subtle.ConstantTimeCompare`（防时序攻击），但无限流意味着暴力破解在理论上是可行的，尤其是当 token 为弱值时。

**修复**：为 Admin API 添加独立的 rate limiter（如每分钟 20 次/IP）：

```go
adminLimiter := service.NewRateLimiter()
admin.Use(func(c *gin.Context) {
    if !adminLimiter.Allow(c.ClientIP(), 20) {
        c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
            "error": gin.H{"message": "too many requests", "type": "rate_limit"},
        })
        return
    }
    c.Next()
})
```

---

## 🟠 High

### S5. 无 HTTPS — API Key 明文传输

**文件**：`cmd/gateway/main.go:68`

```go
if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
```

**风险**：API Key 通过 `Authorization: Bearer sk-agw-xxx` 明文传输。网络中间人（WiFi、ISP、CDN 节点）可抓包窃取 Key，之后冒充用户调用全部代理 API。

**修复**：

- **生产环境**：使用 TLS 证书（Let's Encrypt 或自签名），`srv.ListenAndServeTLS(certFile, keyFile)`
- **MVP/面试展示**：在反向代理层（Nginx/Caddy）终止 TLS，网关保持 HTTP

---

### S6. MySQL 端口对外暴露 + 弱密码

**文件**：`docker-compose.yml:5-10`

```yaml
MYSQL_ROOT_PASSWORD: rootpass    # ← 弱密码
MYSQL_PASSWORD: gateway          # ← 弱密码
ports:
    - "3306:3306"                # ← 对外暴露
```

**风险**：3306 端口映射到宿主机，外部可直接连接 MySQL。结合弱密码（`rootpass`、`gateway`），攻击者可绕过网关直接读写 DB：读取 API Key hash、用量数据，甚至修改 Key 状态。

**修复**：

```yaml
# 1. 移除端口映射（仅容器网络可达）
# ports:
#   - "3306:3306"   ← 删除

# 2. 使用强密码
MYSQL_ROOT_PASSWORD: ${MYSQL_ROOT_PASSWORD}
MYSQL_PASSWORD: ${MYSQL_PASSWORD}
```

---

### S7. Mock Provider 端口对外暴露

**文件**：`docker-compose.yml:23-24`

```yaml
ports:
    - "8081:8080"    # ← 不应暴露
```

**风险**：Mock Provider 无任何鉴权，外部可直接调用 `/v1/chat/completions` 等端点，绕过网关的 scope 校验、限流、用量记录。在生产环境中，AI Provider 端口绝不应暴露。

**修复**：移除 `ports` 映射，仅通过 Docker 内部网络访问：

```yaml
mock-provider:
    build:
      context: .
      dockerfile: Dockerfile.mock
    # ports:          ← 删除
    #   - "8081:8080"
```

---

### S8. 错误消息泄露内部信息

**文件**：`internal/handler/admin.go:110,164`

```go
c.JSON(http.StatusBadRequest, gin.H{
    "error": gin.H{"message": err.Error(), "type": "invalid_request_error"},
})
```

**风险**：`err.Error()` 可能包含 DB 错误详情（表名、连接信息、SQL 语句片段、GORM 内部错误），帮助攻击者了解内部结构，辅助进一步攻击。

**修复**：

```go
// 对外返回通用消息，内部日志记录详细错误
slog.Error("create key failed", "error", err, "tenant_id", tenantID)
c.JSON(http.StatusBadRequest, gin.H{
    "error": gin.H{"message": "failed to create key", "type": "invalid_request_error"},
})
```

---

### S9. 无安全响应头

**文件**：`internal/router/router.go:30-31`

```go
r := gin.New()
r.Use(gin.Logger(), middleware.Recovery())
// ← 无安全 header 中间件
```

**风险**：

| 缺失 Header | 风险 |
|-------------|------|
| `X-Frame-Options: DENY` | Dashboard 可被 iframe 嵌套，攻击者诱导用户点击（点击劫持） |
| `X-Content-Type-Options: nosniff` | 浏览器 MIME 嗅探可能导致 XSS |
| `Strict-Transport-Security` | 无法强制 HTTPS |
| `Content-Security-Policy` | 无法防御 XSS 注入 |

**修复**：添加安全 header 中间件：

```go
r.Use(func(c *gin.Context) {
    c.Header("X-Frame-Options", "DENY")
    c.Header("X-Content-Type-Options", "nosniff")
    c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
    c.Header("Content-Security-Policy", "default-src 'self'; script-src 'self'")
    c.Next()
})
```

---

### S10. DB 连接无 TLS

**文件**：`cmd/gateway/main.go:25`

```go
dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local",
    cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)
```

**风险**：DSN 中无 `tls=true`，DB 凭据和数据明文传输。如果 DB 不在同一主机（云环境 RDS），网络嗅探可窃取 DB 凭据和业务数据。

**修复**：

```go
dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local&tls=true",
    cfg.DBUser, cfg.DBPass, cfg.DBHost, cfg.DBPort, cfg.DBName)
```

---

## 🟡 Medium

### S11. 无输入长度校验

**文件**：`internal/handler/admin.go:42`

```go
Name string `json:"name" binding:"required"`  // ← 只校验非空，无 max length
```

**风险**：租户名、Key 名、scope 中的 model/endpoint 列表均无长度限制。攻击者可提交超长字符串（如 1MB 的租户名），导致 DB 存储膨胀、查询性能下降、前端渲染卡顿。

**修复**：

```go
type createTenantReq struct {
    Name string `json:"name" binding:"required,min=1,max=100"`
}
```

Scope 字段同样加长度限制：

```go
if len(req.Scopes.AllowedModels) > 50 {
    // 返回错误
}
```

---

### S12. SSRF 风险 — MockProviderURL 未校验

**文件**：`internal/config/config.go:26`、`internal/handler/proxy.go:205`

```go
MockProviderURL: getEnv("MOCK_PROVIDER_URL", "http://localhost:8081")
// ...
url := h.mockURL + path
req, err := http.NewRequest(method, url, bytes.NewReader(body))
```

**风险**：如果攻击者能控制环境变量（如容器注入、配置文件篡改），可将 `MOCK_PROVIDER_URL` 指向内网服务（如 `http://169.254.169.254` 云元数据端点），网关会代为请求并返回响应内容。

**修复**：启动时校验 URL，拒绝内网地址：

```go
func validateUpstreamURL(rawURL string) error {
    u, err := url.Parse(rawURL)
    if err != nil {
        return err
    }
    ip := net.ParseIP(u.Hostname())
    if ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()) {
        return fmt.Errorf("upstream URL must not be internal: %s", rawURL)
    }
    return nil
}
```

> 注意：MVP 中 Mock Provider 本身就是 localhost，此项为生产环境加固建议。

---

### S13. 无审计日志

**文件**：`internal/handler/admin.go`（全部 Admin handler）

Admin API 的所有操作（创建/删除租户、创建/禁用/删除 Key）均无审计日志记录。

**风险**：恶意操作（如删除所有 Key 导致服务中断、创建后门 Key）无法追溯，无法满足合规要求。

**修复**：在 Admin handler 中添加审计日志：

```go
slog.Info("admin action",
    "action", "create_key",
    "tenant_id", tenantID,
    "key_id", result.ID,
    "admin_ip", c.ClientIP(),
    "timestamp", time.Now().UTC(),
)
```

---

### S14. 无 API Key 格式预校验

**文件**：`internal/middleware/apikey_auth.go:39-40`

```go
hash := service.HashKey(rawKey)
key, err := keySvc.GetByHash(hash)
```

**风险**：不对 rawKey 做格式校验（如 `sk-agw-` 前缀检查），任意字符串都会 hash 后查 DB。攻击者可用垃圾数据制造大量 DB 查询，增加 DB 负载，间接形成 DoS。

**修复**：

```go
if !strings.HasPrefix(rawKey, "sk-agw-") || len(rawKey) != 40 {
    writeAuthError(c, http.StatusUnauthorized, "invalid API key format", "auth_error")
    return
}
```

---

### S15. 无重放攻击防护

API Key 是静态 Bearer Token，请求中无 nonce、timestamp 或签名。

**风险**：攻击者抓包后可无限重放请求，消耗租户配额、产生虚假用量。即使有 HTTPS，中间人（如企业代理、CDN）仍可能截获。

**修复方案**（按复杂度排序）：

1. **短期**：结合 IP + 短时间窗口的请求去重
2. **中期**：请求签名（HMAC + timestamp + nonce）
3. **长期**：mTLS 双向认证

> MVP 可接受此风险，但应在已知限制中注明。

---

## 🔵 Low

### S16. Dashboard 页面无鉴权

**文件**：`internal/router/router.go:39`

```go
r.Static("/dashboard", "./web")
```

`/dashboard` 路径无 auth 中间件，页面本身可被任何人访问。虽然 API 调用需要 token，但暴露了系统结构信息（功能模块、API 路径、表单字段）。

**修复**：可添加 Basic Auth 保护 `/dashboard` 路径，或将 Dashboard 改为独立的内网服务。

---

### S17. DB 密码明文存储

DB 密码以明文存在于环境变量和 `docker-compose.yml` 中，无 secret manager 集成。

**修复**：

- 使用 Docker Secrets 或 Kubernetes Secrets
- 或使用 HashiCorp Vault、AWS Secrets Manager 等工具管理密钥

---

### S18. gin.Logger() 记录请求路径

**文件**：`internal/router/router.go:31`

Gin 默认 logger 记录完整请求路径。Usage 查询 API 的 query param（`tenant_id`、`key_id`）会被记录到日志中，可能泄露业务信息。

**修复**：自定义 gin logger，脱敏 query param，或仅记录 path 不记录 query string。

---

## 修复优先级

### P0 — 立即修复（阻断攻击路径）

| 编号 | 问题 | 修复工作量 |
|------|------|-----------|
| S1 | Admin Token 硬编码 | 30min |
| S3 | 请求体大小限制 | 15min |
| S6 | MySQL 端口+弱密码 | 10min |
| S7 | Mock Provider 端口暴露 | 5min |

### P1 — 尽快修复（显著降低风险）

| 编号 | 问题 | 修复工作量 |
|------|------|-----------|
| S4 | Admin API 限流 | 30min |
| S2 | Token 存储方式 | 30min |
| S8 | 错误消息泄露 | 20min |
| S9 | 安全响应头 | 15min |

### P2 — 计划修复（加固安全姿态）

| 编号 | 问题 | 修复工作量 |
|------|------|-----------|
| S5 | HTTPS/TLS | 30min |
| S10 | DB 连接 TLS | 10min |
| S11 | 输入长度校验 | 20min |
| S14 | Key 格式预校验 | 10min |

### P3 — 长期改进

| 编号 | 问题 | 修复工作量 |
|------|------|-----------|
| S12 | SSRF 校验 | 30min |
| S13 | 审计日志 | 1h |
| S15 | 重放攻击防护 | 2h+ |
| S16-S18 | 其他加固 | 各 15min |

---

## 已有的安全措施（做得好的部分）

| 措施 | 位置 | 说明 |
|------|------|------|
| API Key 仅存 SHA-256 hash | `service/apikey.go:65` | DB 泄露不暴露明文 key |
| Key 明文仅创建时返回一次 | `service/apikey.go:83-92` | 符合最佳实践 |
| Key 生成使用 crypto/rand | `service/apikey.go:157-158` | 128 bit 熵，不可预测 |
| Admin Token 常量时间比较 | `middleware/admin_auth.go:26` | 防 timing attack |
| Tenant 状态校验 | `middleware/apikey_auth.go:59-67` | 禁用租户的 key 不可用 |
| Key 状态+过期校验 | `middleware/apikey_auth.go:47-56` | 禁用/过期 key 不可用 |
| Recovery 中间件 | `middleware/recovery.go` | Panic 不暴露堆栈给客户端 |
| Gin ReleaseMode | `router.go:15` | 不泄露调试信息 |
| Scope 权限校验 | `handler/proxy.go:242-273` | 按 key 维度限制 endpoint/model |
| XSS 防护函数 | `web/app.js:197-202` | 用户输入经 esc() 转义 |
| SQL 参数化查询 | `repository/*.go` | 全部使用 GORM ?占位符，无注入 |

---

## 附录：安全审查文件清单

| 文件 | 状态 | 涉及问题 |
|------|------|---------|
| `internal/config/config.go` | ⚠️ | S1 默认 token |
| `cmd/gateway/main.go` | ⚠️ | S5 无 TLS, S10 DB 无 TLS |
| `internal/router/router.go` | ⚠️ | S4 无 admin 限流, S9 无安全 header, S16 dashboard 无 auth |
| `internal/handler/admin.go` | ⚠️ | S8 错误泄露, S11 无长度校验, S13 无审计 |
| `internal/handler/proxy.go` | ⚠️ | S3 无 body 限制 |
| `internal/middleware/apikey_auth.go` | ⚠️ | S14 无格式预校验 |
| `internal/middleware/admin_auth.go` | ✅ | ConstantTimeCompare 已用 |
| `internal/middleware/recovery.go` | ✅ | 不泄露堆栈 |
| `internal/service/apikey.go` | ✅ | crypto/rand + SHA-256 hash |
| `internal/service/ratelimit.go` | ✅ | 代理 API 有限流 |
| `internal/repository/*.go` | ✅ | 参数化查询 |
| `web/app.js` | ⚠️ | S2 localStorage, XSS 已有 esc() 防护 |
| `docker-compose.yml` | ⚠️ | S1 硬编码 token, S6 MySQL 暴露+弱密码, S7 mock 暴露 |
| `.env.example` | ⚠️ | S1 弱默认值 |
| `Dockerfile` / `Dockerfile.mock` | ✅ | 无安全问题 |
| `docs/openapi.yaml` | ✅ | 无安全问题 |
