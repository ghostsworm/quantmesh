package web

import (
	"net/http"
	"net/http/pprof"
	"strconv"
	"strings"

	"quantmesh/cfgmgr"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	globalConfig  *config.Config
	configStorage storage.ConfigStorage
	configManager *cfgmgr.ConfigManager
)

// SetGlobalConfig 設置全局配置
func SetGlobalConfig(cfg *config.Config) {
	globalConfig = cfg
}

// ipWhitelistMiddleware IP 白名單中间件
func ipWhitelistMiddleware(allowedIPs []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		clientIP := c.ClientIP()

		// 检查是否在白名單中
		allowed := false
		for _, ip := range allowedIPs {
			if ip == clientIP || ip == "*" {
				allowed = true
				break
			}
		}

		if !allowed {
			logger.Warn("⚠️ [pprof] IP %s 不在白名單中，拒绝访问", clientIP)
			c.JSON(http.StatusForbidden, gin.H{"error": "Access denied"})
			c.Abort()
			return
		}

		c.Next()
	}
}

// SetupRoutes 設置路由
func SetupRoutes(r *gin.Engine) {
	SetupRoutesWithConfig(r, nil)
}

// SetupRoutesWithConfig 設置路由（带配置）
func SetupRoutesWithConfig(r *gin.Engine, cfg *config.Config) {
	globalConfig = cfg

	// 初始化默認數據源提供者（如果還沒有設置）
	InitDefaultDataSourceProvider()

	// 首先处理根路径，回傳 index.html（必須在其他路由之前）
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

	// pprof 性能分析端点（默认关闭，需要通過配置显式啟用）
	pprofEnabled := false
	pprofRequireAuth := true
	var pprofAllowedIPs []string

	if cfg != nil && cfg.Web.Pprof.Enabled {
		pprofEnabled = true
		pprofRequireAuth = cfg.Web.Pprof.RequireAuth
		pprofAllowedIPs = cfg.Web.Pprof.AllowedIPs
		logger.Info("✅ pprof 已啟用 (需要认证: %v, IP白名單: %v)", pprofRequireAuth, len(pprofAllowedIPs) > 0)
	}

	if pprofEnabled {
		pprofGroup := r.Group("/debug/pprof")

		// IP 白名單中间件
		if len(pprofAllowedIPs) > 0 {
			pprofGroup.Use(ipWhitelistMiddleware(pprofAllowedIPs))
		}

		// 认证中间件（如果需要）
		if pprofRequireAuth {
			pprofGroup.Use(authMiddleware())
		}

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
	} else {
		logger.Info("ℹ️ pprof 已禁用（生產环境建议禁用）")
	}

	// API 路由（所有 API 响应带 X-App-Version 头便於排查）
	api := r.Group("/api")
	api.Use(versionHeaderMiddleware())
	{
		// 公开的认证相关路由（不需要认证）
		auth := api.Group("/auth")
		{
			auth.GET("/status", getAuthStatus)
			auth.POST("/password/set", setPassword)
			auth.POST("/password/verify", verifyPassword)
			auth.POST("/logout", logout)
		}

		// 配置引導路由（不需要认证，在配置完成前使用）
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
			webauthn.POST("/login/begin", beginWebAuthnLogin)   // 登錄开始不需要认证
			webauthn.POST("/login/finish", finishWebAuthnLogin) // 登錄完成不需要认证（但需要密碼驗证）
			webauthn.GET("/credentials", authMiddleware(), listWebAuthnCredentials)
			webauthn.POST("/credentials/delete", authMiddleware(), deleteWebAuthnCredential)
		}

		// 需要认证的业務API
		protected := api.Group("")
		protected.Use(authMiddleware())
		{
			protected.GET("/status", getStatus)
			protected.GET("/statuses", getStatuses)
			protected.GET("/services/status", getServicesStatus)
			protected.GET("/symbols", getSymbols)
			protected.GET("/bots", getBots)
			protected.POST("/bots/create", postBotCreate)
			protected.GET("/bots/:id", getBotByID)
			protected.DELETE("/bots/:id", deleteBot)
			protected.POST("/bots/:id/start", postBotStart)
			protected.POST("/bots/:id/stop", postBotStop)
			protected.POST("/bots/:id/enable", postBotEnable)
			protected.PUT("/bots/:id/strategy", putBotStrategy)

			// Bot 配置文件 API（独立配置文件系统）
			protected.GET("/bots/:id/config-file", getBotConfigFile)
			protected.PUT("/bots/:id/config-file", putBotConfigFile)
			protected.DELETE("/bots/:id/config-file", deleteBotConfigFile)
			protected.PUT("/bots/:id/strategy-config", putBotStrategyConfig)
			protected.POST("/bots/:id/strategies", addBotStrategy)
			protected.DELETE("/bots/:id/strategies/:index", removeBotStrategy)
			protected.GET("/bots/:id/validate", getBotConfigValidation)
			protected.POST("/bots/:id/apply-template", applyStrategyTemplate)

			// 混合策略 API
			protected.GET("/bots/:id/hybrid-config", getHybridStrategyConfig)
			protected.PUT("/bots/:id/hybrid-config", updateHybridStrategy)
			protected.POST("/bots/:id/enable-hybrid", enableHybridMode)
			protected.POST("/bots/:id/disable-hybrid", disableHybridMode)
			protected.GET("/bots/:id/hybrid-status", getHybridStrategyStatus)
			protected.GET("/hybrid/rules/templates", getBuiltInRuleTemplates)

			// 策略模板 API
			protected.GET("/strategy-templates", getStrategyTemplates)
			protected.GET("/strategy-templates/:id", getStrategyTemplate)
			protected.GET("/strategy-templates/category/:category", getStrategyTemplatesByCategory)
			protected.GET("/strategy-templates/:id/export", exportStrategyTemplate)
			protected.POST("/strategy-templates/custom", saveCustomTemplate)
			protected.DELETE("/strategy-templates/:id", deleteCustomTemplate)
			protected.POST("/strategy-templates/import", importStrategyTemplate)

			protected.GET("/bot-groups", getBotGroups)
			protected.GET("/bot-groups/:id", getBotGroupByID)
			protected.POST("/bot-groups", postBotGroupCreate)
			protected.DELETE("/bot-groups/:id", deleteBotGroup)
			protected.GET("/exchanges", getExchanges)
			protected.GET("/positions", getPositions)
			protected.GET("/positions/summary", getPositionsSummary)
			protected.GET("/positions/summary/all", getPositionsSummaryAll)
			protected.GET("/orders", getOrders)
			protected.GET("/orders/history", getOrderHistory)
			protected.POST("/orders/sync", syncOrders)
			protected.GET("/fix/sessions", getFixSessions)
			protected.GET("/fix/orders", getFixOrderLinks)
			protected.POST("/fix/sessions/logon", fixLogonSession)
			protected.POST("/fix/sessions/heartbeat", fixHeartbeat)
			protected.POST("/fix/orders/new", fixNewOrder)
			protected.POST("/fix/orders/cancel", fixCancelOrder)
			protected.POST("/fix/orders/replace", fixReplaceOrder)
			protected.GET("/statistics", getStatistics)
			protected.GET("/statistics/daily", getDailyStatistics)
			protected.GET("/statistics/daily/breakdown", getDailyPnLBreakdown)
			protected.GET("/statistics/trades", getTradeStatistics)
			protected.GET("/statistics/pnl/symbol", getPnLBySymbol)
			protected.GET("/statistics/pnl/time-range", getPnLByTimeRange)
			protected.GET("/statistics/pnl/exchange", getPnLByExchange)
			protected.GET("/statistics/pnl/diagnosis", getExchangePnLDiagnosis)
			protected.GET("/statistics/anomalous-trades", getAnomalousTrades)
			protected.GET("/reconciliation/status", getReconciliationStatus)

			// 资金分配管理 API
			protected.GET("/allocation/status", getAllocationStatus)
			protected.GET("/allocation/status/:exchange/:symbol", getAllocationStatusBySymbol)

			// 開倉管理 API
			protected.GET("/opening-control/status", getOpeningControlStatus)
			protected.POST("/opening-control/pause", pauseOpening)
			protected.POST("/opening-control/resume", resumeOpening)
			protected.GET("/opening-control/config", getOpeningControlConfig)
			protected.PUT("/opening-control/config", putOpeningControlConfig)

			// 網格上移/下移 API
			protected.POST("/grid/shift-up", postGridShiftUp)
			protected.POST("/grid/shift-down", postGridShiftDown)

			// 倉位目標计划 API（check 須在 :id 前注册）
			protected.GET("/position-plans/check", getPositionPlanCheck)
			protected.GET("/position-plans", getPositionPlans)
			protected.GET("/position-plans/:id", getPositionPlanByID)
			protected.POST("/position-plans", createPositionPlan)
			protected.PUT("/position-plans/:id", updatePositionPlan)
			protected.DELETE("/position-plans/:id", cancelPositionPlan)

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
				backtestAPI.GET("/strategies", getBacktestStrategies)
				backtestAPI.GET("/presets/:symbol", getBacktestPreset)
				backtestAPI.GET("/exchanges", getBacktestExchanges)        // 獲取交易所列表
				backtestAPI.GET("/symbols", getBacktestSymbols)            // 獲取交易對列表（按交易所+市場類型）
				backtestAPI.GET("/config-params", getBacktestConfigParams) // 獲取已配置的策略参數
				backtestAPI.POST("/cache/generate", postCacheGenerate)
				backtestAPI.GET("/cache/status", getCacheStatus)
				backtestAPI.GET("/cache/stats", getCacheStats)
				backtestAPI.GET("/cache/list", listCache)
				backtestAPI.DELETE("/cache/:key", deleteCache)
				backtestAPI.DELETE("/cache", clearCache)
				backtestAPI.POST("/tasks", postBacktestTasks)
				backtestAPI.GET("/tasks", getBacktestTasks)
				backtestAPI.GET("/tasks/:id", getBacktestTaskByID)
				backtestAPI.GET("/tasks/:id/result", getBacktestTaskResult)
				backtestAPI.GET("/tasks/:id/klines", getBacktestTaskKlines)
				backtestAPI.GET("/tasks/:id/report", getBacktestTaskReport)
				backtestAPI.GET("/tasks/:id/trades/export", getBacktestTaskTradesExport)
				backtestAPI.GET("/tasks/:id/trades", getBacktestTaskTrades)
				backtestAPI.DELETE("/tasks/:id", deleteBacktestTask)
				// 智能參數推薦 API
				backtestAPI.GET("/smart-params", getSmartParamsRecommendation)
				backtestAPI.POST("/smart-params", postSmartParamsRecommendation)
				backtestAPI.GET("/smart-params/multiple", getMultipleSmartParams)
				// 預計算回測 API
				backtestAPI.GET("/precomputed", getPrecomputedResults)
				backtestAPI.GET("/precomputed/:symbol/:strategy", getPrecomputedResult)
				backtestAPI.POST("/precomputed/trigger", triggerPrecompute)
				backtestAPI.GET("/scheduler/status", getAutoSchedulerStatus)
				// 參數優化 API
				backtestAPI.POST("/optim/tasks", postOptimTasks)
				backtestAPI.GET("/optim/tasks", getOptimTasks)
				backtestAPI.GET("/optim/tasks/:id", getOptimTaskByID)
				backtestAPI.GET("/optim/tasks/:id/result", getOptimTaskResult)
				backtestAPI.DELETE("/optim/tasks/:id", deleteOptimTask)
				backtestAPI.GET("/optim/space/:strategy", getOptimSearchSpace)
			}

			// 网格参數优化 API
			optimizerAPI := protected.Group("/optimizer")
			{
				optimizerAPI.GET("/price", getOptimizerPrice)
				optimizerAPI.POST("/run", postOptimizerRun)
				optimizerAPI.GET("/status/:id", getOptimizerStatus)
				optimizerAPI.GET("/result/:id", getOptimizerResult)
				optimizerAPI.POST("/stop/:id", postOptimizerStop)
			}

			// 加密貨幣支付 API
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
			protected.GET("/reconciliation/aggregated", getReconciliationAggregated)
			protected.GET("/risk/status", getRiskStatus)
			protected.GET("/risk/monitor", getRiskMonitorData)
			// 新聞分析 API
			protected.GET("/news/analysis", getNewsAnalysis)
			protected.GET("/news/predictions", getNewsPredictions)
			protected.POST("/news/analyze", postNewsAnalyze)
			protected.GET("/news/collected", getNewsCollected)
			protected.GET("/news/keywords", getNewsKeywords)
			protected.PUT("/news/keywords", putNewsKeywords)
			protected.GET("/news/history", getNewsHistory)
			protected.GET("/news/history/:id", getNewsHistoryByID)
			protected.GET("/predictions/accuracy", getPredictionsAccuracy)
			protected.GET("/predictions/history", getPredictionsHistory)
			protected.GET("/risk/history", getRiskCheckHistory)
			protected.GET("/risk/newbie-check", getNewbieRiskCheck)
			protected.POST("/risk/newbie-check/apply", applyNewbieSecurityConfig)

			// 市场行情 API（当前价、标记价、24h 高低）
			protected.GET("/market/ticker", getMarketTicker)

			// 配置参数建议 API
			protected.GET("/config/param-advisor", getParamAdvisor)
			protected.GET("/config/exchange-fees", getExchangeFees)
			protected.GET("/config/price-range", getPriceRangeHandler)

			// 配置管理API
			protected.GET("/config", getConfigHandler)
			protected.GET("/config/json", getConfigJSONHandler)
			protected.POST("/config/validate", validateConfigHandler)
			protected.POST("/config/validate-yaml", validateConfigYAMLHandler)
			protected.POST("/config/preview", previewConfigHandler)
			protected.POST("/config/update", updateConfigHandler)
			protected.POST("/config/update-yaml", updateConfigYAMLHandler)
			protected.POST("/config/test-notification", testNotificationHandler)
			protected.GET("/config/backups", getBackupsHandler)
			protected.POST("/config/restore/:backup_id", restoreBackupHandler)
			protected.DELETE("/config/backup/:backup_id", deleteBackupHandler)

			// 配置历史API
			protected.GET("/config/history", getConfigHistoryListHandler)
			protected.GET("/config/history/:version", getConfigHistoryHandler)
			protected.POST("/config/history/:version/restore", restoreConfigHistoryHandler)
			protected.POST("/config/history/diff", diffConfigHistoryHandler)

			// 數據導出 API
			protected.GET("/export/config", exportConfigHandler)
			protected.GET("/export/config/history/:version", exportConfigHistoryHandler)
			protected.GET("/export/trades", exportTradesHandler)
			protected.GET("/export/orders", exportOrdersHandler)
			protected.GET("/export/positions", exportPositionsHandler)
			protected.GET("/export/statistics", exportStatisticsHandler)
			protected.GET("/export/reconciliation", exportReconciliationHandler)
			protected.GET("/export/risk-checks", exportRiskChecksHandler)
			protected.GET("/export/system-metrics", exportSystemMetricsHandler)
			protected.GET("/export/logs", exportLogsHandler)
			protected.GET("/export/audit-logs", exportAuditLogsHandler)
			protected.GET("/export/backtest-reports", exportBacktestReportsHandler)
			protected.GET("/export/all", exportAllHandler)

			protected.POST("/trading/start", startTrading)
			protected.POST("/trading/stop", stopTrading)
			protected.POST("/trading/close-positions", closeAllPositions)

			// 系统監控API
			protected.GET("/system/metrics", getSystemMetrics)
			protected.GET("/system/metrics/current", getCurrentSystemMetrics)
			protected.GET("/system/metrics/daily", getDailySystemMetrics)

			// 系统设置 API
			protected.GET("/system/settings", getSystemSettings)
			protected.GET("/system/settings/:key", getSystemSetting)
			protected.POST("/system/settings", setSystemSetting)
			protected.DELETE("/system/settings/:key", deleteSystemSetting)
			protected.GET("/system/local-dev-mode", getLocalDevMode)
			protected.POST("/system/local-dev-mode", setLocalDevMode)

			// 配置管理 API
			if configStorage != nil {
				configAPI := NewConfigManagerAPI(configStorage)
				configAPI.RegisterRoutes(protected)
			}

			// 日志API
			protected.GET("/logs", getLogs)
			protected.POST("/logs/clean", cleanLogs)
			protected.GET("/logs/stats", getLogStats)
			protected.POST("/logs/vacuum", vacuumLogs)

			// 槽位API
			protected.GET("/slots", getSlots)

			// 策略资金分配API（注意：release-capital 路由移到 strategies 组中以避免路由冲突）
			protected.GET("/strategies/allocation", getStrategyAllocation)

			// 待成交订單API
			protected.GET("/orders/pending", getPendingOrders)
			protected.GET("/orders/exchange-open", getExchangeOpenOrders)
			protected.POST("/orders/:id/cancel", cancelOrder)
			protected.POST("/orders/cancel", batchCancelOrders)
			protected.POST("/orders/cancel-all-exchange", cancelAllExchangeOrders)

			// K線數據API
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
			protected.GET("/ai/task/:task_id", getAITaskStatus)
			protected.GET("/ai/tasks", getAITasks)
			protected.GET("/ai/tasks/stats", getAITaskStats)
			protected.POST("/ai/apply-config", applyAIConfig)

			// AI 市场解读 API（latest/history 须在 :task_id 之前注册）
			protected.POST("/ai/market-interpret", createMarketInterpret)
			protected.GET("/ai/market-interpret/latest", getMarketInterpretLatest)
			protected.GET("/ai/market-interpret/history", getMarketInterpretHistory)
			protected.GET("/ai/market-interpret/:task_id", getMarketInterpretStatus)

			protected.GET("/funding/history", getFundingRateHistory)

			// 價差監控
			protected.GET("/basis/current", getBasisCurrent)
			protected.GET("/basis/history", getBasisHistory)
			protected.GET("/basis/statistics", getBasisStatistics)

			// 市场情报API
			protected.GET("/market-intelligence", getMarketIntelligence)

			// 宏觀事件預測市場 API
			protected.GET("/macro/events", getMacroEvents)
			protected.GET("/macro/impact", getMacroImpact)

			// API 权限检测
			protected.GET("/permissions/check", getAPIPermissions)

			// 审计日志
			protected.GET("/audit/logs", getAuditLogs)

			// 策略管理 API
			strategies := protected.Group("/strategies")
			{
				strategies.GET("", getStrategiesHandler)
				strategies.GET("/templates", getStrategyTemplatesHandler)
				strategies.GET("/types", getStrategyTypesHandler)
				strategies.GET("/configs", getStrategyConfigsHandler)
				strategies.GET("/enabled", getEnabledStrategiesHandler)
				strategies.GET("/runtime", getStrategyRuntimeStatusHandler)         // 獲取所有策略運行狀態
				strategies.GET("/runtime/all", getStrategyRuntimeAllHandler)        // 獲取所有幣種下所有策略運行狀態（聚合）
				strategies.GET("/runtime/:id", getStrategyRuntimeStatusByIDHandler) // 獲取單個策略運行狀態
				strategies.POST("/batch-update", batchUpdateStrategiesHandler)
				strategies.POST("/release-all-capital", releaseAllStrategiesCapital) // 释放所有策略锁定资金
				strategies.GET("/:id", getStrategyDetailHandler)
				strategies.POST("/:id/enable", enableStrategyHandler)
				strategies.POST("/:id/disable", disableStrategyHandler)
				strategies.GET("/:id/license", getStrategyLicenseHandler)
				strategies.PUT("/:id/config", updateStrategyConfigHandler)
				strategies.POST("/:id/purchase", purchaseStrategyHandler)
				strategies.POST("/:id/release-capital", releaseStrategyCapital) // 释放单个策略锁定资金
			}

			// 盈利管理 API
			profit := protected.Group("/profit")
			{
				profit.GET("/summary", getProfitSummaryHandler)
				profit.GET("/funding", getFundingHistoryHandler)
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
				capital.GET("/usage", getCapitalUsageHandler)
				capital.GET("/allocation", getCapitalAllocationHandler)
				capital.PUT("/allocation", updateCapitalAllocationHandler)
				capital.GET("/allocation/:id", getStrategyCapitalDetailHandler)
				capital.PUT("/allocation/:id", updateStrategyCapitalHandler)
				capital.POST("/allocation/:id/lock", lockStrategyCapitalHandler)
				capital.POST("/rebalance", rebalanceCapitalHandler)
				capital.GET("/history", getCapitalHistoryHandler)
				capital.PUT("/reserve", setReserveCapitalHandler)
			}

			// K线数据文件管理 API
			klineFiles := protected.Group("/kline-files")
			{
				klineFiles.GET("", listKlineFilesHandler)
				klineFiles.GET("/available", listAvailableKlineFilesHandler)
				klineFiles.POST("/:filename/protect", protectKlineFileHandler)
				klineFiles.DELETE("/:filename/protect", unprotectKlineFileHandler)
				klineFiles.GET("/:filename/download", downloadKlineFileHandler)
			}

			// V2 API: 平倉和槽位管理
			v2 := protected.Group("/v2")
			{
				bots := v2.Group("/bots")
				{
					bots.POST("/:id/close-positions", closePositionsV2)
					bots.GET("/:id/close-records", getClosePositionRecords)
					bots.GET("/:id/slots", getBotSlots)
					bots.GET("/:id/slot-filter", getSlotFilter)
					bots.POST("/:id/slot-filter", setSlotFilter)
					bots.POST("/:id/slot-filter/rules", addSlotFilterRule)
					bots.DELETE("/:id/slot-filter/rules/:index", removeSlotFilterRule)
					bots.POST("/:id/slots/toggle", toggleSlotEnabled)

					// 风控管理 API
					bots.GET("/:id/risk-control", getBotRiskControl)
					bots.PUT("/:id/risk-control", updateBotRiskControl)
					bots.POST("/:id/pause-opening", pauseBotOpening)
					bots.POST("/:id/resume-opening", resumeBotOpening)
					bots.GET("/:id/position-status", getBotPositionStatus)

					// 回测 API
					bots.POST("/:id/backtest", postBotBacktestCreate)
					bots.GET("/:id/backtest/tasks", listBotBacktestTasks)
				}
				v2.POST("/close-records/:record_id/retry", retryClosePosition)

				// 回测任务管理 API
				backtest := v2.Group("/bot/backtest")
				{
					backtest.GET("/:taskId", getBotBacktestTask)
					backtest.GET("/:taskId/result", getBotBacktestResult)
					backtest.DELETE("/:taskId", deleteBotBacktestTask)
				}

				// 历史数据管理 API
				data := v2.Group("/backtest/data")
				{
					data.POST("/download", postBinanceDataDownload)
					data.GET("/info", getBinanceDataInfo)
					data.GET("/availability", getBinanceDataAvailability)
					data.GET("/latest", getBinanceLatestDataTime)
				}
			}

			// 全局熔断器 API
			circuitBreaker := protected.Group("/circuit-breaker")
			{
				circuitBreaker.GET("/status", getCircuitBreakerStatus)
				circuitBreaker.GET("/events", getCircuitBreakerEvents)
				circuitBreaker.POST("/trigger", manualTriggerCircuitBreaker)
				circuitBreaker.POST("/recover", manualRecoverCircuitBreaker)
				circuitBreaker.POST("/metrics", updateCircuitBreakerMetrics)
				circuitBreaker.POST("/report/auth-failure", reportAuthFailure)
				circuitBreaker.POST("/report/websocket-disconnect", reportWebSocketDisconnect)
				circuitBreaker.POST("/report/websocket-reconnected", reportWebSocketReconnected)
			}

			// 紧急操作中心 API
			emergency := protected.Group("/emergency")
			{
				emergency.GET("/scenarios", getEmergencyScenarios)
				emergency.POST("/execute", executeEmergencyScenario)
				emergency.GET("/operations", getEmergencyOperations)
				emergency.GET("/mode", getEmergencyModeStatus)
				emergency.POST("/mode/disable", disableEmergencyMode)
			}

			// 动态止损 API
			dynamicSL := protected.Group("/dynamic-stop-loss")
			{
				dynamicSL.GET("/slots", getDynamicStopLossSlots)
				dynamicSL.GET("/stats", getDynamicStopLossStats)
				dynamicSL.POST("/adjust", adjustStopLoss)
			}

			// AI Agent API
			agent := protected.Group("/agent")
			{
				agent.GET("/sessions", listSessions)
				agent.POST("/sessions", createSession)
				agent.POST("/sessions/:id/messages", sendMessage)
				agent.GET("/sessions/:id/history", getSessionHistory)
				agent.GET("/sessions/:id/config", getSessionConfig)
				agent.POST("/sessions/:id/apply", applyConfig)
				agent.DELETE("/sessions/:id", deleteSession)
				agent.GET("/sessions/:id/ws", handleAgentWebSocket)
			}
		}

		// 事件中心 API
		registerEventRoutes(api, authMiddleware())

		// Webhooks (不需要认证,但需要驗证签名)
		api.POST("/billing/webhook/stripe", stripeWebhookHandler)
		api.POST("/payment/crypto/webhook/coinbase", coinbaseWebhookHandler)
	}

	// WebSocket 路由
	r.GET("/ws", handleWebSocket)

	// 静態资源文件（CSS、JS、图片等）
	// 注意：Vite 構建后的资源在 dist/assets 目錄下
	assetsFS := GetAssetsFS()
	if assetsFS != nil {
		// 使用文件系统提供 /assets 路径下的文件
		r.StaticFS("/assets", assetsFS)
	}

	// 图標目錄
	iconsFS := GetIconsFS()
	if iconsFS != nil {
		r.StaticFS("/icons", iconsFS)
	}

	// 共享目錄（AI 生成的图片、视频等）
	if cfg != nil && cfg.Web.SharedDir != "" {
		sharedDir := cfg.Web.SharedDir
		r.Static("/shared", sharedDir)
		logger.Info("✅ 共享目錄已啟用: %s -> /shared", sharedDir)
	}

	// PWA 相关静態文件（Service Worker、Manifest 等）
	// 这些文件需要從根路径访问
	pwaFiles := map[string]string{
		"/registerSW.js":        "dist/registerSW.js",
		"/sw.js":                "dist/sw.js",
		"/manifest.webmanifest": "dist/manifest.webmanifest",
		"/manifest.json":        "dist/manifest.json",
	}
	for urlPath, filePath := range pwaFiles {
		fp := filePath // 捕獲变量
		r.GET(urlPath, func(c *gin.Context) {
			data, err := staticFiles.ReadFile(fp)
			if err != nil {
				c.Status(http.StatusNotFound)
				return
			}
			// 根據文件類型設置正确的 Content-Type
			contentType := "application/javascript"
			if strings.HasSuffix(fp, ".json") || strings.HasSuffix(fp, ".webmanifest") {
				contentType = "application/json"
			}
			c.Data(http.StatusOK, contentType, data)
		})
	}

	// SPA 路由回退（所有未匹配的路由回傳 index.html）
	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		// 跳過 API 和 WebSocket 路径
		if strings.HasPrefix(path, "/api") || strings.HasPrefix(path, "/ws") || strings.HasPrefix(path, "/shared") {
			c.Status(http.StatusNotFound)
			return
		}
		// 跳過静態资源路径（如果已經通過 StaticFS 处理）
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

		// 其他路径都回傳 index.html（SPA 路由）
		index, err := staticFiles.ReadFile("dist/index.html")
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}
		c.Data(http.StatusOK, "text/html; charset=utf-8", index)
	})
}

// parseIntParam 解析整数参数，带范围检查
func parseIntParam(s string, min, max int) (int, error) {
	parsed, err := strconv.Atoi(s)
	if err != nil {
		return 0, err
	}
	if parsed < min {
		return min, nil
	}
	if parsed > max {
		return max, nil
	}
	return parsed, nil
}
