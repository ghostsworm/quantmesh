package web

import (
	"net/http"

	"quantmesh/ai/geminiusage"

	"github.com/gin-gonic/gin"
)

// getGeminiUsageLog GET /api/ai/gemini/usage — 進程內 Gemini 調用記錄（時間、輸入/輸出 token）
func getGeminiUsageLog(c *gin.Context) {
	snap := geminiusage.Snapshot()
	sum := geminiusage.Aggregate(snap)
	c.JSON(http.StatusOK, gin.H{
		"entries": snap,
		"summary": sum,
	})
}
