package config

import (
	"fmt"
	"log/slog"
	"os"
)

// Config 配置结构体，从环境变量加载。
type Config struct {
	DBHost          string
	DBPort          string
	DBUser          string
	DBPass          string
	DBName          string
	AdminToken      string
	MockProviderURL string
	ListenAddr      string
}

// Load 从环境变量加载配置。AdminToken 为必填项，拒绝默认值。
func Load() (*Config, error) {
	token := os.Getenv("ADMIN_TOKEN")
	if token == "" || token == "admin-secret-token" {
		slog.Error("ADMIN_TOKEN 未设置或使用了默认值，必须设置非默认值")
		return nil, fmt.Errorf("ADMIN_TOKEN must be set and must not be the default value")
	}

	return &Config{
		DBHost:          getEnv("DB_HOST", "localhost"),
		DBPort:          getEnv("DB_PORT", "3306"),
		DBUser:          getEnv("DB_USER", "gateway"),
		DBPass:          getEnv("DB_PASS", "gateway"),
		DBName:          getEnv("DB_NAME", "ai_gateway"),
		AdminToken:      token,
		MockProviderURL: getEnv("MOCK_PROVIDER_URL", "http://localhost:8081"),
		ListenAddr:      getEnv("LISTEN_ADDR", ":8080"),
	}, nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}
