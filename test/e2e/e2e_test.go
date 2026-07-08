//go:build e2e

package e2e

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ai-gateway/internal/model"
)

func TestHealth(t *testing.T) {
	r := noAuthReq("GET", "/health", nil)
	assertStatus(t, "health", r.Status, 200)
	if r.mapBody()["status"] != "ok" {
		t.Errorf("health: status = %v, want ok", r.mapBody()["status"])
	}
}

func TestAdminAuth(t *testing.T) {
	t.Run("missing_header", func(t *testing.T) {
		r := noAuthReq("GET", "/admin/v1/tenants", nil)
		assertStatus(t, "no auth", r.Status, 401)
		assertErrorType(t, "no auth", r.Body, "auth_error")
	})

	t.Run("wrong_token", func(t *testing.T) {
		r := doReq(gatewayURL, "wrong-token", "GET", "/admin/v1/tenants", nil)
		assertStatus(t, "wrong token", r.Status, 401)
		assertErrorType(t, "wrong token", r.Body, "auth_error")
	})

	t.Run("correct_token", func(t *testing.T) {
		r := adminReq("GET", "/admin/v1/tenants", nil)
		assertStatus(t, "correct token", r.Status, 200)
	})
}

func TestTenantManagement(t *testing.T) {
	cleanDB()

	t.Run("create", func(t *testing.T) {
		r := adminReq("POST", "/admin/v1/tenants", map[string]string{"name": "Tenant A"})
		assertStatus(t, "create", r.Status, 201)
		m := r.mapBody()
		if m["name"] != "Tenant A" {
			t.Errorf("create: name = %v, want Tenant A", m["name"])
		}
		if m["status"] != "active" {
			t.Errorf("create: status = %v, want active", m["status"])
		}
		if m["id"] == nil {
			t.Error("create: id is nil")
		}
	})

	t.Run("create_missing_name", func(t *testing.T) {
		r := adminReq("POST", "/admin/v1/tenants", map[string]string{})
		assertStatus(t, "missing name", r.Status, 400)
		assertErrorType(t, "missing name", r.Body, "invalid_request_error")
	})

	t.Run("list", func(t *testing.T) {
		cleanDB()
		createTenant("List A")
		createTenant("List B")
		r := adminReq("GET", "/admin/v1/tenants", nil)
		assertStatus(t, "list", r.Status, 200)
		arr := r.arrBody()
		if len(arr) != 2 {
			t.Errorf("list: count = %d, want 2", len(arr))
		}
	})

	t.Run("get_by_id", func(t *testing.T) {
		id := createTenant("Get Test")
		r := adminReq("GET", "/admin/v1/tenants/"+id, nil)
		assertStatus(t, "get", r.Status, 200)
		if r.mapBody()["id"] != id {
			t.Errorf("get: id = %v, want %s", r.mapBody()["id"], id)
		}
	})

	t.Run("get_not_found", func(t *testing.T) {
		r := adminReq("GET", "/admin/v1/tenants/nonexistent-id", nil)
		assertStatus(t, "not found", r.Status, 404)
		assertErrorType(t, "not found", r.Body, "not_found")
	})
}

func TestKeyManagement(t *testing.T) {
	cleanDB()
	tid := createTenant("Key Test Tenant")

	t.Run("create_unrestricted", func(t *testing.T) {
		r := adminReq("POST", "/admin/v1/tenants/"+tid+"/keys", map[string]interface{}{
			"name":   "Full Access",
			"scopes": model.Scopes{},
		})
		assertStatus(t, "create", r.Status, 201)
		m := r.mapBody()
		key, _ := m["key"].(string)
		if len(key) != 40 || key[:7] != "sk-agw-" {
			t.Errorf("create: key = %q, want sk-agw- prefix, 40 chars", key)
		}
		prefix, _ := m["key_prefix"].(string)
		if len(prefix) < 4 || prefix[len(prefix)-4:] != "****" {
			t.Errorf("create: key_prefix = %q, want **** suffix", prefix)
		}
		if m["status"] != "active" {
			t.Errorf("create: status = %v, want active", m["status"])
		}
	})

	t.Run("create_with_scopes", func(t *testing.T) {
		r := adminReq("POST", "/admin/v1/tenants/"+tid+"/keys", map[string]interface{}{
			"name": "Limited",
			"scopes": map[string]interface{}{
				"allowed_models":    []string{"gpt-4"},
				"allowed_endpoints": []string{"/v1/chat/completions"},
				"rate_limit_rpm":    60,
			},
		})
		assertStatus(t, "create scoped", r.Status, 201)
		scopes, _ := r.mapBody()["scopes"].(map[string]interface{})
		if scopes == nil {
			t.Fatal("create scoped: scopes is nil")
		}
		rpm, _ := scopes["rate_limit_rpm"].(float64)
		if int(rpm) != 60 {
			t.Errorf("create scoped: rate_limit_rpm = %v, want 60", scopes["rate_limit_rpm"])
		}
	})

	t.Run("create_nonexistent_tenant", func(t *testing.T) {
		r := adminReq("POST", "/admin/v1/tenants/nonexistent/keys", map[string]interface{}{
			"name": "Orphan",
		})
		assertStatus(t, "nonexistent tenant", r.Status, 400)
	})

	t.Run("list", func(t *testing.T) {
		createKey(tid, "List Key 1", model.Scopes{})
		createKey(tid, "List Key 2", model.Scopes{})
		r := adminReq("GET", "/admin/v1/tenants/"+tid+"/keys", nil)
		assertStatus(t, "list keys", r.Status, 200)
		if len(r.arrBody()) < 2 {
			t.Errorf("list keys: count = %d, want >= 2", len(r.arrBody()))
		}
	})

	t.Run("disable", func(t *testing.T) {
		keyID, _ := createKey(tid, "Disable Test", model.Scopes{})
		r := adminReq("PATCH", "/admin/v1/keys/"+keyID, map[string]interface{}{"status": "disabled"})
		assertStatus(t, "disable", r.Status, 200)
		if r.mapBody()["status"] != "disabled" {
			t.Errorf("disable: status = %v, want disabled", r.mapBody()["status"])
		}
	})

	t.Run("enable", func(t *testing.T) {
		keyID, _ := createKey(tid, "Enable Test", model.Scopes{})
		adminReq("PATCH", "/admin/v1/keys/"+keyID, map[string]interface{}{"status": "disabled"})
		r := adminReq("PATCH", "/admin/v1/keys/"+keyID, map[string]interface{}{"status": "active"})
		assertStatus(t, "enable", r.Status, 200)
		if r.mapBody()["status"] != "active" {
			t.Errorf("enable: status = %v, want active", r.mapBody()["status"])
		}
	})

	t.Run("delete", func(t *testing.T) {
		keyID, _ := createKey(tid, "Delete Test", model.Scopes{})
		r := adminReq("DELETE", "/admin/v1/keys/"+keyID, nil)
		assertStatus(t, "delete", r.Status, 200)
		getR := adminReq("GET", "/admin/v1/keys/"+keyID, nil)
		assertStatus(t, "get after delete", getR.Status, 404)
	})
}

func TestProxyAuth(t *testing.T) {
	cleanDB()
	tid := createTenant("Proxy Auth Tenant")

	t.Run("missing_auth", func(t *testing.T) {
		r := noAuthReq("POST", "/v1/chat/completions", map[string]interface{}{
			"model": "gpt-4", "messages": []map[string]string{{"role": "user", "content": "hi"}},
		})
		assertStatus(t, "no auth", r.Status, 401)
		assertErrorType(t, "no auth", r.Body, "auth_error")
	})

	t.Run("invalid_key", func(t *testing.T) {
		r := proxyReq("sk-agw-invalidkey000000000000000000000", "POST", "/v1/chat/completions", nil)
		assertStatus(t, "invalid key", r.Status, 401)
		assertErrorType(t, "invalid key", r.Body, "auth_error")
	})

	t.Run("disabled_key", func(t *testing.T) {
		keyID, apiKey := createKey(tid, "Disabled Key", model.Scopes{})
		adminReq("PATCH", "/admin/v1/keys/"+keyID, map[string]interface{}{"status": "disabled"})
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", nil)
		assertStatus(t, "disabled key", r.Status, 403)
		assertErrorType(t, "disabled key", r.Body, "forbidden")
	})

	t.Run("expired_key", func(t *testing.T) {
		r := adminReq("POST", "/admin/v1/tenants/"+tid+"/keys", map[string]interface{}{
			"name":       "Expired Key",
			"scopes":     model.Scopes{},
			"expires_at": "2020-01-01T00:00:00Z",
		})
		apiKey := r.mapBody()["key"].(string)
		r2 := proxyReq(apiKey, "POST", "/v1/chat/completions", nil)
		assertStatus(t, "expired key", r2.Status, 401)
		assertErrorType(t, "expired key", r2.Body, "auth_error")
		errObj, _ := r2.mapBody()["error"].(map[string]interface{})
		if errObj["message"] != "API key expired" {
			t.Errorf("expired key: message = %v, want 'API key expired'", errObj["message"])
		}
	})

	t.Run("disabled_tenant", func(t *testing.T) {
		tid2 := createTenant("Disabled Tenant")
		_, apiKey := createKey(tid2, "Test Key", model.Scopes{})
		db.Model(&model.Tenant{}).Where("id = ?", tid2).Update("status", "disabled")
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", nil)
		assertStatus(t, "disabled tenant", r.Status, 403)
		assertErrorType(t, "disabled tenant", r.Body, "forbidden")
	})
}

func TestChatCompletions(t *testing.T) {
	cleanDB()
	tid := createTenant("Chat Tenant")
	_, apiKey := createKey(tid, "Chat Key", model.Scopes{})
	chatBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "Hello"}},
	}

	t.Run("success", func(t *testing.T) {
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
		assertStatus(t, "chat", r.Status, 200)
		m := r.mapBody()
		if m["object"] != "chat.completion" {
			t.Errorf("chat: object = %v, want chat.completion", m["object"])
		}
		usage, _ := m["usage"].(map[string]interface{})
		if usage == nil {
			t.Fatal("chat: usage is nil")
		}
		if int(usage["total_tokens"].(float64)) != 35 {
			t.Errorf("chat: total_tokens = %v, want 35", usage["total_tokens"])
		}
	})

	t.Run("invalid_body", func(t *testing.T) {
		r := doReqRaw(gatewayURL, apiKey, "POST", "/v1/chat/completions", `{"invalid":"json"}`)
		assertStatus(t, "invalid body", r.Status, 400)
		assertErrorType(t, "invalid body", r.Body, "invalid_request_error")
	})

	t.Run("request_id_header", func(t *testing.T) {
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
		assertStatus(t, "request id", r.Status, 200)
		rid := r.Header.Get("X-Request-ID")
		if rid == "" {
			t.Error("request id: X-Request-ID header is empty")
		}
		if len(rid) != 36 {
			t.Errorf("request id: X-Request-ID = %q, want 36-char UUID", rid)
		}
	})
}

func TestChatCompletionsStreaming(t *testing.T) {
	cleanDB()
	tid := createTenant("Stream Tenant")
	_, apiKey := createKey(tid, "Stream Key", model.Scopes{})

	t.Run("sse_stream", func(t *testing.T) {
		streamBody := map[string]interface{}{
			"model":    "gpt-4",
			"messages": []map[string]string{{"role": "user", "content": "Hello"}},
			"stream":   true,
		}
		r := doReqRaw(gatewayURL, apiKey, "POST", "/v1/chat/completions", toJSON(streamBody))
		assertStatus(t, "stream", r.Status, 200)

		ct := r.Header.Get("Content-Type")
		if ct == "" || !contains(ct, "text/event-stream") {
			t.Errorf("stream: Content-Type = %q, want text/event-stream", ct)
		}

		bodyStr, _ := r.Body.(string)
		if bodyStr == "" {
			t.Fatal("stream: response body is empty")
		}
		if !strings.Contains(bodyStr, "data: ") {
			t.Error("stream: body should contain SSE data: prefix")
		}
		if !strings.Contains(bodyStr, "[DONE]") {
			t.Error("stream: body should contain [DONE] marker")
		}
	})

	t.Run("non_stream_unchanged", func(t *testing.T) {
		nonStreamBody := map[string]interface{}{
			"model":    "gpt-4",
			"messages": []map[string]string{{"role": "user", "content": "Hello"}},
		}
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", nonStreamBody)
		assertStatus(t, "non-stream", r.Status, 200)
		if r.mapBody()["object"] != "chat.completion" {
			t.Errorf("non-stream: object = %v, want chat.completion", r.mapBody()["object"])
		}
	})
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func toJSON(v interface{}) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestEmbeddings(t *testing.T) {
	cleanDB()
	tid := createTenant("Embed Tenant")
	_, apiKey := createKey(tid, "Embed Key", model.Scopes{})

	t.Run("success", func(t *testing.T) {
		r := proxyReq(apiKey, "POST", "/v1/embeddings", map[string]interface{}{
			"model": "text-embedding-ada-002",
			"input": "hello world",
		})
		assertStatus(t, "embed", r.Status, 200)
		m := r.mapBody()
		if m["object"] != "list" {
			t.Errorf("embed: object = %v, want list", m["object"])
		}
		usage, _ := m["usage"].(map[string]interface{})
		if int(usage["total_tokens"].(float64)) != 8 {
			t.Errorf("embed: total_tokens = %v, want 8", usage["total_tokens"])
		}
	})

	t.Run("invalid_body", func(t *testing.T) {
		r := doReqRaw(gatewayURL, apiKey, "POST", "/v1/embeddings", `{"bad":"json"}`)
		assertStatus(t, "invalid body", r.Status, 400)
	})
}

func TestModels(t *testing.T) {
	cleanDB()
	tid := createTenant("Models Tenant")
	_, apiKey := createKey(tid, "Models Key", model.Scopes{})

	t.Run("success", func(t *testing.T) {
		r := proxyReq(apiKey, "GET", "/v1/models", nil)
		assertStatus(t, "models", r.Status, 200)
		m := r.mapBody()
		if m["object"] != "list" {
			t.Errorf("models: object = %v, want list", m["object"])
		}
		data, ok := m["data"].([]interface{})
		if !ok {
			t.Fatal("models: data is not array")
		}
		if len(data) != 4 {
			t.Errorf("models: data length = %d, want 4", len(data))
		}
	})

	t.Run("no_auth", func(t *testing.T) {
		r := noAuthReq("GET", "/v1/models", nil)
		assertStatus(t, "no auth", r.Status, 401)
	})
}

func TestScopeEnforcement(t *testing.T) {
	cleanDB()
	tid := createTenant("Scope Tenant")
	chatBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}

	t.Run("allowed_model", func(t *testing.T) {
		_, apiKey := createKey(tid, "Model OK", model.Scopes{AllowedModels: []string{"gpt-4"}})
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
		assertStatus(t, "allowed model", r.Status, 200)
	})

	t.Run("disallowed_model", func(t *testing.T) {
		_, apiKey := createKey(tid, "Model Denied", model.Scopes{AllowedModels: []string{"gpt-4"}})
		body := map[string]interface{}{
			"model":    "gpt-3.5-turbo",
			"messages": []map[string]string{{"role": "user", "content": "hi"}},
		}
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", body)
		assertStatus(t, "disallowed model", r.Status, 403)
		assertErrorType(t, "disallowed model", r.Body, "forbidden")
	})

	t.Run("allowed_endpoint", func(t *testing.T) {
		_, apiKey := createKey(tid, "Endpoint OK", model.Scopes{AllowedEndpoints: []string{"/v1/chat/completions"}})
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
		assertStatus(t, "allowed endpoint", r.Status, 200)
	})

	t.Run("disallowed_endpoint", func(t *testing.T) {
		_, apiKey := createKey(tid, "Endpoint Denied", model.Scopes{AllowedEndpoints: []string{"/v1/chat/completions"}})
		r := proxyReq(apiKey, "POST", "/v1/embeddings", map[string]interface{}{
			"model": "text-embedding-ada-002", "input": "test",
		})
		assertStatus(t, "disallowed endpoint", r.Status, 403)
		assertErrorType(t, "disallowed endpoint", r.Body, "forbidden")
	})

	t.Run("no_model_restriction", func(t *testing.T) {
		_, apiKey := createKey(tid, "No Model Restriction", model.Scopes{})
		body := map[string]interface{}{
			"model":    "gpt-4-turbo",
			"messages": []map[string]string{{"role": "user", "content": "hi"}},
		}
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", body)
		assertStatus(t, "no model restriction", r.Status, 200)
	})

	t.Run("no_endpoint_restriction", func(t *testing.T) {
		_, apiKey := createKey(tid, "No Endpoint Restriction", model.Scopes{})
		r := proxyReq(apiKey, "GET", "/v1/models", nil)
		assertStatus(t, "no endpoint restriction", r.Status, 200)
	})
}

func TestRateLimit(t *testing.T) {
	cleanDB()
	tid := createTenant("RateLimit Tenant")
	chatBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}

	t.Run("within_limit", func(t *testing.T) {
		_, apiKey := createKey(tid, "Within Limit", model.Scopes{RateLimitRPM: 10})
		for i := 0; i < 10; i++ {
			r := proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
			if r.Status != 200 {
				t.Errorf("within limit [%d]: status = %d, want 200", i, r.Status)
				break
			}
		}
	})

	t.Run("exceed_limit", func(t *testing.T) {
		_, apiKey := createKey(tid, "Exceed Limit", model.Scopes{RateLimitRPM: 5})
		for i := 0; i < 5; i++ {
			proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
		}
		r := proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
		assertStatus(t, "exceed limit", r.Status, 429)
		assertErrorType(t, "exceed limit", r.Body, "rate_limit")
	})
}

func TestUsageTracking(t *testing.T) {
	cleanDB()
	tid := createTenant("Usage Tenant")
	_, apiKey := createKey(tid, "Usage Key", model.Scopes{})
	chatBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}

	proxyReq(apiKey, "POST", "/v1/chat/completions", chatBody)
	waitForUsage()

	t.Run("recorded", func(t *testing.T) {
		r := adminReq("GET", fmt.Sprintf("/admin/v1/usage?tenant_id=%s", tid), nil)
		assertStatus(t, "usage", r.Status, 200)
		summary, _ := r.mapBody()["summary"].(map[string]interface{})
		if int(summary["total_requests"].(float64)) != 1 {
			t.Errorf("usage: total_requests = %v, want 1", summary["total_requests"])
		}
		if int(summary["total_tokens"].(float64)) != 35 {
			t.Errorf("usage: total_tokens = %v, want 35", summary["total_tokens"])
		}
	})

	t.Run("group_by_model", func(t *testing.T) {
		proxyReq(apiKey, "POST", "/v1/embeddings", map[string]interface{}{
			"model": "text-embedding-ada-002", "input": "test",
		})
		waitForUsage()
		r := adminReq("GET", fmt.Sprintf("/admin/v1/usage?tenant_id=%s&group_by=model", tid), nil)
		assertStatus(t, "group by", r.Status, 200)
		groups := r.mapBody()["groups"].([]interface{})
		if len(groups) < 1 {
			t.Errorf("group by: groups length = %d, want >= 1", len(groups))
		}
	})

	t.Run("filter_by_key", func(t *testing.T) {
		_, key2 := createKey(tid, "Second Key", model.Scopes{})
		proxyReq(key2, "POST", "/v1/chat/completions", chatBody)
		waitForUsage()

		keyID := getKeyID(tid, "Second Key")
		r := adminReq("GET", fmt.Sprintf("/admin/v1/usage?key_id=%s", keyID), nil)
		assertStatus(t, "filter by key", r.Status, 200)
		summary, _ := r.mapBody()["summary"].(map[string]interface{})
		if int(summary["total_requests"].(float64)) < 1 {
			t.Errorf("filter by key: total_requests = %v, want >= 1", summary["total_requests"])
		}
	})

	t.Run("empty_result", func(t *testing.T) {
		waitForUsage()
		cleanDB()
		r := adminReq("GET", "/admin/v1/usage", nil)
		assertStatus(t, "empty", r.Status, 200)
		summary, _ := r.mapBody()["summary"].(map[string]interface{})
		if int(summary["total_requests"].(float64)) != 0 {
			t.Errorf("empty: total_requests = %v, want 0", summary["total_requests"])
		}
	})
}

func TestErrorHandling(t *testing.T) {
	cleanDB()
	tid := createTenant("Error Tenant")
	_, apiKey := createKey(tid, "Error Key", model.Scopes{})
	chatBody := map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "hi"}},
	}

	t.Run("upstream_unavailable_502", func(t *testing.T) {
		badGW, uw := setupGatewayWithMockURL("http://127.0.0.1:1")
		defer badGW.Close()
		defer uw.Shutdown()

		r := doReq(badGW.URL, apiKey, "POST", "/v1/chat/completions", chatBody)
		assertStatus(t, "502", r.Status, 502)
		assertErrorType(t, "502", r.Body, "upstream_error")
	})
}

func TestEndToEndFlow(t *testing.T) {
	cleanDB()

	r := adminReq("POST", "/admin/v1/tenants", map[string]string{"name": "E2E Flow Tenant"})
	assertStatus(t, "create tenant", r.Status, 201)
	tid := r.mapBody()["id"].(string)

	r = adminReq("POST", "/admin/v1/tenants/"+tid+"/keys", map[string]interface{}{
		"name":   "E2E Flow Key",
		"scopes": model.Scopes{},
	})
	assertStatus(t, "create key", r.Status, 201)
	m := r.mapBody()
	apiKey := m["key"].(string)
	keyID := m["id"].(string)

	chatR := proxyReq(apiKey, "POST", "/v1/chat/completions", map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "Hello"}},
	})
	assertStatus(t, "chat completions", chatR.Status, 200)

	embedR := proxyReq(apiKey, "POST", "/v1/embeddings", map[string]interface{}{
		"model": "text-embedding-ada-002",
		"input": "test",
	})
	assertStatus(t, "embeddings", embedR.Status, 200)

	modelsR := proxyReq(apiKey, "GET", "/v1/models", nil)
	assertStatus(t, "models", modelsR.Status, 200)

	waitForUsage()

	usageR := adminReq("GET", fmt.Sprintf("/admin/v1/usage?tenant_id=%s&group_by=model", tid), nil)
	assertStatus(t, "usage query", usageR.Status, 200)
	summary, _ := usageR.mapBody()["summary"].(map[string]interface{})
	if int(summary["total_requests"].(float64)) != 2 {
		t.Errorf("usage: total_requests = %v, want 2", summary["total_requests"])
	}
	groups := usageR.mapBody()["groups"].([]interface{})
	if len(groups) < 1 {
		t.Errorf("usage: groups length = %d, want >= 1", len(groups))
	}

	disableR := adminReq("PATCH", "/admin/v1/keys/"+keyID, map[string]interface{}{"status": "disabled"})
	assertStatus(t, "disable key", disableR.Status, 200)

	disabledR := proxyReq(apiKey, "POST", "/v1/chat/completions", map[string]interface{}{
		"model":    "gpt-4",
		"messages": []map[string]string{{"role": "user", "content": "should fail"}},
	})
	assertStatus(t, "disabled key request", disabledR.Status, 403)

	deleteR := adminReq("DELETE", "/admin/v1/keys/"+keyID, nil)
	assertStatus(t, "delete key", deleteR.Status, 200)

	getR := adminReq("GET", "/admin/v1/keys/"+keyID, nil)
	assertStatus(t, "get deleted key", getR.Status, 404)
}
