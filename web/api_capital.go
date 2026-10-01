package web

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
)

// 資金概覽緩存，降低頻繁刷新時的 GetAccount 調用
var (
	capitalOverviewCache struct {
		mu       sync.RWMutex
		overview CapitalOverview
		at       time.Time
		ttl      time.Duration
	}
)

func init() {
	capitalOverviewCache.ttl = 3 * time.Second
}

// CapitalDataSource 资金數據源介面（由 main.go 實現）
type CapitalDataSource interface {
	GetExchanges() []exchange.IExchange
	GetStrategyConfigs() map[string]config.StrategyConfig
	GetPositionManagers() []PositionManagerInfo
	GetConfig() *config.Config // 新增
}

// PositionManagerInfo 倉位管理器信息
type PositionManagerInfo struct {
	Exchange string
	Symbol   string
	Manager  *position.SuperPositionManager
}

var capitalDataSource CapitalDataSource

func getCapitalReservationsHandler(c *gin.Context) {
	sessionValue, ok := c.Get("session")
	session, isSession := sessionValue.(*Session)
	if c.GetBool("local_dev_mode") || !ok || !isSession || session.Role != "admin" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	if storageServiceProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "capital_reservation_storage_unavailable"})
		return
	}
	store := storageServiceProvider.GetStorage()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "capital_reservation_storage_unavailable"})
		return
	}
	reader, ok := store.(storage.AccountWalletCapitalReservationReader)
	if !ok {
		c.JSON(http.StatusNotImplemented, gin.H{"error": "capital_reservation_audit_unsupported"})
		return
	}
	afterWalletKey, afterBotKey, err := decodeCapitalReservationCursor(strings.TrimSpace(c.Query("cursor")))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_cursor"})
		return
	}
	pageSize := storage.AccountWalletCapitalReservationAuditPageSize
	reservations, err := reader.ListAccountWalletCapitalReservations(c.Request.Context(), afterWalletKey, afterBotKey, pageSize)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "list account wallet capital reservations: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "capital_reservation_audit_failed"})
		return
	}
	hasMore := len(reservations) > pageSize
	nextCursor := ""
	if hasMore {
		reservations = reservations[:pageSize]
		last := reservations[len(reservations)-1]
		nextCursor = encodeCapitalReservationCursor(last.WalletKey, last.BotKey)
	}
	c.JSON(http.StatusOK, gin.H{"reservations": reservations, "page_size": pageSize, "has_more": hasMore, "next_cursor": nextCursor})
}

func encodeCapitalReservationCursor(walletKey, botKey string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(walletKey + ":" + botKey))
}

func decodeCapitalReservationCursor(value string) (string, string, error) {
	if value == "" {
		return "", "", nil
	}
	decoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil || len(decoded) != 129 {
		return "", "", fmt.Errorf("invalid reservation cursor")
	}
	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid reservation cursor")
	}
	for _, part := range parts {
		digest, err := hex.DecodeString(part)
		if err != nil || len(digest) != 32 {
			return "", "", fmt.Errorf("invalid reservation cursor")
		}
	}
	return parts[0], parts[1], nil
}

// SetCapitalDataSource 設置资金數據源
func SetCapitalDataSource(ds CapitalDataSource) {
	capitalDataSource = ds
}

// CapitalOverview 资金概览（彙總或分交易所）
type CapitalOverview struct {
	TotalBalance      float64                  `json:"totalBalance"`     // 總权益
	AllocatedCapital  float64                  `json:"allocatedCapital"` // 已分配给策略的资金
	UsedCapital       float64                  `json:"usedCapital"`      // 實際已占用保证金
	AvailableCapital  float64                  `json:"availableCapital"` // 交易所可用餘額
	ReservedCapital   float64                  `json:"reservedCapital"`  // 用戶預留资金（不可用於策略）
	UnrealizedPnL     float64                  `json:"unrealizedPnL"`    // 未實現盈亏
	MarginRatio       float64                  `json:"marginRatio"`      // 保证金占用率
	Exchanges         []ExchangeCapitalSummary `json:"exchanges,omitempty"`
	ValuationComplete bool                     `json:"valuationComplete"`
	ValuationError    string                   `json:"valuationError,omitempty"`
	ValuationAsset    string                   `json:"valuationAsset,omitempty"`
	LastUpdated       string                   `json:"lastUpdated"`
}

// getCompleteExchangeBalance only returns an aggregate when every unique
// exchange account produced a finite balance. Partial snapshots must never
// drive allocation or rebalancing decisions.
func getCompleteExchangeBalance(ctx context.Context, exchanges []exchange.IExchange) (float64, error) {
	seen := make(map[string]struct{}, len(exchanges))
	var total float64
	valuationAsset := ""
	for _, ex := range exchanges {
		if ex == nil {
			return 0, fmt.Errorf("exchange account source contains a nil exchange")
		}
		name := strings.ToLower(strings.TrimSpace(ex.GetName()))
		if name == "" {
			return 0, fmt.Errorf("exchange account source contains an unnamed exchange")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		account, err := ex.GetAccount(ctx)
		if err != nil {
			return 0, fmt.Errorf("read %s account balance: %w", name, err)
		}
		if account == nil || math.IsNaN(account.TotalMarginBalance) || math.IsInf(account.TotalMarginBalance, 0) || account.TotalMarginBalance < 0 {
			return 0, fmt.Errorf("%s account returned an invalid balance snapshot", name)
		}
		asset := strings.ToUpper(strings.TrimSpace(account.BalanceAsset))
		if asset == "" || !strings.EqualFold(asset, ex.GetQuoteAsset()) {
			return 0, fmt.Errorf("%s account balance valuation asset is missing or inconsistent", name)
		}
		if valuationAsset == "" {
			valuationAsset = asset
		} else if valuationAsset != asset {
			return 0, fmt.Errorf("cannot aggregate exchange balances denominated in different assets (%s and %s)", valuationAsset, asset)
		}
		total += account.TotalMarginBalance
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return 0, fmt.Errorf("aggregate account balance is not finite")
		}
	}
	if len(seen) == 0 {
		return 0, fmt.Errorf("no exchange account balances are available")
	}
	return total, nil
}

// ExchangeCapitalSummary 交易所资金摘要
type ExchangeCapitalSummary struct {
	ExchangeID   string  `json:"exchangeId"`
	ExchangeName string  `json:"exchangeName"`
	BalanceAsset string  `json:"balanceAsset,omitempty"`
	TotalBalance float64 `json:"totalBalance"`
	Available    float64 `json:"available"`
	Used         float64 `json:"used"`
	PnL          float64 `json:"pnl"`
	Status       string  `json:"status"`    // online, offline, error
	IsTestnet    bool    `json:"isTestnet"` // 是否使用測試網
}

// ExchangeCapitalDetail 交易所资金详情（包含资產层级）
type ExchangeCapitalDetail struct {
	ExchangeID   string            `json:"exchangeId"`
	ExchangeName string            `json:"exchangeName"`
	Assets       []AssetAllocation `json:"assets"`
	IsTestnet    bool              `json:"isTestnet"` // 是否使用測試網
}

// AssetAllocation 资產分配（如 USDT 下的策略分配）
type AssetAllocation struct {
	Asset                 string                  `json:"asset"`
	TotalBalance          float64                 `json:"totalBalance"`
	AvailableBalance      float64                 `json:"availableBalance"`
	AllocatedToStrategies float64                 `json:"allocatedToStrategies"`
	Unallocated           float64                 `json:"unallocated"`
	Strategies            []StrategyCapitalDetail `json:"strategies"`
}

// StrategyCapitalDetail 策略资金详情
type StrategyCapitalDetail struct {
	StrategyID      string  `json:"strategyId"`
	StrategyName    string  `json:"strategyName"`
	StrategyType    string  `json:"strategyType"`
	ExchangeID      string  `json:"exchangeId"` // 所属交易所
	Asset           string  `json:"asset"`      // 結算资產 (如 USDT)
	Allocated       float64 `json:"allocated"`  // 分配金額
	Used            float64 `json:"used"`       // 已占用
	Available       float64 `json:"available"`  // 可用配額
	Weight          float64 `json:"weight"`     // 权重 (0-1)
	MaxCapital      float64 `json:"maxCapital"` // 最大固定限額
	MaxPercentage   float64 `json:"maxPercentage"`
	ReserveRatio    float64 `json:"reserveRatio"`
	AutoRebalance   bool    `json:"autoRebalance"`
	Priority        int     `json:"priority"`
	UtilizationRate float64 `json:"utilizationRate"`
	Status          string  `json:"status"`
}

// CapitalAllocationConfig 资金分配配置
type CapitalAllocationConfig struct {
	StrategyID    string  `json:"strategyId"`
	MaxCapital    float64 `json:"maxCapital"`
	MaxPercentage float64 `json:"maxPercentage"`
	ReserveRatio  float64 `json:"reserveRatio"`
	AutoRebalance bool    `json:"autoRebalance"`
	Priority      int     `json:"priority"`
}

// RebalanceResult 再平衡結果
type RebalanceResult struct {
	Success         bool                    `json:"success"`
	Message         string                  `json:"message"`
	Changes         []RebalanceChange       `json:"changes"` // 添加此字段以匹配前端
	TotalMoved      float64                 `json:"totalMoved"`
	MovementDetails []CapitalMovement       `json:"movementDetails"`
	NewAllocations  []StrategyCapitalDetail `json:"newAllocations"`
	ExecutedAt      string                  `json:"executedAt"`
}

// RebalanceChange 策略分配变化
type RebalanceChange struct {
	StrategyID         string  `json:"strategyId"`
	PreviousAllocation float64 `json:"previousAllocation"`
	NewAllocation      float64 `json:"newAllocation"`
	Difference         float64 `json:"difference"`
}

// CapitalMovement 资金移动详情
type CapitalMovement struct {
	FromStrategy string  `json:"fromStrategy"`
	ToStrategy   string  `json:"toStrategy"`
	Amount       float64 `json:"amount"`
	Reason       string  `json:"reason"`
}

// CapitalHistoryPoint 资金历史点
type CapitalHistoryPoint struct {
	Timestamp string  `json:"timestamp"`
	Total     float64 `json:"total"`
	Allocated float64 `json:"allocated"`
	Available float64 `json:"available"`
	PnL       float64 `json:"pnl"`
}

// CapitalUsageResponse 资金使用视图（主打查看：交易所 -> Bot 占用明细）
type CapitalUsageResponse struct {
	Exchanges []ExchangeUsageDetail `json:"exchanges"`
}

// ExchangeUsageDetail 交易所资金使用详情
type ExchangeUsageDetail struct {
	ExchangeID   string         `json:"exchangeId"`
	ExchangeName string         `json:"exchangeName"`
	TotalBalance float64        `json:"totalBalance"`
	Available    float64        `json:"available"`
	Used         float64        `json:"used"`
	PnL          float64        `json:"pnl"`
	Status       string         `json:"status"`
	IsTestnet    bool           `json:"isTestnet"`
	Bots         []BotUsageInfo `json:"bots"`
}

// BotUsageInfo Bot 资金占用信息
type BotUsageInfo struct {
	BotID         string  `json:"botId"`
	Symbol        string  `json:"symbol"`
	OrderValue    float64 `json:"orderValue"`    // 委托资金（挂单占用）
	PositionValue float64 `json:"positionValue"` // 持仓占用
	TotalUsed     float64 `json:"totalUsed"`     // 合计占用
	OrderPct      float64 `json:"orderPct"`      // 委托占比（占该交易所总余额）
	PositionPct   float64 `json:"positionPct"`   // 持仓占比
	TotalUsedPct  float64 `json:"totalUsedPct"`  // 合计占比
}

// 獲取资金概览
func getCapitalOverviewHandler(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("❌ [资金概览] panic: %v", r)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"success":  false,
				"message":  fmt.Sprintf("资金概览处理异常: %v", r),
				"overview": CapitalOverview{LastUpdated: time.Now().Format(time.RFC3339)},
			})
		}
	}()
	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{
			"success": false,
			"message": "资金數據源未就绪",
			"overview": CapitalOverview{
				LastUpdated: time.Now().Format(time.RFC3339),
			},
		})
		return
	}

	// 檢查緩存：3 秒內重複請求直接返回
	capitalOverviewCache.mu.RLock()
	if time.Since(capitalOverviewCache.at) < capitalOverviewCache.ttl {
		cached := capitalOverviewCache.overview
		capitalOverviewCache.mu.RUnlock()
		c.JSON(http.StatusOK, gin.H{
			"success":  true,
			"overview": cached,
		})
		return
	}
	capitalOverviewCache.mu.RUnlock()

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	strategyConfigs := capitalDataSource.GetStrategyConfigs()
	posManagers := capitalDataSource.GetPositionManagers()

	var overview CapitalOverview
	overview.ValuationComplete = true
	overview.LastUpdated = time.Now().Format(time.RFC3339)

	// 1. 彙總交易所實時數據
	exchangeMap := make(map[string]bool)
	valuationAsset := ""
	for _, ex := range exchanges {
		name := ex.GetName()
		if exchangeMap[name] {
			continue
		}
		exchangeMap[name] = true

		acc, err := ex.GetAccount(ctx)
		if err != nil {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = fmt.Sprintf("%s account balance is unavailable", name)
			}
			logger.Error("❌ [资金概览] 獲取交易所 %s 帳戶資訊失败: %v", name, err)
			// 🔥 改進：报錯也要加進列表，显示為 error 状態
			// 從配置中獲取測試網状態
			isTestnet := false
			if cfg := capitalDataSource.GetConfig(); cfg != nil {
				if exCfg, ok := cfg.Exchanges[name]; ok {
					isTestnet = exCfg.Testnet
				}
			}
			overview.Exchanges = append(overview.Exchanges, ExchangeCapitalSummary{
				ExchangeID:   name,
				ExchangeName: name,
				TotalBalance: 0,
				Available:    0,
				Status:       "error",
				IsTestnet:    isTestnet,
			})
			continue
		}
		if acc == nil {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = fmt.Sprintf("%s account returned no balance snapshot", name)
			}
			logger.Warn("⚠️ [资金概览] 交易所 %s 返回空帳戶", name)
			continue
		}
		if !finiteCapitalValue(acc.TotalMarginBalance) || !finiteCapitalValue(acc.TotalWalletBalance) || !finiteCapitalValue(acc.AvailableBalance) ||
			acc.TotalMarginBalance < 0 || acc.TotalWalletBalance < 0 || acc.AvailableBalance < 0 ||
			strings.TrimSpace(acc.BalanceAsset) == "" || !strings.EqualFold(acc.BalanceAsset, ex.GetQuoteAsset()) {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = fmt.Sprintf("%s account returned an invalid balance snapshot", name)
			}
			overview.Exchanges = append(overview.Exchanges, ExchangeCapitalSummary{
				ExchangeID: name, ExchangeName: name, Status: "error",
			})
			continue
		}
		asset := strings.ToUpper(strings.TrimSpace(acc.BalanceAsset))
		if valuationAsset == "" {
			valuationAsset = asset
		} else if valuationAsset != asset {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = "exchange balances use different valuation assets"
			}
		}
		overview.ValuationAsset = valuationAsset

		// 從配置中獲取測試網状態
		isTestnet := false
		if cfg := capitalDataSource.GetConfig(); cfg != nil {
			if exCfg, ok := cfg.Exchanges[name]; ok {
				isTestnet = exCfg.Testnet
			}
		}

		summary := ExchangeCapitalSummary{
			ExchangeID:   name,
			ExchangeName: name,
			BalanceAsset: asset,
			Status:       "online",
			IsTestnet:    isTestnet,
		}
		usedBalance, usedOK := addFiniteProfitValues(acc.TotalMarginBalance, -acc.AvailableBalance)
		unrealizedPnL, pnlOK := addFiniteProfitValues(acc.TotalMarginBalance, -acc.TotalWalletBalance)
		var balanceOK, availableOK, usedRoundOK, pnlRoundOK bool
		if summary.TotalBalance, balanceOK = roundProfitToCents(acc.TotalMarginBalance); balanceOK {
			summary.Available, availableOK = roundProfitToCents(acc.AvailableBalance)
		}
		if usedOK {
			summary.Used, usedRoundOK = roundProfitToCents(usedBalance)
		}
		if pnlOK {
			summary.PnL, pnlRoundOK = roundProfitToCents(unrealizedPnL)
		}
		if !balanceOK || !availableOK || !usedOK || !usedRoundOK || !pnlOK || !pnlRoundOK {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = fmt.Sprintf("%s balance calculation overflowed", name)
			}
			summary.Status = "error"
			summary.BalanceAsset = ""
			overview.Exchanges = append(overview.Exchanges, summary)
			continue
		}
		overview.Exchanges = append(overview.Exchanges, summary)
		var aggregateOK bool
		if overview.TotalBalance, aggregateOK = addFiniteProfitValues(overview.TotalBalance, acc.TotalMarginBalance); aggregateOK {
			overview.AvailableCapital, aggregateOK = addFiniteProfitValues(overview.AvailableCapital, acc.AvailableBalance)
		}
		if aggregateOK {
			overview.UnrealizedPnL, aggregateOK = addFiniteProfitValues(overview.UnrealizedPnL, unrealizedPnL)
		}
		if !aggregateOK {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = "aggregate exchange balance overflowed"
			}
			continue
		}
	}
	if !overview.ValuationComplete {
		overview.TotalBalance = 0
		overview.AvailableCapital = 0
		overview.UnrealizedPnL = 0
	}

	// 2. 彙總策略分配數據
	for _, cfg := range strategyConfigs {
		if cfg.Enabled {
			if !finiteCapitalValue(cfg.Weight) || cfg.Weight < 0 {
				overview.ValuationComplete = false
				if overview.ValuationError == "" {
					overview.ValuationError = "strategy allocation weight is invalid"
				}
				continue
			}
			alloc := overview.TotalBalance * cfg.Weight
			var ok bool
			if overview.AllocatedCapital, ok = addFiniteProfitValues(overview.AllocatedCapital, alloc); !ok {
				overview.ValuationComplete = false
				if overview.ValuationError == "" {
					overview.ValuationError = "strategy allocation total overflowed"
				}
			}
		}
	}

	// 3. 彙總實際占用资金
	for _, pm := range posManagers {
		if pm.Manager == nil {
			continue
		}
		quantity := pm.Manager.GetTotalBuyQty()
		priceInterval := pm.Manager.GetPriceInterval()
		if !finiteCapitalValue(quantity) || !finiteCapitalValue(priceInterval) || quantity < 0 || priceInterval < 0 {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = "position capital input is invalid"
			}
			continue
		}
		positionCapital := quantity * priceInterval
		var ok bool
		if overview.UsedCapital, ok = addFiniteProfitValues(overview.UsedCapital, positionCapital); !ok {
			overview.ValuationComplete = false
			if overview.ValuationError == "" {
				overview.ValuationError = "position capital total overflowed"
			}
		}
	}

	if overview.TotalBalance > 0 {
		overview.MarginRatio = overview.UsedCapital / overview.TotalBalance
	}

	if !roundCapitalOverviewAmounts(&overview) {
		overview.ValuationComplete = false
		if overview.ValuationError == "" {
			overview.ValuationError = "capital overview contains a non-finite total"
		}
	}
	if !overview.ValuationComplete {
		overview.TotalBalance = 0
		overview.AllocatedCapital = 0
		overview.UsedCapital = 0
		overview.AvailableCapital = 0
		overview.UnrealizedPnL = 0
		overview.MarginRatio = 0
	}

	// 更新緩存
	capitalOverviewCache.mu.Lock()
	capitalOverviewCache.overview = overview
	capitalOverviewCache.at = time.Now()
	capitalOverviewCache.mu.Unlock()

	c.JSON(http.StatusOK, gin.H{
		"success":  true,
		"overview": overview,
	})
}

func finiteCapitalValue(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}

func roundCapitalOverviewAmounts(overview *CapitalOverview) bool {
	if overview == nil {
		return false
	}
	amounts := []*float64{
		&overview.TotalBalance, &overview.AllocatedCapital, &overview.UsedCapital,
		&overview.AvailableCapital, &overview.UnrealizedPnL,
	}
	for _, amount := range amounts {
		rounded, ok := roundProfitToCents(*amount)
		if !ok {
			return false
		}
		*amount = rounded
	}
	return finiteCapitalValue(overview.MarginRatio)
}

func hasUnattributedCapitalPositionManagers(managers []PositionManagerInfo) bool {
	for _, manager := range managers {
		if manager.Manager != nil {
			return true
		}
	}
	return false
}

// 獲取资金使用视图（主打查看：各交易所、各 Bot 的委托/持仓占用）
func getCapitalUsageHandler(c *gin.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("❌ [资金使用] panic: %v", r)
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"success":   false,
				"message":   fmt.Sprintf("资金使用处理异常: %v", r),
				"exchanges": []ExchangeUsageDetail{},
			})
		}
	}()
	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{
			"success":   false,
			"code":      "capital_data_source_unavailable",
			"message":   "资金數據源未就绪",
			"exchanges": []ExchangeUsageDetail{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	posManagers := capitalDataSource.GetPositionManagers()
	cfg := capitalDataSource.GetConfig()

	formatExchangeName := func(exID string) string {
		exIDLower := strings.ToLower(exID)
		switch exIDLower {
		case "binance":
			return "Binance"
		case "gate":
			return "Gate.io"
		case "okx":
			return "OKX"
		case "bitget":
			return "Bitget"
		case "bybit":
			return "Bybit"
		case "huobi":
			return "Huobi"
		case "kucoin":
			return "KuCoin"
		default:
			if len(exID) > 0 {
				return strings.ToUpper(exID[:1]) + strings.ToLower(exID[1:])
			}
			return exID
		}
	}

	// 按交易所聚合
	exchangeMap := make(map[string]*ExchangeUsageDetail)
	exchangeInstanceMap := make(map[string]exchange.IExchange)

	for _, ex := range exchanges {
		name := ex.GetName()
		exchangeInstanceMap[strings.ToLower(name)] = ex
	}

	// 建立交易所条目：从配置、position managers、运行实例收集
	for exLower := range exchangeInstanceMap {
		if _, ok := exchangeMap[exLower]; ok {
			continue
		}
		isTestnet := false
		if cfg != nil {
			for exKey := range cfg.Exchanges {
				if strings.ToLower(exKey) == exLower {
					if exCfg, ok := cfg.Exchanges[exKey]; ok {
						isTestnet = exCfg.Testnet
					}
					break
				}
			}
		}
		exchangeMap[exLower] = &ExchangeUsageDetail{
			ExchangeID:   exLower,
			ExchangeName: formatExchangeName(exLower),
			Bots:         []BotUsageInfo{},
			IsTestnet:    isTestnet,
		}
	}
	if cfg != nil {
		for exName := range cfg.Exchanges {
			exLower := strings.ToLower(exName)
			if _, ok := exchangeMap[exLower]; ok {
				continue
			}
			isTestnet := false
			if exCfg, ok := cfg.Exchanges[exName]; ok {
				isTestnet = exCfg.Testnet
			}
			exchangeMap[exLower] = &ExchangeUsageDetail{
				ExchangeID:   exLower,
				ExchangeName: formatExchangeName(exName),
				Bots:         []BotUsageInfo{},
				IsTestnet:    isTestnet,
			}
		}
	}
	for _, pm := range posManagers {
		exLower := strings.ToLower(pm.Exchange)
		if _, ok := exchangeMap[exLower]; ok {
			continue
		}
		isTestnet := false
		if cfg != nil {
			for exKey := range cfg.Exchanges {
				if strings.ToLower(exKey) == exLower {
					if exCfg, ok := cfg.Exchanges[exKey]; ok {
						isTestnet = exCfg.Testnet
					}
					break
				}
			}
		}
		exchangeMap[exLower] = &ExchangeUsageDetail{
			ExchangeID:   exLower,
			ExchangeName: formatExchangeName(pm.Exchange),
			Bots:         []BotUsageInfo{},
			IsTestnet:    isTestnet,
		}
	}

	// 获取各交易所余额并填充 Bot 占用
	for exLower, exDetail := range exchangeMap {
		if ex, hasInstance := exchangeInstanceMap[exLower]; hasInstance {
			acc, err := ex.GetAccount(ctx)
			if err != nil {
				exDetail.Status = "error"
				exDetail.TotalBalance = 0
				exDetail.Available = 0
				exDetail.Used = 0
				continue
			}
			if acc == nil {
				exDetail.Status = "error"
				continue
			}
			if !finiteCapitalValue(acc.TotalMarginBalance) || !finiteCapitalValue(acc.AvailableBalance) || !finiteCapitalValue(acc.TotalWalletBalance) ||
				acc.TotalMarginBalance < 0 || acc.AvailableBalance < 0 || acc.TotalWalletBalance < 0 {
				exDetail.Status = "error"
				continue
			}
			exDetail.Status = "online"
			var amountsOK bool
			if exDetail.TotalBalance, amountsOK = roundProfitToCents(acc.TotalMarginBalance); amountsOK {
				exDetail.Available, amountsOK = roundProfitToCents(acc.AvailableBalance)
			}
			usedBalance, usedOK := addFiniteProfitValues(acc.TotalMarginBalance, -acc.AvailableBalance)
			profitLoss, pnlOK := addFiniteProfitValues(acc.TotalMarginBalance, -acc.TotalWalletBalance)
			if amountsOK && usedOK {
				exDetail.Used, amountsOK = roundProfitToCents(usedBalance)
			}
			if amountsOK && pnlOK {
				exDetail.PnL, amountsOK = roundProfitToCents(profitLoss)
			}
			if !amountsOK {
				exDetail.Status = "error"
				exDetail.TotalBalance, exDetail.Available, exDetail.Used, exDetail.PnL = 0, 0, 0, 0
				continue
			}
		} else {
			exDetail.Status = "offline"
		}

		// 填充该交易所下的 Bot 占用
		usageInvalid := false
		for _, pm := range posManagers {
			if strings.ToLower(pm.Exchange) != exLower {
				continue
			}
			if pm.Manager == nil {
				continue
			}
			orderVal, orderOK := roundProfitToCents(pm.Manager.GetPendingBuyOrderValueUSDT())
			positionValue, valuationVerified := pm.Manager.GetTotalPositionValueUSDTVerified()
			posVal, positionOK := roundProfitToCents(positionValue)
			if !orderOK || !valuationVerified || !positionOK || orderVal < 0 || posVal < 0 {
				usageInvalid = true
				break
			}
			totalUsed, usedOK := addFiniteProfitValues(orderVal, posVal)
			if !usedOK {
				usageInvalid = true
				break
			}

			botID := config.GenerateBotID(pm.Exchange, pm.Symbol, "")
			orderPct := 0.0
			positionPct := 0.0
			totalUsedPct := 0.0
			if exDetail.TotalBalance > 0 {
				orderPct = (orderVal / exDetail.TotalBalance) * 100
				positionPct = (posVal / exDetail.TotalBalance) * 100
				totalUsedPct = (totalUsed / exDetail.TotalBalance) * 100
			}
			roundedOrderPct, orderPctOK := roundProfitToPrecision(orderPct, 100)
			roundedPositionPct, positionPctOK := roundProfitToPrecision(positionPct, 100)
			roundedTotalUsedPct, totalPctOK := roundProfitToPrecision(totalUsedPct, 100)
			if !orderPctOK || !positionPctOK || !totalPctOK {
				usageInvalid = true
				break
			}

			exDetail.Bots = append(exDetail.Bots, BotUsageInfo{
				BotID:         botID,
				Symbol:        pm.Symbol,
				OrderValue:    orderVal,
				PositionValue: posVal,
				TotalUsed:     totalUsed,
				OrderPct:      roundedOrderPct,
				PositionPct:   roundedPositionPct,
				TotalUsedPct:  roundedTotalUsedPct,
			})
		}
		if usageInvalid {
			exDetail.Status = "error"
			exDetail.TotalBalance, exDetail.Available, exDetail.Used, exDetail.PnL = 0, 0, 0, 0
			exDetail.Bots = []BotUsageInfo{}
		}
	}

	// 按交易所 ID 排序输出
	var result []ExchangeUsageDetail
	order := []string{"binance", "gate", "okx", "bybit", "bitget", "huobi", "kucoin"}
	seen := make(map[string]bool)
	for _, exLower := range order {
		if d, ok := exchangeMap[exLower]; ok {
			result = append(result, *d)
			seen[exLower] = true
		}
	}
	for exLower, d := range exchangeMap {
		if !seen[exLower] {
			result = append(result, *d)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"exchanges": result,
	})
}

// 獲取资金分配配置
func getCapitalAllocationHandler(c *gin.Context) {
	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{
			"success":   false,
			"message":   "资金數據源未就绪",
			"exchanges": []ExchangeCapitalDetail{},
		})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	strategyConfigs := capitalDataSource.GetStrategyConfigs()
	posManagers := capitalDataSource.GetPositionManagers()
	if hasUnattributedCapitalPositionManagers(posManagers) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "倉位管理器缺少策略归属，无法核验策略级资金占用"})
		return
	}
	cfg := capitalDataSource.GetConfig()

	var details []ExchangeCapitalDetail
	exchangeMap := make(map[string]*ExchangeCapitalDetail)
	exchangeInstanceMap := make(map[string]exchange.IExchange)

	// 建立交易所實例映射（正在运行的交易所）
	for _, ex := range exchanges {
		name := ex.GetName()
		exchangeInstanceMap[strings.ToLower(name)] = ex
	}

	// 格式化交易所显示名称
	formatExchangeName := func(exID string) string {
		exIDLower := strings.ToLower(exID)
		switch exIDLower {
		case "binance":
			return "Binance"
		case "gate":
			return "Gate.io"
		case "okx":
			return "OKX"
		case "bitget":
			return "Bitget"
		case "bybit":
			return "Bybit"
		case "huobi":
			return "Huobi"
		case "kucoin":
			return "KuCoin"
		default:
			// 首字母大写
			if len(exID) > 0 {
				return strings.ToUpper(exID[:1]) + strings.ToLower(exID[1:])
			}
			return exID
		}
	}

	// 從配置中獲取所有配置的交易所（與 getExchanges API 保持一致的逻辑）
	configuredExchanges := make(map[string]bool)
	if cfg != nil {
		// 從配置的 exchanges 中读取
		for exName := range cfg.Exchanges {
			if exName != "" {
				configuredExchanges[strings.ToLower(exName)] = true
			}
		}
		// 從交易對配置中读取交易所
		for _, sym := range cfg.Trading.Symbols {
			if sym.Exchange != "" {
				configuredExchanges[strings.ToLower(sym.Exchange)] = true
			} else if cfg.App.CurrentExchange != "" {
				configuredExchanges[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
		// 如果只有單交易對配置
		if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
			if cfg.App.CurrentExchange != "" {
				configuredExchanges[strings.ToLower(cfg.App.CurrentExchange)] = true
			}
		}
	}

	// 🔥 关键修複：添加所有正在运行的交易所實例（确保它们被包含）
	for exNameLower := range exchangeInstanceMap {
		configuredExchanges[exNameLower] = true
	}

	// 🔥 從运行状態中读取交易所（與 getExchanges API 保持一致）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st != nil && st.Exchange != "" {
			configuredExchanges[strings.ToLower(st.Exchange)] = true
		}
	}
	statusMu.RUnlock()

	// 向后兼容：如果仍然没有交易所，尝試從 currentStatus 读取
	if len(configuredExchanges) == 0 && currentStatus != nil && currentStatus.Exchange != "" {
		configuredExchanges[strings.ToLower(currentStatus.Exchange)] = true
	}

	logger.Debug("ℹ️ [资金分配] 找到 %d 個交易所: %v", len(configuredExchanges), configuredExchanges)

	// 处理所有配置的交易所（包括正在运行的）
	for exNameLower := range configuredExchanges {
		if _, ok := exchangeMap[exNameLower]; ok {
			continue
		}

		// 從配置中獲取測試網状態（使用原始大小写的键查找）
		isTestnet := false
		if cfg != nil {
			// 尝試查找原始键（可能大小写不同）
			for exKey := range cfg.Exchanges {
				if strings.ToLower(exKey) == exNameLower {
					if exCfg, ok := cfg.Exchanges[exKey]; ok {
						isTestnet = exCfg.Testnet
					}
					break
				}
			}
		}

		// 如果有运行的實例，尝試獲取帳戶信息
		var exDetail *ExchangeCapitalDetail
		if ex, hasInstance := exchangeInstanceMap[exNameLower]; hasInstance {
			acc, err := ex.GetAccount(ctx)
			if err != nil {
				logger.Error("❌ [资金分配] 獲取交易所 %s 帳戶資訊失败: %v", exNameLower, err)
				// 獲取失败也要显示，只是餘額為 0
				exDetail = &ExchangeCapitalDetail{
					ExchangeID:   exNameLower,
					ExchangeName: formatExchangeName(exNameLower),
					Assets: []AssetAllocation{
						{
							Asset:            "UNVERIFIED",
							TotalBalance:     0,
							AvailableBalance: 0,
						},
					},
					IsTestnet: isTestnet,
				}
			} else if acc == nil {
				exDetail = &ExchangeCapitalDetail{
					ExchangeID: exNameLower, ExchangeName: formatExchangeName(exNameLower),
					Assets: []AssetAllocation{{Asset: "UNVERIFIED"}}, IsTestnet: isTestnet,
				}
			} else {
				balanceAsset := strings.ToUpper(strings.TrimSpace(acc.BalanceAsset))
				balanceVerified := balanceAsset != "" && strings.EqualFold(balanceAsset, ex.GetQuoteAsset()) &&
					finiteCapitalValue(acc.TotalMarginBalance) && finiteCapitalValue(acc.AvailableBalance) &&
					acc.TotalMarginBalance >= 0 && acc.AvailableBalance >= 0
				if !balanceVerified {
					balanceAsset = "UNVERIFIED"
				}
				totalBalance, availableBalance := 0.0, 0.0
				if balanceVerified {
					var totalOK, availableOK bool
					totalBalance, totalOK = roundProfitToCents(acc.TotalMarginBalance)
					availableBalance, availableOK = roundProfitToCents(acc.AvailableBalance)
					if !totalOK || !availableOK {
						balanceVerified = false
						balanceAsset = "UNVERIFIED"
						totalBalance, availableBalance = 0, 0
					}
				}
				exDetail = &ExchangeCapitalDetail{
					ExchangeID:   exNameLower,
					ExchangeName: formatExchangeName(exNameLower),
					Assets: []AssetAllocation{
						{
							Asset:            balanceAsset,
							TotalBalance:     totalBalance,
							AvailableBalance: availableBalance,
						},
					},
					IsTestnet: isTestnet,
				}
			}
		} else {
			// 没有运行的實例，显示為未连接状態
			logger.Debug("ℹ️ [资金分配] 交易所 %s 已配置但未运行", exNameLower)
			exDetail = &ExchangeCapitalDetail{
				ExchangeID:   exNameLower,
				ExchangeName: formatExchangeName(exNameLower),
				Assets: []AssetAllocation{
					{
						Asset:            "UNVERIFIED",
						TotalBalance:     0,
						AvailableBalance: 0,
					},
				},
				IsTestnet: isTestnet,
			}
		}

		exchangeMap[exNameLower] = exDetail
		details = append(details, *exDetail)
	}

	// 填充策略分配
	for strategyID, cfg := range strategyConfigs {
		if !cfg.Enabled {
			continue
		}

		for i := range details {
			for j := range details[i].Assets {
				asset := &details[i].Assets[j]

				if !finiteCapitalValue(cfg.Weight) || cfg.Weight < 0 {
					c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略权重配置无效，无法核验资金分配"})
					return
				}
				alloc, allocOK := roundProfitToCents(asset.TotalBalance * cfg.Weight)
				if !allocOK {
					c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略资金分配溢出"})
					return
				}

				// 從配置中读取 maxCapital 和 maxPercentage
				maxCapital := 0.0
				maxPercentage := 100.0
				if cfg.Config != nil {
					if val, ok := cfg.Config["max_capital"].(float64); ok {
						maxCapital = val
					} else if val, ok := cfg.Config["max_capital"].(int); ok {
						maxCapital = float64(val)
					}
					if val, ok := cfg.Config["max_percentage"].(float64); ok {
						maxPercentage = val
					} else if val, ok := cfg.Config["max_percentage"].(int); ok {
						maxPercentage = float64(val)
					}
				}

				strategy := StrategyCapitalDetail{
					StrategyID:    strategyID,
					StrategyName:  getStrategyName(strategyID),
					StrategyType:  strategyID,
					ExchangeID:    details[i].ExchangeID,
					Asset:         asset.Asset,
					Allocated:     alloc,
					Weight:        cfg.Weight,
					MaxCapital:    maxCapital,
					MaxPercentage: maxPercentage,
					Status:        "active",
				}

				// 從配置中读取其他字段
				if cfg.Config != nil {
					if val, ok := cfg.Config["reserve_ratio"].(float64); ok {
						strategy.ReserveRatio = val
					} else {
						strategy.ReserveRatio = 0.1 // 默认值
					}
					if val, ok := cfg.Config["auto_rebalance"].(bool); ok {
						strategy.AutoRebalance = val
					}
					if val, ok := cfg.Config["priority"].(int); ok {
						strategy.Priority = val
					} else if val, ok := cfg.Config["priority"].(float64); ok {
						strategy.Priority = int(val)
					} else {
						strategy.Priority = 1 // 默认值
					}
				} else {
					strategy.ReserveRatio = 0.1
					strategy.AutoRebalance = false
					strategy.Priority = 1
				}

				// 计算實際占用
				for _, pm := range posManagers {
					if pm.Manager != nil && strings.EqualFold(pm.Exchange, details[i].ExchangeID) {
						// 这里需要判断該 PM 是否属於該策略
						// TODO: 完善策略與交易對的关联逻辑
						strategy.Used += pm.Manager.GetTotalBuyQty() * pm.Manager.GetPriceInterval()
					}
				}

				var usedOK, availableOK bool
				strategy.Used, usedOK = roundProfitToCents(strategy.Used)
				strategy.Available, availableOK = roundProfitToCents(strategy.Allocated - strategy.Used)
				if !usedOK || !availableOK {
					c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略已用或可用资金金额溢出"})
					return
				}
				if strategy.Allocated > 0 {
					strategy.UtilizationRate = strategy.Used / strategy.Allocated
				}

				asset.Strategies = append(asset.Strategies, strategy)
				var allocationOK bool
				asset.AllocatedToStrategies, allocationOK = addFiniteProfitValues(asset.AllocatedToStrategies, strategy.Allocated)
				if !allocationOK {
					c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略分配汇总溢出"})
					return
				}
			}
		}
	}

	// 计算未分配资金
	for i := range details {
		for j := range details[i].Assets {
			asset := &details[i].Assets[j]
			var allocatedOK, unallocatedOK bool
			asset.AllocatedToStrategies, allocatedOK = roundProfitToCents(asset.AllocatedToStrategies)
			asset.Unallocated, unallocatedOK = roundProfitToCents(asset.TotalBalance - asset.AllocatedToStrategies)
			if !allocatedOK || !unallocatedOK {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "未分配资金金额溢出"})
				return
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success":   true,
		"exchanges": details,
	})
}

// 更新资金分配
func updateCapitalAllocationHandler(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "资金上限配置尚未接入实时下单风控，未修改任何配置",
	})
}

// 更新單個策略的资金配置
func updateStrategyCapitalHandler(c *gin.Context) {
	var req CapitalAllocationConfig
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "策略资金上限尚未接入实时下单风控，未修改任何配置",
	})
}

// 獲取單個策略的资金详情
func getStrategyCapitalDetailHandler(c *gin.Context) {
	strategyID := c.Param("id")

	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "资金數據源未就绪"})
		return
	}

	configs := capitalDataSource.GetStrategyConfigs()
	cfg, ok := configs[strategyID]
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "未找到策略配置"})
		return
	}

	// 彙總該策略在所有交易所的资金
	ctx, cancel := context.WithTimeout(c.Request.Context(), 5*time.Second)
	defer cancel()

	exchanges := capitalDataSource.GetExchanges()
	posManagers := capitalDataSource.GetPositionManagers()
	if hasUnattributedCapitalPositionManagers(posManagers) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "倉位管理器缺少策略归属，无法核验策略资金详情"})
		return
	}

	var totalAllocated, totalUsed float64
	totalBalance, balanceErr := getCompleteExchangeBalance(ctx, exchanges)
	if balanceErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法取得完整账户余额快照，策略资金详情不可用"})
		return
	}
	if !finiteCapitalValue(cfg.Weight) || cfg.Weight < 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略权重无效，无法核验资金详情"})
		return
	}
	var allocatedOK bool
	totalAllocated, allocatedOK = roundProfitToCents(totalBalance * cfg.Weight)
	if !allocatedOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略分配金额溢出"})
		return
	}

	for _, pm := range posManagers {
		if pm.Manager == nil {
			continue
		}
		quantity := pm.Manager.GetTotalBuyQty()
		priceInterval := pm.Manager.GetPriceInterval()
		if !finiteCapitalValue(quantity) || !finiteCapitalValue(priceInterval) || quantity < 0 || priceInterval < 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略持仓占用输入无效"})
			return
		}
		positionCapital := quantity * priceInterval
		var usedOK bool
		if totalUsed, usedOK = addFiniteProfitValues(totalUsed, positionCapital); !usedOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略持仓占用金额溢出"})
			return
		}
	}

	maxCap := 0.0
	if val, ok := cfg.Config["max_capital"].(float64); ok {
		maxCap = val
	} else if val, ok := cfg.Config["max_capital"].(int); ok {
		maxCap = float64(val)
	}
	if !finiteCapitalValue(maxCap) || maxCap < 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略资金上限配置无效"})
		return
	}
	roundedUsed, usedOK := roundProfitToCents(totalUsed)
	roundedAvailable, availableOK := roundProfitToCents(totalAllocated - totalUsed)
	if !usedOK || !availableOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略已用或可用资金无法安全舍入"})
		return
	}

	utilizationRate := 0.0
	if totalAllocated > 0 {
		utilizationRate = totalUsed / totalAllocated
		if !finiteCapitalValue(utilizationRate) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略资金利用率计算溢出"})
			return
		}
	}
	capital := StrategyCapitalDetail{
		StrategyID:   strategyID,
		StrategyName: getStrategyName(strategyID),
		StrategyType: strategyID,
		Allocated:    totalAllocated,
		Used:         roundedUsed,
		Available:    roundedAvailable,
		Weight:       cfg.Weight,
		MaxCapital:   maxCap,
		Status:       "active",
	}
	capital.UtilizationRate = utilizationRate

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"capital": capital,
	})
}

// 触发资金再平衡
func rebalanceCapitalHandler(c *gin.Context) {
	var req struct {
		Mode   string `json:"mode"` // equal, weighted, priority
		Force  bool   `json:"force"`
		DryRun bool   `json:"dryRun"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的再平衡请求"})
		return
	}
	if req.Mode == "" {
		req.Mode = "weighted"
	}
	if req.Mode != "equal" && req.Mode != "weighted" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "不支持的再平衡模式"})
		return
	}
	if !req.DryRun {
		c.JSON(http.StatusNotImplemented, gin.H{"success": false, "message": "策略资金上限尚未接入实时下单风控，已拒绝应用再平衡"})
		return
	}

	if capitalDataSource == nil {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "资金數據源未就绪"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()

	// 1. 獲取總资產 (實時從交易所取)
	exchanges := capitalDataSource.GetExchanges()
	totalBalance, balanceErr := getCompleteExchangeBalance(ctx, exchanges)
	if balanceErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法取得完整账户余额快照，已取消资金再平衡"})
		return
	}

	if totalBalance <= 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "無法獲取帳戶餘額或餘額為0"})
		return
	}

	// 2. 獲取策略配置
	stratConfigs := capitalDataSource.GetStrategyConfigs()
	enabledStrategies := make([]string, 0)
	for id, cfg := range stratConfigs {
		if cfg.Enabled {
			enabledStrategies = append(enabledStrategies, id)
		}
	}

	if len(enabledStrategies) == 0 {
		c.JSON(http.StatusOK, gin.H{"success": false, "message": "没有已啟用的策略"})
		return
	}

	// 3. 计算新分配
	changes := make([]RebalanceChange, 0)
	newAllocations := make([]StrategyCapitalDetail, 0)

	count := float64(len(enabledStrategies))
	totalWeight := 0.0
	for _, id := range enabledStrategies {
		weight := stratConfigs[id].Weight
		if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略权重配置无效，已取消再平衡预览"})
			return
		}
		var weightOK bool
		if totalWeight, weightOK = addFiniteProfitValues(totalWeight, weight); !weightOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略权重汇总溢出，已取消再平衡预览"})
			return
		}
	}

	for _, id := range enabledStrategies {
		cfg := stratConfigs[id]

		// 计算目標分配
		var targetAllocation float64
		switch req.Mode {
		case "equal":
			targetAllocation = totalBalance / count
		case "weighted":
			if totalWeight > 0 {
				targetAllocation = (cfg.Weight / totalWeight) * totalBalance
			} else {
				targetAllocation = totalBalance / count
			}
		default:
			if totalWeight > 0 {
				targetAllocation = (cfg.Weight / totalWeight) * totalBalance
			} else {
				targetAllocation = totalBalance / count
			}
		}

		// 獲取當前分配（從配置读取）
		prevAllocation := 0.0
		if val, ok := cfg.Config["max_capital"].(float64); ok {
			prevAllocation = val
		} else if val, ok := cfg.Config["max_capital"].(int); ok {
			prevAllocation = float64(val)
		}
		if !finiteCapitalValue(prevAllocation) || prevAllocation < 0 {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "当前策略分配配置无效，已取消再平衡预览"})
			return
		}

		if !finiteCapitalValue(targetAllocation) {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "目标策略分配金额溢出，已取消再平衡预览"})
			return
		}
		diff, diffOK := addFiniteProfitValues(targetAllocation, -prevAllocation)
		previousRounded, previousOK := roundProfitToCents(prevAllocation)
		targetRounded, targetOK := roundProfitToCents(targetAllocation)
		differenceRounded, differenceOK := roundProfitToCents(diff)
		if !diffOK || !previousOK || !targetOK || !differenceOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "再平衡金额无法安全计算或舍入"})
			return
		}

		changes = append(changes, RebalanceChange{
			StrategyID:         id,
			PreviousAllocation: previousRounded,
			NewAllocation:      targetRounded,
			Difference:         differenceRounded,
		})

		newAllocations = append(newAllocations, StrategyCapitalDetail{
			StrategyID:   id,
			StrategyName: getStrategyName(id),
			Allocated:    targetRounded,
			Status:       "active",
		})
	}

	result := RebalanceResult{
		Success:        true,
		Message:        "再平衡计算完成",
		Changes:        changes,
		NewAllocations: newAllocations,
		ExecutedAt:     time.Now().Format(time.RFC3339),
	}

	result.Message = "模拟再平衡预览（未应用；策略资金上限未接入实时风控）"

	c.JSON(http.StatusOK, result)
}

// 獲取资金历史記錄
func getCapitalHistoryHandler(c *gin.Context) {
	c.JSON(http.StatusServiceUnavailable, gin.H{
		"success": false,
		"message": "资金历史尚未接入可信持久化账本，暂不可用",
		"history": []CapitalHistoryPoint{},
	})
}

// 設置預留保证金
func setReserveCapitalHandler(c *gin.Context) {
	var req struct {
		Amount float64 `json:"amount"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	if req.Amount < 0 || math.IsNaN(req.Amount) || math.IsInf(req.Amount, 0) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "預留保证金不能為负數",
		})
		return
	}

	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "预留保证金尚未接入实时保证金管理，未修改任何配置",
	})
}

// 鎖定/解鎖策略资金
func lockStrategyCapitalHandler(c *gin.Context) {
	var req struct {
		Locked bool `json:"locked"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusNotImplemented, gin.H{
		"success": false,
		"message": "策略资金锁定尚未接入实时分配器，未修改任何配置",
	})
}
