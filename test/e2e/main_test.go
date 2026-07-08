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

	"github.com/ai-gateway/internal/config"
	"github.com/ai-gateway/internal/model"
	"github.com/ai-gateway/internal/repository"
	"github.com/ai-gateway/internal/router"
	"github.com/ai-gateway/internal/service"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const testAdminToken = "test-admin-token"

var (
	db          *gorm.DB
	gatewayURL  string
	gatewaySrv  *httptest.Server
	mockSrv     *httptest.Server
	usageWriter *service.UsageWriter
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
	r := router.Setup(db, cfg, usageWriter)
	gatewaySrv = httptest.NewServer(r)
	gatewayURL = gatewaySrv.URL

	code := m.Run()

	usageWriter.Shutdown()
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
}

func setupGatewayWithMockURL(mockURL string) (*httptest.Server, *service.UsageWriter) {
	uw := service.NewUsageWriter(repository.NewUsageStore(db))
	cfg := &config.Config{AdminToken: testAdminToken, MockProviderURL: mockURL}
	r := router.Setup(db, cfg, uw)
	return httptest.NewServer(r), uw
}

type apiResp struct {
	Status int
	Body   interface{}
	Header http.Header
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
	return apiResp{Status: resp.StatusCode, Body: v, Header: resp.Header}
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
	return apiResp{Status: resp.StatusCode, Body: v, Header: resp.Header}
}

func createTenant(name string) string {
	r := adminReq("POST", "/admin/v1/tenants", map[string]string{"name": name})
	return r.mapBody()["id"].(string)
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
	time.Sleep(6 * time.Second)
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
