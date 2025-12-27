package web

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SetupRoutes 设置路由
func SetupRoutes(r *gin.Engine) {
	// 首先处理根路径，返回 index.html（必须在其他路由之前）
	r.GET("/", func(c *gin.Context) {
		index, err := staticFiles.ReadFile("dist/index.html")
		if err != nil {
			// 如果找不到文件，返回404
			c.Status(http.StatusNotFound)
			c.String(http.StatusNotFound, "Frontend not found. Please rebuild the project.")
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})

	// API 路由
	api := r.Group("/api")
	{
		// 公开的认证相关路由（不需要认证）
		auth := api.Group("/auth")
		{
			auth.GET("/status", getAuthStatus)
			auth.POST("/password/set", setPassword)
			auth.POST("/password/verify", verifyPassword)
			auth.POST("/logout", logout)
		}

		// 需要认证的认证路由
		authProtected := api.Group("/auth")
		authProtected.Use(authMiddleware())
		{
			authProtected.POST("/password/change", changePassword)
		}

		// WebAuthn API（部分需要认证，部分不需要）
		webauthn := api.Group("/webauthn")
		{
			webauthn.POST("/register/begin", authMiddleware(), beginWebAuthnRegistration)
			webauthn.POST("/register/finish", authMiddleware(), finishWebAuthnRegistration)
			webauthn.POST("/login/begin", beginWebAuthnLogin)   // 登录开始不需要认证
			webauthn.POST("/login/finish", finishWebAuthnLogin) // 登录完成不需要认证（但需要密码验证）
			webauthn.GET("/credentials", authMiddleware(), listWebAuthnCredentials)
			webauthn.POST("/credentials/delete", authMiddleware(), deleteWebAuthnCredential)
		}

		// 需要认证的业务API
		protected := api.Group("")
		protected.Use(authMiddleware())
		{
			protected.GET("/status", getStatus)
			protected.GET("/symbols", getSymbols)
			protected.GET("/exchanges", getExchanges)
			protected.GET("/positions", getPositions)
			protected.GET("/positions/summary", getPositionsSummary)
			protected.GET("/orders", getOrders)
			protected.GET("/orders/history", getOrderHistory)
			protected.GET("/statistics", getStatistics)
			protected.GET("/statistics/daily", getDailyStatistics)
			protected.GET("/statistics/trades", getTradeStatistics)
			protected.GET("/statistics/pnl/symbol", getPnLBySymbol)
			protected.GET("/statistics/pnl/time-range", getPnLByTimeRange)
			protected.GET("/reconciliation/status", getReconciliationStatus)
			protected.GET("/reconciliation/history", getReconciliationHistory)
			protected.GET("/risk/status", getRiskStatus)
			protected.GET("/risk/monitor", getRiskMonitorData)
			protected.GET("/risk/history", getRiskCheckHistory)

			// 配置管理API
			protected.GET("/config", getConfigHandler)
			protected.GET("/config/json", getConfigJSONHandler)
			protected.POST("/config/validate", validateConfigHandler)
			protected.POST("/config/preview", previewConfigHandler)
			protected.POST("/config/update", updateConfigHandler)
			protected.GET("/config/backups", getBackupsHandler)
			protected.POST("/config/restore/:backup_id", restoreBackupHandler)
			protected.DELETE("/config/backup/:backup_id", deleteBackupHandler)

			protected.POST("/trading/start", startTrading)
			protected.POST("/trading/stop", stopTrading)

			// 系统监控API
			protected.GET("/system/metrics", getSystemMetrics)
			protected.GET("/system/metrics/current", getCurrentSystemMetrics)
			protected.GET("/system/metrics/daily", getDailySystemMetrics)

			// 日志API
			protected.GET("/logs", getLogs)

			// 槽位API
			protected.GET("/slots", getSlots)

			// 策略资金分配API
			protected.GET("/strategies/allocation", getStrategyAllocation)

			// 待成交订单API
			protected.GET("/orders/pending", getPendingOrders)

			// K线数据API
			protected.GET("/klines", getKlines)

			// 资金费率API
			protected.GET("/funding/current", getFundingRate)

			// AI分析API
			protected.GET("/ai/status", getAIAnalysisStatus)
			protected.GET("/ai/analysis/market", getAIMarketAnalysis)
			protected.GET("/ai/analysis/parameter", getAIParameterOptimization)
			protected.GET("/ai/analysis/risk", getAIRiskAnalysis)
			protected.GET("/ai/analysis/sentiment", getAISentimentAnalysis)
			protected.GET("/ai/analysis/polymarket", getAIPolymarketSignal)
			protected.POST("/ai/analysis/trigger/:module", triggerAIAnalysis)
			protected.GET("/ai/prompts", getAIPrompts)
			protected.POST("/ai/prompts", updateAIPrompt)
			protected.GET("/funding/history", getFundingRateHistory)

			// 市场情报API
			protected.GET("/market-intelligence", getMarketIntelligence)
		}
	}

	// WebSocket 路由
	r.GET("/ws", handleWebSocket)

	// 静态资源文件（CSS、JS、图片等）
	// 注意：Vite 构建后的资源在 dist/assets 目录下
	assetsFS := GetAssetsFS()
	if assetsFS != nil {
		// 使用文件系统提供 /assets 路径下的文件
		r.StaticFS("/assets", assetsFS)
	}

	// SPA 路由回退（所有未匹配的路由返回 index.html）
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// 跳过 API 和 WebSocket 路径
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/ws") {
			c.Status(http.StatusNotFound)
			return
		}
		// 跳过静态资源路径（如果已经通过 StaticFS 处理）
		if strings.HasPrefix(path, "/assets") {
			c.Status(http.StatusNotFound)
			return
		}
		// 其他路径都返回 index.html（SPA 路由）
		index, err := staticFiles.ReadFile("dist/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}
