package model

import "time"

// UsageRecord 用量记录。
type UsageRecord struct {
	ID               string    `json:"id" gorm:"type:char(36);primaryKey"`
	TenantID         string    `json:"tenant_id" gorm:"type:char(36);not null;index:idx_usage_tenant_time"`
	KeyID            string    `json:"key_id" gorm:"type:char(36);not null;index:idx_usage_key_time"`
	Model            string    `json:"model" gorm:"type:varchar(50);not null;index:idx_usage_model_time"`
	PromptTokens     int       `json:"prompt_tokens"`
	CompletionTokens int       `json:"completion_tokens"`
	TotalTokens      int       `json:"total_tokens"`
	RequestID        string    `json:"request_id" gorm:"type:varchar(64)"`
	StatusCode       int       `json:"status_code"`
	LatencyMs        int       `json:"latency_ms"`
	CreatedAt        time.Time `json:"created_at" gorm:"index:idx_usage_tenant_time,priority:2;index:idx_usage_key_time,priority:2;index:idx_usage_model_time,priority:2"`
}
