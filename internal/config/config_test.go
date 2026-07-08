package config

import (
	"testing"
)

func TestLoad_validToken(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "my-secure-token")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}

	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"DB_HOST", cfg.DBHost, "localhost"},
		{"DB_PORT", cfg.DBPort, "3306"},
		{"DB_USER", cfg.DBUser, "gateway"},
		{"DB_PASS", cfg.DBPass, "gateway"},
		{"DB_NAME", cfg.DBName, "ai_gateway"},
		{"ADMIN_TOKEN", cfg.AdminToken, "my-secure-token"},
		{"MOCK_PROVIDER_URL", cfg.MockProviderURL, "http://localhost:8081"},
		{"LISTEN_ADDR", cfg.ListenAddr, ":8080"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.got)
			}
		})
	}
}

func TestLoad_tokenValidation(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr bool
	}{
		{
			name:    "默认token被拒绝",
			token:   "admin-secret-token",
			wantErr: true,
		},
		{
			name:    "空token被拒绝",
			token:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.token != "" {
				t.Setenv("ADMIN_TOKEN", tt.token)
			}
			// 不设置 ADMIN_TOKEN 时环境变量为空字符串

			_, err := Load()
			if tt.wantErr && err == nil {
				t.Error("期望返回错误但未返回")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("不期望错误: %v", err)
			}
		})
	}
}

func TestLoad_fromEnv(t *testing.T) {
	tests := []struct {
		name   string
		envKey string
		envVal string
		want   string
	}{
		{
			name:   "DB_HOST覆盖",
			envKey: "DB_HOST",
			envVal: "test-db",
			want:   "test-db",
		},
		{
			name:   "ADMIN_TOKEN覆盖",
			envKey: "ADMIN_TOKEN",
			envVal: "test-token",
			want:   "test-token",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 为所有用例设置必要的环境变量
			t.Setenv("ADMIN_TOKEN", "my-secure-token")
			t.Setenv(tt.envKey, tt.envVal)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load() 失败: %v", err)
			}

			var got string
			switch tt.envKey {
			case "DB_HOST":
				got = cfg.DBHost
			case "ADMIN_TOKEN":
				got = cfg.AdminToken
			}

			if got != tt.want {
				t.Errorf("%s: got %q, want %q", tt.envKey, got, tt.want)
			}
		})
	}
}
