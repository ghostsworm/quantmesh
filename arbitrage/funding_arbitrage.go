package arbitrage

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// IExchange 交易所介面（避免循環引用）
type IExchange interface {
	GetName() string
	GetMarketType() string
	PlaceOrder(ctx context.Context, req interface{}) (interface{}, error)
	CancelOrder(ctx context.Context, symbol string, orderID int64) error
	GetPositions(ctx context.Context, symbol string) (interface{}, error)
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)
	GetBalance(ctx context.Context, asset string) (float64, error)
	GetBaseAsset() string
	GetQuoteAsset() string
}

// OrderRequest 訂單請求
type OrderRequest struct {
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	PriceDecimals int
	PostOnly      bool
	ClientOrderID string
}

// Order 訂單信息
type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	Status        string
}

// Position 持倉信息
type Position struct {
	Symbol         string
	Size           float64 // 正數表示多倉，负數表示空倉
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

// FundingMonitor 資金費率監控介面
type FundingMonitor interface {
	GetBuyBias() float64
	IsHighRate() bool
	GetCurrentRate() float64
	ShouldPauseBuying() bool
	GetNextFundingTime() time.Time
}

// HedgeStatus 對沖狀態
type HedgeStatus struct {
	Enabled          bool      `json:"enabled"`
	FuturesPosition  float64   `json:"futures_position"`
	SpotPosition     float64   `json:"spot_position"`
	HedgedAmount     float64   `json:"hedged_amount"`
	HedgeRatio       float64   `json:"hedge_ratio"`
	CurrentRate      float64   `json:"current_rate"`
	EstimatedSavings float64   `json:"estimated_savings"`
	LastSyncTime     time.Time `json:"last_sync_time"`
	SyncStatus       string    `json:"sync_status"`
}

// PositionChangeEvent 倉位變化事件
type PositionChangeEvent struct {
	Symbol    string
	Delta     float64 // 正數為買入，負數為賣出
	Price     float64
	Timestamp time.Time
}

// FundingArbitrageManager 資金費率套利管理器
// 實現期現套利：在高費率時，將部分合約持倉轉換為現貨持倉，避免支付費用
type FundingArbitrageManager struct {
	cfg             *config.Config
	futuresExchange IExchange
	spotExchange    IExchange
	fundingMonitor  FundingMonitor
	symbol          string
	baseAsset       string // 如 BTC
	quoteAsset      string // 如 USDT

	// 倉位狀態
	futuresPosition float64 // 合約持倉數量
	spotPosition    float64 // 現貨持倉數量
	hedgedAmount    float64 // 已對沖的數量

	// 訂單追踪
	pendingOrders map[string]*Order
	
	// 狀態
	mu           sync.RWMutex
	running      bool
	stopCh       chan struct{}
	lastSyncTime time.Time
	syncStatus   string

	// 倉位變化通道
	positionChangeCh chan PositionChangeEvent
}

// NewFundingArbitrageManager 創建資金費率套利管理器
func NewFundingArbitrageManager(
	cfg *config.Config,
	futuresExchange IExchange,
	spotExchange IExchange,
	fundingMonitor FundingMonitor,
	symbol string,
) *FundingArbitrageManager {
	baseAsset := ""
	if futuresExchange != nil {
		baseAsset = futuresExchange.GetBaseAsset()
	}
	quoteAsset := ""
	if futuresExchange != nil {
		quoteAsset = futuresExchange.GetQuoteAsset()
	}

	return &FundingArbitrageManager{
		cfg:              cfg,
		futuresExchange:  futuresExchange,
		spotExchange:     spotExchange,
		fundingMonitor:   fundingMonitor,
		symbol:           symbol,
		baseAsset:        baseAsset,
		quoteAsset:       quoteAsset,
		pendingOrders:    make(map[string]*Order),
		stopCh:           make(chan struct{}),
		syncStatus:       "idle",
		positionChangeCh: make(chan PositionChangeEvent, 100),
	}
}

// Start 啟動套利管理器
func (a *FundingArbitrageManager) Start(ctx context.Context) {
	if !a.cfg.FundingRate.ArbitrageEnabled {
		logger.Info("⚠️ 期現套利未啟用")
		return
	}

	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return
	}
	a.running = true
	a.mu.Unlock()

	logger.Info("💱 啟動期現套利管理器 (交易對: %s, 最小對沖: %.2f USDT)",
		a.symbol, a.cfg.FundingRate.HedgeMinPosition)

	// 初始化倉位
	if err := a.syncPositions(ctx); err != nil {
		logger.Warn("⚠️ 初始同步倉位失敗: %v", err)
	}

	// 啟動主循環
	go a.mainLoop(ctx)

	// 啟動倉位變化處理器
	go a.positionChangeHandler(ctx)
}

// Stop 停止套利管理器
func (a *FundingArbitrageManager) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.running {
		return
	}

	a.running = false
	close(a.stopCh)
	logger.Info("💱 期現套利管理器已停止")
}

// mainLoop 主循環
func (a *FundingArbitrageManager) mainLoop(ctx context.Context) {
	// 每分鐘檢查一次
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case <-ticker.C:
			// 檢查是否需要調整對沖
			if err := a.checkAndAdjustHedge(ctx); err != nil {
				logger.Warn("⚠️ 調整對沖失敗: %v", err)
			}
		}
	}
}

// positionChangeHandler 倉位變化處理器
func (a *FundingArbitrageManager) positionChangeHandler(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.stopCh:
			return
		case event := <-a.positionChangeCh:
			a.handlePositionChange(ctx, event)
		}
	}
}

// OnGridPositionChange 網格倉位變化回調
// 由網格管理器調用，當有買入/賣出成交時通知套利管理器
func (a *FundingArbitrageManager) OnGridPositionChange(delta float64, price float64) {
	if !a.cfg.FundingRate.ArbitrageEnabled {
		return
	}

	event := PositionChangeEvent{
		Symbol:    a.symbol,
		Delta:     delta,
		Price:     price,
		Timestamp: time.Now(),
	}

	// 非阻塞發送
	select {
	case a.positionChangeCh <- event:
	default:
		logger.Warn("⚠️ 倉位變化事件通道已滿，跳過事件")
	}
}

// handlePositionChange 處理倉位變化
func (a *FundingArbitrageManager) handlePositionChange(ctx context.Context, event PositionChangeEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 更新合約持倉
	a.futuresPosition += event.Delta

	// 檢查是否需要調整對沖
	if a.shouldHedge() {
		if event.Delta > 0 {
			// 合約買入 -> 考慮現貨買入對沖
			a.adjustHedgeForBuy(ctx, event.Delta, event.Price)
		} else {
			// 合約賣出 -> 考慮現貨賣出減少對沖
			a.adjustHedgeForSell(ctx, -event.Delta, event.Price)
		}
	}
}

// shouldHedge 判斷是否應該開啟對沖
func (a *FundingArbitrageManager) shouldHedge() bool {
	if a.fundingMonitor == nil {
		return false
	}

	// 檢查費率閾值
	rate := a.fundingMonitor.GetCurrentRate()
	threshold := a.cfg.FundingRate.HedgeRateThreshold
	if threshold <= 0 {
		threshold = 0.001 // 默認 0.1%
	}

	return rate > threshold
}

// adjustHedgeForBuy 處理買入時的對沖調整
func (a *FundingArbitrageManager) adjustHedgeForBuy(ctx context.Context, quantity float64, price float64) {
	// 計算對沖價值
	hedgeValue := quantity * price
	minPosition := a.cfg.FundingRate.HedgeMinPosition
	if minPosition <= 0 {
		minPosition = 100
	}

	// 如果對沖價值太小，跳過
	if hedgeValue < minPosition {
		return
	}

	// 檢查價差
	if !a.checkSpread(ctx, price) {
		logger.Warn("💱 價差過大，跳過對沖")
		return
	}

	// 執行現貨買入
	if err := a.buySpot(ctx, quantity, price); err != nil {
		logger.Warn("💱 現貨買入失敗: %v", err)
		return
	}

	a.hedgedAmount += quantity
	a.spotPosition += quantity
	logger.Info("💱 [對沖買入] 數量: %.8f, 價格: %.2f, 總對沖: %.8f",
		quantity, price, a.hedgedAmount)
}

// adjustHedgeForSell 處理賣出時的對沖調整
func (a *FundingArbitrageManager) adjustHedgeForSell(ctx context.Context, quantity float64, price float64) {
	// 如果沒有對沖倉位，跳過
	if a.hedgedAmount <= 0 {
		return
	}

	// 計算需要賣出的現貨數量
	sellQty := quantity
	if sellQty > a.hedgedAmount {
		sellQty = a.hedgedAmount
	}

	// 執行現貨賣出
	if err := a.sellSpot(ctx, sellQty, price); err != nil {
		logger.Warn("💱 現貨賣出失敗: %v", err)
		return
	}

	a.hedgedAmount -= sellQty
	a.spotPosition -= sellQty
	logger.Info("💱 [對沖賣出] 數量: %.8f, 價格: %.2f, 剩餘對沖: %.8f",
		sellQty, price, a.hedgedAmount)
}

// checkSpread 檢查期現價差
func (a *FundingArbitrageManager) checkSpread(ctx context.Context, futuresPrice float64) bool {
	if a.spotExchange == nil {
		return false
	}

	spotPrice, err := a.spotExchange.GetLatestPrice(ctx, a.symbol)
	if err != nil {
		logger.Warn("💱 獲取現貨價格失敗: %v", err)
		return false
	}

	// 計算價差百分比
	spread := math.Abs(futuresPrice-spotPrice) / spotPrice * 100
	maxSpread := a.cfg.FundingRate.MaxSpreadPercent
	if maxSpread <= 0 {
		maxSpread = 0.5 // 默認 0.5%
	}

	if spread > maxSpread {
		logger.Warn("💱 價差 %.4f%% 超過閾值 %.4f%%", spread, maxSpread)
		return false
	}

	return true
}

// buySpot 買入現貨
func (a *FundingArbitrageManager) buySpot(ctx context.Context, quantity float64, price float64) error {
	if a.spotExchange == nil {
		return fmt.Errorf("現貨交易所未配置")
	}

	// 使用限價單買入
	_, err := a.spotExchange.PlaceOrder(ctx, &OrderRequest{
		Symbol:   a.symbol,
		Side:     "BUY",
		Price:    price * 1.001, // 略高於市價確保成交
		Quantity: quantity,
		PostOnly: false,
	})

	if err != nil {
		return fmt.Errorf("下單失敗: %w", err)
	}

	logger.Info("💱 現貨買入訂單已提交: 數量=%.8f, 價格=%.2f", quantity, price)

	return nil
}

// sellSpot 賣出現貨
func (a *FundingArbitrageManager) sellSpot(ctx context.Context, quantity float64, price float64) error {
	if a.spotExchange == nil {
		return fmt.Errorf("現貨交易所未配置")
	}

	// 使用限價單賣出
	_, err := a.spotExchange.PlaceOrder(ctx, &OrderRequest{
		Symbol:   a.symbol,
		Side:     "SELL",
		Price:    price * 0.999, // 略低於市價確保成交
		Quantity: quantity,
		PostOnly: false,
	})

	if err != nil {
		return fmt.Errorf("下單失敗: %w", err)
	}

	logger.Info("💱 現貨賣出訂單已提交: 數量=%.8f, 價格=%.2f", quantity, price)

	return nil
}

// syncPositions 同步倉位
func (a *FundingArbitrageManager) syncPositions(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.syncStatus = "syncing"

	// 獲取合約持倉
	if a.futuresExchange != nil {
		_, err := a.futuresExchange.GetPositions(ctx, a.symbol)
		if err != nil {
			a.syncStatus = "error"
			return fmt.Errorf("獲取合約持倉失敗: %w", err)
		}
		// 解析持倉（這裡需要根據實際返回類型處理）
		// 暫時簡化處理
		a.futuresPosition = 0
	}

	// 獲取現貨餘額
	if a.spotExchange != nil && a.baseAsset != "" {
		balance, err := a.spotExchange.GetBalance(ctx, a.baseAsset)
		if err != nil {
			logger.Warn("獲取現貨餘額失敗: %v", err)
		} else {
			a.spotPosition = balance
		}
	}

	a.lastSyncTime = time.Now()
	a.syncStatus = "synced"

	logger.Info("💱 倉位同步完成: 合約=%.8f, 現貨=%.8f, 對沖=%.8f",
		a.futuresPosition, a.spotPosition, a.hedgedAmount)

	return nil
}

// parsePositionSize 解析持倉大小
func parsePositionSize(positions []*Position) float64 {
	if positions == nil || len(positions) == 0 {
		return 0
	}
	
	totalSize := float64(0)
	for _, pos := range positions {
		totalSize += pos.Size
	}
	return totalSize
}

// checkAndAdjustHedge 檢查並調整對沖
func (a *FundingArbitrageManager) checkAndAdjustHedge(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// 如果費率不再高，考慮關閉對沖
	if !a.shouldHedge() && a.hedgedAmount > 0 {
		logger.Info("💱 費率恢復正常，準備關閉對沖")
		// 這裡可以實現逐步關閉對沖的邏輯
	}

	return nil
}

// SyncHedge 手動同步對沖（外部調用）
func (a *FundingArbitrageManager) SyncHedge(ctx context.Context) error {
	return a.syncPositions(ctx)
}

// GetHedgeStatus 獲取對沖狀態
func (a *FundingArbitrageManager) GetHedgeStatus() HedgeStatus {
	a.mu.RLock()
	defer a.mu.RUnlock()

	hedgeRatio := float64(0)
	if a.futuresPosition > 0 {
		hedgeRatio = a.hedgedAmount / a.futuresPosition * 100
	}

	currentRate := float64(0)
	if a.fundingMonitor != nil {
		currentRate = a.fundingMonitor.GetCurrentRate()
	}

	// 預估節省的費用（每 8 小時）
	estimatedSavings := a.hedgedAmount * currentRate

	return HedgeStatus{
		Enabled:          a.cfg.FundingRate.ArbitrageEnabled,
		FuturesPosition:  a.futuresPosition,
		SpotPosition:     a.spotPosition,
		HedgedAmount:     a.hedgedAmount,
		HedgeRatio:       hedgeRatio,
		CurrentRate:      currentRate,
		EstimatedSavings: estimatedSavings,
		LastSyncTime:     a.lastSyncTime,
		SyncStatus:       a.syncStatus,
	}
}

// CloseAllHedge 關閉所有對沖（緊急操作）
func (a *FundingArbitrageManager) CloseAllHedge(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.hedgedAmount <= 0 {
		return nil
	}

	logger.Warn("💱 [緊急] 關閉所有對沖倉位: %.8f", a.hedgedAmount)

	// 獲取當前價格
	price, err := a.spotExchange.GetLatestPrice(ctx, a.symbol)
	if err != nil {
		return fmt.Errorf("獲取價格失敗: %w", err)
	}

	// 賣出所有現貨對沖
	if err := a.sellSpot(ctx, a.hedgedAmount, price); err != nil {
		return fmt.Errorf("關閉對沖失敗: %w", err)
	}

	a.hedgedAmount = 0
	return nil
}

// IsRunning 判斷是否正在運行
func (a *FundingArbitrageManager) IsRunning() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.running
}
