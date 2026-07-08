package model

import "time"

// TenantStatus 租户状态。
type TenantStatus string

const (
	TenantStatusActive   TenantStatus = "active"
	TenantStatusDisabled TenantStatus = "disabled"
)

// Tenant 租户数据模型。
type Tenant struct {
	ID        string       `json:"id" gorm:"type:char(36);primaryKey"`
	Name      string       `json:"name" gorm:"type:varchar(100);not null"`
	Status    TenantStatus `json:"status" gorm:"type:varchar(20);not null;default:active"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}
