package web

import (
	"net/http"
	"strconv"
	"time"

	"quantmesh/ai/geminiusage"
	"github.com/gin-gonic/gin"
)

// getGeminiUsageLog GET /api/ai/gemini/usage
// 優先從主庫 gemini_usage 表讀取；無主庫時回退進程內緩存。
// Query: limit, offset, start_time, end_time（RFC3339）
func getGeminiUsageLog(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	if offset < 0 {
		offset = 0
	}

	var startPtr, endPtr *time.Time
	if s := c.Query("start_time"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			startPtr = &t
		}
	}
	if s := c.Query("end_time"); s != "" {
		if t, err := time.Parse(time.RFC3339, s); err == nil {
			endPtr = &t
		}
	}

	st := GetPrimaryStorageForAppConfig()
	if st != nil {
		records, total, err := st.QueryGeminiUsageRecords(startPtr, endPtr, limit, offset)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "error.internal", err)
			return
		}
		agg, inTok, outTok, err := st.AggregateGeminiUsageTotals(startPtr, endPtr)
		if err != nil {
			respondError(c, http.StatusInternalServerError, "error.internal", err)
			return
		}
		entries := make([]gin.H, 0, len(records))
		for _, r := range records {
			entries = append(entries, gin.H{
				"id":             r.ID,
				"at":             r.CalledAt.UTC().Format(time.RFC3339Nano),
				"model":          r.Model,
				"source":         r.Source,
				"input_tokens":   r.InputTokens,
				"output_tokens":  r.OutputTokens,
				"duration_ms":    r.DurationMs,
			})
		}
		c.JSON(http.StatusOK, gin.H{
			"entries": entries,
			"summary": gin.H{
				"call_count":           agg,
				"total_input_tokens":   inTok,
				"total_output_tokens":  outTok,
			},
			"total":  total,
			"limit":  limit,
			"offset": offset,
			"source": "database",
		})
		return
	}

	snap := geminiusage.Snapshot()
	total := len(snap)
	sumAll := geminiusage.Aggregate(snap)
	start := offset
	if start > total {
		start = total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := snap[start:end]
	entries := make([]gin.H, 0, len(page))
	for _, e := range page {
		entries = append(entries, gin.H{
			"at":            e.At.UTC().Format(time.RFC3339Nano),
			"model":         e.Model,
			"source":        e.Source,
			"input_tokens":  e.InputTokens,
			"output_tokens": e.OutputTokens,
			"duration_ms":   e.DurationMs,
		})
	}
	c.JSON(http.StatusOK, gin.H{
		"entries": entries,
		"summary": gin.H{
			"call_count":          sumAll.CallCount,
			"total_input_tokens":  sumAll.TotalInputTokens,
			"total_output_tokens": sumAll.TotalOutputTokens,
		},
		"total":  int64(total),
		"limit":  limit,
		"offset": offset,
		"source": "memory",
	})
}
