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

func TestLoad_defaultToken_rejected(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "admin-secret-token")

	_, err := Load()
	if err == nil {
		t.Error("默认 ADMIN_TOKEN 应被拒绝")
	}
}

func TestLoad_emptyToken_rejected(t *testing.T) {
	// t.Setenv 不设置 ADMIN_TOKEN，环境变量为空

	_, err := Load()
	if err == nil {
		t.Error("空 ADMIN_TOKEN 应被拒绝")
	}
}

func TestLoad_fromEnv(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "test-token")
	t.Setenv("DB_HOST", "test-db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() 失败: %v", err)
	}

	if cfg.DBHost != "test-db" {
		t.Errorf("DB_HOST: expected 'test-db', got %q", cfg.DBHost)
	}
	if cfg.AdminToken != "test-token" {
		t.Errorf("ADMIN_TOKEN: expected 'test-token', got %q", cfg.AdminToken)
	}
}
