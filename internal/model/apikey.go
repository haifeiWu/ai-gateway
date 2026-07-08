package model

import (
	"database/sql/driver"
	"encoding/json"
	"errors"
	"time"
)

// KeyStatus API Key 状态。
type KeyStatus string

const (
	KeyStatusActive   KeyStatus = "active"
	KeyStatusDisabled KeyStatus = "disabled"
)

// Scopes API Key 的权限范围配置。
type Scopes struct {
	AllowedModels    []string `json:"allowed_models"`
	AllowedEndpoints []string `json:"allowed_endpoints"`
	RateLimitRPM     int      `json:"rate_limit_rpm"`
}

// Scan 实现 sql.Scanner 接口，从 DB 读取 JSON。
func (s *Scopes) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return errors.New("invalid scopes type")
	}
	if len(bytes) == 0 {
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// Value 实现 driver.Valuer 接口，写入 DB 的 JSON。
func (s Scopes) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// APIKey API Key 数据模型。
type APIKey struct {
	ID        string    `json:"id" gorm:"type:char(36);primaryKey"`
	TenantID  string    `json:"tenant_id" gorm:"type:char(36);not null;index"`
	KeyHash   string    `json:"-" gorm:"type:char(64);uniqueIndex;not null"`
	KeyPrefix string    `json:"key_prefix" gorm:"type:varchar(20);not null"`
	Name      string    `json:"name" gorm:"type:varchar(100);not null"`
	Scopes    Scopes    `json:"scopes" gorm:"type:json"`
	Status    KeyStatus `json:"status" gorm:"type:varchar(20);not null;default:active"`
	ExpiresAt *time.Time `json:"expires_at" gorm:"type:datetime;null"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`

	// 关联租户信息（按需由调用方通过 TenantID 查询填充）。
	Tenant *Tenant `json:"tenant,omitempty" gorm:"-"`
}
