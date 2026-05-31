package web

import (
	"context"
	"net/http"
	"reflect"
	"strconv"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/database"
	"quantmesh/exchange"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// ========== AI 配置生成 / 任務管理 API ==========

// SymbolCapitalRequest 币种资金配置请求
type SymbolCapitalRequest struct {
	Symbol  string  `json:"symbol"`
	Capital float64 `json:"capital"`
}

// generateAIConfig 生成 AI 配置建议
// POST /api/ai/generate-config
func generateAIConfig(c *gin.Context) {
	var req struct {
		Exchange       string                 `json:"exchange"`
		Symbols        []string               `json:"symbols"`
		TotalCapital   float64                `json:"total_capital"`
		SymbolCapitals []SymbolCapitalRequest `json:"symbol_capitals"`
		CapitalMode    string                 `json:"capital_mode"` // total 或 per_symbol
		RiskProfile    string                 `json:"risk_profile"`
		GeminiAPIKey   string                 `json:"gemini_api_key"` // 向后兼容：前端傳入的 API Key
		APIKey         string                 `json:"api_key"`        // 可選，通用 API Key（优先于 gemini_api_key）
		Provider       string                 `json:"provider"`       // 可選，gemini/openai/claude
		Model          string                 `json:"model"`          // 可選
		BaseURL        string                 `json:"base_url"`       // 可選，自定义端点

		// 资產优先重構新增字段
		SymbolAllocations map[string]float64                   `json:"symbol_allocations"`
		StrategySplits    map[string][]config.StrategyInstance `json:"strategy_splits"`
		WithdrawalPolicy  config.WithdrawalPolicy              `json:"withdrawal_policy"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 獲取配置
	if globalConfig == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg := globalConfig
	if cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	// 解析 AI 上游：以全局配置为基底，前端传入字段优先覆盖
	upstream := config.ResolveGlobalAI(cfg)
	if v := req.APIKey; v != "" {
		upstream.APIKey = v
	} else if req.GeminiAPIKey != "" {
		upstream.APIKey = req.GeminiAPIKey
	}
	if req.Provider != "" {
		upstream.Provider = req.Provider
	}
	if req.Model != "" {
		upstream.Model = req.Model
	}
	if req.BaseURL != "" {
		upstream.BaseURL = req.BaseURL
	}

	if upstream.APIKey == "" {
		respondError(c, http.StatusBadRequest, "error.ai_api_key_not_configured")
		return
	}

	// 獲取當前價格
	currentPrices := make(map[string]float64)
	if symbolManagerProvider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 尝試從运行中的交易對獲取價格
		for _, symbol := range req.Symbols {
			rtInterface, exists := symbolManagerProvider.Get(req.Exchange, symbol)
			if exists {
				// 使用反射獲取 PriceMonitor
				rtVal := reflect.ValueOf(rtInterface)
				if rtVal.Kind() == reflect.Ptr {
					rtVal = rtVal.Elem()
				}

				priceMonitorField := rtVal.FieldByName("PriceMonitor")
				if priceMonitorField.IsValid() && !priceMonitorField.IsNil() {
					priceMonitor := priceMonitorField.Interface()
					// 尝試調用 GetLastPrice 方法
					getPriceMethod := reflect.ValueOf(priceMonitor).MethodByName("GetLastPrice")
					if getPriceMethod.IsValid() {
						results := getPriceMethod.Call(nil)
						if len(results) > 0 {
							if price, ok := results[0].Interface().(float64); ok && price > 0 {
								currentPrices[symbol] = price
								continue
							}
						}
					}
				}

				// 如果 PriceMonitor 不可用，尝試從 Exchange 獲取
				exchangeField := rtVal.FieldByName("Exchange")
				if exchangeField.IsValid() && !exchangeField.IsNil() {
					ex := exchangeField.Interface()
					if exchange, ok := ex.(exchange.IExchange); ok {
						if price, err := exchange.GetLatestPrice(ctx, symbol); err == nil && price > 0 {
							currentPrices[symbol] = price
							continue
						}
					}
				}
			}
		}
	}

	// 如果某些币种没有獲取到價格，記錄警告但不阻止继续
	if len(currentPrices) < len(req.Symbols) {
		logger.Warn("⚠️ 部分币种未能獲取到價格，將使用默认值")
	}

	// 轉换 SymbolCapitals 格式
	var symbolCapitals []ai.SymbolCapitalConfig
	for _, sc := range req.SymbolCapitals {
		symbolCapitals = append(symbolCapitals, ai.SymbolCapitalConfig{
			Symbol:  sc.Symbol,
			Capital: sc.Capital,
		})
	}

	// 确定资金模式，默认為 total
	capitalMode := req.CapitalMode
	if capitalMode == "" {
		capitalMode = "total"
	}

	// 調用 Gemini API
	// 創建异步任務
	task := aiTaskManager.CreateTask()

	// 立即返回任務 ID
	c.JSON(http.StatusAccepted, gin.H{
		"task_id": task.TaskID,
		"status":  "pending",
		"message": "任務已創建，正在处理中...",
	})

	// 在后台 goroutine 中執行 AI 配置生成
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		// 更新任務状態為运行中
		aiTaskManager.UpdateTask(task.TaskID, TaskStatusRunning, nil, nil)
		logger.Info("🔄 [AI任務] %s 开始執行", task.TaskID)

		aiClient := ai.NewClientFromUpstream(upstream)

		logger.Info("🔄 [AI任務] %s 調用 AI (provider=%s) 生成配置", task.TaskID, upstream.Provider)
		aiConfig, err := aiClient.GenerateConfig(ctx, &ai.GenerateConfigRequest{
			Exchange:          req.Exchange,
			Symbols:           req.Symbols,
			TotalCapital:      req.TotalCapital,
			SymbolCapitals:    symbolCapitals,
			CapitalMode:       capitalMode,
			RiskProfile:       req.RiskProfile,
			CurrentPrices:     currentPrices,
			SymbolAllocations: req.SymbolAllocations,
			StrategySplits:    req.StrategySplits,
			WithdrawalPolicy:  req.WithdrawalPolicy,
		})

		if err != nil {
			logger.Error("❌ [AI任務] %s 配置生成失败: %v", task.TaskID, err)
			aiTaskManager.UpdateTask(task.TaskID, TaskStatusFailed, nil, err)
			return
		}

		logger.Info("✅ [AI任務] %s Gemini API 返回結果，开始驗证配置", task.TaskID)

		// 计算總资金用於驗证
		totalCapital := req.TotalCapital
		if capitalMode == "per_symbol" && len(symbolCapitals) > 0 {
			totalCapital = 0
			for _, sc := range symbolCapitals {
				totalCapital += sc.Capital
			}
		}

		// 驗证配置
		configService := ai.NewConfigService()
		if err := configService.ValidateAIConfig(aiConfig, totalCapital); err != nil {
			logger.Error("❌ [AI任務] %s 配置驗证失败: %v", task.TaskID, err)
			aiTaskManager.UpdateTask(task.TaskID, TaskStatusFailed, nil, err)
			return
		}

		// 更新任務状態為完成
		logger.Info("✅ [AI任務] %s 配置生成完成，更新任務状態為 completed", task.TaskID)
		aiTaskManager.UpdateTask(task.TaskID, TaskStatusCompleted, aiConfig, nil)
	}()
}

// TaskProvider 任務數據提供者接口
type TaskProvider interface {
	GetAsyncTasks(ctx context.Context, filter *database.AsyncTaskFilter) ([]*database.AsyncTask, error)
	GetAsyncTaskStats(ctx context.Context, startTime, endTime *time.Time) (*database.AsyncTaskStats, error)
}

var taskProvider TaskProvider

// SetTaskProvider 設置任務提供者
func SetTaskProvider(provider TaskProvider) {
	taskProvider = provider
}

// getAITasks 獲取 AI 任務列表
// GET /api/ai/tasks
func getAITasks(c *gin.Context) {
	if taskProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.task_service_unavailable")
		return
	}

	// 解析查詢参數
	filter := &database.AsyncTaskFilter{
		Status:   c.Query("status"),
		TaskType: c.Query("task_type"),
	}

	// 解析時间範圍
	if startTimeStr := c.Query("start_time"); startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			filter.StartTime = &t
		}
	}
	if endTimeStr := c.Query("end_time"); endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			filter.EndTime = &t
		}
	}

	// 解析分页参數
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	filter.Limit = limit
	filter.Offset = offset

	// 查詢任務
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tasks, err := taskProvider.GetAsyncTasks(ctx, filter)
	if err != nil {
		logger.Error("❌ 查詢任務失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.query_tasks_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"tasks": tasks,
		"count": len(tasks),
	})
}

// getAITaskStats 獲取 AI 任務统计
// GET /api/ai/tasks/stats
func getAITaskStats(c *gin.Context) {
	if taskProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.task_service_unavailable")
		return
	}

	// 解析時间範圍（可選）
	var startTime, endTime *time.Time
	if startTimeStr := c.Query("start_time"); startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			startTime = &t
		}
	}
	if endTimeStr := c.Query("end_time"); endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			endTime = &t
		}
	}

	// 查詢统计
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := taskProvider.GetAsyncTaskStats(ctx, startTime, endTime)
	if err != nil {
		logger.Error("❌ 查詢任務统计失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.query_task_stats_failed", err)
		return
	}

	c.JSON(http.StatusOK, stats)
}

// getAITaskStatus 獲取 AI 任務状態
// GET /api/ai/task/:task_id
func getAITaskStatus(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		respondError(c, http.StatusBadRequest, "error.missing_task_id")
		return
	}

	task, ok := aiTaskManager.GetTask(taskID)
	if !ok {
		respondError(c, http.StatusNotFound, "error.task_not_found")
		return
	}

	response := gin.H{
		"task_id":    task.TaskID,
		"status":     string(task.Status),
		"progress":   task.Progress,
		"created_at": task.CreatedAt.Format(time.RFC3339),
		"updated_at": task.UpdatedAt.Format(time.RFC3339),
	}

	if task.Status == TaskStatusCompleted && task.Result != nil {
		response["result"] = task.Result
		logger.Debug("📊 [AI任務] %s 返回完成状態，包含結果", taskID)
	} else {
		logger.Debug("📊 [AI任務] %s 當前状態: %s, 進度: %d%%, 有結果: %v",
			taskID, task.Status, task.Progress, task.Result != nil)
	}

	if task.Status == TaskStatusFailed && task.Error != "" {
		response["error"] = task.Error
	}

	c.JSON(http.StatusOK, response)
}

// applyAIConfig 应用 AI 配置
// POST /api/ai/apply-config
func applyAIConfig(c *gin.Context) {
	var req ai.GenerateConfigResponse

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	if globalConfig == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg := globalConfig
	if cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	configService := ai.NewConfigService()
	if err := configService.ApplyAIConfig(&req, cfg); err != nil {
		logger.Error("❌ 应用 AI 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.apply_config_failed", err)
		return
	}
	if fileConfigManager == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}
	if err := fileConfigManager.UpdateConfigWithBotHistorySource(cfg, "post_ai_apply_config"); err != nil {
		logger.Error("❌ 持久化 AI 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.apply_config_failed", err)
		return
	}
	SetGlobalConfig(cfg)
	if configHotReloader != nil {
		_, _ = configHotReloader.UpdateConfig(cfg)
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "配置已成功应用並寫入主庫",
	})
}
