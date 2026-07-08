package handler

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ai-gateway/internal/model"
	"github.com/ai-gateway/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// RateLimiter 限流器接口。
type RateLimiter interface {
	Allow(keyID string, rpm int) bool
}

// UsageRecorder 用量记录接口。
type UsageRecorder interface {
	Record(r *model.UsageRecord)
}

// ProxyHandler 代理 API 处理器。
type ProxyHandler struct {
	mockURL      string
	httpClient   *http.Client
	streamClient *http.Client
	rateLimiter  RateLimiter
	usageWriter  UsageRecorder
}

// NewProxyHandler 创建代理 handler。
func NewProxyHandler(mockURL string, rateLimiter RateLimiter, usageWriter UsageRecorder) *ProxyHandler {
	return &ProxyHandler{
		mockURL: mockURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		streamClient: &http.Client{
			Timeout: 0, // 流式无超时
		},
		rateLimiter: rateLimiter,
		usageWriter: usageWriter,
	}
}

// ChatCompletions POST /v1/chat/completions
func (h *ProxyHandler) ChatCompletions(c *gin.Context) {
	key := h.getKey(c)
	if key == nil || !h.checkEndpoint(key, "/v1/chat/completions", c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeError(c, http.StatusBadRequest, "request body too large or unreadable", "invalid_request_error")
		return
	}

	var req openai.ChatCompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body", "invalid_request_error")
		return
	}

	if !h.checkModel(key, req.Model, c) || !h.checkRateLimit(key, c) {
		return
	}

	// 流式请求走 SSE 代理通道
	if req.Stream != nil && *req.Stream {
		h.proxyStream(body, req.Model, key, c)
		return
	}

	resp, statusCode, latencyMs, err := h.doProxy("/v1/chat/completions", body, req.Model, key, c)
	if err != nil {
		return
	}

	var usage openai.Usage
	var chatResp openai.ChatCompletionResponse
	if json.Unmarshal(resp, &chatResp) == nil {
		usage = chatResp.Usage
	}
	h.recordAndRespond(key, req.Model, resp, statusCode, latencyMs, usage, c)
}

// Embeddings POST /v1/embeddings
func (h *ProxyHandler) Embeddings(c *gin.Context) {
	h.proxyModelEndpoint("/v1/embeddings", c,
		func(body []byte) (string, error) {
			var req openai.EmbeddingRequest
			if err := json.Unmarshal(body, &req); err != nil {
				return "", err
			}
			return req.Model, nil
		},
		func(resp []byte) openai.Usage {
			var embResp struct {
				Usage openai.Usage `json:"usage"`
			}
			if json.Unmarshal(resp, &embResp) == nil {
				return embResp.Usage
			}
			return openai.Usage{}
		},
	)
}

// proxyModelEndpoint 代理模型请求的通用流程：读 body → 解析 model → scope 校验 → 转发 → 记录/返回。
func (h *ProxyHandler) proxyModelEndpoint(endpoint string, c *gin.Context,
	parseModel func([]byte) (string, error),
	parseUsage func([]byte) openai.Usage) {

	key := h.getKey(c)
	if key == nil || !h.checkEndpoint(key, endpoint, c) {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 10<<20) // 10MB 限制
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		writeError(c, http.StatusBadRequest, "request body too large or unreadable", "invalid_request_error")
		return
	}

	modelName, err := parseModel(body)
	if err != nil {
		writeError(c, http.StatusBadRequest, "invalid request body", "invalid_request_error")
		return
	}

	if !h.checkModel(key, modelName, c) || !h.checkRateLimit(key, c) {
		return
	}

	resp, statusCode, latencyMs, err := h.doProxy(endpoint, body, modelName, key, c)
	if err != nil {
		return
	}

	usage := parseUsage(resp)
	h.recordAndRespond(key, modelName, resp, statusCode, latencyMs, usage, c)
}

// doProxy 执行代理转发逻辑，返回响应体、状态码和延迟。
// 若上游错误，直接向客户端写入错误响应并返回 error。
func (h *ProxyHandler) doProxy(endpoint string, body []byte, modelName string,
	key *model.APIKey, c *gin.Context) ([]byte, int, int64, error) {

	start := time.Now()
	resp, statusCode, err := h.forward(endpoint, body)
	latencyMs := time.Since(start).Milliseconds()

	if err != nil {
		slog.Error("upstream error", "endpoint", endpoint, "error", err)
		if os.IsTimeout(err) {
			writeError(c, http.StatusGatewayTimeout, "upstream timeout", "timeout")
		} else {
			writeError(c, http.StatusBadGateway, "upstream error", "upstream_error")
		}
		h.recordUsage(key, modelName, 0, 0, 0, statusCode, int(latencyMs), c)
		return nil, statusCode, latencyMs, err
	}

	return resp, statusCode, latencyMs, nil
}

// proxyStream 处理 SSE 流式请求：转发到下游，逐 chunk 透传给客户端，流结束后记录用量。
func (h *ProxyHandler) proxyStream(body []byte, modelName string, key *model.APIKey, c *gin.Context) {
	url := h.mockURL + "/v1/chat/completions"
	req, err := http.NewRequestWithContext(c.Request.Context(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		writeError(c, http.StatusInternalServerError, "create request failed", "server_error")
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")

	start := time.Now()
	resp, err := h.streamClient.Do(req)
	if err != nil {
		slog.Error("upstream stream error", "error", err)
		if os.IsTimeout(err) {
			writeError(c, http.StatusGatewayTimeout, "upstream timeout", "timeout")
		} else {
			writeError(c, http.StatusBadGateway, "upstream error", "upstream_error")
		}
		h.recordUsage(key, modelName, 0, 0, 0, 502, int(time.Since(start).Milliseconds()), c)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 50<<20))
		c.Data(resp.StatusCode, "application/json", bodyBytes)
		h.recordUsage(key, modelName, 0, 0, 0, resp.StatusCode, int(time.Since(start).Milliseconds()), c)
		return
	}

	// 设置 SSE 响应头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Request-ID", c.GetString("request_id"))
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeError(c, http.StatusInternalServerError, "streaming not supported", "server_error")
		return
	}

	var totalUsage openai.Usage
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Text()
		if _, err := fmt.Fprintf(c.Writer, "%s\n", line); err != nil {
			slog.Warn("stream write error", "error", err)
			break
		}
		flusher.Flush()

		// 从 chunk 中提取 usage（最后一个 chunk 通常携带 usage）
		if strings.HasPrefix(line, "data: ") && !strings.Contains(line, "[DONE]") {
			data := strings.TrimPrefix(line, "data: ")
			if data == "" {
				continue
			}
			var chunk openai.ChatCompletionChunk
			if json.Unmarshal([]byte(data), &chunk) == nil && chunk.Usage != nil {
				totalUsage = *chunk.Usage
			}
		}
	}

	if err := scanner.Err(); err != nil {
		slog.Warn("stream read error", "error", err)
	}

	latencyMs := int(time.Since(start).Milliseconds())
	h.recordUsage(key, modelName,
		totalUsage.PromptTokens,
		totalUsage.CompletionTokens,
		totalUsage.TotalTokens,
		200, latencyMs, c)
}

// recordAndRespond 记录用量并返回响应给客户端。
func (h *ProxyHandler) recordAndRespond(key *model.APIKey, modelName string,
	resp []byte, statusCode int, latencyMs int64, usage openai.Usage, c *gin.Context) {

	if statusCode >= 400 {
		h.recordUsage(key, modelName, 0, 0, 0, statusCode, int(latencyMs), c)
		c.Data(statusCode, "application/json", resp)
		return
	}

	h.recordUsage(key, modelName,
		usage.PromptTokens,
		usage.CompletionTokens,
		usage.TotalTokens,
		statusCode, int(latencyMs), c)

	c.Data(statusCode, "application/json", resp)
}

// Models GET /v1/models
func (h *ProxyHandler) Models(c *gin.Context) {
	key := h.getKey(c)

	if !h.checkEndpoint(key, "/v1/models", c) {
		return
	}

	if !h.checkRateLimit(key, c) {
		return
	}

	// 设置 X-Request-ID 响应头
	requestID := c.GetString("request_id")
	if requestID == "" {
		requestID = uuid.NewString()
	}
	c.Header("X-Request-ID", requestID)

	resp, statusCode, err := h.forward("/v1/models", nil)
	if err != nil {
		slog.Error("upstream error", "endpoint", "/v1/models", "error", err)
		writeError(c, http.StatusBadGateway, "upstream error", "upstream_error")
		return
	}

	c.Data(statusCode, "application/json", resp)
}

// forward 转发请求到 Mock Provider。
func (h *ProxyHandler) forward(path string, body []byte) ([]byte, int, error) {
	url := h.mockURL + path
	method := http.MethodGet
	if body != nil {
		method = http.MethodPost
	}

	req, err := http.NewRequest(method, url, bytes.NewReader(body))
	if err != nil {
		return nil, 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := h.httpClient.Do(req)
	if err != nil {
		return nil, 502, fmt.Errorf("upstream error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 50<<20)) // 50MB 限制
	if err != nil {
		return nil, resp.StatusCode, fmt.Errorf("read response: %w", err)
	}

	return respBody, resp.StatusCode, nil
}

// getKey 从 context 获取 API Key。
func (h *ProxyHandler) getKey(c *gin.Context) *model.APIKey {
	v, _ := c.Get("api_key")
	if v == nil {
		return nil
	}
	return v.(*model.APIKey)
}

// checkEndpoint 校验 endpoint 是否在 allowed_endpoints 中。
func (h *ProxyHandler) checkEndpoint(key *model.APIKey, endpoint string, c *gin.Context) bool {
	if key == nil {
		return false
	}
	if len(key.Scopes.AllowedEndpoints) == 0 {
		return true
	}
	for _, e := range key.Scopes.AllowedEndpoints {
		if e == endpoint {
			return true
		}
	}
	writeError(c, http.StatusForbidden, "endpoint not allowed", "forbidden")
	return false
}

// checkModel 校验 model 是否在 allowed_models 中。
func (h *ProxyHandler) checkModel(key *model.APIKey, modelName string, c *gin.Context) bool {
	if key == nil {
		return false
	}
	if len(key.Scopes.AllowedModels) == 0 {
		return true
	}
	for _, m := range key.Scopes.AllowedModels {
		if m == modelName {
			return true
		}
	}
	writeError(c, http.StatusForbidden, "model not allowed", "forbidden")
	return false
}

// checkRateLimit 检查 rate limit。
func (h *ProxyHandler) checkRateLimit(key *model.APIKey, c *gin.Context) bool {
	if key == nil {
		return false
	}
	if !h.rateLimiter.Allow(key.ID, key.Scopes.RateLimitRPM) {
		writeError(c, http.StatusTooManyRequests, "rate limit exceeded", "rate_limit")
		return false
	}
	return true
}

// recordUsage 异步记录用量。
func (h *ProxyHandler) recordUsage(key *model.APIKey, modelName string,
	promptTokens, completionTokens, totalTokens, statusCode, latencyMs int,
	c *gin.Context) {

	requestID := c.GetString("request_id")
	if requestID == "" {
		requestID = uuid.NewString()
	}
	c.Header("X-Request-ID", requestID)

	h.usageWriter.Record(&model.UsageRecord{
		ID:               uuid.NewString(),
		TenantID:         key.TenantID,
		KeyID:            key.ID,
		Model:            modelName,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		TotalTokens:      totalTokens,
		RequestID:        requestID,
		StatusCode:       statusCode,
		LatencyMs:        latencyMs,
		CreatedAt:        time.Now(),
	})
}

func writeError(c *gin.Context, code int, message, errType string) {
	c.AbortWithStatusJSON(code, gin.H{
		"error": gin.H{"message": message, "type": errType},
	})
}
