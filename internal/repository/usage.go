package repository

import (
	"fmt"
	"time"

	"github.com/ai-gateway/internal/model"
	"gorm.io/gorm"
)

// UsageStore 用量记录数据访问。
type UsageStore struct {
	db *gorm.DB
}

// NewUsageStore 创建用量存储实例。
func NewUsageStore(db *gorm.DB) *UsageStore {
	return &UsageStore{db: db}
}

// BatchCreate 批量写入用量记录。
func (s *UsageStore) BatchCreate(records []*model.UsageRecord) error {
	if len(records) == 0 {
		return nil
	}
	return s.db.Create(records).Error
}

// UsageQuery 用量查询参数。
type UsageQuery struct {
	TenantID string
	KeyID    string
	Model    string
	Start    time.Time
	End      time.Time
	GroupBy  string // "model" | "day" | "hour" | "tenant" | "" (不分组)
	Page     int
	PageSize int
}

// UsageGroup 分组聚合结果。
type UsageGroup struct {
	Model            string `json:"model,omitempty"`
	TenantID         string `json:"tenant_id,omitempty"`
	Date             string `json:"date,omitempty"`
	Hour             string `json:"hour,omitempty"`
	Requests         int64  `json:"requests"`
	PromptTokens     int64  `json:"prompt_tokens"`
	CompletionTokens int64  `json:"completion_tokens"`
	TotalTokens      int64  `json:"total_tokens"`
}

// UsageSummary 用量汇总。
type UsageSummary struct {
	TotalRequests int64 `json:"total_requests"`
	TotalTokens   int64 `json:"total_tokens"`
}

// UsageResult 用量查询结果。
type UsageResult struct {
	Summary  UsageSummary `json:"summary"`
	Groups   []UsageGroup `json:"groups"`
	Total    int64        `json:"total"`
	Page     int          `json:"page"`
	PageSize int          `json:"page_size"`
}

// Query 查询用量并聚合，支持分页和多种分组维度。
func (s *UsageStore) Query(q UsageQuery) (*UsageResult, error) {
	base := s.db.Model(&model.UsageRecord{})

	if q.TenantID != "" {
		base = base.Where("tenant_id = ?", q.TenantID)
	}
	if q.KeyID != "" {
		base = base.Where("key_id = ?", q.KeyID)
	}
	if q.Model != "" {
		base = base.Where("model = ?", q.Model)
	}
	if !q.Start.IsZero() {
		base = base.Where("created_at >= ?", q.Start)
	}
	if !q.End.IsZero() {
		base = base.Where("created_at <= ?", q.End)
	}

	// 汇总（不受分页影响）
	var summary UsageSummary
	if err := base.Select(
		"COUNT(*) as total_requests, COALESCE(SUM(total_tokens), 0) as total_tokens",
	).Scan(&summary).Error; err != nil {
		return nil, fmt.Errorf("query usage summary: %w", err)
	}

	// 默认分页
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}

	// 分组
	var groups []UsageGroup
	groupByExpr := q.GroupBy

	selectCols := "COUNT(*) as requests, " +
		"COALESCE(SUM(prompt_tokens), 0) as prompt_tokens, " +
		"COALESCE(SUM(completion_tokens), 0) as completion_tokens, " +
		"COALESCE(SUM(total_tokens), 0) as total_tokens"

	switch groupByExpr {
	case "model":
		if err := base.Select("model, "+selectCols).Group("model").Order("total_tokens DESC").
			Scan(&groups).Error; err != nil {
			return nil, fmt.Errorf("query usage groups: %w", err)
		}
	case "day":
		if err := base.Select("DATE(created_at) as date, "+selectCols).Group("DATE(created_at)").
			Order("date DESC").Scan(&groups).Error; err != nil {
			return nil, fmt.Errorf("query usage groups: %w", err)
		}
	case "hour":
		if err := base.Select(
			"DATE_FORMAT(created_at, '%Y-%m-%d %H:00') as hour, "+selectCols,
		).Group("DATE_FORMAT(created_at, '%Y-%m-%d %H:00')").Order("hour DESC").
			Scan(&groups).Error; err != nil {
			return nil, fmt.Errorf("query usage groups: %w", err)
		}
	case "tenant":
		if err := base.Select("tenant_id, "+selectCols).Group("tenant_id").Order("total_tokens DESC").
			Scan(&groups).Error; err != nil {
			return nil, fmt.Errorf("query usage groups: %w", err)
		}
	}

	// 有分组时统计分组数作为 total
	total := int64(len(groups))
	if groupByExpr == "" {
		total = summary.TotalRequests
	}

	// 分页切片（仅在有分组时生效）
	if groupByExpr != "" && len(groups) > 0 {
		start := (page - 1) * pageSize
		if start >= len(groups) {
			groups = nil
		} else {
			end := start + pageSize
			if end > len(groups) {
				end = len(groups)
			}
			groups = groups[start:end]
		}
	}

	return &UsageResult{
		Summary:  summary,
		Groups:   groups,
		Total:    total,
		Page:     page,
		PageSize: pageSize,
	}, nil
}
