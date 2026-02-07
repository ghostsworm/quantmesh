package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/database"
	"quantmesh/exchange"
	qmi18n "quantmesh/i18n"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
	ordersync "quantmesh/sync"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// respondError 返回翻譯后的錯误响应
func respondError(c *gin.Context, status int, messageKey string, args ...interface{}) {
	lang := GetLanguage(c)

	var data map[string]interface{}
	var errObj error

	// 解析参數
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			errObj = err
		} else if m, ok := arg.(map[string]interface{}); ok {
			data = m
		}
	}

	// 翻譯錯误消息
	message := qmi18n.TWithLang(lang, messageKey, data)

	// 如果有實際的錯误對象，添加详细信息（僅在开发模式）
	if errObj != nil && status >= 500 {
		// 在生產环境可能需要隐藏详细錯误信息
		message = fmt.Sprintf("%s: %v", message, errObj)
	}

	c.JSON(status, gin.H{"error": message})
}

// SystemStatus 系统状態
type SystemStatus struct {
	Running       bool    `json:"running"`
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	CurrentPrice  float64 `json:"current_price"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	RiskTriggered bool    `json:"risk_triggered"`
	Uptime        int64   `json:"uptime"` // 运行時间（秒）
}

var (
	// 全局状態（需要從 main.go 注入）
	currentStatus *SystemStatus
	// 多交易對状態（key: exchange:symbol）
	statusBySymbol   = make(map[string]*SystemStatus)
	defaultSymbolKey string
	// 保护 statusBySymbol 的读写鎖
	statusMu sync.RWMutex
	// 版本号（需要從 main.go 注入）
	appVersion string
)

// AITaskStatus AI 任務状態
type AITaskStatus string

const (
	TaskStatusPending   AITaskStatus = "pending"
	TaskStatusRunning   AITaskStatus = "running"
	TaskStatusCompleted AITaskStatus = "completed"
	TaskStatusFailed    AITaskStatus = "failed"
)

// AITask AI 任務信息
type AITask struct {
	TaskID    string                     `json:"task_id"`
	Status    AITaskStatus               `json:"status"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Result    *ai.GenerateConfigResponse `json:"result,omitempty"`
	Error     string                     `json:"error,omitempty"`
	Progress  int                        `json:"progress"` // 0-100
}

// AITaskManager AI 任務管理器
type AITaskManager struct {
	tasks map[string]*AITask
	mu    sync.RWMutex
}

var aiTaskManager = &AITaskManager{
	tasks: make(map[string]*AITask),
}

// CreateTask 創建新任務
func (m *AITaskManager) CreateTask() *AITask {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	task := &AITask{
		TaskID:    taskID,
		Status:    TaskStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Progress:  0,
	}
	m.tasks[taskID] = task
	return task
}

// GetTask 獲取任務
func (m *AITaskManager) GetTask(taskID string) (*AITask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[taskID]
	return task, ok
}

// UpdateTask 更新任務状態
func (m *AITaskManager) UpdateTask(taskID string, status AITaskStatus, result *ai.GenerateConfigResponse, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.tasks[taskID]; ok {
		task.Status = status
		task.UpdatedAt = time.Now()
		if result != nil {
			task.Result = result
			task.Progress = 100
		}
		if err != nil {
			task.Error = err.Error()
		}
		if status == TaskStatusRunning {
			task.Progress = 50 // 运行中設置為 50%
		}
	}
}

// CleanupOldTasks 清理舊任務（超過1小時）
func (m *AITaskManager) CleanupOldTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for taskID, task := range m.tasks {
		if now.Sub(task.CreatedAt) > time.Hour {
			delete(m.tasks, taskID)
		}
	}
}

// SymbolScopedProviders 组合一個交易對的所有依赖
type SymbolScopedProviders struct {
	Status   *SystemStatus
	Price    PriceProvider
	Exchange ExchangeProvider
	Position PositionManagerProvider
	Risk     RiskMonitorProvider
	Storage  StorageServiceProvider
	Funding  FundingMonitorProvider
}

func makeSymbolKey(exchange, symbol string) string {
	return strings.ToLower(fmt.Sprintf("%s:%s", exchange, symbol))
}

// SetStatusProvider 設置状態提供者
func SetStatusProvider(status *SystemStatus) {
	currentStatus = status
}

// SetVersion 設置版本号
func SetVersion(version string) {
	appVersion = version
}

// RegisterSymbolProviders 注册單個交易對的提供者集合
func RegisterSymbolProviders(exchange, symbol string, providers *SymbolScopedProviders) {
	if providers == nil {
		return
	}
	key := makeSymbolKey(exchange, symbol)

	logger.Info("[DEBUG] RegisterSymbolProviders - registering key=%s, hasPosition=%v, hasPrice=%v",
		key, providers.Position != nil, providers.Price != nil)

	// 使用写鎖保护並发写入
	statusMu.Lock()
	statusBySymbol[key] = providers.Status
	statusMu.Unlock()

	providersMu.Lock()
	if providers.Price != nil {
		priceProviders[key] = providers.Price
		logger.Info("[DEBUG] RegisterSymbolProviders - registered price provider for key=%s", key)
	}
	if providers.Exchange != nil {
		exchangeProviders[key] = providers.Exchange
	}
	if providers.Position != nil {
		positionProviders[key] = providers.Position
		logger.Info("[DEBUG] RegisterSymbolProviders - registered position provider for key=%s", key)
	}
	if providers.Risk != nil {
		riskProviders[key] = providers.Risk
	}
	if providers.Storage != nil {
		storageProviders[key] = providers.Storage
	}
	if providers.Funding != nil {
		fundingProviders[key] = providers.Funding
	}
	providersMu.Unlock()
}

// RegisterFundingProvider 單独注册资金费率提供者
func RegisterFundingProvider(exchange, symbol string, provider FundingMonitorProvider) {
	if provider == nil {
		return
	}
	key := makeSymbolKey(exchange, symbol)

	// 使用写鎖保护並发写入
	providersMu.Lock()
	fundingProviders[key] = provider
	providersMu.Unlock()
}

// SetDefaultSymbolKey 設置默认交易對（兼容舊接口）
func SetDefaultSymbolKey(exchange, symbol string) {
	defaultSymbolKey = makeSymbolKey(exchange, symbol)
}

// resolveSymbolKey 根據查詢参數獲取 key
func resolveSymbolKey(c *gin.Context) string {
	ex := c.Query("exchange")
	sym := c.Query("symbol")
	if ex != "" && sym != "" {
		key := makeSymbolKey(ex, sym)
		logger.Info("[DEBUG] resolveSymbolKey - ex=%s, sym=%s, key=%s", ex, sym, key)
		return key
	}
	logger.Info("[DEBUG] resolveSymbolKey - no params, returning defaultSymbolKey=%s", defaultSymbolKey)
	return defaultSymbolKey
}

// === Provider 映射 ===
var (
	priceProviders    = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders     = make(map[string]RiskMonitorProvider)
	storageProviders  = make(map[string]StorageServiceProvider)
	fundingProviders  = make(map[string]FundingMonitorProvider)
	// 保护所有 provider 映射的读写鎖
	providersMu sync.RWMutex
)

func pickStatus(c *gin.Context) *SystemStatus {
	if key := resolveSymbolKey(c); key != "" {
		statusMu.RLock()
		st, ok := statusBySymbol[key]
		statusMu.RUnlock()
		if ok && st != nil {
			return st
		}
	}
	return currentStatus
}

func PickPriceProvider(c *gin.Context) PriceProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := priceProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			logger.Info("[DEBUG] PickPriceProvider - found provider for key=%s", key)
			return p
		}
		logger.Warn("⚠️ [PickPriceProvider] no provider found for key=%s, falling back to default", key)
	}
	logger.Info("[DEBUG] PickPriceProvider - using default priceProvider")
	return priceProvider
}

func pickExchangeProvider(c *gin.Context) ExchangeProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := exchangeProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return exchangeProvider
}

func PickPositionProvider(c *gin.Context) PositionManagerProvider {
	key := resolveSymbolKey(c)
	logger.Info("[DEBUG] PickPositionProvider - resolvedKey=%s", key)

	if key != "" {
		providersMu.RLock()
		p, ok := positionProviders[key]
		providersMu.RUnlock()

		logger.Info("[DEBUG] PickPositionProvider - found in map: %v, provider!=nil: %v", ok, p != nil)

		if ok && p != nil {
			return p
		}
	}

	logger.Info("[DEBUG] PickPositionProvider - returning default provider")
	return positionManagerProvider
}

func PickRiskProvider(c *gin.Context) RiskMonitorProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := riskProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return riskMonitorProvider
}

func PickStorageProvider(c *gin.Context) StorageServiceProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := storageProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return storageServiceProvider
}

func PickFundingProvider(c *gin.Context) FundingMonitorProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := fundingProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return fundingMonitorProvider
}

func getStatus(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	// 如果指定了 exchange 和 symbol，尝試獲取對应的状態
	if exchange != "" && symbol != "" {
		key := makeSymbolKey(exchange, symbol)
		statusMu.RLock()
		st, ok := statusBySymbol[key]
		statusMu.RUnlock()

		if ok && st != nil {
			// 找到了运行中的状態
			c.JSON(http.StatusOK, st)
			return
		}

		// 没有找到运行中的状態，检查配置中是否有這個币种
		if configManager != nil {
			cfg, err := configManager.GetConfig()
			if err == nil && cfg != nil {
				// 检查配置中是否有這個币种
				for _, symCfg := range cfg.Trading.Symbols {
					if strings.EqualFold(symCfg.Exchange, exchange) &&
						strings.EqualFold(symCfg.Symbol, symbol) {
						// 配置中存在但未运行，返回正确的状態信息
						c.JSON(http.StatusOK, &SystemStatus{
							Running:       false,
							Exchange:      exchange,
							Symbol:        symbol,
							CurrentPrice:  0,
							TotalPnL:      0,
							TotalTrades:   0,
							RiskTriggered: false,
							Uptime:        0,
						})
						return
					}
				}
			}
		}

		// 配置中也没有找到，返回未运行状態（但包含请求的 exchange 和 symbol）
		c.JSON(http.StatusOK, &SystemStatus{
			Running:       false,
			Exchange:      exchange,
			Symbol:        symbol,
			CurrentPrice:  0,
			TotalPnL:      0,
			TotalTrades:   0,
			RiskTriggered: false,
			Uptime:        0,
		})
		return
	}

	// 没有指定 exchange 和 symbol，使用原来的逻辑
	status := pickStatus(c)
	if status == nil {
		c.JSON(http.StatusOK, &SystemStatus{
			Running: false,
		})
		return
	}
	c.JSON(http.StatusOK, status)
}

// StatusesResponse 批量状態响应
type StatusesResponse struct {
	Statuses []SystemStatus `json:"statuses"`
}

// getStatuses 批量返回所有交易對的系统状態（用於概览页一次拉取）
// GET /api/statuses
func getStatuses(c *gin.Context) {
	// 使用 map 做去重，key 為 exchange:symbol（小写）
	statusMap := make(map[string]SystemStatus)

	// 1) 先從配置中構造“未运行”的默认状態，确保列表完整
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			for _, sym := range cfg.Trading.Symbols {
				if sym.Symbol == "" {
					continue
				}
				ex := sym.Exchange
				if ex == "" {
					ex = cfg.App.CurrentExchange
				}
				if ex == "" {
					continue
				}
				key := strings.ToLower(fmt.Sprintf("%s:%s", ex, sym.Symbol))
				if _, exists := statusMap[key]; !exists {
					statusMap[key] = SystemStatus{
						Running:       false,
						Exchange:      strings.ToLower(ex),
						Symbol:        sym.Symbol,
						CurrentPrice:  0,
						TotalPnL:      0,
						TotalTrades:   0,
						RiskTriggered: false,
						Uptime:        0,
					}
				}
			}

			// 兼容舊的單交易對配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" && cfg.App.CurrentExchange != "" {
				key := strings.ToLower(fmt.Sprintf("%s:%s", cfg.App.CurrentExchange, cfg.Trading.Symbol))
				if _, exists := statusMap[key]; !exists {
					statusMap[key] = SystemStatus{
						Running:       false,
						Exchange:      strings.ToLower(cfg.App.CurrentExchange),
						Symbol:        cfg.Trading.Symbol,
						CurrentPrice:  0,
						TotalPnL:      0,
						TotalTrades:   0,
						RiskTriggered: false,
						Uptime:        0,
					}
				}
			}
		}
	}

	// 2) 再用运行中状態覆盖（複制一份，避免並发读写數據竞争）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st == nil {
			continue
		}
		copySt := *st
		copySt.Exchange = strings.ToLower(copySt.Exchange)
		key := strings.ToLower(fmt.Sprintf("%s:%s", copySt.Exchange, copySt.Symbol))
		statusMap[key] = copySt
	}
	statusMu.RUnlock()

	// 3) 向后兼容：如果没有多交易對數據，使用舊的單交易對状態
	if len(statusMap) == 0 && currentStatus != nil {
		statusMu.RLock()
		copySt := *currentStatus
		statusMu.RUnlock()
		copySt.Exchange = strings.ToLower(copySt.Exchange)
		key := strings.ToLower(fmt.Sprintf("%s:%s", copySt.Exchange, copySt.Symbol))
		statusMap[key] = copySt
	}

	// 4) 轉為 slice 並排序
	statuses := make([]SystemStatus, 0, len(statusMap))
	for _, st := range statusMap {
		statuses = append(statuses, st)
	}
	sort.Slice(statuses, func(i, j int) bool {
		if statuses[i].Exchange == statuses[j].Exchange {
			return strings.ToLower(statuses[i].Symbol) < strings.ToLower(statuses[j].Symbol)
		}
		return statuses[i].Exchange < statuses[j].Exchange
	})

	c.JSON(http.StatusOK, StatusesResponse{Statuses: statuses})
}

// SymbolItem 用於返回可用的交易所/交易對列表
type SymbolItem struct {
	Exchange     string  `json:"exchange"`
	Symbol       string  `json:"symbol"`
	IsActive     bool    `json:"is_active"`
	CurrentPrice float64 `json:"current_price"`
	MarketType   string  `json:"market_type,omitempty"` // 市場類型：spot/futures
	Direction    string  `json:"direction,omitempty"`   // 交易方向：LONG/SHORT，預設 LONG
}

// getSymbols 返回可用的交易對列表
func getSymbols(c *gin.Context) {
	// 使用 map 来去重，key 為 exchange:symbol
	symbolMap := make(map[string]*SymbolItem)
	activeList := make([]SymbolItem, 0)
	inactiveList := make([]SymbolItem, 0)

	// 首先從配置文件中读取所有配置的交易對
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			// 從交易對配置中读取
			for _, sym := range cfg.Trading.Symbols {
				if sym.Symbol == "" {
					continue
				}
				exchange := sym.Exchange
				if exchange == "" {
					exchange = cfg.App.CurrentExchange
				}
				if exchange == "" {
					continue
				}
				key := strings.ToLower(fmt.Sprintf("%s:%s", exchange, sym.Symbol))
				if _, exists := symbolMap[key]; !exists {
					marketType := sym.MarketType
					if marketType == "" {
						marketType = "futures" // 默认合約
					}
					symbolMap[key] = &SymbolItem{
						Exchange:     strings.ToLower(exchange),
						Symbol:       sym.Symbol,
						IsActive:     false, // 默认未运行，后面會更新
						CurrentPrice: 0,
						MarketType:   marketType,
						Direction:    sym.GetDirection(),
					}
				}
			}
			// 如果只有單交易對配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
				exchange := cfg.App.CurrentExchange
				if exchange != "" {
					key := strings.ToLower(fmt.Sprintf("%s:%s", exchange, cfg.Trading.Symbol))
					if _, exists := symbolMap[key]; !exists {
						direction := cfg.Trading.Direction
						if direction != "SHORT" {
							direction = "LONG"
						}
						symbolMap[key] = &SymbolItem{
							Exchange:     strings.ToLower(exchange),
							Symbol:       cfg.Trading.Symbol,
							IsActive:     false,
							CurrentPrice: 0,
							MarketType:   "futures", // 舊版單交易對配置默認為合約
							Direction:    direction,
						}
					}
				}
			}
		}
	}

	// 然后從运行状態中更新（确保正在运行的交易對状態正确）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st == nil {
			continue
		}
		key := strings.ToLower(fmt.Sprintf("%s:%s", st.Exchange, st.Symbol))
		if item, exists := symbolMap[key]; exists {
			// 更新已存在的交易對状態
			item.IsActive = st.Running
			item.CurrentPrice = st.CurrentPrice
		} else {
			// 添加新的运行中的交易對
			symbolMap[key] = &SymbolItem{
				Exchange:     strings.ToLower(st.Exchange),
				Symbol:       st.Symbol,
				IsActive:     st.Running,
				CurrentPrice: st.CurrentPrice,
				MarketType:   "futures", // 運行中的交易對默認合約（無法動態檢測）
			}
		}
	}
	statusMu.RUnlock()

	// 向后兼容：如果没有多交易對數據，使用舊的單交易對状態
	if len(symbolMap) == 0 && currentStatus != nil {
		key := strings.ToLower(fmt.Sprintf("%s:%s", currentStatus.Exchange, currentStatus.Symbol))
		symbolMap[key] = &SymbolItem{
			Exchange:     strings.ToLower(currentStatus.Exchange),
			Symbol:       currentStatus.Symbol,
			IsActive:     currentStatus.Running,
			CurrentPrice: currentStatus.CurrentPrice,
		}
	}

	// 轉换為列表並分组
	for _, item := range symbolMap {
		if item.IsActive {
			activeList = append(activeList, *item)
		} else {
			inactiveList = append(inactiveList, *item)
		}
	}

	// 活跃的交易對排在前面
	list := make([]SymbolItem, 0)
	list = append(list, activeList...)
	list = append(list, inactiveList...)

	c.JSON(http.StatusOK, gin.H{"symbols": list})
}

// getVersion 返回版本号（不需要认证）
func getVersion(c *gin.Context) {
	version := appVersion
	if version == "" {
		version = "unknown"
	}
	c.JSON(http.StatusOK, gin.H{"version": version})
}

// versionHeaderMiddleware 在响应头中設置 X-App-Version，便於排查
func versionHeaderMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		v := appVersion
		if v == "" {
			v = "unknown"
		}
		c.Header("X-App-Version", v)
		c.Next()
	}
}

// getExchanges 返回所有配置的交易所列表
func getExchanges(c *gin.Context) {
	exchangeSet := make(map[string]bool)

	// 首先從配置文件中读取所有配置的交易所
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			// 從配置的 exchanges 中读取
			for ex := range cfg.Exchanges {
				if ex != "" {
					exchangeSet[strings.ToLower(ex)] = true
				}
			}
			// 從交易對配置中读取交易所
			for _, sym := range cfg.Trading.Symbols {
				if sym.Exchange != "" {
					exchangeSet[strings.ToLower(sym.Exchange)] = true
				} else if cfg.App.CurrentExchange != "" {
					exchangeSet[strings.ToLower(cfg.App.CurrentExchange)] = true
				}
			}
			// 如果只有單交易對配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
				if cfg.App.CurrentExchange != "" {
					exchangeSet[strings.ToLower(cfg.App.CurrentExchange)] = true
				}
			}
		}
	}

	// 然后從运行状態中读取（确保正在运行的交易所也在列表中）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st == nil {
			continue
		}
		exchangeSet[strings.ToLower(st.Exchange)] = true
	}
	statusMu.RUnlock()

	// 向后兼容
	if len(exchangeSet) == 0 && currentStatus != nil {
		exchangeSet[strings.ToLower(currentStatus.Exchange)] = true
	}

	exchanges := make([]string, 0, len(exchangeSet))
	for ex := range exchangeSet {
		exchanges = append(exchanges, ex)
	}

	// 排序交易所列表（可選，但有助於一致性）
	sort.Strings(exchanges)

	c.JSON(http.StatusOK, gin.H{"exchanges": exchanges})
}

// PositionSummary 持倉彙總信息
type PositionSummary struct {
	TotalQuantity float64        `json:"total_quantity"` // 總持倉數量
	TotalValue    float64        `json:"total_value"`    // 總持倉價值（當前價格 * 數量）
	PositionCount int            `json:"position_count"` // 持倉槽位數
	AveragePrice  float64        `json:"average_price"`  // 平均持倉價格
	CurrentPrice  float64        `json:"current_price"`  // 當前市場價格
	UnrealizedPnL float64        `json:"unrealized_pnl"` // 未實現盈亏
	PnlPercentage float64        `json:"pnl_percentage"` // 盈亏百分比
	ActualMargin  float64        `json:"actual_margin"`  // 實際资金占用（實際保证金）
	Leverage      int            `json:"leverage"`       // 杠杆倍數
	Positions     []PositionInfo `json:"positions"`      // 持倉列表
}

// PositionInfo 單個持倉資訊
type PositionInfo struct {
	Price         float64 `json:"price"`          // 持倉價格
	Quantity      float64 `json:"quantity"`       // 持倉數量
	Value         float64 `json:"value"`          // 持倉價值
	UnrealizedPnL float64 `json:"unrealized_pnl"` // 未實現盈亏
}

var (
	// 價格提供者（需要從main.go注入）
	priceProvider PriceProvider
)

// PriceProvider 價格提供者接口
type PriceProvider interface {
	GetLastPrice() float64
}

// SetPriceProvider 設置價格提供者
func SetPriceProvider(provider PriceProvider) {
	priceProvider = provider
}

var (
	// 交易所提供者（需要從main.go注入）
	exchangeProvider ExchangeProvider
	// 按交易所 ID 獲取 IExchange（用於利润提取內部轉帳，由 main 注入）
	exchangeGetterFunc func(exchangeID string) exchange.IExchange
)

// SetExchangeGetter 設置按交易所 ID 獲取交易所實例的函數（供利润提取 API 調用 InternalTransfer）
func SetExchangeGetter(f func(exchangeID string) exchange.IExchange) {
	exchangeGetterFunc = f
}

// ExchangeProvider 交易所提供者接口
type ExchangeProvider interface {
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error)
	GetFundingRate(ctx context.Context, symbol string) (float64, error)
	// GetPositions 獲取交易所真實持倉資訊
	GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error)
}

// SetExchangeProvider 設置交易所提供者
func SetExchangeProvider(provider ExchangeProvider) {
	exchangeProvider = provider
}

// getPositions 獲取持倉列表（從槽位數據筛选）
func getPositions(c *gin.Context) {
	// 調試：記錄接收到的参數
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	resolvedKey := resolveSymbolKey(c)
	logger.Info("[DEBUG] getPositions called - exchange=%s, symbol=%s, resolvedKey=%s", exchange, symbol, resolvedKey)

	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)

	if pmProvider == nil {
		// 與有 provider 時保持同一響應結構，避免前端報 "Invalid response format"
		c.JSON(http.StatusOK, gin.H{
			"summary": PositionSummary{
				TotalQuantity: 0,
				TotalValue:    0,
				PositionCount: 0,
				AveragePrice:  0,
				CurrentPrice:  0,
				UnrealizedPnL: 0,
				PnlPercentage: 0,
				ActualMargin:  0,
				Leverage:      1,
				Positions:     []PositionInfo{},
			},
		})
		return
	}

	slots := pmProvider.GetAllSlots()
	logger.Info("[DEBUG] getPositions - got %d slots for key=%s", len(slots), resolvedKey)
	var positions []PositionInfo
	currentPrice := 0.0
	if priceProv != nil {
		currentPrice = priceProv.GetLastPrice()
		logger.Info("[DEBUG] getPositions - [%s:%s] resolvedKey=%s, priceProvider!=nil, currentPrice=%.2f",
			exchange, symbol, resolvedKey, currentPrice)
	} else {
		logger.Warn("⚠️ [getPositions] [%s:%s] resolvedKey=%s, priceProvider is nil!",
			exchange, symbol, resolvedKey)
	}

	totalQuantity := 0.0
	totalValue := 0.0
	positionCount := 0

	// 筛选有持倉的槽位
	for _, slot := range slots {
		// 🔥 新增價格驗证：确保槽位價格有效（大於0且合理）
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
			// 🔥 價格合理性检查：如果當前價格可用，检查槽位價格是否在合理範圍内
			if currentPrice > 0 {
				priceRatio := slot.Price / currentPrice
				// 如果槽位價格是當前價格的100倍以上或0.01倍以下，可能是單位錯误
				if priceRatio > 100 || priceRatio < 0.01 {
					logger.Warn("⚠️ [getPositions] [%s:%s] 检测到异常槽位價格: slotPrice=%.2f, currentPrice=%.2f, 比例=%.2f, 數量=%.4f, resolvedKey=%s",
						exchange, symbol, slot.Price, currentPrice, priceRatio, slot.PositionQty, resolvedKey)
					// 继续处理，但記錄警告
				}
			}

			positionCount++
			totalQuantity += slot.PositionQty

			// 计算持倉價值（使用當前價格）
			value := slot.PositionQty * currentPrice
			if currentPrice == 0 {
				// 如果當前價格不可用，使用持倉價格
				value = slot.PositionQty * slot.Price
			}
			totalValue += value

			// 计算未實現盈亏
			unrealizedPnL := 0.0
			if currentPrice > 0 && slot.Price > 0 {
				// 🔥 新增價格合理性检查：如果當前價格相對於持倉價格偏差過大，可能是價格异常
				priceDeviation := (currentPrice - slot.Price) / slot.Price

				// 检查是否是單位问题（比如當前價格是持倉價格的100倍或0.01倍）
				priceRatio := currentPrice / slot.Price
				adjustedCurrentPrice := currentPrice
				if priceRatio > 50 {
					// 當前價格可能是持倉價格的100倍，尝試除以100
					adjustedPrice := currentPrice / 100
					if math.Abs(adjustedPrice-slot.Price)/slot.Price < 0.1 {
						logger.Warn("⚠️ [getPositions] [%s:%s] 检测到價格單位问题（當前價格可能是持倉價格的100倍），已自动修正: %.2f -> %.2f",
							exchange, symbol, currentPrice, adjustedPrice)
						adjustedCurrentPrice = adjustedPrice
					}
				} else if priceRatio < 0.02 {
					// 當前價格可能是持倉價格的0.01倍，尝試乘以100
					adjustedPrice := currentPrice * 100
					if math.Abs(adjustedPrice-slot.Price)/slot.Price < 0.1 {
						logger.Warn("⚠️ [getPositions] [%s:%s] 检测到價格單位问题（當前價格可能是持倉價格的0.01倍），已自动修正: %.2f -> %.2f",
							exchange, symbol, currentPrice, adjustedPrice)
						adjustedCurrentPrice = adjustedPrice
					}
				}

				// 重新计算價格偏差
				priceDeviation = (adjustedCurrentPrice - slot.Price) / slot.Price
				if priceDeviation > 0.5 || priceDeviation < -0.5 {
					// 價格偏差仍然過大，使用持倉價格（未實現盈亏為0）
					logger.Warn("⚠️ [getPositions] [%s:%s] 價格偏差過大，使用持倉價格计算（未實現盈亏設為0）: currentPrice=%.2f, slotPrice=%.2f, 偏差=%.2f%%, resolvedKey=%s",
						exchange, symbol, adjustedCurrentPrice, slot.Price, priceDeviation*100, resolvedKey)
					adjustedCurrentPrice = slot.Price
				}

				unrealizedPnL = (adjustedCurrentPrice - slot.Price) * slot.PositionQty
			}

			positions = append(positions, PositionInfo{
				Price:         slot.Price,
				Quantity:      slot.PositionQty,
				Value:         value,
				UnrealizedPnL: unrealizedPnL,
			})
		}
	}

	// 计算平均持倉價格
	averagePrice := 0.0
	if totalQuantity > 0 {
		totalCost := 0.0
		for _, pos := range positions {
			totalCost += pos.Price * pos.Quantity
		}
		averagePrice = totalCost / totalQuantity
	}

	// 计算總未實現盈亏
	totalUnrealizedPnL := 0.0
	if currentPrice > 0 {
		for _, pos := range positions {
			totalUnrealizedPnL += pos.UnrealizedPnL
		}
	}

	// 计算總持倉成本
	totalCost := 0.0
	for _, pos := range positions {
		totalCost += pos.Price * pos.Quantity
	}

	// 计算亏损率（相對於持倉成本的百分比）
	pnlPercentage := 0.0
	if totalCost > 0 {
		pnlPercentage = (totalUnrealizedPnL / totalCost) * 100.0
	}

	// 计算實際资金占用（實際保证金 = 總持倉價值 / 杠杆倍數）
	leverage := 1 // 默认1倍（無杠杆）
	if pmProvider != nil {
		leverage = pmProvider.GetLeverage()
	}
	actualMargin := 0.0
	if leverage > 0 && totalValue > 0 {
		actualMargin = totalValue / float64(leverage)
	}

	summary := PositionSummary{
		TotalQuantity: totalQuantity,
		TotalValue:    totalValue,
		PositionCount: positionCount,
		AveragePrice:  averagePrice,
		CurrentPrice:  currentPrice,
		UnrealizedPnL: totalUnrealizedPnL,
		PnlPercentage: pnlPercentage,
		ActualMargin:  actualMargin,
		Leverage:      leverage,
		Positions:     positions,
	}

	// 調試：在响应中包含请求的交易對信息
	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"_debug": gin.H{
			"exchange":    exchange,
			"symbol":      symbol,
			"resolvedKey": resolvedKey,
			"slotCount":   len(slots),
		},
	})
}

// getPositionsSummary 獲取持倉彙總
// GET /api/positions/summary
func getPositionsSummary(c *gin.Context) {
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)
	exchProv := pickExchangeProvider(c)

	// 獲取请求参數
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		if k := resolveSymbolKey(c); k != "" {
			parts := strings.SplitN(k, ":", 2)
			if len(parts) == 2 {
				exchange, symbol = parts[0], parts[1]
			}
		}
	}

	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"total_quantity": 0,
			"total_value":    0,
			"position_count": 0,
			"average_price":  0,
			"current_price":  0,
			"unrealized_pnl": 0,
			"pnl_percentage": 0,
			"actual_margin":  0,
			"leverage":       1,
		})
		return
	}

	slots := pmProvider.GetAllSlots()
	wsPrice := 0.0 // WebSocket 實時價格
	if priceProv != nil {
		wsPrice = priceProv.GetLastPrice()
	}

	// ========== 槽位计算部分 ==========
	slotTotalQuantity := 0.0
	slotTotalValue := 0.0
	slotPositionCount := 0
	slotTotalCost := 0.0

	// 筛选有持倉的槽位
	for _, slot := range slots {
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
			slotPositionCount++
			slotTotalQuantity += slot.PositionQty
			slotTotalCost += slot.Price * slot.PositionQty

			if wsPrice > 0 {
				slotTotalValue += slot.PositionQty * wsPrice
			} else {
				slotTotalValue += slot.PositionQty * slot.Price
			}
		}
	}

	// 槽位平均持倉價格
	slotAveragePrice := 0.0
	if slotTotalQuantity > 0 {
		slotAveragePrice = slotTotalCost / slotTotalQuantity
	}

	// 槽位计算的未實現盈亏
	slotUnrealizedPnL := 0.0
	if wsPrice > 0 && slotTotalQuantity > 0 && slotAveragePrice > 0 {
		slotUnrealizedPnL = (wsPrice - slotAveragePrice) * slotTotalQuantity
	}

	// ========== 交易所數據部分 ==========
	exchangeUnrealizedPnL := 0.0
	exchangeMarkPrice := 0.0
	exchangeEntryPrice := 0.0
	exchangePositionSize := 0.0
	exchangeLeverage := 0
	hasExchangeData := false

	if exchProv != nil && symbol != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		positions, err := exchProv.GetPositions(ctx, symbol)
		cancel()

		if err == nil && len(positions) > 0 {
			for _, pos := range positions {
				if pos.Size != 0 {
					exchangeUnrealizedPnL += pos.UnrealizedPNL
					exchangeMarkPrice = pos.MarkPrice
					exchangeEntryPrice = pos.EntryPrice
					exchangePositionSize += pos.Size // 累加（支援多倉位）
					exchangeLeverage = pos.Leverage
					hasExchangeData = true
				}
			}
		} else if err != nil {
			logger.Warn("⚠️ [getPositionsSummary] 從交易所獲取倉位失败: %v", err)
		}
	}

	// ========== 差异分析 ==========
	// 返回結構化原因供前端 i18n 格式化，不再硬編碼中文
	type reasonItem map[string]interface{}
	var discrepancyReasons []reasonItem
	pnlDiff := 0.0

	if hasExchangeData && slotTotalQuantity > 0 {
		pnlDiff = exchangeUnrealizedPnL - slotUnrealizedPnL

		// 1. 數量差异分析
		quantityDiff := exchangePositionSize - slotTotalQuantity
		if math.Abs(quantityDiff) > 0.000001 {
			diffPercent := (quantityDiff / slotTotalQuantity) * 100
			if math.Abs(diffPercent) > 1 {
				discrepancyReasons = append(discrepancyReasons, reasonItem{
					"type":     "quantity_diff",
					"exchange": exchangePositionSize,
					"slot":     slotTotalQuantity,
					"diff":     quantityDiff,
					"diff_pct": diffPercent,
				})
			}
		}

		// 2. 入场價格差异分析
		priceDiff := exchangeEntryPrice - slotAveragePrice
		if math.Abs(priceDiff) > 0.01 {
			diffPercent := (priceDiff / slotAveragePrice) * 100
			if math.Abs(diffPercent) > 0.1 {
				discrepancyReasons = append(discrepancyReasons, reasonItem{
					"type":     "entry_price_diff",
					"exchange": exchangeEntryPrice,
					"slot_avg": slotAveragePrice,
					"diff":     priceDiff,
					"diff_pct": diffPercent,
				})
			}
		}

		// 3. 當前價格差异分析（標記價格 vs WebSocket價格）
		if wsPrice > 0 && exchangeMarkPrice > 0 {
			markPriceDiff := exchangeMarkPrice - wsPrice
			if math.Abs(markPriceDiff) > 0.01 {
				diffPercent := (markPriceDiff / wsPrice) * 100
				discrepancyReasons = append(discrepancyReasons, reasonItem{
					"type":       "price_diff",
					"mark_price": exchangeMarkPrice,
					"ws_price":   wsPrice,
					"diff":       markPriceDiff,
					"diff_pct":   diffPercent,
				})
			}
		}

		// 4. 如果數量和價格都接近但盈亏差异大，可能是其他原因
		if len(discrepancyReasons) == 0 && math.Abs(pnlDiff) > 1 {
			discrepancyReasons = append(discrepancyReasons, reasonItem{
				"type":     "pnl_diff_other",
				"pnl_diff": pnlDiff,
			})
		}

		// 記錄详细日志
		logger.Info("📊 [getPositionsSummary] 盈亏對比分析:")
		logger.Info("  交易所: size=%.6f, entryPrice=%.2f, markPrice=%.2f, pnl=%.4f, leverage=%d",
			exchangePositionSize, exchangeEntryPrice, exchangeMarkPrice, exchangeUnrealizedPnL, exchangeLeverage)
		logger.Info("  槽位:   size=%.6f, avgPrice=%.2f, wsPrice=%.2f, pnl=%.4f",
			slotTotalQuantity, slotAveragePrice, wsPrice, slotUnrealizedPnL)
		logger.Info("  差异:   pnlDiff=%.4f USDT", pnlDiff)
		for i, r := range discrepancyReasons {
			logger.Info("  原因[%d]: type=%v", i, r["type"])
		}
	}

	// ========== 决定使用哪個數據作為主要显示 ==========
	// 优先使用交易所數據（因為这是真實的盈亏）
	displayUnrealizedPnL := slotUnrealizedPnL
	displayCurrentPrice := wsPrice
	if hasExchangeData {
		displayUnrealizedPnL = exchangeUnrealizedPnL
		if exchangeMarkPrice > 0 {
			displayCurrentPrice = exchangeMarkPrice
		}
	}

	// 计算亏损率（相對於持倉成本的百分比）
	pnlPercentage := 0.0
	if slotTotalCost > 0 {
		pnlPercentage = (displayUnrealizedPnL / slotTotalCost) * 100.0
	}

	// 计算實際资金占用
	leverage := 1
	if pmProvider != nil {
		leverage = pmProvider.GetLeverage()
	}
	// 如果交易所返回了杠杆，优先使用
	if exchangeLeverage > 0 {
		leverage = exchangeLeverage
	}
	actualMargin := 0.0
	if leverage > 0 && slotTotalValue > 0 {
		actualMargin = slotTotalValue / float64(leverage)
	}

	// 構建响应
	response := gin.H{
		// 維度標識（按交易所、币种、策略）
		"exchange": exchange,
		"symbol":   symbol,
		"strategy": "grid",
		// 主要显示數據（优先使用交易所數據）
		"total_quantity": slotTotalQuantity,
		"total_value":    slotTotalValue,
		"position_count": slotPositionCount,
		"average_price":  slotAveragePrice,
		"current_price":  displayCurrentPrice,
		"unrealized_pnl": displayUnrealizedPnL,
		"pnl_percentage": pnlPercentage,
		"actual_margin":  actualMargin,
		"leverage":       leverage,

		// 槽位计算數據
		"slot_data": gin.H{
			"quantity":       slotTotalQuantity,
			"average_price":  slotAveragePrice,
			"unrealized_pnl": slotUnrealizedPnL,
			"ws_price":       wsPrice,
		},

		// 交易所數據
		"exchange_data": gin.H{
			"has_data":       hasExchangeData,
			"quantity":       exchangePositionSize,
			"entry_price":    exchangeEntryPrice,
			"mark_price":     exchangeMarkPrice,
			"unrealized_pnl": exchangeUnrealizedPnL,
			"leverage":       exchangeLeverage,
		},

		// 差异分析
		"discrepancy": gin.H{
			"pnl_diff": pnlDiff,
			"reasons":  discrepancyReasons,
		},
	}

	c.JSON(http.StatusOK, response)
}

// getPositionsSummaryAll 獲取所有交易對的持倉彙總（按交易所、币种、策略列出）
// GET /api/positions/summary/all
func getPositionsSummaryAll(c *gin.Context) {
	// 获取所有配置的币种
	configuredSymbols := make(map[string]bool) // key: exchange:symbol
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			for _, sym := range cfg.Trading.Symbols {
				if sym.Symbol == "" {
					continue
				}
				exchange := sym.Exchange
				if exchange == "" {
					exchange = cfg.App.CurrentExchange
				}
				if exchange == "" {
					continue
				}
				key := strings.ToLower(fmt.Sprintf("%s:%s", exchange, sym.Symbol))
				configuredSymbols[key] = true
			}
			// 如果只有單交易對配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
				exchange := cfg.App.CurrentExchange
				if exchange != "" {
					key := strings.ToLower(fmt.Sprintf("%s:%s", exchange, cfg.Trading.Symbol))
					configuredSymbols[key] = true
				}
			}
		}
	}

	providersMu.RLock()
	keys := make([]string, 0, len(positionProviders))
	for k := range positionProviders {
		keys = append(keys, k)
	}
	providersMu.RUnlock()

	// 确保所有配置的币种都在 keys 中
	for key := range configuredSymbols {
		found := false
		for _, k := range keys {
			if strings.ToLower(k) == key {
				found = true
				break
			}
		}
		if !found {
			keys = append(keys, key)
		}
	}

	var result []gin.H
	for _, key := range keys {
		parts := strings.SplitN(key, ":", 2)
		if len(parts) != 2 {
			continue
		}
		exchangeName, symbol := parts[0], parts[1]

		providersMu.RLock()
		pmProvider := positionProviders[key]
		priceProv := priceProviders[key]
		exchProv := exchangeProviders[key]
		providersMu.RUnlock()

		// 如果没有 provider，但配置中有这个币种，仍然返回（持仓为0）
		if pmProvider == nil {
			// 检查是否在配置中
			if configuredSymbols[strings.ToLower(key)] {
				// 返回持仓为0的记录
				result = append(result, gin.H{
					"exchange":       exchangeName,
					"symbol":         symbol,
					"strategy":       "grid",
					"total_quantity": 0.0,
					"total_value":    0.0,
					"position_count": 0,
					"average_price":  0.0,
					"current_price":  0.0,
					"unrealized_pnl": 0.0,
					"pnl_percentage": 0.0,
					"actual_margin":  0.0,
					"leverage":       1,
					"exchange_data": gin.H{
						"has_data":       false,
						"quantity":       0.0,
						"entry_price":    0.0,
						"mark_price":     0.0,
						"unrealized_pnl": 0.0,
						"leverage":       0,
					},
				})
			}
			continue
		}

		slots := pmProvider.GetAllSlots()
		wsPrice := 0.0
		if priceProv != nil {
			wsPrice = priceProv.GetLastPrice()
		}

		// 槽位计算
		slotTotalQuantity := 0.0
		slotTotalValue := 0.0
		slotPositionCount := 0
		slotTotalCost := 0.0
		for _, slot := range slots {
			if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
				slotPositionCount++
				slotTotalQuantity += slot.PositionQty
				slotTotalCost += slot.Price * slot.PositionQty
				if wsPrice > 0 {
					slotTotalValue += slot.PositionQty * wsPrice
				} else {
					slotTotalValue += slot.PositionQty * slot.Price
				}
			}
		}

		slotAveragePrice := 0.0
		if slotTotalQuantity > 0 {
			slotAveragePrice = slotTotalCost / slotTotalQuantity
		}
		slotUnrealizedPnL := 0.0
		if wsPrice > 0 && slotTotalQuantity > 0 && slotAveragePrice > 0 {
			slotUnrealizedPnL = (wsPrice - slotAveragePrice) * slotTotalQuantity
		}

		// 交易所數據
		exchangeUnrealizedPnL := 0.0
		exchangeMarkPrice := 0.0
		exchangeEntryPrice := 0.0
		exchangePositionSize := 0.0
		exchangeLeverage := 0
		hasExchangeData := false
		if exchProv != nil && symbol != "" {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			positions, err := exchProv.GetPositions(ctx, symbol)
			cancel()
			if err == nil && len(positions) > 0 {
				for _, pos := range positions {
					if pos.Size != 0 {
						exchangeUnrealizedPnL += pos.UnrealizedPNL
						exchangeMarkPrice = pos.MarkPrice
						exchangeEntryPrice = pos.EntryPrice
						exchangePositionSize += pos.Size
						exchangeLeverage = pos.Leverage
						hasExchangeData = true
					}
				}
			}
		}

		displayUnrealizedPnL := slotUnrealizedPnL
		displayCurrentPrice := wsPrice
		if hasExchangeData {
			displayUnrealizedPnL = exchangeUnrealizedPnL
			if exchangeMarkPrice > 0 {
				displayCurrentPrice = exchangeMarkPrice
			}
		}

		leverage := pmProvider.GetLeverage()
		if exchangeLeverage > 0 {
			leverage = exchangeLeverage
		}
		actualMargin := 0.0
		if leverage > 0 && slotTotalValue > 0 {
			actualMargin = slotTotalValue / float64(leverage)
		}
		pnlPercentage := 0.0
		if slotTotalCost > 0 {
			pnlPercentage = (displayUnrealizedPnL / slotTotalCost) * 100.0
		}

		// 即使持仓为0，也返回记录（如果配置中有这个币种）
		if slotPositionCount == 0 && !configuredSymbols[strings.ToLower(key)] {
			// 如果持仓为0且不在配置中，跳过
			continue
		}

		result = append(result, gin.H{
			"exchange":       exchangeName,
			"symbol":         symbol,
			"strategy":       "grid", // 當前架構每個交易對為網格策略
			"total_quantity": slotTotalQuantity,
			"total_value":    slotTotalValue,
			"position_count": slotPositionCount,
			"average_price":  slotAveragePrice,
			"current_price":  displayCurrentPrice,
			"unrealized_pnl": displayUnrealizedPnL,
			"pnl_percentage": pnlPercentage,
			"actual_margin":  actualMargin,
			"leverage":       leverage,
			"exchange_data": gin.H{
				"has_data":       hasExchangeData,
				"quantity":       exchangePositionSize,
				"entry_price":    exchangeEntryPrice,
				"mark_price":     exchangeMarkPrice,
				"unrealized_pnl": exchangeUnrealizedPnL,
				"leverage":       exchangeLeverage,
			},
		})
	}

	// 按交易所、币种排序
	sort.Slice(result, func(i, j int) bool {
		ei, _ := result[i]["exchange"].(string)
		ej, _ := result[j]["exchange"].(string)
		if ei != ej {
			return ei < ej
		}
		si, _ := result[i]["symbol"].(string)
		sj, _ := result[j]["symbol"].(string)
		return strings.ToLower(si) < strings.ToLower(sj)
	})

	c.JSON(http.StatusOK, gin.H{"positions": result})
}

// getOrders 獲取訂單列表（历史订單）
// GET /api/orders
func getOrders(c *gin.Context) {
	// 优先使用特定交易對的 storage provider
	storageProv := PickStorageProvider(c)

	// 如果找不到特定的 provider，使用全局的 storageServiceProvider
	if storageProv == nil {
		storageProv = storageServiceProvider
	}

	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	// 解析参數
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	status := c.Query("status")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	orders, err := storage.QueryOrdersWithTimeRange(limit, offset, status, nil, nil)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_orders_failed", err)
		return
	}

	// 轉换時间為UTC+8
	ordersResponse := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		ordersResponse[i] = map[string]interface{}{
			"order_id":        order.OrderID,
			"client_order_id": order.ClientOrderID,
			"symbol":          order.Symbol,
			"side":            order.Side,
			"price":           order.Price,
			"quantity":        order.Quantity,
			"status":          order.Status,
			"created_at":      utils.ToUTC8(order.CreatedAt),
			"updated_at":      utils.ToUTC8(order.UpdatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": ordersResponse})
}

// syncOrders 手动同步订单（仅币安）
// POST /api/orders/sync
func syncOrders(c *gin.Context) {
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchangeName == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol", fmt.Errorf("exchange和symbol参数必填"))
		return
	}

	// 检查是否是币安交易所
	if exchangeName != "binance" {
		respondError(c, http.StatusBadRequest, "error.only_binance_supported", fmt.Errorf("当前仅支持币安交易所的订单同步"))
		return
	}

	// 获取exchange provider
	exProvider := pickExchangeProvider(c)
	if exProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.exchange_provider_not_found", fmt.Errorf("未找到交易所provider"))
		return
	}

	// 获取storage provider
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil {
		respondError(c, http.StatusInternalServerError, "error.storage_provider_not_found", fmt.Errorf("未找到storage provider"))
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		respondError(c, http.StatusInternalServerError, "error.storage_not_found", fmt.Errorf("未找到storage"))
		return
	}

	// 获取exchange实例（需要转换为IExchange接口）
	// 由于ExchangeProvider接口不包含IExchange的所有方法，我们需要通过symbol manager获取
	// 这里我们创建一个临时的同步服务
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 尝试从symbol manager获取exchange实例
	var ex exchange.IExchange
	if symbolManagerProvider != nil {
		rtInterface, exists := symbolManagerProvider.Get(exchangeName, symbol)
		if exists {
			// 使用反射获取Exchange字段
			rtVal := reflect.ValueOf(rtInterface)
			if rtVal.Kind() == reflect.Ptr {
				rtVal = rtVal.Elem()
			}
			exchangeField := rtVal.FieldByName("Exchange")
			if exchangeField.IsValid() && !exchangeField.IsNil() {
				if exInterface, ok := exchangeField.Interface().(exchange.IExchange); ok {
					ex = exInterface
				}
			}
		}
	}

	if ex == nil {
		respondError(c, http.StatusInternalServerError, "error.exchange_instance_not_found", fmt.Errorf("未找到交易所实例，请确保交易对正在运行"))
		return
	}

	// 创建临时同步服务并执行同步
	orderSync := ordersync.NewOrderSyncService(
		ex,
		storage,
		symbol,
		"", // accountID暂时为空
		exchangeName,
		10*time.Minute, // syncInterval，这里只是用于创建，不会实际使用
	)

	// 执行同步
	if err := orderSync.Sync(ctx); err != nil {
		logger.Error("❌ [订单同步] 手动同步失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.sync_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "订单同步成功",
	})
}

// getOrderHistory 獲取訂單历史
// GET /api/orders/history
func getOrderHistory(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	logger.Info("[訂單歷史] 查詢参數: exchange=%s, symbol=%s", exchange, symbol)

	// 优先使用特定交易對的 storage provider
	storageProv := PickStorageProvider(c)

	// 如果找不到特定的 provider，使用全局的 storageServiceProvider
	if storageProv == nil {
		logger.Info("[訂單歷史] 未找到特定交易對的 provider，使用全局 storageServiceProvider")
		storageProv = storageServiceProvider
	}

	if storageProv == nil {
		logger.Warn("[訂單歷史] storageServiceProvider 也為 nil，無法查詢")
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		logger.Warn("[訂單歷史] storage.GetStorage() 回傳 nil")
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	logger.Info("[訂單歷史] storage 獲取成功，准备查詢數據库")

	// 解析参數
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 解析时间范围（RFC3339格式）
	var startTime, endTime *time.Time
	now := utils.NowUTC()
	defaultStartTime := now.Add(-24 * time.Hour) // 默认最近24小时

	if startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			startTime = &t
		} else {
			logger.Warn("[訂單歷史] 解析 start_time 失败: %v，使用默认值", err)
		}
	}
	if endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			endTime = &t
		} else {
			logger.Warn("[訂單歷史] 解析 end_time 失败: %v，使用默认值", err)
		}
	}

	// 如果缺失时间参数，使用默认值（最近24小时）
	if startTime == nil {
		startTime = &defaultStartTime
	}
	if endTime == nil {
		endTime = &now
	}

	// 验证时间范围
	if endTime.Before(*startTime) {
		respondError(c, http.StatusBadRequest, "orders.timeRangeInvalid")
		return
	}

	// 验证时间跨度不超过7天
	diffDays := endTime.Sub(*startTime).Hours() / 24
	if diffDays > 7 {
		respondError(c, http.StatusBadRequest, "orders.timeRangeMaxDays")
		return
	}

	// 使用新的筛选方法查询已完成和已取消的订单（支持 exchange 和 symbol 筛选）
	orders, err := storage.QueryOrdersWithFilter(limit, offset, "FILLED", exchange, symbol, startTime, endTime)
	if err != nil {
		// 如果查詢失败，尝試查詢所有状態的订單
		orders, err = storage.QueryOrdersWithFilter(limit, offset, "", exchange, symbol, startTime, endTime)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 也查詢已取消的订單（使用相同的筛选条件）
	canceledOrders, err := storage.QueryOrdersWithFilter(limit, offset, "CANCELED", exchange, symbol, startTime, endTime)
	if err == nil {
		orders = append(orders, canceledOrders...)
	}

	// 收集已成交的賣單 ID，查詢對應盈虧
	sellOrderIDs := make([]int64, 0)
	for _, o := range orders {
		if o.Status == "FILLED" && o.Side == "SELL" {
			sellOrderIDs = append(sellOrderIDs, o.OrderID)
		}
	}
	pnlMap := make(map[int64]float64)
	if len(sellOrderIDs) > 0 {
		if m, err := storage.GetTradesBySellOrderIDs(sellOrderIDs); err == nil {
			pnlMap = m
		}
	}

	// 轉换時间為UTC+8並格式化返回數據，附加盈虧字段
	ordersResponse := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		resp := map[string]interface{}{
			"order_id":        order.OrderID,
			"client_order_id": order.ClientOrderID,
			"symbol":          order.Symbol,
			"side":            order.Side,
			"exchange":        order.Exchange,
			"type":            order.Type,
			"price":           order.Price,
			"quantity":        order.Quantity,
			"filled_qty":      order.FilledQty,
			"status":          order.Status,
			"strategy_name":   order.StrategyName,
			"strategy_type":   order.StrategyType,
			"order_source":    order.OrderSource,
			"created_at":      utils.ToUTC8(order.CreatedAt),
			"updated_at":      utils.ToUTC8(order.UpdatedAt),
		}
		// pnl = 网格策略计算的盈亏（基于买卖配对）
		if pnl, ok := pnlMap[order.OrderID]; ok {
			resp["pnl"] = pnl
		} else {
			resp["pnl"] = nil
		}
		// exchange_pnl = 交易所计算的已实现盈亏（基于加权平均成本法）
		if order.RealizedPnL != nil {
			resp["exchange_pnl"] = *order.RealizedPnL
		} else {
			resp["exchange_pnl"] = nil
		}
		ordersResponse[i] = resp
	}

	// 查詢真實的订單总数（不受 limit 限制）
	totalCount := int64(len(orders))
	todayCount := int64(0)

	// 尝試從數據库获取真实总数（使用带筛选的计数方法）
	type orderCounter interface {
		CountOrdersWithFilter(status, exchange, symbol string) (int64, error)
	}
	if counter, ok := storage.(orderCounter); ok {
		filledCount, err1 := counter.CountOrdersWithFilter("FILLED", exchange, symbol)
		canceledCount, err2 := counter.CountOrdersWithFilter("CANCELED", exchange, symbol)
		if err1 == nil && err2 == nil {
			totalCount = filledCount + canceledCount
		}
	}

	// 计算今日订單数（从已返回的订單中统计）
	nowLocal := utils.NowConfiguredTimezone()
	todayStr := nowLocal.Format("2006-01-02")
	for _, order := range orders {
		orderDate := utils.ToConfiguredTimezone(order.CreatedAt).Format("2006-01-02")
		if orderDate == todayStr {
			todayCount++
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"orders":      ordersResponse,
		"total_count": totalCount,
		"today_count": todayCount,
	})
}

var (
	// 存儲服務提供者（需要從main.go注入）
	storageServiceProvider StorageServiceProvider
)

// StorageServiceProvider 存儲服務提供者接口
type StorageServiceProvider interface {
	GetStorage() storage.Storage
}

// SetStorageServiceProvider 設置存儲服務提供者
func SetStorageServiceProvider(provider StorageServiceProvider) {
	storageServiceProvider = provider
}

// storageServiceAdapter 存儲服務适配器
type storageServiceAdapter struct {
	service *storage.StorageService
}

// NewStorageServiceAdapter 創建存儲服務适配器
func NewStorageServiceAdapter(service *storage.StorageService) StorageServiceProvider {
	return &storageServiceAdapter{service: service}
}

// GetStorage 獲取存儲接口
func (a *storageServiceAdapter) GetStorage() storage.Storage {
	if a.service == nil {
		logger.Warn("⚠️ storageServiceAdapter.GetStorage: service 為 nil")
		return nil
	}
	st := a.service.GetStorage()
	if st == nil {
		logger.Warn("⚠️ storageServiceAdapter.GetStorage: service.GetStorage() 回傳 nil，storage.enabled 可能為 false 或初始化失败")
	}
	return st
}

// getStatistics 獲取统计數據
// GET /api/statistics
func getStatistics(c *gin.Context) {
	// 优先使用特定交易對的 storage provider
	storageProv := PickStorageProvider(c)

	// 如果找不到特定的 provider，使用全局的 storageServiceProvider
	if storageProv == nil {
		logger.Info("[统计] 未找到特定交易對的 provider，使用全局 storageServiceProvider")
		storageProv = storageServiceProvider
	}

	if storageProv == nil {
		logger.Warn("[统计] storageServiceProvider 也為 nil，無法查詢")
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    0,
			"win_rate":     0,
		})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		logger.Warn("[统计] storage.GetStorage() 回傳 nil")
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    0,
			"win_rate":     0,
		})
		return
	}

	logger.Info("[统计] storage 獲取成功，准备查詢數據库")

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()
	logger.Info("[统计] accountID: %s", accountID)

	// 獲取 exchange、symbol 参數（如果有）
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	// 從數據库獲取统计彙總
	var summary interface{}
	var err error
	if exchange != "" && symbol != "" {
		// 指定了交易所和交易對時，查詢該交易對的统计（概覽頁顯示當前交易對的總盈虧）
		summary, err = storage.GetStatisticsSummaryByExchangeAndSymbol(exchange, symbol, accountID)
		logger.Info("[统计] 查詢交易所 %s 交易對 %s 的统计，accountID: %s", exchange, symbol, accountID)
	} else if exchange != "" {
		// 只指定了交易所，查詢該交易所的统计
		summary, err = storage.GetStatisticsSummaryByExchange(exchange, accountID)
		logger.Info("[统计] 查詢交易所 %s 的统计，accountID: %s", exchange, accountID)
	} else {
		// 否则查詢所有交易所的统计
		summary, err = storage.GetStatisticsSummary(accountID)
		logger.Info("[统计] 查詢所有交易所的统计，accountID: %s", accountID)
	}

	if err != nil {
		logger.Error("[统计] 查詢失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 使用反射獲取字段值（避免類型断言问题）
	statValue := reflect.ValueOf(summary)
	if statValue.Kind() != reflect.Ptr || statValue.Elem().Kind() != reflect.Struct {
		logger.Error("[统计] 统计數據格式錯误")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "统计數據格式錯误"})
		return
	}

	elem := statValue.Elem()
	totalTrades := int(elem.FieldByName("TotalTrades").Int())
	totalPnL := elem.FieldByName("TotalPnL").Float()
	totalVolume := elem.FieldByName("TotalVolume").Float()
	winRate := elem.FieldByName("WinRate").Float()
	grossPnL := elem.FieldByName("GrossPnL").Float()
	totalFee := elem.FieldByName("TotalFee").Float()
	totalBuyDeviation := elem.FieldByName("TotalBuyDeviation").Float()
	totalSellDeviation := elem.FieldByName("TotalSellDeviation").Float()

	logger.Info("[统计] 查詢結果: TotalTrades=%d, TotalPnL=%.2f, GrossPnL=%.2f, TotalFee=%.2f, TotalVolume=%.2f, BuyDeviation=%.2f, SellDeviation=%.2f", totalTrades, totalPnL, grossPnL, totalFee, totalVolume, totalBuyDeviation, totalSellDeviation)

	// 如果數據库没有數據，尝試從 SuperPositionManager 计算
	pmProvider := PickPositionProvider(c)
	if totalTrades == 0 && pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		totalBuyQty := 0.0
		totalSellQty := 0.0

		for _, slot := range slots {
			if slot.OrderSide == "BUY" && slot.OrderStatus == "FILLED" {
				totalBuyQty += slot.OrderFilledQty
			} else if slot.OrderSide == "SELL" && slot.OrderStatus == "FILLED" {
				totalSellQty += slot.OrderFilledQty
			}
		}

		// 估算交易數（買賣配對）
		estimatedTrades := int((totalBuyQty + totalSellQty) / 2)
		if estimatedTrades > 0 {
			totalTrades = estimatedTrades
			totalVolume = totalBuyQty + totalSellQty
		}
	}

	// 🔥 查詢交易所已實現盈虧（從 orders 表的 realized_pnl 聚合）
	exchangePnL := 0.0
	type exchangePnLGetter interface {
		GetExchangePnLTotal(exchange, symbol string) (float64, error)
	}
	if epGetter, ok := storage.(exchangePnLGetter); ok {
		if ep, err := epGetter.GetExchangePnLTotal(exchange, symbol); err == nil {
			exchangePnL = ep
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_trades":         totalTrades,
		"total_volume":         totalVolume,
		"total_pnl":            totalPnL,
		"gross_pnl":            grossPnL,
		"total_fee":            totalFee,
		"win_rate":             winRate,
		"total_buy_deviation":  totalBuyDeviation,  // 🔥 買入價格偏差總和
		"total_sell_deviation": totalSellDeviation,  // 🔥 賣出價格偏差總和
		"exchange_pnl":         exchangePnL,         // 🔥 交易所已實現盈虧合計
	})
}

// getDailyStatistics 獲取每日统计（混合模式：优先使用 statistics 表，缺失的日期從 trades 表补充）
// GET /api/statistics/daily
func getDailyStatistics(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}, "max_drawdown": 0, "max_drawdown_pct": 0})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}, "max_drawdown": 0, "max_drawdown_pct": 0})
		return
	}

	// 解析参數
	daysStr := c.DefaultQuery("days", "30")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	startDate := utils.NowConfiguredTimezone().AddDate(0, 0, -days)
	endDate := utils.NowConfiguredTimezone()

	// 1. 先從 statistics 表查詢
	statsFromTable, err := st.QueryStatistics(startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 2. 構建日期映射（statistics 表中已有的日期）
	statsMap := make(map[string]*storage.Statistics)
	for _, stat := range statsFromTable {
		dateKey := stat.Date.Format("2006-01-02")
		statsMap[dateKey] = stat
	}

	// 3. 從 trades 表查詢所有日期（包含缺失的日期和盈利/亏损交易數）
	tradesStatsMap := make(map[string]*storage.DailyStatisticsWithTradeCount)
	accountID := GetCurrentAccountID()
	tradesStats, err2 := st.QueryDailyStatisticsFromTrades(accountID, startDate, endDate)
	if err2 == nil {
		for _, tradeStat := range tradesStats {
			dateKey := tradeStat.Date.Format("2006-01-02")
			tradesStatsMap[dateKey] = tradeStat
		}
	}

	// 3b. 從每日快照表查詢未實現盈虧與日內最大回撤
	snapshotMap := make(map[string]*storage.DailySnapshot)
	status := pickStatus(c)
	if status != nil && status.Exchange != "" && status.Symbol != "" {
		snapshots, errSnap := st.QueryDailySnapshots(status.Exchange, status.Symbol, accountID, startDate, endDate)
		if errSnap == nil {
			for _, snap := range snapshots {
				dateKey := snap.Date.Format("2006-01-02")
				snapshotMap[dateKey] = snap
			}
		}
	}

	// 4. 獲取日K線數據用於计算开盘/收盘價和涨跌幅
	klineMap := make(map[string]*exchange.Candle)
	exchProv := pickExchangeProvider(c)
	if exchProv != nil && status != nil && status.Symbol != "" {
		ctx := c.Request.Context()
		// 獲取日K線數據（1d 周期），限制天數+1以确保覆盖範圍
		candles, err := exchProv.GetHistoricalKlines(ctx, status.Symbol, "1d", days+1)
		if err == nil && len(candles) > 0 {
			candles = exchange.ClipKlineSpikes(candles, 0.03)
			for _, candle := range candles {
				// 將時间戳轉换為日期字符串
				candleTime := time.Unix(candle.Timestamp/1000, 0).UTC()
				dateKey := candleTime.Format("2006-01-02")
				klineMap[dateKey] = candle
			}
		}
	}

	// 4b. 獲取每日資金費用
	fundingMap := make(map[string]float64)
	type dailyFundingGetter interface {
		GetDailyFundingPayments(account, exchange string, startTime, endTime time.Time) (map[string]float64, error)
	}
	if stWithFunding, ok := st.(dailyFundingGetter); ok {
		exchangeID := ""
		if status != nil {
			exchangeID = status.Exchange
		}
		dailyFunding, err := stWithFunding.GetDailyFundingPayments(accountID, exchangeID, startDate, endDate)
		if err == nil {
			fundingMap = dailyFunding
		}
	}

	// 4c. 獲取每日交易所已實現盈虧（從 orders 表聚合 realized_pnl）
	exchangePnLMap := make(map[string]float64)
	type dailyExchangePnLGetter interface {
		GetDailyExchangePnL(exchange, symbol string, startDate, endDate time.Time) (map[string]float64, error)
	}
	if epGetter, ok := st.(dailyExchangePnLGetter); ok {
		exchangeID := ""
		symbolID := ""
		if status != nil {
			exchangeID = status.Exchange
			symbolID = status.Symbol
		}
		if dailyEP, err := epGetter.GetDailyExchangePnL(exchangeID, symbolID, startDate, endDate); err == nil {
			exchangePnLMap = dailyEP
		}
	}

	// 5. 合並數據：优先使用 statistics 表的數據，缺失的日期使用 trades 表的數據
	// 構建最终結果
	var result []map[string]interface{}
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	// 处理所有日期（包括 statistics 表和 trades 表中的日期）
	allDates := make(map[string]bool)
	for dateKey := range statsMap {
		allDates[dateKey] = true
	}
	for dateKey := range tradesStatsMap {
		allDates[dateKey] = true
	}

	// 轉换為列表
	var dateList []string
	for dateKey := range allDates {
		if dateKey >= startDateStr && dateKey <= endDateStr {
			dateList = append(dateList, dateKey)
		}
	}

	// 按日期倒序排序
	for i := 0; i < len(dateList)-1; i++ {
		for j := i + 1; j < len(dateList); j++ {
			if dateList[i] < dateList[j] {
				dateList[i], dateList[j] = dateList[j], dateList[i]
			}
		}
	}

	// 用於计算最大回撤的累计盈亏數據
	var cumulativePnLList []float64
	cumulativePnL := 0.0

	// 構建結果（需要按日期正序计算累计盈亏，然后再反轉）
	// 先按日期正序处理
	var tempResult []map[string]interface{}
	for i := len(dateList) - 1; i >= 0; i-- {
		dateKey := dateList[i]
		item := make(map[string]interface{})
		item["date"] = dateKey

		var dailyPnL float64

		// 优先使用 statistics 表的數據
		if stat, exists := statsMap[dateKey]; exists {
			item["total_trades"] = stat.TotalTrades
			item["total_volume"] = stat.TotalVolume
			item["total_pnl"] = stat.TotalPnL
			item["win_rate"] = stat.WinRate
			dailyPnL = stat.TotalPnL
		} else if tradeStat, exists := tradesStatsMap[dateKey]; exists {
			// 使用 trades 表的數據
			item["total_trades"] = tradeStat.TotalTrades
			item["total_volume"] = tradeStat.TotalVolume
			item["total_pnl"] = tradeStat.TotalPnL
			item["gross_pnl"] = tradeStat.GrossPnL
			item["total_fee"] = tradeStat.TotalFee
			item["win_rate"] = tradeStat.WinRate
			item["winning_trades"] = tradeStat.WinningTrades
			item["losing_trades"] = tradeStat.LosingTrades
			item["volume_profit"] = tradeStat.VolumeProfit
			item["volume_stop_loss"] = tradeStat.VolumeStopLoss
			dailyPnL = tradeStat.TotalPnL
		} else {
			continue
		}

		// 如果 statistics 表的數據存在，但從 trades 表可以獲取盈利/亏损交易數和交易量細分，也添加進去
		if _, exists := statsMap[dateKey]; exists {
			if tradeStat, exists := tradesStatsMap[dateKey]; exists {
				item["winning_trades"] = tradeStat.WinningTrades
				item["losing_trades"] = tradeStat.LosingTrades
				item["volume_profit"] = tradeStat.VolumeProfit
				item["volume_stop_loss"] = tradeStat.VolumeStopLoss
			}
		}

		// 添加K線數據（开盘價、收盘價、涨跌幅）
		if candle, exists := klineMap[dateKey]; exists {
			item["open_price"] = candle.Open
			item["close_price"] = candle.Close
			priceChange := candle.Close - candle.Open
			item["price_change"] = priceChange
			if candle.Open > 0 {
				item["price_change_pct"] = (priceChange / candle.Open) * 100
			} else {
				item["price_change_pct"] = 0
			}
		}

		// 计算累计盈亏
		cumulativePnL += dailyPnL
		cumulativePnLList = append(cumulativePnLList, cumulativePnL)
		item["cumulative_pnl"] = cumulativePnL

		// 合併每日快照：未實現盈虧、日內最大回撤
		if snap, ok := snapshotMap[dateKey]; ok {
			item["unrealized_pnl"] = snap.UnrealizedPnL
			item["intraday_max_drawdown"] = snap.IntradayMaxDrawdown
			item["intraday_max_drawdown_pct"] = snap.IntradayMaxDrawdownPct
		}

		// 合併每日資金費用
		if funding, ok := fundingMap[dateKey]; ok {
			item["funding_fee"] = funding
		} else {
			item["funding_fee"] = 0.0
		}

		// 合併每日交易所已實現盈虧
		if epnl, ok := exchangePnLMap[dateKey]; ok {
			item["exchange_pnl"] = epnl
		} else {
			item["exchange_pnl"] = 0.0
		}

		// 賬面盈虧 = 已平倉盈虧 + 未實現盈虧（真正帳面值）
		unrealized := 0.0
		if snap, ok := snapshotMap[dateKey]; ok {
			unrealized = snap.UnrealizedPnL
		}
		item["book_value_pnl"] = dailyPnL + unrealized

		tempResult = append(tempResult, item)
	}

	// 反轉結果，使其按日期倒序
	for i := len(tempResult) - 1; i >= 0; i-- {
		result = append(result, tempResult[i])
	}

	// 6. 计算最大回撤
	// 注意：这里使用净值（equity）= 虚拟初始本金 + 累计盈亏 来计算回撤
	// 这样可以保证回撤百分比不會超過100%
	// 我们使用累计盈亏的最小值来估算需要的初始本金
	maxDrawdown := 0.0
	maxDrawdownPct := 0.0
	if len(cumulativePnLList) > 0 {
		// 找出累计盈亏的最小值，用於确定虚拟初始本金
		minPnL := cumulativePnLList[0]
		for _, pnl := range cumulativePnLList {
			if pnl < minPnL {
				minPnL = pnl
			}
		}

		// 虚拟初始本金：确保净值始终為正
		// 如果最小累计盈亏是负數，初始本金需要大於其绝對值
		// 使用 |minPnL| * 2 作為初始本金，确保即使在最低点也有正的净值
		initialCapital := 1000.0 // 默认初始本金
		if minPnL < 0 {
			initialCapital = -minPnL * 2 // 确保最低点時净值仍為正
			if initialCapital < 1000 {
				initialCapital = 1000
			}
		}

		// 使用净值计算最大回撤
		peak := initialCapital + cumulativePnLList[0]
		for _, pnl := range cumulativePnLList {
			equity := initialCapital + pnl
			if equity > peak {
				peak = equity
			}
			if peak > 0 {
				drawdown := peak - equity
				drawdownPct := (drawdown / peak) * 100
				if drawdown > maxDrawdown {
					maxDrawdown = drawdown
				}
				if drawdownPct > maxDrawdownPct {
					maxDrawdownPct = drawdownPct
				}
			}
		}

		// 安全检查：回撤百分比不应超過100%
		if maxDrawdownPct > 100 {
			maxDrawdownPct = 100
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"statistics":       result,
		"max_drawdown":     maxDrawdown,
		"max_drawdown_pct": maxDrawdownPct,
	})
}

// getTradeStatistics 獲取交易统计
// GET /api/statistics/trades
func getTradeStatistics(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
		return
	}

	// 解析参數
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		startTime = utils.NowConfiguredTimezone().AddDate(0, 0, -7) // 默认最近7天
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = utils.NowConfiguredTimezone()
	}

	trades, err := storage.QueryTrades(startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换時间為UTC+8
	tradesResponse := make([]map[string]interface{}, len(trades))
	for i, trade := range trades {
		tradesResponse[i] = map[string]interface{}{
			"buy_order_id":  trade.BuyOrderID,
			"sell_order_id": trade.SellOrderID,
			"symbol":        trade.Symbol,
			"buy_price":     trade.BuyPrice,
			"sell_price":    trade.SellPrice,
			"quantity":      trade.Quantity,
			"pnl":           trade.PnL,
			"created_at":    utils.ToUTC8(trade.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"trades": tradesResponse})
}

// 这些函數已移动到 web/api_config.go
// 保留这些存根函數以保持向后兼容（如果其他地方有引用）
func getConfig(c *gin.Context) {
	getConfigHandler(c)
}

func updateConfig(c *gin.Context) {
	updateConfigHandler(c)
}

func startTrading(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	err := symbolManagerProvider.StartSymbol(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 啟动交易失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新状態
	key := makeSymbolKey(exchange, symbol)
	statusMu.Lock()
	if status, ok := statusBySymbol[key]; ok {
		status.Running = true
	} else {
		statusBySymbol[key] = &SystemStatus{
			Running:  true,
			Exchange: exchange,
			Symbol:   symbol,
		}
	}
	statusMu.Unlock()

	logger.Info("✅ [%s:%s] 交易已啟动", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("交易已啟动: %s:%s", exchange, symbol)})
}

func stopTrading(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	err := symbolManagerProvider.StopSymbol(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 停止交易失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新状態
	key := makeSymbolKey(exchange, symbol)
	statusMu.Lock()
	if status, ok := statusBySymbol[key]; ok {
		status.Running = false
	}
	statusMu.Unlock()

	logger.Info("⏹️ [%s:%s] 交易已停止", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("交易已停止: %s:%s", exchange, symbol)})
}

// ClosePositionsResponse 平倉响应
type ClosePositionsResponse struct {
	SuccessCount int    `json:"success_count"`
	FailCount    int    `json:"fail_count"`
	Message      string `json:"message"`
}

func closeAllPositions(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	// 通過适配器調用 ClosePositions 方法
	adapter, ok := symbolManagerProvider.(interface {
		ClosePositions(exchange, symbol string) (*ClosePositionsResponse, error)
	})
	if !ok {
		respondError(c, http.StatusInternalServerError, "error.close_positions_not_supported")
		return
	}

	result, err := adapter.ClosePositions(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 平倉失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("📊 [%s:%s] 平倉完成: 成功=%d, 失败=%d", exchange, symbol, result.SuccessCount, result.FailCount)
	c.JSON(http.StatusOK, result)
}

// ========== 交易控制相关API ==========

var (
	// SymbolManager 提供者（需要從main.go注入）
	symbolManagerProvider SymbolManagerProvider
)

// SymbolManagerProvider SymbolManager 提供者接口
type SymbolManagerProvider interface {
	Get(exchange, symbol string) (interface{}, bool) // 回傳 SymbolRuntime（使用 interface{} 避免循环依赖）
	List() []interface{}                             // 回傳 SymbolRuntime 列表
	StartSymbol(exchange, symbol string) error       // 啟动指定交易所/币种的交易
	StopSymbol(exchange, symbol string) error        // 停止指定交易所/币种的交易
}

// TradingParamsUpdater 交易参數热更新接口（可选接口，用於配置变更時推送到运行時）
type TradingParamsUpdater interface {
	UpdateTradingParams(latestConfig *config.Config) []string
}

// RegisterSymbolManager 注册 SymbolManager
func RegisterSymbolManager(provider SymbolManagerProvider) {
	symbolManagerProvider = provider
}

// ========== 系统監控相关API ==========

var (
	// 系统監控數據提供者（需要從main.go注入）
	systemMetricsProvider SystemMetricsProvider
)

// SystemMetricsProvider 系统監控數據提供者接口
type SystemMetricsProvider interface {
	GetCurrentMetrics() (*SystemMetricsResponse, error)
	GetMetrics(startTime, endTime time.Time, granularity string) ([]*SystemMetricsResponse, error)
	GetDailyMetrics(days int) ([]*DailySystemMetricsResponse, error)
}

// SystemMetricsResponse 系统監控數據响应
type SystemMetricsResponse struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"`
	ProcessID     int       `json:"process_id"`
}

// DailySystemMetricsResponse 每日彙總數據响应
type DailySystemMetricsResponse struct {
	Date          time.Time `json:"date"`
	AvgCPUPercent float64   `json:"avg_cpu_percent"`
	MaxCPUPercent float64   `json:"max_cpu_percent"`
	MinCPUPercent float64   `json:"min_cpu_percent"`
	AvgMemoryMB   float64   `json:"avg_memory_mb"`
	MaxMemoryMB   float64   `json:"max_memory_mb"`
	MinMemoryMB   float64   `json:"min_memory_mb"`
	SampleCount   int       `json:"sample_count"`
}

// SetSystemMetricsProvider 設置系统監控數據提供者
func SetSystemMetricsProvider(provider SystemMetricsProvider) {
	systemMetricsProvider = provider
}

// getSystemMetrics 獲取系统監控數據
// GET /api/system/metrics
// 参數：
//   - start_time: 开始時间（可選，ISO 8601格式，默认最近7天）
//   - end_time: 結束時间（可選，ISO 8601格式，默认當前時间）
//   - granularity: 粒度（detail/daily，默认detail）
func getSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": []interface{}{}})
		return
	}

	// 解析参數
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	granularity := c.DefaultQuery("granularity", "detail")

	var startTime, endTime time.Time
	var err error

	if startTimeStr == "" {
		// 默认最近7天
		startTime = utils.NowConfiguredTimezone().Add(-7 * 24 * time.Hour)
	} else {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr == "" {
		endTime = utils.NowConfiguredTimezone()
	} else {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}

	if granularity == "daily" {
		// 返回每日彙總數據
		days := int(endTime.Sub(startTime).Hours() / 24)
		if days <= 0 {
			days = 30 // 默认30天
		}
		// 限制查詢天數，防止返回過多數據
		if days > 365 {
			days = 365 // 最多查詢1年
		}
		dailyMetrics, err := systemMetricsProvider.GetDailyMetrics(days)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": dailyMetrics, "granularity": "daily"})
	} else {
		// 返回细粒度數據
		metrics, err := systemMetricsProvider.GetMetrics(startTime, endTime, "detail")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": metrics, "granularity": "detail"})
	}
}

// getCurrentSystemMetrics 獲取當前系统状態
// GET /api/system/metrics/current
func getCurrentSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		// 返回完整的對象結構，避免前端访问 undefined 字段
		c.JSON(http.StatusOK, &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		})
		return
	}

	metrics, err := systemMetricsProvider.GetCurrentMetrics()
	if err != nil {
		// 即使出錯也返回完整的對象結構
		c.JSON(http.StatusOK, &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		})
		return
	}

	// 确保所有字段都有默认值
	if metrics == nil {
		metrics = &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}
	}

	c.JSON(http.StatusOK, metrics)
}

// getDailySystemMetrics 獲取每日彙總數據
// GET /api/system/metrics/daily
// 参數：
//   - days: 查詢天數（默认30天）
func getDailySystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": []interface{}{}})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	metrics, err := systemMetricsProvider.GetDailyMetrics(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"metrics": metrics})
}

// ========== 槽位數據相关API ==========

var (
	// 槽位數據提供者（需要從main.go注入）
	positionManagerProvider PositionManagerProvider
	// 订單金額配置（用於计算订單數量）
	orderQuantityConfig float64
)

// SetOrderQuantityConfig 設置订單金額配置
func SetOrderQuantityConfig(quantity float64) {
	orderQuantityConfig = quantity
}

// PositionManagerProvider 槽位數據提供者接口
type PositionManagerProvider interface {
	GetAllSlots() []SlotInfo
	GetSlotCount() int
	GetReconcileCount() int64
	GetLastReconcileTime() time.Time
	GetTotalBuyQty() float64
	GetTotalSellQty() float64
	GetPriceInterval() float64
	GetProfitSpread() float64
	GetLeverage() int // 獲取杠杆倍數
}

// SlotInfo 槽位信息
type SlotInfo struct {
	Exchange       string    `json:"exchange"`
	Symbol         string    `json:"symbol"`
	Price          float64   `json:"price"`
	PositionStatus string    `json:"position_status"` // EMPTY/FILLED
	PositionQty    float64   `json:"position_qty"`
	OrderID        int64     `json:"order_id"`
	ClientOID      string    `json:"client_order_id"`
	OrderSide      string    `json:"order_side"`   // BUY/SELL
	OrderStatus    string    `json:"order_status"` // NOT_PLACED/PLACED/CONFIRMED/PARTIALLY_FILLED/FILLED/CANCELED
	OrderPrice     float64   `json:"order_price"`
	OrderFilledQty float64   `json:"order_filled_qty"`
	OrderCreatedAt time.Time `json:"order_created_at"`
	SlotStatus     string    `json:"slot_status"`   // FREE/PENDING/LOCKED
	StrategyName   string    `json:"strategy_name"` // 策略名称
	StrategyType   string    `json:"strategy_type"` // 策略類型
}

// SetPositionManagerProvider 設置槽位數據提供者
func SetPositionManagerProvider(provider PositionManagerProvider) {
	positionManagerProvider = provider
}

// positionManagerAdapter 槽位管理器适配器
type positionManagerAdapter struct {
	manager *position.SuperPositionManager
}

// NewPositionManagerAdapter 創建槽位管理器适配器
func NewPositionManagerAdapter(manager *position.SuperPositionManager) PositionManagerProvider {
	return &positionManagerAdapter{manager: manager}
}

// GetAllSlots 獲取所有槽位信息
func (a *positionManagerAdapter) GetAllSlots() []SlotInfo {
	detailedSlots := a.manager.GetAllSlotsDetailed()

	// 🔥 調試：打印管理器的交易對信息
	symbol := a.manager.GetSymbol()
	exchange := a.manager.GetExchange()
	anchorPrice := a.manager.GetAnchorPrice()
	logger.Info("[DEBUG] GetAllSlots called - exchange=%s, symbol=%s, anchorPrice=%.2f, slotsCount=%d",
		exchange, symbol, anchorPrice, len(detailedSlots))

	slots := make([]SlotInfo, len(detailedSlots))
	for i, ds := range detailedSlots {
		slots[i] = SlotInfo{
			Exchange:       exchange,
			Symbol:         symbol,
			Price:          ds.Price,
			PositionStatus: ds.PositionStatus,
			PositionQty:    ds.PositionQty,
			OrderID:        ds.OrderID,
			ClientOID:      ds.ClientOID,
			OrderSide:      ds.OrderSide,
			OrderStatus:    ds.OrderStatus,
			OrderPrice:     ds.OrderPrice,
			OrderFilledQty: ds.OrderFilledQty,
			OrderCreatedAt: utils.ToUTC8(ds.OrderCreatedAt),
			SlotStatus:     ds.SlotStatus,
			StrategyName:   ds.StrategyName,
			StrategyType:   ds.StrategyType,
		}
	}
	return slots
}

// GetSlotCount 獲取槽位總數
func (a *positionManagerAdapter) GetSlotCount() int {
	return a.manager.GetSlotCount()
}

// GetReconcileCount 獲取對账次數
func (a *positionManagerAdapter) GetReconcileCount() int64 {
	return a.manager.GetReconcileCount()
}

// GetLastReconcileTime 獲取最后對账時间
func (a *positionManagerAdapter) GetLastReconcileTime() time.Time {
	return a.manager.GetLastReconcileTime()
}

// GetTotalBuyQty 獲取累计買入數量
func (a *positionManagerAdapter) GetTotalBuyQty() float64 {
	return a.manager.GetTotalBuyQty()
}

// GetTotalSellQty 獲取累计賣出數量
func (a *positionManagerAdapter) GetTotalSellQty() float64 {
	return a.manager.GetTotalSellQty()
}

// GetPriceInterval 獲取價格间隔
func (a *positionManagerAdapter) GetPriceInterval() float64 {
	return a.manager.GetPriceInterval()
}

// GetProfitSpread 獲取利潤間距（平倉價差）
func (a *positionManagerAdapter) GetProfitSpread() float64 {
	return a.manager.GetProfitSpread()
}

// GetLeverage 獲取杠杆倍數
func (a *positionManagerAdapter) GetLeverage() int {
	return a.manager.GetLeverage()
}

// getSlots 獲取所有槽位信息
// GET /api/slots
func getSlots(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"slots": []interface{}{}, "count": 0})
		return
	}

	slots := pmProvider.GetAllSlots()
	count := pmProvider.GetSlotCount()

	// 🔥 調試：打印前3個槽位的價格
	if len(slots) > 0 {
		logger.Info("[DEBUG] getSlots - exchange=%s, symbol=%s, total=%d, first 3 prices: %.2f, %.2f, %.2f",
			exchange, symbol, len(slots),
			slots[0].Price,
			slots[min(1, len(slots)-1)].Price,
			slots[min(2, len(slots)-1)].Price)
	}

	c.JSON(http.StatusOK, gin.H{
		"slots": slots,
		"count": count,
	})
}

// ========== 策略资金分配相关API ==========

var (
	// 策略數據提供者（需要從main.go注入）
	strategyProvider StrategyProvider
)

// StrategyProvider 策略资金分配提供者接口
type StrategyProvider interface {
	GetCapitalAllocation() map[string]StrategyCapitalInfo
	ReleaseLockedCapital(strategyName string) float64
	ReleaseAllLockedCapital() map[string]float64
}

// StrategyCapitalInfo 策略资金信息
type StrategyCapitalInfo struct {
	Allocated float64 `json:"allocated"`  // 分配的资金
	Used      float64 `json:"used"`       // 已使用的资金（保证金）
	Available float64 `json:"available"`  // 可用资金
	Weight    float64 `json:"weight"`     // 权重
	FixedPool float64 `json:"fixed_pool"` // 固定资金池（如果指定）
}

// SetStrategyProvider 設置策略數據提供者
func SetStrategyProvider(provider StrategyProvider) {
	strategyProvider = provider
}

// strategyProviderAdapter 策略提供者适配器
type strategyProviderAdapter struct {
	getAllocationFunc     func() map[string]StrategyCapitalInfo
	releaseCapitalFunc    func(strategyName string) float64
	releaseAllCapitalFunc func() map[string]float64
}

// NewStrategyProviderAdapter 創建策略提供者适配器
func NewStrategyProviderAdapter(
	getAllocationFunc func() map[string]StrategyCapitalInfo,
	releaseCapitalFunc func(strategyName string) float64,
	releaseAllCapitalFunc func() map[string]float64,
) StrategyProvider {
	return &strategyProviderAdapter{
		getAllocationFunc:     getAllocationFunc,
		releaseCapitalFunc:    releaseCapitalFunc,
		releaseAllCapitalFunc: releaseAllCapitalFunc,
	}
}

// GetCapitalAllocation 獲取策略资金分配信息
func (a *strategyProviderAdapter) GetCapitalAllocation() map[string]StrategyCapitalInfo {
	return a.getAllocationFunc()
}

// ReleaseLockedCapital 释放指定策略的锁定资金
func (a *strategyProviderAdapter) ReleaseLockedCapital(strategyName string) float64 {
	if a.releaseCapitalFunc != nil {
		return a.releaseCapitalFunc(strategyName)
	}
	return 0
}

// ReleaseAllLockedCapital 释放所有策略的锁定资金
func (a *strategyProviderAdapter) ReleaseAllLockedCapital() map[string]float64 {
	if a.releaseAllCapitalFunc != nil {
		return a.releaseAllCapitalFunc()
	}
	return map[string]float64{}
}

// getStrategyAllocation 獲取策略资金分配信息
// GET /api/strategies/allocation
func getStrategyAllocation(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusOK, gin.H{"allocation": map[string]interface{}{}})
		return
	}

	allocation := strategyProvider.GetCapitalAllocation()
	c.JSON(http.StatusOK, gin.H{"allocation": allocation})
}

// releaseStrategyCapital 释放策略的锁定资金
// POST /api/strategies/:id/release-capital
func releaseStrategyCapital(c *gin.Context) {
	strategyName := c.Param("id")
	if strategyName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少策略名称"})
		return
	}

	if strategyProvider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略服務未初始化"})
		return
	}

	released := strategyProvider.ReleaseLockedCapital(strategyName)
	logger.Info("💰 [手动释放资金] 策略 %s 已释放锁定资金: %.2f USDT", strategyName, released)

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"message":  fmt.Sprintf("已释放 %.2f USDT 锁定资金", released),
		"released": released,
		"strategy": strategyName,
	})
}

// releaseAllStrategiesCapital 释放所有策略的锁定资金
// POST /api/strategies/release-all-capital
func releaseAllStrategiesCapital(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略服務未初始化"})
		return
	}

	released := strategyProvider.ReleaseAllLockedCapital()

	totalReleased := 0.0
	for name, amount := range released {
		totalReleased += amount
		logger.Info("💰 [手动释放资金] 策略 %s 已释放锁定资金: %.2f USDT", name, amount)
	}

	c.JSON(http.StatusOK, gin.H{
		"success":        true,
		"message":        fmt.Sprintf("已释放所有策略的锁定资金，總計 %.2f USDT", totalReleased),
		"released":       released,
		"total_released": totalReleased,
	})
}

// ========== 待成交訂單相關API ==========

// getPendingOrders 獲取待成交订單列表
// GET /api/orders/pending
func getPendingOrders(c *gin.Context) {
	// 解析筛选参数
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	slots := pmProvider.GetAllSlots()
	var pendingOrders []PendingOrderInfo

	for _, slot := range slots {
		// 筛选状態為 PLACED/CONFIRMED/PARTIALLY_FILLED 的订單
		if slot.OrderStatus == "PLACED" || slot.OrderStatus == "CONFIRMED" || slot.OrderStatus == "PARTIALLY_FILLED" {
			// 按 exchange 和 symbol 筛选
			if exchange != "" && !strings.EqualFold(slot.Exchange, exchange) {
				continue
			}
			if symbol != "" && slot.Symbol != symbol {
				continue
			}

			// 计算订單原始數量：使用配置的订單金額 / 订單價格
			var quantity float64
			if slot.OrderPrice > 0 && orderQuantityConfig > 0 {
				quantity = orderQuantityConfig / slot.OrderPrice
			} else if slot.OrderFilledQty > 0 {
				// 如果無法计算，使用已成交數量作為估算
				quantity = slot.OrderFilledQty
			}

			pendingOrders = append(pendingOrders, PendingOrderInfo{
				OrderID:        slot.OrderID,
				ClientOrderID:  slot.ClientOID,
				Price:          slot.OrderPrice,
				Quantity:       quantity,
				Side:           slot.OrderSide,
				Status:         slot.OrderStatus,
				FilledQuantity: slot.OrderFilledQty,
				CreatedAt:      utils.ToUTC8(slot.OrderCreatedAt),
				SlotPrice:      slot.Price,
				StrategyName:   slot.StrategyName,
				StrategyType:   slot.StrategyType,
				Symbol:         slot.Symbol,
				Exchange:       slot.Exchange,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": pendingOrders, "count": len(pendingOrders)})
}

// PendingOrderInfo 待成交订單信息
type PendingOrderInfo struct {
	OrderID        int64     `json:"order_id"`
	ClientOrderID  string    `json:"client_order_id"`
	Price          float64   `json:"price"`
	Quantity       float64   `json:"quantity"`
	Side           string    `json:"side"` // BUY/SELL
	Status         string    `json:"status"`
	FilledQuantity float64   `json:"filled_quantity"`
	CreatedAt      time.Time `json:"created_at"`
	SlotPrice      float64   `json:"slot_price"`    // 槽位價格
	StrategyName   string    `json:"strategy_name"` // 策略名称
	StrategyType   string    `json:"strategy_type"` // 策略類型
	Symbol         string    `json:"symbol"`        // 交易對
	Exchange       string    `json:"exchange"`      // 交易所
}

// cancelOrder 取消订單
// POST /api/orders/:id/cancel
func cancelOrder(c *gin.Context) {
	orderIDStr := c.Param("id")
	orderID, err := strconv.ParseInt(orderIDStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "無效的订單ID"})
		return
	}

	// 獲取交易所和交易對
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchangeName == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 或 symbol 参數"})
		return
	}

	// 獲取交易所實例
	if exchangeGetterFunc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "交易所服務未初始化"})
		return
	}

	ex := exchangeGetterFunc(exchangeName)
	if ex == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "交易所不存在: " + exchangeName})
		return
	}

	// 調用交易所取消订單
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	if err := ex.CancelOrder(ctx, symbol, orderID); err != nil {
		logger.Error("❌ [取消订單] 失败: orderID=%d, exchange=%s, symbol=%s, error=%v", orderID, exchangeName, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "取消订單失败: " + err.Error()})
		return
	}

	logger.Info("✅ [取消订單] 成功: orderID=%d, exchange=%s, symbol=%s", orderID, exchangeName, symbol)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "订單已取消", "order_id": orderID})
}

// batchCancelOrders 批量取消订單
// POST /api/orders/cancel
func batchCancelOrders(c *gin.Context) {
	var req struct {
		OrderIDs []int64 `json:"order_ids"`
		Exchange string  `json:"exchange"`
		Symbol   string  `json:"symbol"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "無效的请求數據"})
		return
	}

	if len(req.OrderIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "订單ID列表為空"})
		return
	}

	if req.Exchange == "" || req.Symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "缺少 exchange 或 symbol 参數"})
		return
	}

	// 獲取交易所實例
	if exchangeGetterFunc == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "交易所服務未初始化"})
		return
	}

	ex := exchangeGetterFunc(req.Exchange)
	if ex == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "交易所不存在: " + req.Exchange})
		return
	}

	// 調用交易所批量取消订單
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	if err := ex.BatchCancelOrders(ctx, req.Symbol, req.OrderIDs); err != nil {
		logger.Error("❌ [批量取消订單] 失败: orderIDs=%v, exchange=%s, symbol=%s, error=%v", req.OrderIDs, req.Exchange, req.Symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "批量取消订單失败: " + err.Error()})
		return
	}

	logger.Info("✅ [批量取消订單] 成功: count=%d, exchange=%s, symbol=%s", len(req.OrderIDs), req.Exchange, req.Symbol)
	c.JSON(http.StatusOK, gin.H{"success": true, "message": fmt.Sprintf("已取消 %d 個订單", len(req.OrderIDs)), "count": len(req.OrderIDs)})
}

// ========== 日志相关API ==========

var (
	// 日志存儲提供者（需要從main.go注入）
	logStorageProvider LogStorageProvider
)

// LogStorageProvider 日志存儲提供者接口
type LogStorageProvider interface {
	GetLogs(startTime, endTime time.Time, level, keyword string, limit, offset int) ([]*LogRecordResponse, int, error)
	CleanOldLogsByLevel(days int, levels []string) (int64, error)
	Vacuum() error
	GetLogStats() (map[string]interface{}, error)
}

// logStorageAdapter 日志存儲适配器
type logStorageAdapter struct {
	storage *storage.LogStorage
}

// NewLogStorageAdapter 創建日志存儲适配器
func NewLogStorageAdapter(ls *storage.LogStorage) LogStorageProvider {
	return &logStorageAdapter{storage: ls}
}

// GetLogs 實現 LogStorageProvider 接口
func (a *logStorageAdapter) GetLogs(startTime, endTime time.Time, level, keyword string, limit, offset int) ([]*LogRecordResponse, int, error) {
	params := storage.LogQueryParams{
		StartTime: startTime,
		EndTime:   endTime,
		Level:     level,
		Keyword:   keyword,
		Limit:     limit,
		Offset:    offset,
	}

	logs, total, err := a.storage.GetLogs(params)
	if err != nil {
		return nil, 0, err
	}

	// 轉换為响应格式
	result := make([]*LogRecordResponse, len(logs))
	for i, log := range logs {
		result[i] = &LogRecordResponse{
			ID:        log.ID,
			Timestamp: utils.ToUTC8(log.Timestamp),
			Level:     log.Level,
			Message:   log.Message,
		}
	}

	return result, total, nil
}

// CleanOldLogsByLevel 實現 LogStorageProvider 接口
func (a *logStorageAdapter) CleanOldLogsByLevel(days int, levels []string) (int64, error) {
	return a.storage.CleanOldLogsByLevel(days, levels)
}

// Vacuum 實現 LogStorageProvider 接口
func (a *logStorageAdapter) Vacuum() error {
	return a.storage.Vacuum()
}

// GetLogStats 實現 LogStorageProvider 接口
func (a *logStorageAdapter) GetLogStats() (map[string]interface{}, error) {
	return a.storage.GetLogStats()
}

// LogRecordResponse 日志記錄响应
type LogRecordResponse struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// SetLogStorageProvider 設置日志存儲提供者
func SetLogStorageProvider(provider LogStorageProvider) {
	logStorageProvider = provider
}

// getLogs 獲取日志
// GET /api/logs
// 参數：
//   - start_time: 开始時间（可選，ISO 8601格式）
//   - end_time: 結束時间（可選，ISO 8601格式，默认當前時间）
//   - level: 日志级别（可選，DEBUG/INFO/WARN/ERROR/FATAL）
//   - keyword: 关键词搜索（可選）
//   - limit: 每页數量（可選，預設 100，最大1000）
//   - offset: 偏移量（可選，默认0）
func getLogs(c *gin.Context) {
	if logStorageProvider == nil {
		c.JSON(http.StatusOK, gin.H{"logs": []interface{}{}, "total": 0})
		return
	}

	// 解析参數
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	level := c.Query("level")
	keyword := c.Query("keyword")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 如果没有指定开始時间，默认最近7天
	if startTime.IsZero() {
		startTime = endTime.AddDate(0, 0, -7)
	}

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000
		}
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 查詢日志
	logs, total, err := logStorageProvider.GetLogs(startTime, endTime, level, keyword, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// cleanLogs 清理日志
// POST /api/logs/clean
// 参數：
//   - days: 保留天數（默认7天）
//   - levels: 要清理的日志级别列表，如 ["INFO", "WARN"]（可選，默认清理所有级别）
func cleanLogs(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存儲未初始化")
		return
	}

	var req struct {
		Days   int      `json:"days"`
		Levels []string `json:"levels"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_request")
		return
	}

	if req.Days <= 0 {
		req.Days = 7 // 默认7天
	}

	var rowsAffected int64
	var err error

	if len(req.Levels) > 0 {
		// 清理指定级别的日志
		rowsAffected, err = logStorageProvider.CleanOldLogsByLevel(req.Days, req.Levels)
	} else {
		// 清理所有级别的日志
		rowsAffected, err = logStorageProvider.CleanOldLogsByLevel(req.Days, []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"})
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"rows_affected": rowsAffected,
		"message":       fmt.Sprintf("已清理 %d 条日志", rowsAffected),
	})
}

// getLogStats 獲取日志统计信息
// GET /api/logs/stats
func getLogStats(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存儲未初始化")
		return
	}

	stats, err := logStorageProvider.GetLogStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// vacuumLogs 优化日志數據库
// POST /api/logs/vacuum
func vacuumLogs(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存儲未初始化")
		return
	}

	if err := logStorageProvider.Vacuum(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "數據库优化完成",
	})
}

// ReconciliationStatus 對账状態
type ReconciliationStatus struct {
	ReconcileCount    int64     `json:"reconcile_count"`     // 對账次數
	LastReconcileTime time.Time `json:"last_reconcile_time"` // 最后對账時间
	LocalPosition     float64   `json:"local_position"`      // 本地持倉
	TotalBuyQty       float64   `json:"total_buy_qty"`       // 累计買入
	TotalSellQty      float64   `json:"total_sell_qty"`      // 累计賣出
	EstimatedProfit   float64   `json:"estimated_profit"`    // 預计盈利
	ActualProfit      float64   `json:"actual_profit"`       // 實際盈利（来自 trades 表）
}

// ReconciliationHistoryInfo 對账历史信息
type ReconciliationHistoryInfo struct {
	ID               int64     `json:"id"`
	Exchange         string    `json:"exchange"`
	Symbol           string    `json:"symbol"`
	ReconcileTime    time.Time `json:"reconcile_time"`
	LocalPosition    float64   `json:"local_position"`
	ExchangePosition float64   `json:"exchange_position"`
	PositionDiff     float64   `json:"position_diff"`
	ActiveBuyOrders  int       `json:"active_buy_orders"`
	ActiveSellOrders int       `json:"active_sell_orders"`
	PendingSellQty   float64   `json:"pending_sell_qty"`
	TotalBuyQty      float64   `json:"total_buy_qty"`
	TotalSellQty     float64   `json:"total_sell_qty"`
	EstimatedProfit  float64   `json:"estimated_profit"`
	ActualProfit     float64   `json:"actual_profit"`
	CreatedAt        time.Time `json:"created_at"`
}

// getReconciliationStatus 獲取對账状態
// GET /api/reconciliation/status
func getReconciliationStatus(c *gin.Context) {
	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"reconcile_count":     0,
			"last_reconcile_time": time.Time{},
			"local_position":      0,
			"total_buy_qty":       0,
			"total_sell_qty":      0,
			"estimated_profit":    0,
			"actual_profit":       0,
		})
		return
	}

	// 從 PositionManager 獲取對账统计
	reconcileCount := pmProvider.GetReconcileCount()
	lastReconcileTime := pmProvider.GetLastReconcileTime()
	profitSpread := pmProvider.GetProfitSpread()

	// 优先從數據库實時计算累计買入和累计賣出（更准确，不受重啟影响）
	totalBuyQty := 0.0
	totalSellQty := 0.0

	storageProv := PickStorageProvider(c)
	symbol := c.Query("symbol")
	if symbol == "" {
		if st := pickStatus(c); st != nil {
			symbol = st.Symbol
		}
	}

	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		// 從數據库直接计算累计買入和累计賣出（更高效）
		accountID := GetCurrentAccountID()
		buyQty, sellQty, err := storageProv.GetStorage().GetTotalBuySellQty(symbol, accountID)
		if err == nil {
			totalBuyQty = buyQty
			totalSellQty = sellQty
			logger.Info("📊 [對账状態] 從數據库查詢: symbol=%s, accountID=%s, 累计買入=%.4f, 累计賣出=%.4f", symbol, accountID, buyQty, sellQty)
		} else {
			logger.Warn("⚠️ 查詢累计買賣數量失败: symbol=%s, accountID=%s, error=%v", symbol, accountID, err)
		}

		// 如果數據库查詢返回0，尝試不限制account再查詢一次（兼容舊數據）
		if totalBuyQty == 0 && totalSellQty == 0 && accountID != "" {
			buyQty2, sellQty2, err2 := storageProv.GetStorage().GetTotalBuySellQty(symbol, "")
			if err2 == nil && (buyQty2 > 0 || sellQty2 > 0) {
				totalBuyQty = buyQty2
				totalSellQty = sellQty2
				logger.Info("📊 [對账状態] 從數據库查詢(無account限制): symbol=%s, 累计買入=%.4f, 累计賣出=%.4f", symbol, buyQty2, sellQty2)
			}
		}
	}

	// 如果數據库中没有數據，尝試從記憶體獲取（作為后备）
	if totalBuyQty == 0 && totalSellQty == 0 {
		memBuyQty := pmProvider.GetTotalBuyQty()
		memSellQty := pmProvider.GetTotalSellQty()
		if memBuyQty > 0 || memSellQty > 0 {
			totalBuyQty = memBuyQty
			totalSellQty = memSellQty
			logger.Info("📊 [對账状態] 從記憶體獲取: symbol=%s, 累计買入=%.4f, 累计賣出=%.4f", symbol, memBuyQty, memSellQty)
		}
	}

	estimatedProfit := totalSellQty * profitSpread

	// 计算本地持倉
	slots := pmProvider.GetAllSlots()
	localPosition := 0.0
	for _, slot := range slots {
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 {
			localPosition += slot.PositionQty
		}
	}

	// 獲取實際盈利
	actualProfit := 0.0
	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		// 查詢截止到現在的累计實際盈利
		accountID := GetCurrentAccountID()
		actualProfit, _ = storageProv.GetStorage().GetActualProfitBySymbol(symbol, accountID, time.Now().UTC())
	}

	status := ReconciliationStatus{
		ReconcileCount:    reconcileCount,
		LastReconcileTime: utils.ToUTC8(lastReconcileTime),
		LocalPosition:     localPosition,
		TotalBuyQty:       totalBuyQty,
		TotalSellQty:      totalSellQty,
		EstimatedProfit:   estimatedProfit,
		ActualProfit:      actualProfit,
	}

	c.JSON(http.StatusOK, status)
}

// getReconciliationHistory 獲取對账历史
// GET /api/reconciliation/history
func getReconciliationHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析参數
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天，确保能查詢到更多历史記錄
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢對账历史
	histories, err := storage.QueryReconciliationHistory(exchangeName, symbol, accountID, startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為 API 响应格式
	result := make([]ReconciliationHistoryInfo, len(histories))
	for i, h := range histories {
		result[i] = ReconciliationHistoryInfo{
			ID:               h.ID,
			Exchange:         h.Exchange,
			Symbol:           h.Symbol,
			ReconcileTime:    utils.ToUTC8(h.ReconcileTime),
			LocalPosition:    h.LocalPosition,
			ExchangePosition: h.ExchangePosition,
			PositionDiff:     h.PositionDiff,
			ActiveBuyOrders:  h.ActiveBuyOrders,
			ActiveSellOrders: h.ActiveSellOrders,
			PendingSellQty:   h.PendingSellQty,
			TotalBuyQty:      h.TotalBuyQty,
			TotalSellQty:     h.TotalSellQty,
			EstimatedProfit:  h.EstimatedProfit,
			ActualProfit:     h.ActualProfit,
			CreatedAt:        utils.ToUTC8(h.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": result})
}

// ReconciliationAggregatedData 聚合的對账數據
type ReconciliationAggregatedData struct {
	Date                string  `json:"date"`                  // 日期（格式根據聚合類型：2026-01-25、2026-W04、2026-01）
	AvgLocalPosition    float64 `json:"avg_local_position"`    // 平均本地持倉
	AvgExchangePosition float64 `json:"avg_exchange_position"` // 平均交易所持倉
	AvgPositionDiff     float64 `json:"avg_position_diff"`     // 平均持倉差异
	TotalBuyQty         float64 `json:"total_buy_qty"`         // 累计買入
	TotalSellQty        float64 `json:"total_sell_qty"`        // 累计賣出
	EstimatedProfit     float64 `json:"estimated_profit"`      // 預计盈利
	ActualProfit        float64 `json:"actual_profit"`         // 實際盈利
	RecordCount         int     `json:"record_count"`          // 記錄數量
}

// getReconciliationAggregated 獲取聚合的對账數據
// GET /api/reconciliation/aggregated
// 参數: period=day|week|month, exchange, symbol, start_time, end_time
func getReconciliationAggregated(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"data": []interface{}{}})
		return
	}

	// 解析参數
	period := c.DefaultQuery("period", "day") // day, week, month
	exchangeName := c.Query("exchange")
	symbol := c.Query("symbol")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 根據聚合周期設置默认時间範圍
		switch period {
		case "month":
			startTime = time.Now().AddDate(0, -12, 0) // 最近12個月
		case "week":
			startTime = time.Now().AddDate(0, 0, -90) // 最近90天
		default: // day
			startTime = time.Now().AddDate(0, 0, -30) // 最近30天
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢對账历史（獲取所有數據用於聚合）
	histories, err := storage.QueryReconciliationHistory(exchangeName, symbol, accountID, startTime, endTime, 10000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 按時间聚合數據
	aggregatedMap := make(map[string]*ReconciliationAggregatedData)

	for _, h := range histories {
		var dateKey string
		t := h.ReconcileTime

		switch period {
		case "month":
			dateKey = t.Format("2006-01")
		case "week":
			year, week := t.ISOWeek()
			dateKey = fmt.Sprintf("%d-W%02d", year, week)
		default: // day
			dateKey = t.Format("2006-01-02")
		}

		if _, exists := aggregatedMap[dateKey]; !exists {
			aggregatedMap[dateKey] = &ReconciliationAggregatedData{
				Date: dateKey,
			}
		}

		agg := aggregatedMap[dateKey]
		agg.AvgLocalPosition += h.LocalPosition
		agg.AvgExchangePosition += h.ExchangePosition
		agg.AvgPositionDiff += h.PositionDiff

		// 對於累计值，取該時间段内的最大值（因為是累计的）
		if h.TotalBuyQty > agg.TotalBuyQty {
			agg.TotalBuyQty = h.TotalBuyQty
		}
		if h.TotalSellQty > agg.TotalSellQty {
			agg.TotalSellQty = h.TotalSellQty
		}
		if h.EstimatedProfit > agg.EstimatedProfit {
			agg.EstimatedProfit = h.EstimatedProfit
		}
		if h.ActualProfit > agg.ActualProfit {
			agg.ActualProfit = h.ActualProfit
		}

		agg.RecordCount++
	}

	// 计算平均值
	result := make([]ReconciliationAggregatedData, 0, len(aggregatedMap))
	for _, agg := range aggregatedMap {
		if agg.RecordCount > 0 {
			agg.AvgLocalPosition /= float64(agg.RecordCount)
			agg.AvgExchangePosition /= float64(agg.RecordCount)
			agg.AvgPositionDiff /= float64(agg.RecordCount)
		}
		result = append(result, *agg)
	}

	// 按日期排序
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})

	c.JSON(http.StatusOK, gin.H{"data": result})
}

// PnLSummaryResponse 盈亏彙總响应
type PnLSummaryResponse struct {
	Symbol        string  `json:"symbol"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	WinRate       float64 `json:"win_rate"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
}

// getPnLBySymbol 按币种對查詢盈亏數據
// GET /api/statistics/pnl/symbol
func getPnLBySymbol(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol_param")
		return
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢盈亏數據
	summary, err := storage.GetPnLBySymbol(symbol, accountID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := PnLSummaryResponse{
		Symbol:        summary.Symbol,
		TotalPnL:      summary.TotalPnL,
		TotalTrades:   summary.TotalTrades,
		TotalVolume:   summary.TotalVolume,
		WinRate:       summary.WinRate,
		WinningTrades: summary.WinningTrades,
		LosingTrades:  summary.LosingTrades,
	}

	c.JSON(http.StatusOK, response)
}

// PnLBySymbolResponse 按币种對的盈亏數據
type PnLBySymbolResponse struct {
	Symbol        string  `json:"symbol"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	WinRate       float64 `json:"win_rate"`
	UnrealizedPnL float64 `json:"unrealized_pnl,omitempty"` // 時段內最後一天的收盤未實現盈虧（來自每日快照）
}

// getPnLByTimeRange 按時间区间查詢盈亏數據（按币种對分组）
// GET /api/statistics/pnl/time-range
func getPnLByTimeRange(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢盈亏數據
	results, err := storage.GetPnLByTimeRange(accountID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為 API 响应格式，並補齊未實現盈虧（取自時段最後一天的每日快照）
	response := make([]PnLBySymbolResponse, len(results))
	endDate := time.Date(endTime.Year(), endTime.Month(), endTime.Day(), 0, 0, 0, 0, endTime.Location())
	for i, r := range results {
		resp := PnLBySymbolResponse{
			Symbol:      r.Symbol,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		}
		if snap, err := storage.GetDailySnapshot(r.Exchange, r.Symbol, accountID, endDate); err == nil && snap != nil {
			resp.UnrealizedPnL = snap.UnrealizedPnL
		}
		response[i] = resp
	}

	c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": response})
}

// ExchangePnLResponse 按交易所分组的盈亏响应
type ExchangePnLResponse struct {
	Exchange    string          `json:"exchange"`
	TotalPnL    float64         `json:"total_pnl"`
	TotalTrades int             `json:"total_trades"`
	TotalVolume float64         `json:"total_volume"`
	WinRate     float64         `json:"win_rate"`
	Symbols     []SymbolPnLInfo `json:"symbols"`
}

// SymbolPnLInfo 币种盈亏信息
type SymbolPnLInfo struct {
	Symbol      string  `json:"symbol"`
	TotalPnL    float64 `json:"total_pnl"`
	TotalTrades int     `json:"total_trades"`
	TotalVolume float64 `json:"total_volume"`
	WinRate     float64 `json:"win_rate"`
}

// getPnLByExchange 按交易所分组查詢盈亏數據
// GET /api/statistics/pnl/exchange
func getPnLByExchange(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"exchanges": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"exchanges": []interface{}{}})
		return
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()

	// 查詢所有币种的盈亏數據（現在包含 exchange 字段）
	results, err := storage.GetPnLByTimeRange(accountID, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 按交易所分组（直接使用 exchange 字段）
	exchangeMap := make(map[string]*ExchangePnLResponse)
	for _, r := range results {
		exchange := strings.ToLower(r.Exchange)
		if exchange == "" {
			// 兼容舊數據：如果没有 exchange，默认為 binance
			exchange = "binance"
		}

		if _, exists := exchangeMap[exchange]; !exists {
			exchangeMap[exchange] = &ExchangePnLResponse{
				Exchange:    exchange,
				TotalPnL:    0,
				TotalTrades: 0,
				TotalVolume: 0,
				WinRate:     0,
				Symbols:     []SymbolPnLInfo{},
			}
		}

		exData := exchangeMap[exchange]
		exData.TotalPnL += r.TotalPnL
		exData.TotalTrades += r.TotalTrades
		exData.TotalVolume += r.TotalVolume

		// 添加币种信息
		exData.Symbols = append(exData.Symbols, SymbolPnLInfo{
			Symbol:      r.Symbol,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		})
	}

	// 计算每個交易所的胜率
	for _, exData := range exchangeMap {
		if exData.TotalTrades > 0 {
			winningTrades := 0
			for _, sym := range exData.Symbols {
				winningTrades += int(float64(sym.TotalTrades) * sym.WinRate)
			}
			exData.WinRate = float64(winningTrades) / float64(exData.TotalTrades)
		}
	}

	// 轉换為列表
	response := make([]ExchangePnLResponse, 0, len(exchangeMap))
	for _, exData := range exchangeMap {
		response = append(response, *exData)
	}

	// 按交易所名称排序
	sort.Slice(response, func(i, j int) bool {
		return response[i].Exchange < response[j].Exchange
	})

	c.JSON(http.StatusOK, gin.H{"exchanges": response})
}

// getAnomalousTrades 检查异常交易記錄（用於調試盈亏计算问题）
// GET /api/statistics/anomalous-trades
func getAnomalousTrades(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"anomalous_trades": []interface{}{}})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"anomalous_trades": []interface{}{}})
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol_param")
		return
	}

	// 查詢所有交易記錄
	trades, err := st.QueryTrades(time.Time{}, time.Now(), 1000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var anomalousTrades []map[string]interface{}
	for _, trade := range trades {
		if trade.Symbol != symbol {
			continue
		}

		// 计算订單金額
		orderAmount := trade.BuyPrice * trade.Quantity

		// 检查是否异常：盈亏超過订單金額的50%可能是錯误的
		if orderAmount > 0 && math.Abs(trade.PnL) > orderAmount*0.5 {
			anomalousTrades = append(anomalousTrades, map[string]interface{}{
				"buy_order_id":  trade.BuyOrderID,
				"sell_order_id": trade.SellOrderID,
				"symbol":        trade.Symbol,
				"buy_price":     trade.BuyPrice,
				"sell_price":    trade.SellPrice,
				"quantity":      trade.Quantity,
				"pnl":           trade.PnL,
				"order_amount":  orderAmount,
				"pnl_rate":      (trade.PnL / orderAmount) * 100,
				"created_at":    utils.ToUTC8(trade.CreatedAt),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"anomalous_trades": anomalousTrades,
		"count":            len(anomalousTrades),
	})
}

// getExchangePnLDiagnosis 诊断交易所盈亏數據
// GET /api/statistics/pnl/diagnosis
func getExchangePnLDiagnosis(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"error": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"error": "存儲接口未就绪"})
		return
	}

	exchangeID := strings.ToLower(c.DefaultQuery("exchange", "binance"))
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认查詢所有历史數據
		startTime = time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 查詢該交易所的所有交易記錄
	trades, err := st.QueryTrades(startTime, endTime, 100000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 過滤指定交易所的交易
	var filteredTrades []*storage.Trade
	totalPnL := 0.0
	totalTrades := 0
	totalVolume := 0.0
	winningTrades := 0
	losingTrades := 0

	// 按币种分组统计
	symbolStats := make(map[string]map[string]interface{})

	// 按日期分组统计
	dateStats := make(map[string]map[string]interface{})

	for _, trade := range trades {
		tradeExchange := strings.ToLower(trade.Exchange)
		if tradeExchange == "" {
			tradeExchange = "binance" // 兼容舊數據
		}

		if tradeExchange != exchangeID {
			continue
		}

		filteredTrades = append(filteredTrades, trade)
		totalPnL += trade.PnL
		totalTrades++
		totalVolume += trade.Quantity

		if trade.PnL > 0 {
			winningTrades++
		} else if trade.PnL < 0 {
			losingTrades++
		}

		// 按币种统计
		if _, exists := symbolStats[trade.Symbol]; !exists {
			symbolStats[trade.Symbol] = map[string]interface{}{
				"total_pnl":      0.0,
				"total_trades":   0,
				"total_volume":   0.0,
				"winning_trades": 0,
				"losing_trades":  0,
			}
		}
		stats := symbolStats[trade.Symbol]
		stats["total_pnl"] = stats["total_pnl"].(float64) + trade.PnL
		stats["total_trades"] = stats["total_trades"].(int) + 1
		stats["total_volume"] = stats["total_volume"].(float64) + trade.Quantity
		if trade.PnL > 0 {
			stats["winning_trades"] = stats["winning_trades"].(int) + 1
		} else if trade.PnL < 0 {
			stats["losing_trades"] = stats["losing_trades"].(int) + 1
		}

		// 按日期统计
		dateStr := trade.CreatedAt.Format("2006-01-02")
		if _, exists := dateStats[dateStr]; !exists {
			dateStats[dateStr] = map[string]interface{}{
				"total_pnl":    0.0,
				"total_trades": 0,
			}
		}
		dateStat := dateStats[dateStr]
		dateStat["total_pnl"] = dateStat["total_pnl"].(float64) + trade.PnL
		dateStat["total_trades"] = dateStat["total_trades"].(int) + 1
	}

	// 计算平均盈亏
	avgPnL := 0.0
	if totalTrades > 0 {
		avgPnL = totalPnL / float64(totalTrades)
	}

	// 计算胜率
	winRate := 0.0
	if totalTrades > 0 {
		winRate = float64(winningTrades) / float64(totalTrades)
	}

	// 找出最大的單笔盈亏
	maxProfit := 0.0
	maxLoss := 0.0
	for _, trade := range filteredTrades {
		if trade.PnL > maxProfit {
			maxProfit = trade.PnL
		}
		if trade.PnL < maxLoss {
			maxLoss = trade.PnL
		}
	}

	// 轉换為列表格式
	symbolList := make([]map[string]interface{}, 0, len(symbolStats))
	for symbol, stats := range symbolStats {
		symbolList = append(symbolList, map[string]interface{}{
			"symbol":         symbol,
			"total_pnl":      stats["total_pnl"],
			"total_trades":   stats["total_trades"],
			"total_volume":   stats["total_volume"],
			"winning_trades": stats["winning_trades"],
			"losing_trades":  stats["losing_trades"],
		})
	}

	// 按日期排序
	dateList := make([]map[string]interface{}, 0, len(dateStats))
	for date, stats := range dateStats {
		dateList = append(dateList, map[string]interface{}{
			"date":         date,
			"total_pnl":    stats["total_pnl"],
			"total_trades": stats["total_trades"],
		})
	}
	sort.Slice(dateList, func(i, j int) bool {
		return dateList[i]["date"].(string) < dateList[j]["date"].(string)
	})

	c.JSON(http.StatusOK, gin.H{
		"exchange": exchangeID,
		"time_range": gin.H{
			"start": startTime.Format(time.RFC3339),
			"end":   endTime.Format(time.RFC3339),
		},
		"summary": gin.H{
			"total_pnl":      math.Round(totalPnL*100) / 100,
			"total_trades":   totalTrades,
			"total_volume":   math.Round(totalVolume*100) / 100,
			"winning_trades": winningTrades,
			"losing_trades":  losingTrades,
			"win_rate":       math.Round(winRate*10000) / 100, // 百分比，保留2位小數
			"avg_pnl":        math.Round(avgPnL*100) / 100,
			"max_profit":     math.Round(maxProfit*100) / 100,
			"max_loss":       math.Round(maxLoss*100) / 100,
		},
		"by_symbol": symbolList,
		"by_date":   dateList,
		"note":      "注意：當前盈亏计算未扣除手续费，實際亏损可能更大",
	})
}

// RiskMonitorProvider 风控監控提供者接口
type RiskMonitorProvider interface {
	IsTriggered() bool
	GetTriggeredTime() time.Time
	GetRecoveredTime() time.Time
	GetMonitorSymbols() []string
	GetSymbolData(symbol string) interface{}
}

var (
	riskMonitorProvider RiskMonitorProvider
)

// SetRiskMonitorProvider 設置风控監控提供者
func SetRiskMonitorProvider(provider RiskMonitorProvider) {
	riskMonitorProvider = provider
}

// RiskStatusResponse 风控状態响应
type RiskStatusResponse struct {
	Triggered      bool      `json:"triggered"`
	TriggeredTime  time.Time `json:"triggered_time"`
	RecoveredTime  time.Time `json:"recovered_time"`
	MonitorSymbols []string  `json:"monitor_symbols"`
}

// SymbolMonitorData 币种監控數據
type SymbolMonitorData struct {
	Symbol         string    `json:"symbol"`
	CurrentPrice   float64   `json:"current_price"`
	AveragePrice   float64   `json:"average_price"`
	PriceDeviation float64   `json:"price_deviation"`
	CurrentVolume  float64   `json:"current_volume"`
	AverageVolume  float64   `json:"average_volume"`
	VolumeRatio    float64   `json:"volume_ratio"`
	IsAbnormal     bool      `json:"is_abnormal"`
	LastUpdate     time.Time `json:"last_update"`
}

// getRiskStatus 獲取风控状態
// GET /api/risk/status
func getRiskStatus(c *gin.Context) {
	riskProv := PickRiskProvider(c)
	if riskProv == nil {
		c.JSON(http.StatusOK, RiskStatusResponse{
			Triggered:      false,
			MonitorSymbols: []string{},
		})
		return
	}

	response := RiskStatusResponse{
		Triggered:      riskProv.IsTriggered(),
		TriggeredTime:  riskProv.GetTriggeredTime(),
		RecoveredTime:  riskProv.GetRecoveredTime(),
		MonitorSymbols: riskProv.GetMonitorSymbols(),
	}

	c.JSON(http.StatusOK, response)
}

// getRiskMonitorData 獲取監控币种數據
// GET /api/risk/monitor
func getRiskMonitorData(c *gin.Context) {
	riskProv := PickRiskProvider(c)
	if riskProv == nil {
		c.JSON(http.StatusOK, gin.H{"symbols": []interface{}{}})
		return
	}

	symbols := riskProv.GetMonitorSymbols()
	var monitorData []SymbolMonitorData

	for _, symbol := range symbols {
		data := riskProv.GetSymbolData(symbol)
		if data == nil {
			continue
		}

		// 使用反射提取數據
		v := reflect.ValueOf(data)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}

		symbolData := SymbolMonitorData{
			Symbol: symbol,
		}

		// 提取字段
		if field := v.FieldByName("CurrentPrice"); field.IsValid() && field.CanFloat() {
			symbolData.CurrentPrice = field.Float()
		}
		if field := v.FieldByName("AveragePrice"); field.IsValid() && field.CanFloat() {
			symbolData.AveragePrice = field.Float()
		}
		if field := v.FieldByName("CurrentVolume"); field.IsValid() && field.CanFloat() {
			symbolData.CurrentVolume = field.Float()
		}
		if field := v.FieldByName("AverageVolume"); field.IsValid() && field.CanFloat() {
			symbolData.AverageVolume = field.Float()
		}
		if field := v.FieldByName("LastUpdate"); field.IsValid() {
			if t, ok := field.Interface().(time.Time); ok {
				symbolData.LastUpdate = t
			}
		}

		// 计算偏离度和比率
		if symbolData.AveragePrice > 0 {
			symbolData.PriceDeviation = (symbolData.CurrentPrice - symbolData.AveragePrice) / symbolData.AveragePrice * 100
		}
		if symbolData.AverageVolume > 0 {
			symbolData.VolumeRatio = symbolData.CurrentVolume / symbolData.AverageVolume
		}

		// 判断是否异常（简單判断）
		symbolData.IsAbnormal = math.Abs(symbolData.PriceDeviation) > 10 || symbolData.VolumeRatio > 3

		monitorData = append(monitorData, symbolData)
	}

	c.JSON(http.StatusOK, gin.H{"symbols": monitorData})
}

// RiskCheckHistoryResponse 风控检查历史响应
type RiskCheckHistoryResponse struct {
	CheckTime    time.Time             `json:"check_time"`
	Symbols      []RiskCheckSymbolInfo `json:"symbols"`
	HealthyCount int                   `json:"healthy_count"`
	TotalCount   int                   `json:"total_count"`
}

// RiskCheckSymbolInfo 风控检查币种信息
type RiskCheckSymbolInfo struct {
	Symbol         string  `json:"symbol"`
	IsHealthy      bool    `json:"is_healthy"`
	PriceDeviation float64 `json:"price_deviation"`
	VolumeRatio    float64 `json:"volume_ratio"`
	Reason         string  `json:"reason"`
}

// getRiskCheckHistory 獲取风控检查历史
// GET /api/risk/history
// 参數：
//   - start_time: 开始時间（可選，ISO 8601格式，默认最近90天）
//   - end_time: 結束時间（可選，ISO 8601格式，默认當前時间）
func getRiskCheckHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析参數
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.Query("limit")

	var startTime, endTime time.Time
	var err error
	limit := 500 // 默认限制500条

	if startTimeStr == "" {
		// 默认最近7天（减少默认數據量）
		startTime = time.Now().AddDate(0, 0, -7)
	} else {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr == "" {
		endTime = time.Now()
	} else {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}

	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			// 最大限制為2000条
			if limit > 2000 {
				limit = 2000
			}
		}
	}

	// 查詢历史數據
	histories, err := storage.QueryRiskCheckHistory(startTime, endTime, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為 API 响应格式
	result := make([]RiskCheckHistoryResponse, len(histories))
	for i, h := range histories {
		symbols := make([]RiskCheckSymbolInfo, len(h.Symbols))
		for j, s := range h.Symbols {
			symbols[j] = RiskCheckSymbolInfo{
				Symbol:         s.Symbol,
				IsHealthy:      s.IsHealthy,
				PriceDeviation: s.PriceDeviation,
				VolumeRatio:    s.VolumeRatio,
				Reason:         s.Reason,
			}
		}
		result[i] = RiskCheckHistoryResponse{
			CheckTime:    utils.ToUTC8(h.CheckTime),
			Symbols:      symbols,
			HealthyCount: h.HealthyCount,
			TotalCount:   h.TotalCount,
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": result})
}

// KlineData K線數據响应格式
type KlineData struct {
	Time   int64   `json:"time"` // 時间戳（秒）
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// getKlines 獲取K線數據
// GET /api/klines
// 查詢参數：
//   - interval: K線週期（1m/5m/15m/30m/1h/4h/1d等，默认1m）
//   - limit: 返回K線數量（默认500，最大1000）
func getKlines(c *gin.Context) {
	prov := pickExchangeProvider(c)
	if prov == nil {
		c.JSON(http.StatusOK, gin.H{"klines": []interface{}{}})
		return
	}

	// 獲取當前交易币种（從系统状態）
	symbol := c.Query("symbol")
	if symbol == "" {
		if st := pickStatus(c); st != nil {
			symbol = st.Symbol
		}
	}
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無法獲取交易币种"})
		return
	}

	// 解析查詢参數
	interval := c.DefaultQuery("interval", "1m")
	limitStr := c.DefaultQuery("limit", "500")

	limit := 500
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000
		}
	}

	// 呼叫交易所接口獲取K線數據
	ctx := c.Request.Context()
	candles, err := prov.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	// 統一裁剪插針（所有交易所）：將 High/Low 限制在鄰近價格 ±3% 內，避免壞 tick 導致圖表異常
	candles = exchange.ClipKlineSpikes(candles, 0.03)

	// 轉换為API响应格式
	klines := make([]KlineData, len(candles))
	for i, candle := range candles {
		// 將毫秒時间戳轉换為秒（lightweight-charts使用秒级時间戳）
		klines[i] = KlineData{
			Time:   candle.Timestamp / 1000,
			Open:   candle.Open,
			High:   candle.High,
			Low:    candle.Low,
			Close:  candle.Close,
			Volume: candle.Volume,
		}
	}

	c.JSON(http.StatusOK, gin.H{"klines": klines, "symbol": symbol, "interval": interval})
}

// ========== 资金费率相关API ==========

var (
	// 资金费率監控提供者（需要從main.go注入）
	fundingMonitorProvider FundingMonitorProvider
)

// FundingMonitorProvider 资金费率監控提供者接口
type FundingMonitorProvider interface {
	GetCurrentFundingRates() (map[string]float64, error)
}

// SetFundingMonitorProvider 設置资金费率監控提供者
func SetFundingMonitorProvider(provider FundingMonitorProvider) {
	fundingMonitorProvider = provider
}

// getFundingRate 獲取當前资金费率
// GET /api/funding/current
func getFundingRate(c *gin.Context) {
	fundingProv := PickFundingProvider(c)
	storageProv := PickStorageProvider(c)
	status := pickStatus(c)
	exchangeProv := pickExchangeProvider(c)
	rates := make(map[string]interface{})

	// 從監控服務獲取當前资金费率
	if fundingProv != nil {
		currentRates, err := fundingProv.GetCurrentFundingRates()
		if err == nil {
			for symbol, rate := range currentRates {
				rates[symbol] = map[string]interface{}{
					"rate":      rate,
					"rate_pct":  rate * 100, // 轉换為百分比
					"timestamp": time.Now(),
				}
			}
		}
	}

	// 從數據库獲取最新記錄
	exchangeName := ""
	if status != nil {
		exchangeName = status.Exchange
	}

	// 獲取主流交易對的最新资金费率
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT"}
	if storageProv != nil {
		storage := storageProv.GetStorage()
		if storage != nil {
			for _, symbol := range symbols {
				latestRate, err := storage.GetLatestFundingRate(symbol, exchangeName)
				if err == nil {
					// 如果監控服務没有提供，使用數據库中的值
					if _, exists := rates[symbol]; !exists {
						rates[symbol] = map[string]interface{}{
							"rate":      latestRate,
							"rate_pct":  latestRate * 100,
							"timestamp": time.Now(),
						}
					}
				}
			}
		}
	}

	// 如果某些交易對没有數據，從交易所API實時獲取缺失的數據
	if exchangeProv != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		for _, symbol := range symbols {
			// 如果該交易對已經有數據，跳過
			if _, exists := rates[symbol]; exists {
				continue
			}

			// 從交易所API實時獲取
			rate, err := exchangeProv.GetFundingRate(ctx, symbol)
			if err == nil {
				rates[symbol] = map[string]interface{}{
					"rate":      rate,
					"rate_pct":  rate * 100,
					"timestamp": time.Now(),
				}

				// 同時保存到數據库（如果存儲服務可用）
				if storageProv != nil {
					storage := storageProv.GetStorage()
					if storage != nil {
						_ = storage.SaveFundingRate(symbol, exchangeName, rate, time.Now().UTC())
					}
				}
			}
		}
		cancel()
	}

	c.JSON(http.StatusOK, gin.H{"rates": rates})
}

// getFundingRateHistory 獲取资金费率历史
// GET /api/funding/history
// 查詢参數：
//   - symbol: 交易對（可選）
//   - limit: 返回數量（預設 100）
func getFundingRateHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析查詢参數
	symbol := c.Query("symbol")
	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000 // 限制最大數量
		}
	}

	// 獲取交易所名称
	exchangeName := ""
	if currentStatus != nil {
		exchangeName = currentStatus.Exchange
	}

	// 查詢历史數據
	history, err := storage.GetFundingRateHistory(symbol, exchangeName, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為API响应格式
	response := make([]map[string]interface{}, len(history))
	for i, fr := range history {
		response[i] = map[string]interface{}{
			"id":         fr.ID,
			"symbol":     fr.Symbol,
			"exchange":   fr.Exchange,
			"rate":       fr.Rate,
			"rate_pct":   fr.Rate * 100, // 轉换為百分比
			"timestamp":  fr.Timestamp,
			"created_at": fr.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": response})
}

// ========== 市场情报數據源相关API ==========

var (
	// 數據源管理器提供者（需要從main.go注入）
	dataSourceProvider DataSourceProvider
)

// DataSourceProvider 數據源提供者接口
type DataSourceProvider interface {
	GetRSSFeeds() ([]RSSFeedInfo, error)
	GetFearGreedIndex() (*FearGreedIndexInfo, error)
	GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error)
	GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error)
}

// RSSFeedInfo RSS源信息
type RSSFeedInfo struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Items       []RSSItemInfo `json:"items"`
	LastUpdate  time.Time     `json:"last_update"`
}

// RSSItemInfo RSS项信息
type RSSItemInfo struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Link        string    `json:"link"`
	PubDate     time.Time `json:"pub_date"`
	Source      string    `json:"source"`
}

// FearGreedIndexInfo 恐慌贪婪指數信息
type FearGreedIndexInfo struct {
	Value          int       `json:"value"`
	Classification string    `json:"classification"`
	Timestamp      time.Time `json:"timestamp"`
}

// RedditPostInfo Reddit帖子信息
type RedditPostInfo struct {
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	URL         string    `json:"url"`
	Subreddit   string    `json:"subreddit"`
	Score       int       `json:"score"`
	UpvoteRatio float64   `json:"upvote_ratio"`
	CreatedAt   time.Time `json:"created_at"`
	Author      string    `json:"author"`
}

// PolymarketMarketInfo Polymarket市场信息
type PolymarketMarketInfo struct {
	ID          string    `json:"id"`
	Question    string    `json:"question"`
	Description string    `json:"description"`
	EndDate     time.Time `json:"end_date"`
	Outcomes    []string  `json:"outcomes"`
	Volume      float64   `json:"volume"`
	Liquidity   float64   `json:"liquidity"`
}

// SetDataSourceProvider 設置數據源提供者
func SetDataSourceProvider(provider DataSourceProvider) {
	dataSourceProvider = provider
}

// dataSourceAdapter 數據源适配器
// 注意：這個适配器使用反射来調用方法，避免循环依赖
type dataSourceAdapter struct {
	dsm              interface{}
	rssFeeds         []string
	fearGreedAPIURL  string
	polymarketAPIURL string
}

// NewDataSourceAdapter 創建數據源适配器
// dsm 应該是 *ai.DataSourceManager 類型，但使用 interface{} 避免循环依赖
func NewDataSourceAdapter(dsm interface{}, rssFeeds []string, fearGreedAPIURL, polymarketAPIURL string) DataSourceProvider {
	return &dataSourceAdapter{
		dsm:              dsm,
		rssFeeds:         rssFeeds,
		fearGreedAPIURL:  fearGreedAPIURL,
		polymarketAPIURL: polymarketAPIURL,
	}
}

// GetRSSFeeds 獲取RSS源
func (a *dataSourceAdapter) GetRSSFeeds() ([]RSSFeedInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	// 使用反射調用方法（避免循环依赖）
	dsmValue := reflect.ValueOf(a.dsm)
	if !dsmValue.IsValid() {
		return nil, fmt.Errorf("無效的數據源管理器")
	}

	feeds := make([]RSSFeedInfo, 0)

	// 如果没有配置RSS源，使用默认源
	rssFeeds := a.rssFeeds
	if len(rssFeeds) == 0 {
		rssFeeds = []string{
			"https://www.coindesk.com/arc/outboundfeeds/rss/",
			"https://cointelegraph.com/rss",
			"https://cryptonews.com/news/feed/",
		}
	}

	for _, feedURL := range rssFeeds {
		method := dsmValue.MethodByName("FetchRSSFeed")
		if !method.IsValid() {
			continue
		}

		results := method.Call([]reflect.Value{reflect.ValueOf(feedURL)})
		if len(results) != 2 {
			continue
		}

		if !results[1].IsNil() {
			// 錯误，跳過這個源
			continue
		}

		itemsValue := results[0]
		if itemsValue.IsNil() {
			continue
		}

		// 轉换為[]NewsItem（ai包中的類型）
		items := itemsValue.Interface()
		itemsSlice := reflect.ValueOf(items)
		if itemsSlice.Kind() != reflect.Slice {
			continue
		}

		rssItems := make([]RSSItemInfo, 0)
		for i := 0; i < itemsSlice.Len(); i++ {
			item := itemsSlice.Index(i)
			if !item.IsValid() {
				continue
			}

			// 提取字段
			title := getFieldString(item, "Title")
			description := getFieldString(item, "Description")
			url := getFieldString(item, "URL")
			source := getFieldString(item, "Source")
			pubDate := getFieldTime(item, "PublishedAt")

			rssItems = append(rssItems, RSSItemInfo{
				Title:       title,
				Description: description,
				Link:        url,
				PubDate:     pubDate,
				Source:      source,
			})
		}

		if len(rssItems) > 0 {
			// 從URL提取源名称
			sourceName := extractSourceName(feedURL)
			feeds = append(feeds, RSSFeedInfo{
				Title:       sourceName,
				Description: fmt.Sprintf("来自 %s 的加密貨幣新闻", sourceName),
				URL:         feedURL,
				Items:       rssItems,
				LastUpdate:  time.Now(),
			})
		}
	}

	return feeds, nil
}

// GetFearGreedIndex 獲取恐慌贪婪指數
func (a *dataSourceAdapter) GetFearGreedIndex() (*FearGreedIndexInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	apiURL := a.fearGreedAPIURL
	if apiURL == "" {
		apiURL = "https://api.alternative.me/fng/"
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchFearGreedIndex")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{reflect.ValueOf(apiURL)})
	if len(results) != 2 {
		return nil, fmt.Errorf("返回值數量錯误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	indexValue := results[0]
	if indexValue.IsNil() {
		return nil, fmt.Errorf("返回值為空")
	}

	index := indexValue.Elem()
	value := int(getFieldInt(index, "Value"))
	classification := getFieldString(index, "Classification")
	timestamp := getFieldTime(index, "Timestamp")

	return &FearGreedIndexInfo{
		Value:          value,
		Classification: classification,
		Timestamp:      timestamp,
	}, nil
}

// GetRedditPosts 獲取Reddit帖子
func (a *dataSourceAdapter) GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	if len(subreddits) == 0 {
		subreddits = []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchRedditPosts")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(subreddits),
		reflect.ValueOf(limit),
	})

	if len(results) != 2 {
		return nil, fmt.Errorf("返回值數量錯误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	postsValue := results[0]
	if postsValue.IsNil() {
		return []RedditPostInfo{}, nil
	}

	postsSlice := reflect.ValueOf(postsValue.Interface())
	if postsSlice.Kind() != reflect.Slice {
		return []RedditPostInfo{}, nil
	}

	posts := make([]RedditPostInfo, 0)
	for i := 0; i < postsSlice.Len(); i++ {
		post := postsSlice.Index(i)
		if !post.IsValid() {
			continue
		}

		posts = append(posts, RedditPostInfo{
			Title:       getFieldString(post, "Title"),
			Content:     getFieldString(post, "Content"),
			URL:         getFieldString(post, "URL"),
			Subreddit:   getFieldString(post, "Subreddit"),
			Score:       int(getFieldInt(post, "Score")),
			UpvoteRatio: getFieldFloat(post, "UpvoteRatio"),
			CreatedAt:   getFieldTime(post, "CreatedAt"),
			Author:      getFieldString(post, "Author"),
		})
	}

	return posts, nil
}

// GetPolymarketMarkets 獲取Polymarket市场
func (a *dataSourceAdapter) GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	apiURL := a.polymarketAPIURL
	if apiURL == "" {
		apiURL = "https://api.polymarket.com/graphql"
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchPolymarketMarkets")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(apiURL),
		reflect.ValueOf(keywords),
	})

	if len(results) != 2 {
		return nil, fmt.Errorf("返回值數量錯误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	marketsValue := results[0]
	if marketsValue.IsNil() {
		return []PolymarketMarketInfo{}, nil
	}

	marketsSlice := reflect.ValueOf(marketsValue.Interface())
	if marketsSlice.Kind() != reflect.Slice {
		return []PolymarketMarketInfo{}, nil
	}

	markets := make([]PolymarketMarketInfo, 0)
	for i := 0; i < marketsSlice.Len(); i++ {
		market := marketsSlice.Index(i)
		if !market.IsValid() {
			continue
		}

		// 处理指針類型
		if market.Kind() == reflect.Ptr {
			market = market.Elem()
		}

		outcomesValue := market.FieldByName("Outcomes")
		outcomes := []string{}
		if outcomesValue.IsValid() && outcomesValue.Kind() == reflect.Slice {
			for j := 0; j < outcomesValue.Len(); j++ {
				outcomes = append(outcomes, outcomesValue.Index(j).String())
			}
		}

		markets = append(markets, PolymarketMarketInfo{
			ID:          getFieldString(market, "ID"),
			Question:    getFieldString(market, "Question"),
			Description: getFieldString(market, "Description"),
			EndDate:     getFieldTime(market, "EndDate"),
			Outcomes:    outcomes,
			Volume:      getFieldFloat(market, "Volume"),
			Liquidity:   getFieldFloat(market, "Liquidity"),
		})
	}

	return markets, nil
}

// 辅助函數：從反射值獲取字符串字段
func getFieldString(v reflect.Value, fieldName string) string {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return ""
	}
	return field.String()
}

// 辅助函數：從反射值獲取整數字段
func getFieldInt(v reflect.Value, fieldName string) int64 {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return field.Int()
}

// 辅助函數：從反射值獲取浮点數字段
func getFieldFloat(v reflect.Value, fieldName string) float64 {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return field.Float()
}

// 辅助函數：從反射值獲取時间字段
func getFieldTime(v reflect.Value, fieldName string) time.Time {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return time.Now()
	}
	if t, ok := field.Interface().(time.Time); ok {
		return t
	}
	return time.Now()
}

// 辅助函數：從URL提取源名称
func extractSourceName(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return url
}

// getMarketIntelligence 獲取市場情报數據
// GET /api/market-intelligence
// 查詢参數：
//   - source: 數據源類型（rss, fear_greed, reddit, polymarket，默认全部）
//   - keyword: 搜索关键词（可選）
//   - limit: 返回數量限制（默认50）
func getMarketIntelligence(c *gin.Context) {
	if dataSourceProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"rss_feeds":    []interface{}{},
			"fear_greed":   nil,
			"reddit_posts": []interface{}{},
			"polymarket":   []interface{}{},
		})
		return
	}

	source := c.Query("source")
	keyword := c.Query("keyword")
	limitStr := c.DefaultQuery("limit", "50")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 200 {
			limit = 200 // 最大限制200
		}
	}

	result := make(map[string]interface{})

	// 獲取RSS新闻
	if source == "" || source == "rss" {
		rssFeeds, err := dataSourceProvider.GetRSSFeeds()
		if err == nil {
			// 如果有关键词，進行筛选
			if keyword != "" {
				filtered := make([]RSSFeedInfo, 0)
				keywordLower := strings.ToLower(keyword)
				for _, feed := range rssFeeds {
					filteredItems := make([]RSSItemInfo, 0)
					for _, item := range feed.Items {
						titleLower := strings.ToLower(item.Title)
						descLower := strings.ToLower(item.Description)
						if strings.Contains(titleLower, keywordLower) || strings.Contains(descLower, keywordLower) {
							filteredItems = append(filteredItems, item)
						}
					}
					if len(filteredItems) > 0 {
						feed.Items = filteredItems[:min(len(filteredItems), limit)]
						filtered = append(filtered, feed)
					}
				}
				result["rss_feeds"] = filtered
			} else {
				// 限制每個源的条目數
				for i := range rssFeeds {
					if len(rssFeeds[i].Items) > limit {
						rssFeeds[i].Items = rssFeeds[i].Items[:limit]
					}
				}
				result["rss_feeds"] = rssFeeds
			}
		} else {
			result["rss_feeds"] = []interface{}{}
		}
	}

	// 獲取恐慌贪婪指數
	if source == "" || source == "fear_greed" {
		fearGreed, err := dataSourceProvider.GetFearGreedIndex()
		if err == nil {
			result["fear_greed"] = fearGreed
		} else {
			result["fear_greed"] = nil
		}
	}

	// 獲取Reddit帖子
	if source == "" || source == "reddit" {
		// 默认子版塊
		subreddits := []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
		redditPosts, err := dataSourceProvider.GetRedditPosts(subreddits, limit)
		if err == nil {
			// 如果有关键词，進行筛选
			if keyword != "" {
				filtered := make([]RedditPostInfo, 0)
				keywordLower := strings.ToLower(keyword)
				for _, post := range redditPosts {
					titleLower := strings.ToLower(post.Title)
					contentLower := strings.ToLower(post.Content)
					if strings.Contains(titleLower, keywordLower) || strings.Contains(contentLower, keywordLower) {
						filtered = append(filtered, post)
					}
				}
				result["reddit_posts"] = filtered[:min(len(filtered), limit)]
			} else {
				result["reddit_posts"] = redditPosts
			}
		} else {
			result["reddit_posts"] = []interface{}{}
		}
	}

	// 獲取Polymarket市场
	if source == "" || source == "polymarket" {
		keywords := []string{}
		if keyword != "" {
			keywords = []string{keyword}
		}
		polymarketMarkets, err := dataSourceProvider.GetPolymarketMarkets(keywords)
		if err == nil {
			if len(polymarketMarkets) > limit {
				result["polymarket"] = polymarketMarkets[:limit]
			} else {
				result["polymarket"] = polymarketMarkets
			}
		} else {
			result["polymarket"] = []interface{}{}
		}
	}

	c.JSON(http.StatusOK, result)
}

// min 返回两個整數中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ========== AI分析相关API ==========

var (
	// AI模塊提供者（需要從main.go注入）
	aiMarketAnalyzerProvider     AIMarketAnalyzerProvider
	aiParameterOptimizerProvider AIParameterOptimizerProvider
	aiRiskAnalyzerProvider       AIRiskAnalyzerProvider
	aiSentimentAnalyzerProvider  AISentimentAnalyzerProvider
	aiPolymarketSignalProvider   AIPolymarketSignalProvider
	aiPromptManagerProvider      AIPromptManagerProvider
)

// AI提供者接口
type AIMarketAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIParameterOptimizerProvider interface {
	GetLastOptimization() interface{}
	GetLastOptimizationTime() time.Time
	PerformOptimization() error
}

type AIRiskAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AISentimentAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIPolymarketSignalProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIPromptManagerProvider interface {
	GetAllPrompts() (map[string]interface{}, error)
	UpdatePrompt(module, template, systemPrompt string) error
}

// SetAIProviders 設置AI提供者
func SetAIMarketAnalyzerProvider(provider AIMarketAnalyzerProvider) {
	aiMarketAnalyzerProvider = provider
}

func SetAIParameterOptimizerProvider(provider AIParameterOptimizerProvider) {
	aiParameterOptimizerProvider = provider
}

func SetAIRiskAnalyzerProvider(provider AIRiskAnalyzerProvider) {
	aiRiskAnalyzerProvider = provider
}

func SetAISentimentAnalyzerProvider(provider AISentimentAnalyzerProvider) {
	aiSentimentAnalyzerProvider = provider
}

func SetAIPolymarketSignalProvider(provider AIPolymarketSignalProvider) {
	aiPolymarketSignalProvider = provider
}

func SetAIPromptManagerProvider(provider AIPromptManagerProvider) {
	aiPromptManagerProvider = provider
}

// getAIAnalysisStatus 獲取AI系统状態
// GET /api/ai/status
func getAIAnalysisStatus(c *gin.Context) {
	status := map[string]interface{}{
		"enabled": true,
		"modules": map[string]interface{}{
			"market_analysis": map[string]interface{}{
				"enabled":     aiMarketAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"parameter_optimization": map[string]interface{}{
				"enabled":     aiParameterOptimizerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"risk_analysis": map[string]interface{}{
				"enabled":     aiRiskAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"sentiment_analysis": map[string]interface{}{
				"enabled":     aiSentimentAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"polymarket_signal": map[string]interface{}{
				"enabled":     aiPolymarketSignalProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
		},
	}

	// 更新各模塊状態
	if aiMarketAnalyzerProvider != nil {
		lastTime := aiMarketAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiMarketAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["market_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["market_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiParameterOptimizerProvider != nil {
		lastTime := aiParameterOptimizerProvider.GetLastOptimizationTime()
		lastOptimization := aiParameterOptimizerProvider.GetLastOptimization()
		status["modules"].(map[string]interface{})["parameter_optimization"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["parameter_optimization"].(map[string]interface{})["has_data"] = lastOptimization != nil
	}

	if aiRiskAnalyzerProvider != nil {
		lastTime := aiRiskAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiRiskAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["risk_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["risk_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiSentimentAnalyzerProvider != nil {
		lastTime := aiSentimentAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiSentimentAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["sentiment_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["sentiment_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiPolymarketSignalProvider != nil {
		lastTime := aiPolymarketSignalProvider.GetLastAnalysisTime()
		lastAnalysis := aiPolymarketSignalProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["polymarket_signal"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["polymarket_signal"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	c.JSON(http.StatusOK, status)
}

// getAIMarketAnalysis 獲取市場分析結果
// GET /api/ai/analysis/market
func getAIMarketAnalysis(c *gin.Context) {
	if aiMarketAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "市场分析模塊未啟用"})
		return
	}

	analysis := aiMarketAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiMarketAnalyzerProvider.GetLastAnalysisTime()})
}

// getAIParameterOptimization 獲取参數优化結果
// GET /api/ai/analysis/parameter
func getAIParameterOptimization(c *gin.Context) {
	if aiParameterOptimizerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "参數优化模塊未啟用"})
		return
	}

	optimization := aiParameterOptimizerProvider.GetLastOptimization()
	if optimization == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無优化數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"optimization": optimization, "last_update": aiParameterOptimizerProvider.GetLastOptimizationTime()})
}

// getAIRiskAnalysis 獲取风險分析結果
// GET /api/ai/analysis/risk
func getAIRiskAnalysis(c *gin.Context) {
	if aiRiskAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "风險分析模塊未啟用"})
		return
	}

	analysis := aiRiskAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiRiskAnalyzerProvider.GetLastAnalysisTime()})
}

// getAISentimentAnalysis 獲取情绪分析結果
// GET /api/ai/analysis/sentiment
func getAISentimentAnalysis(c *gin.Context) {
	if aiSentimentAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "情绪分析模塊未啟用"})
		return
	}

	analysis := aiSentimentAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiSentimentAnalyzerProvider.GetLastAnalysisTime()})
}

// getAIPolymarketSignal 獲取Polymarket信号分析結果
// GET /api/ai/analysis/polymarket
func getAIPolymarketSignal(c *gin.Context) {
	if aiPolymarketSignalProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "Polymarket信号模塊未啟用"})
		return
	}

	analysis := aiPolymarketSignalProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiPolymarketSignalProvider.GetLastAnalysisTime()})
}

// triggerAIAnalysis 手动触发AI分析
// POST /api/ai/analysis/trigger/:module
func triggerAIAnalysis(c *gin.Context) {
	module := c.Param("module")
	var err error

	switch module {
	case "market":
		if aiMarketAnalyzerProvider != nil {
			err = aiMarketAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "市场分析模塊未啟用"})
			return
		}
	case "parameter":
		if aiParameterOptimizerProvider != nil {
			err = aiParameterOptimizerProvider.PerformOptimization()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参數优化模塊未啟用"})
			return
		}
	case "risk":
		if aiRiskAnalyzerProvider != nil {
			err = aiRiskAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "风險分析模塊未啟用"})
			return
		}
	case "sentiment":
		if aiSentimentAnalyzerProvider != nil {
			err = aiSentimentAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "情绪分析模塊未啟用"})
			return
		}
	case "polymarket":
		if aiPolymarketSignalProvider != nil {
			err = aiPolymarketSignalProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Polymarket信号模塊未啟用"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的模塊: " + module})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "分析已触发"})
}

// getAIPrompts 獲取所有提示词模板
// GET /api/ai/prompts
func getAIPrompts(c *gin.Context) {
	if aiPromptManagerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"prompts": map[string]interface{}{}})
		return
	}

	prompts, err := aiPromptManagerProvider.GetAllPrompts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"prompts": prompts})
}

// updateAIPrompt 更新提示词模板
// POST /api/ai/prompts
func updateAIPrompt(c *gin.Context) {
	if aiPromptManagerProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "提示词管理器未啟用"})
		return
	}

	var req struct {
		Module       string `json:"module"`
		Template     string `json:"template"`
		SystemPrompt string `json:"system_prompt"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Module == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模塊名不能為空"})
		return
	}

	if req.Template == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "提示词模板不能為空"})
		return
	}

	if err := aiPromptManagerProvider.UpdatePrompt(req.Module, req.Template, req.SystemPrompt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "提示词已更新"})
}

// AI模塊适配器
type aiMarketAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiMarketAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiMarketAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiMarketAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiParameterOptimizerAdapter struct {
	optimizer interface {
		GetLastOptimization() interface{}
		GetLastOptimizationTime() time.Time
		PerformOptimization() error
	}
}

func (a *aiParameterOptimizerAdapter) GetLastOptimization() interface{} {
	return a.optimizer.GetLastOptimization()
}

func (a *aiParameterOptimizerAdapter) GetLastOptimizationTime() time.Time {
	return a.optimizer.GetLastOptimizationTime()
}

func (a *aiParameterOptimizerAdapter) PerformOptimization() error {
	return a.optimizer.PerformOptimization()
}

type aiRiskAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiRiskAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiRiskAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiRiskAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiSentimentAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiSentimentAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiSentimentAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiSentimentAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiPolymarketSignalAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiPolymarketSignalAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiPolymarketSignalAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiPolymarketSignalAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiPromptManagerAdapter struct {
	manager interface {
		GetAllPrompts() (map[string]interface{}, error)
		UpdatePrompt(module, template, systemPrompt string) error
	}
}

func (a *aiPromptManagerAdapter) GetAllPrompts() (map[string]interface{}, error) {
	return a.manager.GetAllPrompts()
}

func (a *aiPromptManagerAdapter) UpdatePrompt(module, template, systemPrompt string) error {
	return a.manager.UpdatePrompt(module, template, systemPrompt)
}

// ==================== 價差監控 API ====================

// BasisMonitorProvider 價差監控提供者接口
type BasisMonitorProvider interface {
	GetCurrentBasis(symbol string) (*storage.BasisData, error)
	GetAllCurrentBasis() []*storage.BasisData
	GetBasisHistory(symbol string, limit int) ([]*storage.BasisData, error)
	GetBasisStatistics(symbol string, hours int) (*storage.BasisStats, error)
}

var (
	basisMonitorProvider BasisMonitorProvider
	basisMonitorMu       sync.RWMutex
)

// SetBasisMonitorProvider 設置價差監控提供者
func SetBasisMonitorProvider(provider BasisMonitorProvider) {
	basisMonitorMu.Lock()
	defer basisMonitorMu.Unlock()
	basisMonitorProvider = provider
}

// getBasisMonitorProvider 獲取價差監控提供者
func getBasisMonitorProvider() BasisMonitorProvider {
	basisMonitorMu.RLock()
	defer basisMonitorMu.RUnlock()
	return basisMonitorProvider
}

// getBasisCurrent 獲取當前價差數據
// GET /api/basis/current?symbol=BTCUSDT
func getBasisCurrent(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		// 如果没有指定交易對，返回所有交易對的當前價差
		allBasis := provider.GetAllCurrentBasis()
		c.JSON(http.StatusOK, gin.H{
			"data":  allBasis,
			"count": len(allBasis),
		})
		return
	}

	// 獲取指定交易對的價差
	data, err := provider.GetCurrentBasis(symbol)
	if err != nil {
		respondError(c, http.StatusNotFound, "errors.not_found", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}

// getBasisHistory 獲取價差历史數據
// GET /api/basis/history?symbol=BTCUSDT&limit=100
func getBasisHistory(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	history, err := provider.GetBasisHistory(symbol, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  history,
		"count": len(history),
	})
}

// getBasisStatistics 獲取價差统计數據
// GET /api/basis/statistics?symbol=BTCUSDT&hours=24
func getBasisStatistics(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	hours := 24
	if hoursStr := c.Query("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	stats, err := provider.GetBasisStatistics(symbol, hours)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// getAllocationStatus 獲取资金分配状態
// GET /api/allocation/status
func getAllocationStatus(c *gin.Context) {
	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return
	}

	// 獲取所有运行中的交易對
	runtimes := symbolManagerProvider.List()

	allStatuses := make([]map[string]interface{}, 0)

	for _, rt := range runtimes {
		// 使用反射獲取 AllocationManager
		rtVal := reflect.ValueOf(rt)
		if rtVal.Kind() == reflect.Ptr {
			rtVal = rtVal.Elem()
		}

		// 尝試獲取 PositionManager
		posManagerField := rtVal.FieldByName("PositionManager")
		if !posManagerField.IsValid() || posManagerField.IsNil() {
			continue
		}

		posManager := posManagerField.Interface()
		posManagerVal := reflect.ValueOf(posManager)
		if posManagerVal.Kind() == reflect.Ptr {
			posManagerVal = posManagerVal.Elem()
		}

		// 獲取 allocationManager
		allocManagerField := posManagerVal.FieldByName("allocationManager")
		if !allocManagerField.IsValid() || allocManagerField.IsNil() {
			continue
		}

		// 調用 GetAllStatuses 方法
		allocManager := allocManagerField.Interface()
		method := reflect.ValueOf(allocManager).MethodByName("GetAllStatuses")
		if !method.IsValid() {
			continue
		}

		results := method.Call(nil)
		if len(results) > 0 {
			statuses := results[0].Interface()
			if statusList, ok := statuses.([]*position.AllocationStatus); ok {
				for _, status := range statusList {
					allStatuses = append(allStatuses, map[string]interface{}{
						"exchange":         status.Exchange,
						"symbol":           status.Symbol,
						"max_amount":       status.MaxAmount,
						"used_amount":      status.UsedAmount,
						"available_amount": status.AvailableAmount,
						"usage_percentage": status.UsagePercentage,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"allocations": allStatuses,
		"count":       len(allStatuses),
	})
}

// getAllocationStatusBySymbol 獲取指定交易對的资金分配状態
// GET /api/allocation/status/:exchange/:symbol
func getAllocationStatusBySymbol(c *gin.Context) {
	exchange := c.Param("exchange")
	symbol := c.Param("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return
	}

	// 獲取指定的运行時
	rtInterface, exists := symbolManagerProvider.Get(exchange, symbol)
	if !exists {
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return
	}

	// 使用反射獲取 AllocationManager
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}

	// 尝試獲取 PositionManager
	posManagerField := rtVal.FieldByName("PositionManager")
	if !posManagerField.IsValid() || posManagerField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.position_manager_unavailable")
		return
	}

	posManager := posManagerField.Interface()
	posManagerVal := reflect.ValueOf(posManager)
	if posManagerVal.Kind() == reflect.Ptr {
		posManagerVal = posManagerVal.Elem()
	}

	// 獲取 allocationManager
	allocManagerField := posManagerVal.FieldByName("allocationManager")
	if !allocManagerField.IsValid() || allocManagerField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.allocation_manager_unavailable")
		return
	}

	// 調用 GetStatus 方法
	allocManager := allocManagerField.Interface()
	method := reflect.ValueOf(allocManager).MethodByName("GetStatus")
	if !method.IsValid() {
		respondError(c, http.StatusInternalServerError, "error.method_unavailable")
		return
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(exchange),
		reflect.ValueOf(symbol),
	})

	if len(results) > 0 && !results[0].IsNil() {
		status := results[0].Interface().(*position.AllocationStatus)
		c.JSON(http.StatusOK, gin.H{
			"exchange":         status.Exchange,
			"symbol":           status.Symbol,
			"max_amount":       status.MaxAmount,
			"used_amount":      status.UsedAmount,
			"available_amount": status.AvailableAmount,
			"usage_percentage": status.UsagePercentage,
		})
		return
	}

	respondError(c, http.StatusNotFound, "error.allocation_not_found")
}

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
		GeminiAPIKey   string                 `json:"gemini_api_key"` // 可選，前端傳入的 API Key

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
	if configManager == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}

	// 獲取 Gemini API Key
	// 优先使用请求中傳入的 Key，否则使用配置文件中的 Key
	geminiAPIKey := req.GeminiAPIKey
	if geminiAPIKey == "" {
		// 獲取 Gemini API Key（优先使用 gemini_api_key，否则使用 api_key）
		geminiAPIKey = cfg.AI.GeminiAPIKey
		if geminiAPIKey == "" {
			geminiAPIKey = cfg.AI.APIKey
		}
	}

	if geminiAPIKey == "" {
		respondError(c, http.StatusBadRequest, "error.gemini_api_key_not_configured")
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

		geminiClient := ai.NewGeminiClient(geminiAPIKey)

		logger.Info("🔄 [AI任務] %s 調用 Gemini API 生成配置", task.TaskID)
		aiConfig, err := geminiClient.GenerateConfig(ctx, &ai.GenerateConfigRequest{
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
		configPath := configManager.GetConfigPath()
		configService := ai.NewConfigService(configPath)
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

	if configManager == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}

	configPath := configManager.GetConfigPath()
	configService := ai.NewConfigService(configPath)
	if err := configService.ApplyAIConfig(&req, cfg); err != nil {
		logger.Error("❌ 应用 AI 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.apply_config_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "配置已成功应用，请重啟服務使配置生效",
	})
}
