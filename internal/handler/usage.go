package handler

import (
	"encoding/csv"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/ai-gateway/internal/repository"
	"github.com/gin-gonic/gin"
)

// UsageQueryer 用量查询接口。
type UsageQueryer interface {
	Query(q repository.UsageQuery) (*repository.UsageResult, error)
}

// UsageHandler 用量查询处理器。
type UsageHandler struct {
	store UsageQueryer
}

// NewUsageHandler 创建用量 handler。
func NewUsageHandler(store UsageQueryer) *UsageHandler {
	return &UsageHandler{store: store}
}

// Query GET /admin/v1/usage
func (h *UsageHandler) Query(c *gin.Context) {
	tenantID := c.Query("tenant_id")
	keyID := c.Query("key_id")
	modelName := c.Query("model")
	groupBy := c.Query("group_by")
	format := c.Query("format") // json(默认) | csv

	// 校验 group_by 参数
	if groupBy != "" && groupBy != "model" && groupBy != "day" && groupBy != "hour" && groupBy != "tenant" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{"message": "invalid group_by value, supported: model, day, hour, tenant", "type": "invalid_request_error"},
		})
		return
	}

	// 分页参数
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}

	var start, end time.Time
	if s := c.Query("start"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": "invalid start time format, use RFC3339", "type": "invalid_request_error"},
			})
			return
		}
		start = t
	}
	if s := c.Query("end"); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{"message": "invalid end time format, use RFC3339", "type": "invalid_request_error"},
			})
			return
		}
		end = t
	}

	result, err := h.store.Query(repository.UsageQuery{
		TenantID: tenantID,
		KeyID:    keyID,
		Model:    modelName,
		Start:    start,
		End:      end,
		GroupBy:  groupBy,
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{"message": "failed to query usage", "type": "server_error"},
		})
		return
	}

	// CSV 导出
	if format == "csv" {
		h.writeCSV(c, result)
		return
	}

	c.JSON(http.StatusOK, result)
}

// writeCSV 将查询结果以 CSV 格式返回。
func (h *UsageHandler) writeCSV(c *gin.Context, result *repository.UsageResult) {
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=usage_%s.csv", time.Now().Format("20060102")))
	c.Status(http.StatusOK)

	writer := csv.NewWriter(c.Writer)
	defer writer.Flush()

	// 写入汇总行
	writer.Write([]string{"# Summary", "", "", "", ""})
	writer.Write([]string{"total_requests", "total_tokens", "", "", ""})
	writer.Write([]string{
		strconv.FormatInt(result.Summary.TotalRequests, 10),
		strconv.FormatInt(result.Summary.TotalTokens, 10),
		"", "", "",
	})
	writer.Write([]string{""})

	// 写入分组数据
	if len(result.Groups) > 0 {
		headers := []string{"model", "tenant_id", "date", "hour", "requests", "prompt_tokens", "completion_tokens", "total_tokens"}
		writer.Write(headers)
		for _, g := range result.Groups {
			writer.Write([]string{
				g.Model, g.TenantID, g.Date, g.Hour,
				strconv.FormatInt(g.Requests, 10),
				strconv.FormatInt(g.PromptTokens, 10),
				strconv.FormatInt(g.CompletionTokens, 10),
				strconv.FormatInt(g.TotalTokens, 10),
			})
		}
	}

	// 分页信息
	writer.Write([]string{""})
	writer.Write([]string{"# Pagination", "", "", "", ""})
	writer.Write([]string{
		"total", "page", "page_size", "", "",
	})
	writer.Write([]string{
		strconv.FormatInt(result.Total, 10),
		strconv.Itoa(result.Page),
		strconv.Itoa(result.PageSize),
		"", "", "",
	})
}
