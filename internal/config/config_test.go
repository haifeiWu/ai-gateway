package config

import (
	"os"
	"testing"
)

func TestLoad_validToken(t *testing.T) {
	os.Setenv("ADMIN_TOKEN", "my-secure-token")
	defer os.Unsetenv("ADMIN_TOKEN")

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
	os.Setenv("ADMIN_TOKEN", "admin-secret-token")
	defer os.Unsetenv("ADMIN_TOKEN")

	_, err := Load()
	if err == nil {
		t.Error("默认 ADMIN_TOKEN 应被拒绝")
	}
}

func TestLoad_emptyToken_rejected(t *testing.T) {
	os.Unsetenv("ADMIN_TOKEN")

	_, err := Load()
	if err == nil {
		t.Error("空 ADMIN_TOKEN 应被拒绝")
	}
}

func TestLoad_fromEnv(t *testing.T) {
	os.Setenv("ADMIN_TOKEN", "test-token")
	os.Setenv("DB_HOST", "test-db")
	defer func() {
		os.Unsetenv("ADMIN_TOKEN")
		os.Unsetenv("DB_HOST")
	}()

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
