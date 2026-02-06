package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	aiservice "quantmesh/ai/service"
	"quantmesh/exchange"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// ============================================================================
// Market Interpret Task Manager（市场AI解读异步任务）
// ============================================================================

// MarketInterpretTaskStatus 任务状态
type MarketInterpretTaskStatus string

const (
	MITaskPending   MarketInterpretTaskStatus = "pending"
	MITaskRunning   MarketInterpretTaskStatus = "running"
	MITaskCompleted MarketInterpretTaskStatus = "completed"
	MITaskFailed    MarketInterpretTaskStatus = "failed"
)

// MarketInterpretTask 市场解读任务
type MarketInterpretTask struct {
	TaskID    string                    `json:"task_id"`
	Status    MarketInterpretTaskStatus `json:"status"`
	CreatedAt time.Time                 `json:"created_at"`
	UpdatedAt time.Time                 `json:"updated_at"`
	Result    string                    `json:"result,omitempty"`  // AI 解读结果（Markdown 格式）
	Error     string                    `json:"error,omitempty"`
	Progress  int                       `json:"progress"` // 0-100
}

// MarketInterpretTaskManager 市场解读任务管理器
type MarketInterpretTaskManager struct {
	tasks map[string]*MarketInterpretTask
	mu    sync.RWMutex
}

var miTaskManager = &MarketInterpretTaskManager{
	tasks: make(map[string]*MarketInterpretTask),
}

func (m *MarketInterpretTaskManager) CreateTask() *MarketInterpretTask {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID := fmt.Sprintf("mi_%d", time.Now().UnixNano())
	task := &MarketInterpretTask{
		TaskID:    taskID,
		Status:    MITaskPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Progress:  0,
	}
	m.tasks[taskID] = task
	return task
}

func (m *MarketInterpretTaskManager) GetTask(taskID string) (*MarketInterpretTask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[taskID]
	return task, ok
}

func (m *MarketInterpretTaskManager) UpdateTask(taskID string, status MarketInterpretTaskStatus, result string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.tasks[taskID]; ok {
		task.Status = status
		task.UpdatedAt = time.Now()
		if result != "" {
			task.Result = result
			task.Progress = 100
		}
		if err != nil {
			task.Error = err.Error()
		}
		if status == MITaskRunning {
			task.Progress = 30
		}
	}
}

// CleanupOldTasks 清理旧任务（超过 1 小时）
func (m *MarketInterpretTaskManager) CleanupOldTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for taskID, task := range m.tasks {
		if now.Sub(task.CreatedAt) > time.Hour {
			delete(m.tasks, taskID)
		}
	}
}

// ============================================================================
// API Handlers
// ============================================================================

// MarketInterpretRequest 市场解读请求
type MarketInterpretRequest struct {
	PageType string                 `json:"page_type"` // "basis" 或 "funding"
	Symbol   string                 `json:"symbol"`    // 当前选中的交易对
	PageData map[string]interface{} `json:"page_data"` // 当前页面的数据快照
}

// createMarketInterpret 创建市场 AI 解读任务
// POST /api/ai/market-interpret
func createMarketInterpret(c *gin.Context) {
	var req MarketInterpretRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	if req.Symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol")
		return
	}

	// 获取 Gemini API Key
	if configManager == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}

	geminiAPIKey := cfg.AI.GeminiAPIKey
	if geminiAPIKey == "" {
		geminiAPIKey = cfg.AI.APIKey
	}
	if geminiAPIKey == "" {
		respondError(c, http.StatusBadRequest, "error.gemini_api_key_not_configured")
		return
	}

	// 获取交易所提供者
	exchProv := pickExchangeProvider(c)
	if exchProv == nil {
		respondError(c, http.StatusServiceUnavailable, "error.exchange_not_available")
		return
	}

	// 清理旧任务
	miTaskManager.CleanupOldTasks()

	// 创建任务
	task := miTaskManager.CreateTask()

	// 立即返回任务 ID
	c.JSON(http.StatusAccepted, gin.H{
		"task_id": task.TaskID,
		"status":  "pending",
	})

	// 在后台 goroutine 中执行
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		miTaskManager.UpdateTask(task.TaskID, MITaskRunning, "", nil)
		logger.Info("🔄 [市场解读] %s 开始执行 symbol=%s page=%s", task.TaskID, req.Symbol, req.PageType)

		// 1) 获取最近 30 根 1 分钟 K 线
		klines1m, err := exchProv.GetHistoricalKlines(ctx, req.Symbol, "1m", 30)
		if err != nil {
			logger.Error("❌ [市场解读] %s 获取1m K线失败: %v", task.TaskID, err)
			miTaskManager.UpdateTask(task.TaskID, MITaskFailed, "", fmt.Errorf("获取1分钟K线失败: %v", err))
			return
		}

		// 2) 获取最近 16 根 15 分钟 K 线
		klines15m, err := exchProv.GetHistoricalKlines(ctx, req.Symbol, "15m", 16)
		if err != nil {
			logger.Error("❌ [市场解读] %s 获取15m K线失败: %v", task.TaskID, err)
			miTaskManager.UpdateTask(task.TaskID, MITaskFailed, "", fmt.Errorf("获取15分钟K线失败: %v", err))
			return
		}

		// 3) 构建 prompt
		prompt := buildMarketInterpretPrompt(req, klines1m, klines15m)

		// 4) 调用 Gemini API（启用 Google Search）
		aiSvc := aiservice.NewAIService()
		aiReq := aiservice.AIRequest{
			Prompt:           prompt,
			GeminiAPIKey:     geminiAPIKey,
			UseGoogleSearch:  true,
			Model:            "gemini-2.5-flash-preview-05-20",
			ResponseMimeType: "text/plain", // 市场解读需要 Markdown 文本输出
		}

		logger.Info("🔄 [市场解读] %s 调用 Gemini API ...", task.TaskID)
		resp, err := aiSvc.GenerateContent(ctx, aiReq)
		if err != nil {
			logger.Error("❌ [市场解读] %s Gemini API 调用失败: %v", task.TaskID, err)
			miTaskManager.UpdateTask(task.TaskID, MITaskFailed, "", fmt.Errorf("AI 调用失败: %v", err))
			return
		}

		if !resp.Success {
			logger.Error("❌ [市场解读] %s Gemini 返回错误: %s", task.TaskID, resp.Error)
			miTaskManager.UpdateTask(task.TaskID, MITaskFailed, "", fmt.Errorf("AI 返回错误: %s", resp.Error))
			return
		}

		logger.Info("✅ [市场解读] %s 完成，tokens: input=%d output=%d", task.TaskID, resp.InputTokens, resp.OutputTokens)
		miTaskManager.UpdateTask(task.TaskID, MITaskCompleted, resp.Content, nil)
	}()
}

// getMarketInterpretStatus 查询市场解读任务状态
// GET /api/ai/market-interpret/:task_id
func getMarketInterpretStatus(c *gin.Context) {
	taskID := c.Param("task_id")
	if taskID == "" {
		respondError(c, http.StatusBadRequest, "error.missing_task_id")
		return
	}

	task, ok := miTaskManager.GetTask(taskID)
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

	if task.Status == MITaskCompleted && task.Result != "" {
		response["result"] = task.Result
	}
	if task.Status == MITaskFailed && task.Error != "" {
		response["error"] = task.Error
	}

	c.JSON(http.StatusOK, response)
}

// ============================================================================
// Prompt Builder
// ============================================================================

func buildMarketInterpretPrompt(req MarketInterpretRequest, klines1m, klines15m []*exchange.Candle) string {
	var sb strings.Builder

	sb.WriteString("你是一位专业的加密货币市场分析师。请根据以下数据，结合当前市场新闻和全球宏观经济形势（请通过 Google Search 搜索最新信息），给出深入的市场解读和操作建议。\n\n")

	// 页面数据
	if req.PageType == "basis" {
		sb.WriteString("## 当前页面：现货-合约价差监控\n\n")
		sb.WriteString("### 页面数据快照\n")
		if req.PageData != nil {
			if basisList, ok := req.PageData["current_basis"]; ok {
				sb.WriteString(fmt.Sprintf("当前各交易对价差数据: %v\n", basisList))
			}
			if stats, ok := req.PageData["statistics"]; ok {
				sb.WriteString(fmt.Sprintf("统计数据（最近24小时）: %v\n", stats))
			}
		}
		sb.WriteString("\n分析要点：\n")
		sb.WriteString("- 当前现货-合约价差的大小和方向意味着什么？\n")
		sb.WriteString("- 价差和资金费率之间的关系如何？\n")
		sb.WriteString("- 是否存在套利机会？\n")
		sb.WriteString("- 价差的历史趋势反映了什么样的市场情绪？\n\n")
	} else if req.PageType == "funding" {
		sb.WriteString("## 当前页面：资金费率监控\n\n")
		sb.WriteString("### 页面数据快照\n")
		if req.PageData != nil {
			if rates, ok := req.PageData["current_rates"]; ok {
				sb.WriteString(fmt.Sprintf("当前各交易对资金费率: %v\n", rates))
			}
		}
		sb.WriteString("\n分析要点：\n")
		sb.WriteString("- 当前资金费率水平反映了什么样的市场情绪（看多/看空）？\n")
		sb.WriteString("- 哪些交易对的资金费率异常？意味着什么？\n")
		sb.WriteString("- 当前费率对持仓成本的影响如何？\n")
		sb.WriteString("- 是否有基于资金费率的套利机会？\n\n")
	}

	// 选中的交易对
	sb.WriteString(fmt.Sprintf("## 重点分析交易对: %s\n\n", req.Symbol))

	// 1 分钟 K 线
	sb.WriteString("### 最近 30 根 1 分钟 K 线（短期走势）\n")
	sb.WriteString("时间 | 开盘 | 最高 | 最低 | 收盘 | 成交量\n")
	sb.WriteString("--- | --- | --- | --- | --- | ---\n")
	for _, k := range klines1m {
		t := time.UnixMilli(k.Timestamp).Format("15:04:05")
		sb.WriteString(fmt.Sprintf("%s | %.2f | %.2f | %.2f | %.2f | %.4f\n",
			t, k.Open, k.High, k.Low, k.Close, k.Volume))
	}

	// 15 分钟 K 线
	sb.WriteString("\n### 最近 16 根 15 分钟 K 线（中期走势）\n")
	sb.WriteString("时间 | 开盘 | 最高 | 最低 | 收盘 | 成交量\n")
	sb.WriteString("--- | --- | --- | --- | --- | ---\n")
	for _, k := range klines15m {
		t := time.UnixMilli(k.Timestamp).Format("2006-01-02 15:04")
		sb.WriteString(fmt.Sprintf("%s | %.2f | %.2f | %.2f | %.2f | %.4f\n",
			t, k.Open, k.High, k.Low, k.Close, k.Volume))
	}

	// 要求
	sb.WriteString("\n## 请输出以下内容\n\n")
	sb.WriteString("请用 Markdown 格式输出，不要输出 JSON，直接输出可读的分析报告。\n\n")
	sb.WriteString("1. **市场概况**：结合最新新闻和宏观环境，概述当前市场状况\n")
	sb.WriteString("2. **技术面分析**：根据 K 线数据分析短期和中期走势，关键支撑位和阻力位\n")
	if req.PageType == "basis" {
		sb.WriteString("3. **价差分析**：解读当前现货-合约价差和资金费率的含义\n")
	} else {
		sb.WriteString("3. **资金费率分析**：解读当前各交易对资金费率的含义\n")
	}
	sb.WriteString("4. **风险提示**：需要注意的风险因素\n")
	sb.WriteString("5. **操作建议**：针对网格交易 / DCA / 套利等策略的具体建议\n\n")
	sb.WriteString("请用简体中文回答，语言简洁专业。\n")

	return sb.String()
}
