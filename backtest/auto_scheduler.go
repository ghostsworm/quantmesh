package backtest

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"quantmesh/logger"
)

// AutoBacktestScheduler 自動回測調度器
// 在後台自動運行預計算回測任務，用戶進入頁面時可以直接看到結果
type AutoBacktestScheduler struct {
	mu sync.RWMutex

	// 任務管理器
	taskManager *TaskManager

	// 智能參數服務
	smartParamsService *SmartParamsService

	// 預計算結果緩存 (symbol:strategy -> PrecomputedResult)
	precomputedResults map[string]*PrecomputedResult

	// 配置
	config AutoSchedulerConfig

	// 運行狀態
	running bool
	stopCh  chan struct{}
	wg      sync.WaitGroup
}

// AutoSchedulerConfig 自動調度器配置
type AutoSchedulerConfig struct {
	// 是否啟用自動回測
	Enabled bool

	// 自動回測的交易對列表
	Symbols []SymbolConfig

	// 調度間隔（多久運行一次新的預計算）
	ScheduleInterval time.Duration

	// 預計算結果的有效期
	ResultTTL time.Duration

	// 最大並行任務數
	MaxConcurrentTasks int

	// 默認總資金
	DefaultCapital float64

	// 默認交易所
	DefaultExchange string

	// 默認市場類型
	DefaultMarketType string
}

// SymbolConfig 交易對配置
type SymbolConfig struct {
	Symbol     string   `json:"symbol"`
	Exchange   string   `json:"exchange"`
	MarketType string   `json:"market_type"`
	Strategies []string `json:"strategies"` // 要預計算的策略列表
}

// PrecomputedResult 預計算結果
type PrecomputedResult struct {
	Symbol          string                     `json:"symbol"`
	Exchange        string                     `json:"exchange"`
	MarketType      string                     `json:"market_type"`
	Strategy        string                     `json:"strategy"`
	Recommendation  *SmartParamsRecommendation `json:"recommendation"`
	TaskID          string                     `json:"task_id"`
	TaskStatus      string                     `json:"task_status"`
	Result          *BacktestResult            `json:"result,omitempty"`
	GeneratedAt     time.Time                  `json:"generated_at"`
	CompletedAt     *time.Time                 `json:"completed_at,omitempty"`
	IsReady         bool                       `json:"is_ready"`
	ReasoningReport string                     `json:"reasoning_report,omitempty"`
}

// NewAutoBacktestScheduler 創建自動回測調度器
func NewAutoBacktestScheduler(
	taskManager *TaskManager,
	smartParamsService *SmartParamsService,
	config AutoSchedulerConfig,
) *AutoBacktestScheduler {
	// 設置默認值
	if config.ScheduleInterval == 0 {
		config.ScheduleInterval = 6 * time.Hour // 每6小時運行一次
	}
	if config.ResultTTL == 0 {
		config.ResultTTL = 24 * time.Hour // 結果有效期24小時
	}
	if config.MaxConcurrentTasks == 0 {
		config.MaxConcurrentTasks = 3
	}
	if config.DefaultCapital == 0 {
		config.DefaultCapital = 10000
	}
	if config.DefaultExchange == "" {
		config.DefaultExchange = "binance"
	}
	if config.DefaultMarketType == "" {
		config.DefaultMarketType = "futures"
	}

	// 默認交易對和策略
	if len(config.Symbols) == 0 {
		config.Symbols = []SymbolConfig{
			{Symbol: "BTCUSDT", Exchange: "binance", MarketType: "futures", Strategies: []string{"grid", "dca"}},
			{Symbol: "ETHUSDT", Exchange: "binance", MarketType: "futures", Strategies: []string{"grid", "dca"}},
			{Symbol: "SOLUSDT", Exchange: "binance", MarketType: "futures", Strategies: []string{"grid", "momentum"}},
			{Symbol: "PAXGUSDT", Exchange: "binance", MarketType: "spot", Strategies: []string{"grid", "dca"}},
		}
	}

	return &AutoBacktestScheduler{
		taskManager:        taskManager,
		smartParamsService: smartParamsService,
		precomputedResults: make(map[string]*PrecomputedResult),
		config:             config,
		stopCh:             make(chan struct{}),
	}
}

// Start 啟動自動回測調度器
func (s *AutoBacktestScheduler) Start() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()

	if !s.config.Enabled {
		logger.Info("🔄 自動回測調度器已禁用")
		return
	}

	logger.Info("🚀 啟動自動回測調度器，調度間隔: %v", s.config.ScheduleInterval)

	// 啟動主調度循環
	s.wg.Add(1)
	go s.runScheduler()

	// 立即運行一次預計算
	go s.runPrecompute()
}

// Stop 停止自動回測調度器
func (s *AutoBacktestScheduler) Stop() {
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	s.running = false
	close(s.stopCh)
	s.mu.Unlock()

	s.wg.Wait()
	logger.Info("🛑 自動回測調度器已停止")
}

// runScheduler 運行調度循環
func (s *AutoBacktestScheduler) runScheduler() {
	defer s.wg.Done()

	ticker := time.NewTicker(s.config.ScheduleInterval)
	defer ticker.Stop()

	// 定期更新任務狀態
	statusTicker := time.NewTicker(30 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
			s.runPrecompute()
		case <-statusTicker.C:
			s.updateTaskStatuses()
		}
	}
}

// runPrecompute 運行預計算任務
func (s *AutoBacktestScheduler) runPrecompute() {
	logger.Info("🔄 開始運行自動預計算回測...")

	ctx := context.Background()
	sem := make(chan struct{}, s.config.MaxConcurrentTasks)
	var wg sync.WaitGroup

	for _, symConfig := range s.config.Symbols {
		for _, strategy := range symConfig.Strategies {
			symConfig := symConfig
			strategy := strategy

			// 檢查是否已有有效的預計算結果
			key := s.getCacheKey(symConfig.Symbol, symConfig.Exchange, strategy)
			if s.hasValidResult(key) {
				continue
			}

			wg.Add(1)
			sem <- struct{}{}

			go func() {
				defer wg.Done()
				defer func() { <-sem }()

				s.precomputeOne(ctx, symConfig, strategy)
			}()
		}
	}

	wg.Wait()
	logger.Info("✅ 自動預計算回測完成")
}

// precomputeOne 預計算單個交易對-策略組合
func (s *AutoBacktestScheduler) precomputeOne(ctx context.Context, symConfig SymbolConfig, strategy string) {
	key := s.getCacheKey(symConfig.Symbol, symConfig.Exchange, strategy)
	logger.Info("📊 預計算: %s %s %s", symConfig.Symbol, strategy, symConfig.Exchange)

	// 1. 獲取智能參數推薦
	recommendation, err := s.smartParamsService.GetRecommendation(
		ctx,
		symConfig.Exchange,
		symConfig.MarketType,
		symConfig.Symbol,
		strategy,
		s.config.DefaultCapital,
	)
	if err != nil {
		logger.Error("獲取智能參數推薦失敗 [%s %s]: %v", symConfig.Symbol, strategy, err)
		return
	}

	// 2. 創建預計算結果記錄
	result := &PrecomputedResult{
		Symbol:         symConfig.Symbol,
		Exchange:       symConfig.Exchange,
		MarketType:     symConfig.MarketType,
		Strategy:       strategy,
		Recommendation: recommendation,
		GeneratedAt:    time.Now(),
		TaskStatus:     "pending",
		IsReady:        false,
	}

	// 先保存到緩存
	s.mu.Lock()
	s.precomputedResults[key] = result
	s.mu.Unlock()

	// 3. 創建回測任務
	// 使用最近30天數據進行回測
	endTime := time.Now()
	startTime := endTime.Add(-30 * 24 * time.Hour)

	// 根據交易對預設選擇K線週期
	preset := GetSymbolPreset(symConfig.Symbol)
	interval := preset.RecommendedInterval

	task := &BacktestTask{
		Strategy:     strategy,
		Symbol:       symConfig.Symbol,
		Interval:     interval,
		StartTime:    startTime,
		EndTime:      endTime,
		Params:       recommendation.Params,
		TotalCapital: s.config.DefaultCapital,
	}

	// 標記為自動生成的任務
	if task.Params == nil {
		task.Params = make(map[string]interface{})
	}
	task.Params["_auto_generated"] = true

	if err := s.taskManager.CreateAndRun(task); err != nil {
		logger.Error("創建預計算任務失敗 [%s %s]: %v", symConfig.Symbol, strategy, err)
		result.TaskStatus = "failed"
		return
	}

	// 更新任務 ID
	s.mu.Lock()
	result.TaskID = task.ID
	result.TaskStatus = "running"
	s.mu.Unlock()

	logger.Info("✅ 預計算任務已創建: %s [%s %s]", task.ID, symConfig.Symbol, strategy)
}

// updateTaskStatuses 更新任務狀態
func (s *AutoBacktestScheduler) updateTaskStatuses() {
	s.mu.Lock()
	results := make([]*PrecomputedResult, 0, len(s.precomputedResults))
	for _, r := range s.precomputedResults {
		results = append(results, r)
	}
	s.mu.Unlock()

	store := s.taskManager.GetStore()
	if store == nil {
		return
	}

	for _, result := range results {
		if result.IsReady || result.TaskID == "" {
			continue
		}

		task, err := store.GetBacktestTask(result.TaskID)
		if err != nil {
			continue
		}

		s.mu.Lock()
		result.TaskStatus = task.Status

		if task.Status == "completed" {
			result.IsReady = true
			now := time.Now()
			result.CompletedAt = &now

			// 嘗試加載結果
			if backtestResult, err := s.taskManager.GetResult(task.ID); err == nil {
				result.Result = backtestResult

				// 生成推薦報告
				result.ReasoningReport = s.generateReasoningReport(result)
			}
		} else if task.Status == "failed" {
			result.TaskStatus = "failed"
		}
		s.mu.Unlock()
	}
}

// generateReasoningReport 生成推薦理由報告
func (s *AutoBacktestScheduler) generateReasoningReport(result *PrecomputedResult) string {
	if result.Result == nil || result.Recommendation == nil {
		return ""
	}

	metrics := result.Result.Metrics

	report := fmt.Sprintf(
		"📊 **%s - %s 策略回測結果**\n\n"+
			"**市場分析:**\n%s\n\n"+
			"**回測指標:**\n"+
			"- 總收益率: %.2f%%\n"+
			"- 最大回撤: %.2f%%\n"+
			"- 夏普比率: %.2f\n"+
			"- 總交易次數: %d\n"+
			"- 勝率: %.1f%%\n\n"+
			"**置信度: %.0f%%**\n",
		result.Symbol,
		result.Strategy,
		result.Recommendation.Reasoning,
		metrics.TotalReturn,
		metrics.MaxDrawdown,
		metrics.SharpeRatio,
		metrics.TotalTrades,
		metrics.WinRate,
		result.Recommendation.Confidence,
	)

	// 添加評價
	if metrics.TotalReturn > 10 && metrics.SharpeRatio > 1 {
		report += "\n✅ **推薦使用**: 回測表現良好，參數適合當前市場環境。"
	} else if metrics.TotalReturn > 0 && metrics.SharpeRatio > 0.5 {
		report += "\n⚠️ **謹慎使用**: 回測表現一般，建議根據實際情況調整參數。"
	} else {
		report += "\n❌ **不建議使用**: 回測表現較差，可能需要調整策略或等待更好的市場環境。"
	}

	return report
}

// GetPrecomputedResults 獲取所有預計算結果
func (s *AutoBacktestScheduler) GetPrecomputedResults() []*PrecomputedResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*PrecomputedResult, 0, len(s.precomputedResults))
	for _, r := range s.precomputedResults {
		results = append(results, r)
	}

	// 按置信度排序
	sort.Slice(results, func(i, j int) bool {
		if results[i].Recommendation == nil {
			return false
		}
		if results[j].Recommendation == nil {
			return true
		}
		return results[i].Recommendation.Confidence > results[j].Recommendation.Confidence
	})

	return results
}

// GetPrecomputedResult 獲取特定交易對-策略的預計算結果
func (s *AutoBacktestScheduler) GetPrecomputedResult(symbol, exchange, strategy string) *PrecomputedResult {
	key := s.getCacheKey(symbol, exchange, strategy)

	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.precomputedResults[key]
}

// GetReadyResults 獲取已完成的預計算結果
func (s *AutoBacktestScheduler) GetReadyResults() []*PrecomputedResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*PrecomputedResult, 0)
	for _, r := range s.precomputedResults {
		if r.IsReady {
			results = append(results, r)
		}
	}

	// 按收益率排序
	sort.Slice(results, func(i, j int) bool {
		if results[i].Result == nil {
			return false
		}
		if results[j].Result == nil {
			return true
		}
		return results[i].Result.Metrics.TotalReturn > results[j].Result.Metrics.TotalReturn
	})

	return results
}

// GetResultsBySymbol 按交易對獲取預計算結果
func (s *AutoBacktestScheduler) GetResultsBySymbol(symbol string) []*PrecomputedResult {
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]*PrecomputedResult, 0)
	for _, r := range s.precomputedResults {
		if r.Symbol == symbol {
			results = append(results, r)
		}
	}

	return results
}

// TriggerPrecompute 手動觸發預計算（可用於特定交易對）
func (s *AutoBacktestScheduler) TriggerPrecompute(symbol, exchange, marketType, strategy string) error {
	if s.smartParamsService == nil || s.taskManager == nil {
		return fmt.Errorf("服務未初始化")
	}

	symConfig := SymbolConfig{
		Symbol:     symbol,
		Exchange:   exchange,
		MarketType: marketType,
		Strategies: []string{strategy},
	}

	ctx := context.Background()
	s.precomputeOne(ctx, symConfig, strategy)

	return nil
}

// getCacheKey 生成緩存鍵
func (s *AutoBacktestScheduler) getCacheKey(symbol, exchange, strategy string) string {
	return fmt.Sprintf("%s:%s:%s", symbol, exchange, strategy)
}

// hasValidResult 檢查是否有有效的預計算結果
func (s *AutoBacktestScheduler) hasValidResult(key string) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	result, ok := s.precomputedResults[key]
	if !ok {
		return false
	}

	// 檢查結果是否過期
	if time.Since(result.GeneratedAt) > s.config.ResultTTL {
		return false
	}

	// 如果任務仍在運行中，認為有效
	if result.TaskStatus == "running" || result.TaskStatus == "pending" {
		return true
	}

	// 如果已完成，認為有效
	return result.IsReady
}

// CleanExpiredResults 清理過期結果
func (s *AutoBacktestScheduler) CleanExpiredResults() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for key, result := range s.precomputedResults {
		if now.Sub(result.GeneratedAt) > s.config.ResultTTL {
			delete(s.precomputedResults, key)
		}
	}
}

// GetConfig 獲取調度器配置
func (s *AutoBacktestScheduler) GetConfig() AutoSchedulerConfig {
	return s.config
}

// UpdateConfig 更新調度器配置
func (s *AutoBacktestScheduler) UpdateConfig(config AutoSchedulerConfig) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.config = config
}

// IsRunning 檢查調度器是否正在運行
func (s *AutoBacktestScheduler) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}
