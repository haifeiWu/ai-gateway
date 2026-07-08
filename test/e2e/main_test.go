//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/haifeiWu/ai-gateway/internal/config"
	"github.com/haifeiWu/ai-gateway/internal/model"
	"github.com/haifeiWu/ai-gateway/internal/repository"
	"github.com/haifeiWu/ai-gateway/internal/router"
	"github.com/haifeiWu/ai-gateway/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testAdminToken = "test-admin-token"

var (
	db           *gorm.DB
	gatewayURL   string
	gatewaySrv   *httptest.Server
	mockSrv      *httptest.Server
	usageWriter  *service.UsageWriter
	adminLimiter *service.RateLimiter
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	dsn := "gateway:gateway@tcp(localhost:3306)/ai_gateway?charset=utf8mb4&parseTime=True&loc=Local"
	var err error
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		fmt.Println("SKIP: MySQL not available:", err)
		return 0
	}

	db.AutoMigrate(&model.Tenant{}, &model.APIKey{}, &model.UsageRecord{})
	cleanDB()

	mockSrv = startMockProvider()
	usageWriter = service.NewUsageWriter(repository.NewUsageStore(db))

	cfg := &config.Config{
		AdminToken:      testAdminToken,
		MockProviderURL: mockSrv.URL,
	}
	r, proxyLimiter, adminLim := router.Setup(db, cfg, usageWriter)
	gatewaySrv = httptest.NewServer(r)
	gatewayURL = gatewaySrv.URL
	adminLimiter = adminLim

	code := m.Run()

	usageWriter.Shutdown()
	proxyLimiter.Close()
	adminLim.Close()
	gatewaySrv.Close()
	mockSrv.Close()
	cleanDB()

	return code
}

func startMockProvider() *httptest.Server {
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "ok"})
	})

	r.GET("/v1/models", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"object": "list",
			"data": []gin.H{
				{"id": "gpt-4", "object": "model", "created": 1687882411, "owned_by": "openai"},
				{"id": "gpt-4-turbo", "object": "model", "created": 1692901427, "owned_by": "openai"},
				{"id": "gpt-3.5-turbo", "object": "model", "created": 1677649963, "owned_by": "openai"},
				{"id": "text-embedding-ada-002", "object": "model", "created": 1671217299, "owned_by": "openai"},
			},
		})
	})

	r.POST("/v1/chat/completions", func(c *gin.Context) {
		var req struct {
			Model  string `json:"model"`
			Stream *bool  `json:"stream,omitempty"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": gin.H{"message": "invalid request body", "type": "invalid_request_error"}})
			return
		}

		now := time.Now().Unix()
		content := "Mock response from " + req.Model
		chunkID := "chatcmpl-mock-" + time.Now().Format("20060102150405")

		if req.Stream != nil && *req.Stream {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Status(200)
			flusher, _ := c.Writer.(http.Flusher)

			words := strings.Split(content, " ")
			for i, w := range words {
				chunk := gin.H{
					"id": chunkID, "object": "chat.completion.chunk", "created": now, "model": req.Model,
					"choices": []gin.H{{"index": 0, "delta": gin.H{"content": w + " "}}},
				}
				if i == 0 {
					chunk["choices"].([]gin.H)[0]["delta"].(gin.H)["role"] = "assistant"
				}
				data, _ := json.Marshal(chunk)
				fmt.Fprintf(c.Writer, "data: %s\n\n", data)
				if flusher != nil {
					flusher.Flush()
				}
			}

			final := gin.H{
				"id": chunkID, "object": "chat.completion.chunk", "created": now, "model": req.Model,
				"choices": []gin.H{{"index": 0, "delta": gin.H{}, "finish_reason": "stop"}},
				"usage":   gin.H{"prompt_tokens": 15, "completion_tokens": 20, "total_tokens": 35},
			}
			finalData, _ := json.Marshal(final)
			fmt.Fprintf(c.Writer, "data: %s\n\n", finalData)
			fmt.Fprintf(c.Writer, "data: [DONE]\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			return
		}

		c.JSON(200, gin.H{
			"id":      "chatcmpl-mock-" + time.Now().Format("20060102150405"),
			"object":  "chat.completion",
			"created": now,
			"model":   req.Model,
			"choices": []gin.H{
				{"index": 0, "message": gin.H{"role": "assistant", "content": content}, "finish_reason": "stop"},
			},
			"usage": gin.H{"prompt_tokens": 15, "completion_tokens": 20, "total_tokens": 35},
		})
	})

	r.POST("/v1/embeddings", func(c *gin.Context) {
		c.JSON(200, gin.H{
			"object": "list",
			"data":   []gin.H{{"object": "embedding", "index": 0, "embedding": make([]float64, 1536)}},
			"model":  "text-embedding-ada-002",
			"usage":  gin.H{"prompt_tokens": 8, "total_tokens": 8},
		})
	})

	return httptest.NewServer(r)
}

func cleanDB() {
	db.Exec("DELETE FROM usage_records")
	db.Exec("DELETE FROM api_keys")
	db.Exec("DELETE FROM tenants")
	if adminLimiter != nil {
		adminLimiter.Reset()
	}
}

func setupGatewayWithMockURL(mockURL string) (*httptest.Server, *service.UsageWriter) {
	uw := service.NewUsageWriter(repository.NewUsageStore(db))
	cfg := &config.Config{AdminToken: testAdminToken, MockProviderURL: mockURL}
	r, _, _ := router.Setup(db, cfg, uw)
	return httptest.NewServer(r), uw
}

type apiResp struct {
	Status  int
	Body    interface{}
	Header  http.Header
	RawBody string
}

func (r apiResp) mapBody() map[string]interface{} {
	m, _ := r.Body.(map[string]interface{})
	return m
}

func (r apiResp) arrBody() []interface{} {
	a, _ := r.Body.([]interface{})
	return a
}

func adminReq(method, path string, body interface{}) apiResp {
	if adminLimiter != nil {
		adminLimiter.Reset()
	}
	return doReq(gatewayURL, testAdminToken, method, path, body)
}

func proxyReq(apiKey, method, path string, body interface{}) apiResp {
	return doReq(gatewayURL, apiKey, method, path, body)
}

func noAuthReq(method, path string, body interface{}) apiResp {
	return doReq(gatewayURL, "", method, path, body)
}

func doReq(base, auth, method, path string, body interface{}) apiResp {
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, base+path, r)
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiResp{Status: 0}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var v interface{}
	json.Unmarshal(data, &v)
	return apiResp{Status: resp.StatusCode, Body: v, Header: resp.Header, RawBody: string(data)}
}

func doReqRaw(base, auth, method, path, rawBody string) apiResp {
	req, _ := http.NewRequest(method, base+path, bytes.NewReader([]byte(rawBody)))
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return apiResp{Status: 0}
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var v interface{}
	json.Unmarshal(data, &v)
	return apiResp{Status: resp.StatusCode, Body: v, Header: resp.Header, RawBody: string(data)}
}

func createTenant(name string) string {
	r := adminReq("POST", "/admin/v1/tenants", map[string]string{"name": name})
	id, _ := r.mapBody()["id"].(string)
	return id
}

func createKey(tenantID, name string, scopes model.Scopes) (id, key string) {
	body := map[string]interface{}{"name": name, "scopes": scopes}
	r := adminReq("POST", "/admin/v1/tenants/"+tenantID+"/keys", body)
	m := r.mapBody()
	return m["id"].(string), m["key"].(string)
}

func getKeyID(tenantID, keyName string) string {
	r := adminReq("GET", "/admin/v1/tenants/"+tenantID+"/keys", nil)
	for _, item := range r.arrBody() {
		if m, ok := item.(map[string]interface{}); ok && m["name"] == keyName {
			return m["id"].(string)
		}
	}
	return ""
}

func waitForUsage() {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if adminLimiter != nil {
			adminLimiter.Reset()
		}
		resp := adminReq("GET", "/admin/v1/usage", nil)
		if resp.Status == 200 {
			summary, _ := resp.mapBody()["summary"].(map[string]interface{})
			if total, _ := summary["total_requests"].(float64); total > 0 {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func waitForUsageCount(tid string, n int) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if adminLimiter != nil {
			adminLimiter.Reset()
		}
		resp := adminReq("GET", fmt.Sprintf("/admin/v1/usage?tenant_id=%s", tid), nil)
		if resp.Status == 200 {
			summary, _ := resp.mapBody()["summary"].(map[string]interface{})
			if total, _ := summary["total_requests"].(float64); int(total) >= n {
				return
			}
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// safeMap 对 Body 做安全类型断言，避免 nil panic。
func safeMap(r apiResp) map[string]interface{} {
	m, _ := r.Body.(map[string]interface{})
	return m
}

// getFloat 从 map 安全获取 float64 值。
func getFloat(t *testing.T, m map[string]interface{}, key string) float64 {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("%q: key %q not found in %v", t.Name(), key, m)
	}
	f, ok := v.(float64)
	if !ok {
		t.Fatalf("%q: %q = %T, want float64", t.Name(), key, v)
	}
	return f
}

// getString 从 map 安全获取 string 值。
func getString(t *testing.T, m map[string]interface{}, key string) string {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("%q: key %q not found in %v", t.Name(), key, m)
	}
	s, ok := v.(string)
	if !ok {
		t.Fatalf("%q: %q = %T, want string", t.Name(), key, v)
	}
	return s
}

// getMap 从 map 安全获取嵌套 map。
func getMap(t *testing.T, m map[string]interface{}, key string) map[string]interface{} {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("%q: key %q not found in %v", t.Name(), key, m)
	}
	sub, ok := v.(map[string]interface{})
	if !ok {
		t.Fatalf("%q: %q = %T, want map[string]interface{}", t.Name(), key, v)
	}
	return sub
}

// getArray 从 map 安全获取数组。
func getArray(t *testing.T, m map[string]interface{}, key string) []interface{} {
	t.Helper()
	v, ok := m[key]
	if !ok {
		t.Fatalf("%q: key %q not found in %v", t.Name(), key, m)
	}
	arr, ok := v.([]interface{})
	if !ok {
		t.Fatalf("%q: %q = %T, want []interface{}", t.Name(), key, v)
	}
	return arr
}

func assertStatus(t *testing.T, label string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s: status = %d, want %d", label, got, want)
	}
}

func assertErrorType(t *testing.T, label string, body interface{}, want string) {
	t.Helper()
	m, ok := body.(map[string]interface{})
	if !ok {
		t.Errorf("%s: body is not an object: %v", label, body)
		return
	}
	errObj, ok := m["error"].(map[string]interface{})
	if !ok {
		t.Errorf("%s: no error object: %v", label, m)
		return
	}
	got, _ := errObj["type"].(string)
	if got != want {
		t.Errorf("%s: error.type = %q, want %q", label, got, want)
	}
}
