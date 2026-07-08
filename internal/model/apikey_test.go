package model

import (
	"encoding/json"
	"testing"
)

func TestScopes_Scan(t *testing.T) {
	tests := []struct {
		name    string
		input   interface{}
		want    Scopes
		wantErr bool
	}{
		{
			name:  "正常 JSON",
			input: []byte(`{"allowed_models":["gpt-4"],"allowed_endpoints":[],"rate_limit_rpm":100}`),
			want: Scopes{
				AllowedModels: []string{"gpt-4"},
				RateLimitRPM:  100,
			},
		},
		{
			name:  "空 JSON 对象",
			input: []byte(`{}`),
			want:  Scopes{},
		},
		{
			name:  "完整 scope",
			input: []byte(`{"allowed_models":["gpt-4","gpt-3.5-turbo"],"allowed_endpoints":["/v1/chat/completions"],"rate_limit_rpm":60}`),
			want: Scopes{
				AllowedModels:    []string{"gpt-4", "gpt-3.5-turbo"},
				AllowedEndpoints: []string{"/v1/chat/completions"},
				RateLimitRPM:     60,
			},
		},
		{
			name:    "无效类型",
			input:   "not bytes",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var s Scopes
			err := s.Scan(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("期望错误但未返回")
				}
				return
			}
			if err != nil {
				t.Errorf("不期望错误: %v", err)
				return
			}
			if !equalStringSlices(s.AllowedModels, tt.want.AllowedModels) {
				t.Errorf("AllowedModels: got %v, want %v", s.AllowedModels, tt.want.AllowedModels)
			}
			if !equalStringSlices(s.AllowedEndpoints, tt.want.AllowedEndpoints) {
				t.Errorf("AllowedEndpoints: got %v, want %v", s.AllowedEndpoints, tt.want.AllowedEndpoints)
			}
			if s.RateLimitRPM != tt.want.RateLimitRPM {
				t.Errorf("RateLimitRPM: got %d, want %d", s.RateLimitRPM, tt.want.RateLimitRPM)
			}
		})
	}
}

func TestScopes_Value(t *testing.T) {
	tests := []struct {
		name   string
		scopes Scopes
	}{
		{
			name:   "空 scopes",
			scopes: Scopes{},
		},
		{
			name: "完整 scopes",
			scopes: Scopes{
				AllowedModels:    []string{"gpt-4"},
				AllowedEndpoints: []string{"/v1/chat/completions"},
				RateLimitRPM:     100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			val, err := tt.scopes.Value()
			if err != nil {
				t.Errorf("Value() 返回错误: %v", err)
				return
			}

			// 验证可以再 Scan 回去
			var restored Scopes
			if err := restored.Scan(val); err != nil {
				t.Errorf("回读失败: %v", err)
				return
			}
			if !equalStringSlices(restored.AllowedModels, tt.scopes.AllowedModels) {
				t.Error("回读的 AllowedModels 不一致")
			}
			if !equalStringSlices(restored.AllowedEndpoints, tt.scopes.AllowedEndpoints) {
				t.Error("回读的 AllowedEndpoints 不一致")
			}
			if restored.RateLimitRPM != tt.scopes.RateLimitRPM {
				t.Error("回读的 RateLimitRPM 不一致")
			}
		})
	}
}

func TestScopes_JSONRoundTrip(t *testing.T) {
	original := Scopes{
		AllowedModels:    []string{"gpt-4", "gpt-4-turbo"},
		AllowedEndpoints: []string{"/v1/chat/completions", "/v1/embeddings"},
		RateLimitRPM:     200,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored Scopes
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if !equalStringSlices(restored.AllowedModels, original.AllowedModels) {
		t.Error("AllowedModels 不一致")
	}
	if restored.RateLimitRPM != original.RateLimitRPM {
		t.Error("RateLimitRPM 不一致")
	}
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
