package web

import (
	"net/http"
	"net/http/pprof"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// Prometheus metrics 端点（不需要认证，供 Prometheus 抓取）
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// pprof 性能分析端点（调试用，生产环境建议添加认证或通过防火墙限制访问）
	pprofGroup := r.Group("/debug/pprof")
	{
		pprofGroup.GET("/", gin.WrapF(pprof.Index))
		pprofGroup.GET("/cmdline", gin.WrapF(pprof.Cmdline))
		pprofGroup.GET("/profile", gin.WrapF(pprof.Profile))
		pprofGroup.POST("/symbol", gin.WrapF(pprof.Symbol))
		pprofGroup.GET("/symbol", gin.WrapF(pprof.Symbol))
		pprofGroup.GET("/trace", gin.WrapF(pprof.Trace))
		pprofGroup.GET("/allocs", gin.WrapH(pprof.Handler("allocs")))
		pprofGroup.GET("/block", gin.WrapH(pprof.Handler("block")))
		pprofGroup.GET("/goroutine", gin.WrapH(pprof.Handler("goroutine")))
		pprofGroup.GET("/heap", gin.WrapH(pprof.Handler("heap")))
		pprofGroup.GET("/mutex", gin.WrapH(pprof.Handler("mutex")))
		pprofGroup.GET("/threadcreate", gin.WrapH(pprof.Handler("threadcreate")))
	}

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

		// 配置引导路由（不需要认证，在配置完成前使用）
		setup := api.Group("/setup")
		{
			setup.GET("/status", getSetupStatusHandler)
			setup.POST("/init", initSetupHandler)
			setup.POST("/exchange-symbols", getExchangeSymbolsHandler)
		}

		// 版本号API（不需要认证）
		api.GET("/version", getVersion)

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
			protected.GET("/statistics/pnl/exchange", getPnLByExchange)
			protected.GET("/statistics/anomalous-trades", getAnomalousTrades)
			protected.GET("/reconciliation/status", getReconciliationStatus)

			// 资金分配管理 API
			protected.GET("/allocation/status", getAllocationStatus)
			protected.GET("/allocation/status/:exchange/:symbol", getAllocationStatusBySymbol)

			// SaaS 管理 API
			saas := protected.Group("/saas")
			{
				saas.POST("/instances/create", createInstanceHandler)
				saas.GET("/instances", listInstancesHandler)
				saas.GET("/instances/:id", getInstanceHandler)
				saas.POST("/instances/:id/stop", stopInstanceHandler)
				saas.POST("/instances/:id/start", startInstanceHandler)
				saas.POST("/instances/:id/restart", restartInstanceHandler)
				saas.DELETE("/instances/:id", deleteInstanceHandler)
				saas.GET("/instances/:id/logs", getInstanceLogsHandler)
				saas.GET("/instances/:id/metrics", getInstanceMetricsHandler)
				saas.GET("/metrics", getAllInstancesMetricsHandler)
			}

			// 计费 API
			billing := protected.Group("/billing")
			{
				billing.GET("/plans", getPlansHandler)
				billing.POST("/subscriptions/create", createSubscriptionHandler)
				billing.GET("/subscriptions", getSubscriptionHandler)
				billing.POST("/subscriptions/update-plan", updateSubscriptionPlanHandler)
				billing.POST("/subscriptions/cancel", cancelSubscriptionHandler)
			}

			// 回测 API
			backtestAPI := protected.Group("/backtest")
			{
				backtestAPI.POST("/run", runBacktest)
				backtestAPI.GET("/cache/stats", getCacheStats)
				backtestAPI.GET("/cache/list", listCache)
				backtestAPI.DELETE("/cache/:key", deleteCache)
				backtestAPI.DELETE("/cache", clearCache)
			}

			// 加密货币支付 API
			cryptoPayment := protected.Group("/payment/crypto")
			{
				cryptoPayment.GET("/currencies", getSupportedCryptoCurrenciesHandler)
				cryptoPayment.POST("/coinbase/create", createCoinbasePaymentHandler)
				cryptoPayment.POST("/direct/create", createDirectPaymentHandler)
				cryptoPayment.GET("/list", listUserPaymentsHandler)
				cryptoPayment.GET("/:id", getPaymentStatusHandler)
				cryptoPayment.POST("/:id/submit-tx", submitTransactionHashHandler)
				cryptoPayment.POST("/:id/confirm", confirmDirectPaymentHandler) // 管理员
			}

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
			protected.POST("/trading/close-positions", closeAllPositions)

			// 系统监控API
			protected.GET("/system/metrics", getSystemMetrics)
			protected.GET("/system/metrics/current", getCurrentSystemMetrics)
			protected.GET("/system/metrics/daily", getDailySystemMetrics)

			// 日志API
			protected.GET("/logs", getLogs)
			protected.POST("/logs/clean", cleanLogs)
			protected.GET("/logs/stats", getLogStats)
			protected.POST("/logs/vacuum", vacuumLogs)

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

			// AI 配置助手 API
			protected.POST("/ai/generate-config", generateAIConfig)
			protected.POST("/ai/apply-config", applyAIConfig)

			protected.GET("/funding/history", getFundingRateHistory)

			// 价差监控
			protected.GET("/basis/current", getBasisCurrent)
			protected.GET("/basis/history", getBasisHistory)
			protected.GET("/basis/statistics", getBasisStatistics)

			// 市场情报API
			protected.GET("/market-intelligence", getMarketIntelligence)

			// API 权限检测
			protected.GET("/permissions/check", getAPIPermissions)

			// 审计日志
			protected.GET("/audit/logs", getAuditLogs)

			// 策略管理 API
			strategies := protected.Group("/strategies")
			{
				strategies.GET("", getStrategiesHandler)
				strategies.GET("/types", getStrategyTypesHandler)
				strategies.GET("/configs", getStrategyConfigsHandler)
				strategies.GET("/enabled", getEnabledStrategiesHandler)
				strategies.POST("/batch-update", batchUpdateStrategiesHandler)
				strategies.GET("/:id", getStrategyDetailHandler)
				strategies.POST("/:id/enable", enableStrategyHandler)
				strategies.POST("/:id/disable", disableStrategyHandler)
				strategies.GET("/:id/license", getStrategyLicenseHandler)
				strategies.PUT("/:id/config", updateStrategyConfigHandler)
				strategies.POST("/:id/purchase", purchaseStrategyHandler)
			}

			// 盈利管理 API
			profit := protected.Group("/profit")
			{
				profit.GET("/summary", getProfitSummaryHandler)
				profit.GET("/by-strategy", getStrategyProfitsHandler)
				profit.GET("/by-strategy/:id", getStrategyProfitDetailHandler)
				profit.GET("/withdraw-rules", getWithdrawRulesHandler)
				profit.PUT("/withdraw-rules", updateWithdrawRulesHandler)
				profit.POST("/withdraw-rules/upsert", upsertWithdrawRuleHandler)
				profit.DELETE("/withdraw-rules/:id", deleteWithdrawRuleHandler)
				profit.POST("/withdraw", withdrawProfitHandler)
				profit.GET("/history", getWithdrawHistoryHandler)
				profit.GET("/trend", getProfitTrendHandler)
				profit.POST("/withdraw/estimate", estimateWithdrawFeeHandler)
				profit.POST("/withdraw/:id/cancel", cancelWithdrawHandler)
				profit.GET("/withdraw/:id", getWithdrawDetailHandler)
			}

			// 资金管理 API
			capital := protected.Group("/capital")
			{
				capital.GET("/overview", getCapitalOverviewHandler)
				capital.GET("/allocation", getCapitalAllocationHandler)
				capital.PUT("/allocation", updateCapitalAllocationHandler)
				capital.GET("/allocation/:id", getStrategyCapitalDetailHandler)
				capital.PUT("/allocation/:id", updateStrategyCapitalHandler)
				capital.POST("/allocation/:id/lock", lockStrategyCapitalHandler)
				capital.POST("/rebalance", rebalanceCapitalHandler)
				capital.GET("/history", getCapitalHistoryHandler)
				capital.PUT("/reserve", setReserveCapitalHandler)
			}
		}

		// 事件中心 API
		registerEventRoutes(api, authMiddleware())

		// Webhooks (不需要认证,但需要验证签名)
		api.POST("/billing/webhook/stripe", stripeWebhookHandler)
		api.POST("/payment/crypto/webhook/coinbase", coinbaseWebhookHandler)
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

	// 图标目录
	iconsFS := GetIconsFS()
	if iconsFS != nil {
		r.StaticFS("/icons", iconsFS)
	}

	// PWA 相关静态文件（Service Worker、Manifest 等）
	// 这些文件需要从根路径访问
	pwaFiles := map[string]string{
		"/registerSW.js":        "dist/registerSW.js",
		"/sw.js":                "dist/sw.js",
		"/manifest.webmanifest": "dist/manifest.webmanifest",
		"/manifest.json":        "dist/manifest.json",
	}
	for urlPath, filePath := range pwaFiles {
		fp := filePath // 捕获变量
		r.GET(urlPath, func(c *gin.Context) {
			data, err := staticFiles.ReadFile(fp)
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			// 根据文件类型设置正确的 Content-Type
			contentType := "application/javascript"
			if strings.HasSuffix(fp, ".json") || strings.HasSuffix(fp, ".webmanifest") {
				contentType = "application/json"
			}
			c.Data(http.StatusOK, contentType, data)
		})
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
		if strings.HasPrefix(path, "/assets") || strings.HasPrefix(path, "/icons") {
			c.Status(http.StatusNotFound)
			return
		}

		// 处理 workbox 文件（如 /workbox-3ade98c4.js）
		if strings.HasPrefix(path, "/workbox-") && strings.HasSuffix(path, ".js") {
			filePath := "dist" + path
			data, err := staticFiles.ReadFile(filePath)
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			c.Data(http.StatusOK, "application/javascript", data)
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
