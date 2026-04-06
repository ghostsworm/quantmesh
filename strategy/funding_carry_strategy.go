package strategy

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/position"
)

// CarryDirection 套利方向
type CarryDirection int

const (
	DirectionNone    CarryDirection = 0
	DirectionForward CarryDirection = 1 // 現貨多 + 合約空（正費率收錢）
	DirectionReverse CarryDirection = 2 // 借幣空現貨 + 合約多（負費率收錢）
)

func (d CarryDirection) String() string {
	switch d {
	case DirectionForward:
		return "forward"
	case DirectionReverse:
		return "reverse"
	default:
		return "none"
	}
}

// FundingCarryStrategy 資金費率期現套利
type FundingCarryStrategy struct {
	name   string
	cfg    *config.Config
	symCfg config.SymbolConfig
	fut    exchange.IExchange
	spot   exchange.IExchange
	symbol string

	// 正向參數
	minFundingRate  float64
	exitFundingRate float64
	maxBasisPct     float64
	tickInterval    time.Duration

	// 反向套利參數
	marginEx         exchange.ISpotMarginExchange // nullable
	reverseEnabled   bool
	reverseMinRate   float64 // 負費率絕對值觸發閾值（正值，如 0.0004）
	reverseExitRate  float64 // 負費率退出閾值（正值）
	marginInterestMax float64 // 日利率上限

	// 結算時間感知
	nextSettlement   time.Time
	lastSettlement   time.Time
	settlementBuffer time.Duration
	settledThisTick  bool

	// 資金自動劃轉
	autoTransferEnabled  bool
	transferReserveSpot  float64
	profitHarvestEnabled bool
	profitHarvestMin     float64

	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	eventBus EventBus

	// 持倉狀態（每次 tick 從交易所同步）
	direction  CarryDirection
	spotQty    float64 // 正向：現貨持倉; 反向：0
	futQty     float64 // 正向：合約空頭 size; 反向：合約多頭 size
	marginDebt float64 // 反向：借幣數量

	consecutiveErrors int
}

const (
	maxConsecutiveErrors = 5
	orderWaitTimeout     = 30 * time.Second
	orderPollInterval    = 2 * time.Second
)

// NewFundingCarryStrategy 建立策略
func NewFundingCarryStrategy(
	name string,
	cfg *config.Config,
	symCfg config.SymbolConfig,
	fut exchange.IExchange,
	spot exchange.IExchange,
	marginEx exchange.ISpotMarginExchange,
	stratCfg map[string]interface{},
) *FundingCarryStrategy {
	minR := 0.0004
	exitR := 0.0002
	maxBasis := 0.5
	intervalSec := 45
	settleBuf := 5 * time.Minute

	reverseEnabled := false
	reverseMinRate := 0.0004
	reverseExitRate := 0.0002
	marginInterestMax := 0.001

	autoTransfer := false
	reserveSpot := 50.0
	harvestEnabled := false
	harvestMin := 5.0

	if stratCfg != nil {
		if v, ok := stratCfg["min_funding_rate"].(float64); ok && v > 0 {
			minR = v
		}
		if v, ok := stratCfg["exit_funding_rate"].(float64); ok && v > 0 {
			exitR = v
		}
		if v, ok := stratCfg["max_basis_pct"].(float64); ok && v > 0 {
			maxBasis = v
		}
		if v, ok := stratCfg["rebalance_interval_sec"].(float64); ok && v >= 10 {
			intervalSec = int(v)
		}
		if v, ok := stratCfg["settlement_buffer_min"].(float64); ok && v >= 1 {
			settleBuf = time.Duration(v) * time.Minute
		}
		if v, ok := stratCfg["reverse_enabled"].(bool); ok {
			reverseEnabled = v
		}
		if v, ok := stratCfg["reverse_min_funding_rate"].(float64); ok && v > 0 {
			reverseMinRate = v
		}
		if v, ok := stratCfg["reverse_exit_funding_rate"].(float64); ok && v > 0 {
			reverseExitRate = v
		}
		if v, ok := stratCfg["margin_interest_max"].(float64); ok && v > 0 {
			marginInterestMax = v
		}
		if v, ok := stratCfg["auto_transfer_enabled"].(bool); ok {
			autoTransfer = v
		}
		if v, ok := stratCfg["transfer_reserve_spot"].(float64); ok && v >= 0 {
			reserveSpot = v
		}
		if v, ok := stratCfg["profit_harvest_enabled"].(bool); ok {
			harvestEnabled = v
		}
		if v, ok := stratCfg["profit_harvest_min"].(float64); ok && v > 0 {
			harvestMin = v
		}
	}

	if marginEx == nil {
		reverseEnabled = false
	}

	return &FundingCarryStrategy{
		name:                 name,
		cfg:                  cfg,
		symCfg:               symCfg,
		fut:                  fut,
		spot:                 spot,
		symbol:               symCfg.Symbol,
		minFundingRate:       minR,
		exitFundingRate:      exitR,
		maxBasisPct:          maxBasis,
		tickInterval:         time.Duration(intervalSec) * time.Second,
		marginEx:             marginEx,
		reverseEnabled:       reverseEnabled,
		reverseMinRate:       reverseMinRate,
		reverseExitRate:      reverseExitRate,
		marginInterestMax:    marginInterestMax,
		settlementBuffer:     settleBuf,
		autoTransferEnabled:  autoTransfer,
		transferReserveSpot:  reserveSpot,
		profitHarvestEnabled: harvestEnabled,
		profitHarvestMin:     harvestMin,
	}
}

func (s *FundingCarryStrategy) Name() string { return s.name }

func (s *FundingCarryStrategy) Initialize(*config.Config, position.OrderExecutorInterface, position.IExchange) error {
	return nil
}

func (s *FundingCarryStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
}

func (s *FundingCarryStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx, s.cancel = context.WithCancel(ctx)
	go s.runLoop()
	logger.Info("✅ [%s] 資金費套利策略已啟動 (min=%.5f exit=%.5f reverse=%v)", s.symbol, s.minFundingRate, s.exitFundingRate, s.reverseEnabled)
	return nil
}

func (s *FundingCarryStrategy) Stop() error {
	s.mu.RLock()
	dir := s.direction
	s.mu.RUnlock()

	if dir != DirectionNone {
		logger.Info("⏹️ [%s] 停止前嘗試平倉 (direction=%s)…", s.symbol, dir)
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer closeCancel()
		var err error
		if dir == DirectionForward {
			err = s.closeAll(closeCtx, "bot_stopped")
		} else {
			err = s.closeReverse(closeCtx, "bot_stopped")
		}
		if err != nil {
			logger.Warn("⚠️ [%s] 停止時平倉失敗: %v", s.symbol, err)
			s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
				"action":  "stop_close_failed",
				"error":   err.Error(),
				"message": "Bot 停止時平倉失敗，請手動檢查交易所持倉",
			})
		}
	}

	s.mu.Lock()
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()
	logger.Info("⏹️ [%s] 資金費套利策略已停止", s.symbol)
	return nil
}

func (s *FundingCarryStrategy) OnPriceChange(float64) error        { return nil }
func (s *FundingCarryStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }
func (s *FundingCarryStrategy) GetPositions() []*Position           { return nil }
func (s *FundingCarryStrategy) GetOrders() []*Order                 { return nil }
func (s *FundingCarryStrategy) GetStatistics() *StrategyStatistics  { return &StrategyStatistics{} }

func (s *FundingCarryStrategy) GetVisualizationData() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return map[string]interface{}{
		"type":             "funding_carry",
		"direction":        s.direction.String(),
		"position_open":    s.direction != DirectionNone,
		"spot_qty":         s.spotQty,
		"futures_qty":      s.futQty,
		"margin_debt":      s.marginDebt,
		"next_settlement":  s.nextSettlement.Format(time.RFC3339),
		"reverse_enabled":  s.reverseEnabled,
	}
}

// GetFundingStatus 返回結算時間與持倉摘要（供 API 層使用）
func (s *FundingCarryStrategy) GetFundingStatus() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	secsUntil := 0.0
	if !s.nextSettlement.IsZero() {
		secsUntil = time.Until(s.nextSettlement).Seconds()
		if secsUntil < 0 {
			secsUntil = 0
		}
	}
	return map[string]interface{}{
		"symbol":                   s.symbol,
		"direction":                s.direction.String(),
		"spot_qty":                 s.spotQty,
		"fut_qty":                  s.futQty,
		"margin_debt":              s.marginDebt,
		"next_settlement":          s.nextSettlement.Format(time.RFC3339),
		"seconds_until_settlement": int(secsUntil),
		"reverse_enabled":          s.reverseEnabled,
	}
}

// ---------------------------------------------------------------------------
// Main loop
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) runLoop() {
	t := time.NewTicker(s.tickInterval)
	defer t.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-t.C:
			if err := s.tick(); err != nil {
				s.mu.Lock()
				s.consecutiveErrors++
				errCount := s.consecutiveErrors
				s.mu.Unlock()

				logger.Warn("⚠️ [%s] funding_carry tick error (%d): %v", s.symbol, errCount, err)

				if errCount >= maxConsecutiveErrors {
					s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
						"consecutive_errors": errCount,
						"last_error":         err.Error(),
						"message":            fmt.Sprintf("資金費套利連續 %d 次 tick 異常，請檢查 API 狀態", errCount),
					})
					s.mu.Lock()
					s.consecutiveErrors = 0
					s.mu.Unlock()
				}
			} else {
				s.mu.Lock()
				s.consecutiveErrors = 0
				s.mu.Unlock()
			}
		}
	}
}

func (s *FundingCarryStrategy) tick() error {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	if err := s.syncPositions(ctx); err != nil {
		return fmt.Errorf("syncPositions: %w", err)
	}

	// 嘗試獲取結算時間（非致命，失敗時用 UTC 8h 估算）
	s.updateSettlementTime(ctx)

	// 結算後首個 tick：收割利潤
	s.mu.RLock()
	settled := s.settledThisTick
	s.mu.RUnlock()
	if settled && s.profitHarvestEnabled {
		s.harvestProfit(ctx)
	}

	rate, err := s.fut.GetFundingRate(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("GetFundingRate: %w", err)
	}

	s.mu.RLock()
	dir := s.direction
	spotQ := s.spotQty
	futQ := s.futQty
	debt := s.marginDebt
	nearSettlement := s.isNearSettlement()
	s.mu.RUnlock()

	hasPosition := dir != DirectionNone

	// 腿不平衡檢測
	if hasPosition {
		imbalanced := false
		if dir == DirectionForward && ((spotQ > 0) != (futQ > 0)) {
			imbalanced = true
		}
		if dir == DirectionReverse && ((debt > 0) != (futQ > 0)) {
			imbalanced = true
		}
		if imbalanced {
			logger.Warn("⚠️ [%s] 腿不平衡! dir=%s spot=%.8f fut=%.8f debt=%.8f", s.symbol, dir, spotQ, futQ, debt)
			s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
				"spot_qty":    spotQ,
				"fut_qty":     futQ,
				"margin_debt": debt,
				"direction":   dir.String(),
				"message":     "期現對沖腿不平衡，建議手動檢查",
			})
		}
	}

	// 持倉中：檢查退出條件
	if hasPosition {
		if dir == DirectionForward && rate < s.exitFundingRate {
			logger.Info("📉 [%s] 資金費 %.5f < 退出閾值 %.5f，正向平倉", s.symbol, rate, s.exitFundingRate)
			return s.closeAll(ctx, "exit_funding_rate")
		}
		if dir == DirectionReverse && rate > -s.reverseExitRate {
			logger.Info("📈 [%s] 資金費 %.5f > 反向退出閾值 -%.5f，反向平倉", s.symbol, rate, s.reverseExitRate)
			return s.closeReverse(ctx, "exit_reverse_rate")
		}
		return nil
	}

	// 無倉位：結算臨近時不開新倉
	if nearSettlement {
		logger.Info("⏳ [%s] 距結算 < %v，暫停開倉", s.symbol, s.settlementBuffer)
		return nil
	}

	// 評估費率方向
	if rate >= s.minFundingRate {
		futPx, err := s.fut.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		spotPx, err := s.spot.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		basisPct := math.Abs(futPx-spotPx) / spotPx * 100
		if basisPct > s.maxBasisPct {
			logger.Warn("⚠️ [%s] 期現價差 %.4f%% 超過上限 %.4f%%，暫不開倉", s.symbol, basisPct, s.maxBasisPct)
			return nil
		}
		return s.openHedge(ctx, futPx, spotPx, rate)
	}

	if s.reverseEnabled && rate <= -s.reverseMinRate {
		futPx, err := s.fut.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		spotPx, err := s.spot.GetLatestPrice(ctx, s.symbol)
		if err != nil {
			return err
		}
		basisPct := math.Abs(futPx-spotPx) / spotPx * 100
		if basisPct > s.maxBasisPct {
			logger.Warn("⚠️ [%s] 期現價差 %.4f%% 超上限 %.4f%%，暫不反向開倉", s.symbol, basisPct, s.maxBasisPct)
			return nil
		}
		return s.openReverseHedge(ctx, futPx, spotPx, rate)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Settlement time awareness
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) updateSettlementTime(ctx context.Context) {
	info, err := s.fut.GetFundingInfo(ctx, s.symbol)

	s.mu.Lock()
	defer s.mu.Unlock()

	s.settledThisTick = false

	if err != nil {
		// API 不支持或臨時失敗：用 UTC 8h 估算
		if s.nextSettlement.IsZero() {
			s.nextSettlement = estimateNextSettlement(time.Now().UTC())
		} else if time.Now().After(s.nextSettlement) {
			s.lastSettlement = s.nextSettlement
			s.nextSettlement = estimateNextSettlement(time.Now().UTC())
			s.settledThisTick = true
		}
		return
	}

	// API 返回了精確的下次結算時間
	if !s.nextSettlement.IsZero() && !info.NextFundingTime.Equal(s.nextSettlement) && time.Now().After(s.nextSettlement) {
		s.lastSettlement = s.nextSettlement
		s.settledThisTick = true
	}
	s.nextSettlement = info.NextFundingTime
}

func (s *FundingCarryStrategy) isNearSettlement() bool {
	if s.nextSettlement.IsZero() {
		return false
	}
	return time.Until(s.nextSettlement) < s.settlementBuffer
}

func estimateNextSettlement(now time.Time) time.Time {
	hour := now.Hour()
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case hour < 8:
		return base.Add(8 * time.Hour)
	case hour < 16:
		return base.Add(16 * time.Hour)
	default:
		return base.Add(24 * time.Hour)
	}
}

// ---------------------------------------------------------------------------
// Auto transfer
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) ensureFuturesMargin(ctx context.Context, requiredUSDT float64) {
	if !s.autoTransferEnabled {
		return
	}
	futBal, err := s.fut.GetBalance(ctx, "USDT")
	if err != nil {
		logger.Warn("⚠️ [%s] 查詢合約 USDT 餘額失敗: %v", s.symbol, err)
		return
	}
	if futBal >= requiredUSDT {
		return
	}
	need := requiredUSDT - futBal
	spotBal, err := s.spot.GetBalance(ctx, "USDT")
	if err != nil {
		logger.Warn("⚠️ [%s] 查詢現貨 USDT 餘額失敗: %v", s.symbol, err)
		return
	}
	available := spotBal - s.transferReserveSpot
	if available <= 0 {
		logger.Warn("⚠️ [%s] 現貨餘額 %.2f 扣除保留 %.2f 後不足劃轉", s.symbol, spotBal, s.transferReserveSpot)
		return
	}
	if need > available {
		need = available
	}
	txID, err := s.spot.InternalTransfer(ctx, "SPOT", "UMFUTURE", "USDT", need)
	if err != nil {
		logger.Warn("⚠️ [%s] SPOT→UMFUTURE 劃轉 %.2f USDT 失敗: %v", s.symbol, need, err)
		return
	}
	logger.Info("💸 [%s] 自動劃轉 SPOT→UMFUTURE %.2f USDT (txID=%s)", s.symbol, need, txID)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"action":  "auto_transfer_in",
		"amount":  need,
		"tx_id":   txID,
		"message": fmt.Sprintf("自動劃轉 %.2f USDT 到合約帳戶", need),
	})
}

func (s *FundingCarryStrategy) harvestProfit(ctx context.Context) {
	if !s.profitHarvestEnabled {
		return
	}
	futBal, err := s.fut.GetBalance(ctx, "USDT")
	if err != nil {
		return
	}
	s.mu.RLock()
	dir := s.direction
	futQ := s.futQty
	s.mu.RUnlock()

	// 估算持倉佔用保證金（簡化：用合約持倉 × 當前價 ÷ 槓桿）
	var requiredMargin float64
	if dir != DirectionNone && futQ > 0 {
		if px, e := s.fut.GetLatestPrice(ctx, s.symbol); e == nil {
			requiredMargin = futQ * px / 5 // 假設 5x 槓桿
		}
	}
	surplus := futBal - requiredMargin - 50 // 留 50 USDT buffer
	if surplus < s.profitHarvestMin {
		return
	}

	txID, err := s.fut.InternalTransfer(ctx, "UMFUTURE", "SPOT", "USDT", surplus)
	if err != nil {
		logger.Warn("⚠️ [%s] 利潤歸集失敗 %.2f USDT: %v", s.symbol, surplus, err)
		return
	}
	logger.Info("💰 [%s] 結算後利潤歸集 UMFUTURE→SPOT %.2f USDT (txID=%s)", s.symbol, surplus, txID)
	s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
		"action":  "profit_harvest",
		"amount":  surplus,
		"tx_id":   txID,
		"message": fmt.Sprintf("利潤歸集 %.2f USDT 到現貨帳戶", surplus),
	})
}

// ---------------------------------------------------------------------------
// Position sync
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) syncPositions(ctx context.Context) error {
	// 合約持倉
	var futShort, futLong float64
	pos, err := s.fut.GetPositions(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("fut.GetPositions: %w", err)
	}
	for _, p := range pos {
		if p == nil {
			continue
		}
		if p.Size < 0 {
			futShort += math.Abs(p.Size)
		} else if p.Size > 0 {
			futLong += p.Size
		}
	}

	// 現貨餘額
	base := s.spot.GetBaseAsset()
	spotBal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		return fmt.Errorf("spot.GetBalance(%s): %w", base, err)
	}

	// 保證金借幣負債（反向套利用）
	var debt float64
	if s.marginEx != nil {
		if marginPos, e := s.marginEx.GetPositions(ctx, s.symbol); e == nil {
			for _, mp := range marginPos {
				if mp != nil && mp.Size < 0 {
					debt += math.Abs(mp.Size)
				}
			}
		}
	}

	s.mu.Lock()
	s.spotQty = spotBal
	s.marginDebt = debt
	if futShort > 0 && spotBal > 0 {
		s.direction = DirectionForward
		s.futQty = futShort
	} else if futLong > 0 && debt > 0 {
		s.direction = DirectionReverse
		s.futQty = futLong
	} else if futShort > 0 || futLong > 0 || spotBal > 0 || debt > 0 {
		// 有殘留但不配對——保持上次方向讓平衡檢測處理
		if futShort > 0 {
			s.futQty = futShort
		} else {
			s.futQty = futLong
		}
	} else {
		s.direction = DirectionNone
		s.futQty = 0
	}
	s.mu.Unlock()
	return nil
}

// ---------------------------------------------------------------------------
// Forward: open hedge
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) capitalUSDT() float64 {
	c := s.symCfg.TotalAllocatedCapital
	if c <= 0 {
		c = s.symCfg.OrderQuantity
	}
	return c
}

func (s *FundingCarryStrategy) openHedge(ctx context.Context, futPx, spotPx, rate float64) error {
	cap := s.capitalUSDT()
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小，建議 ≥200 USDT", cap)
	}
	legNotional := cap / 2
	if legNotional < 100 {
		return fmt.Errorf("單腿名義 %.2f USDT 低於合約最小要求", legNotional)
	}

	s.ensureFuturesMargin(ctx, legNotional)

	qty := legNotional / spotPx
	qty = s.roundQty(qty, s.spot.GetQuantityDecimals())

	buyPrice := spotPx * 1.005
	buyPrice = s.roundPrice(buyPrice, s.spot.GetPriceDecimals())

	spotOrder, err := s.spot.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideBuy,
		Type:          exchange.OrderTypeLimit,
		Quantity:      qty,
		Price:         buyPrice,
		PriceDecimals: s.spot.GetPriceDecimals(),
		StrategyType:  "funding_carry",
	})
	if err != nil {
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "spot_buy", "error": err.Error(), "message": "資金費套利現貨買入失敗",
		})
		return fmt.Errorf("現貨買入: %w", err)
	}

	filledQty, spotFillErr := s.waitOrderFill(ctx, s.spot, spotOrder.OrderID, orderWaitTimeout)
	if spotFillErr != nil || filledQty <= 0 {
		logger.Warn("⚠️ [%s] 現貨買入未成交，撤單回滾 orderID=%d", s.symbol, spotOrder.OrderID)
		_ = s.spot.CancelOrder(ctx, s.symbol, spotOrder.OrderID)
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "spot_buy", "order_id": spotOrder.OrderID, "message": "現貨買入超時未成交，已撤單",
		})
		if spotFillErr != nil {
			return fmt.Errorf("等待現貨成交: %w", spotFillErr)
		}
		return fmt.Errorf("現貨買入超時未成交，已撤單")
	}

	futQty := s.roundQty(filledQty, s.fut.GetQuantityDecimals())
	if futQty <= 0 {
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "現貨已成交但合約數量精度截斷為 0", "spot_qty": filledQty,
		})
		return fmt.Errorf("合約精度截斷導致數量=0，spot 已買入 %.8f 但無法對沖", filledQty)
	}

	_, err = s.fut.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeMarket,
		Quantity:      futQty,
		Price:         0,
		PriceDecimals: s.fut.GetPriceDecimals(),
		StrategyType:  "funding_carry",
	})
	if err != nil {
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"side": "futures_sell", "error": err.Error(), "spot_qty": filledQty,
			"message": "合約開空失敗！現貨已買入，請立即手動處理",
		})
		return fmt.Errorf("合約開空失敗（現貨已買入 %.8f，存在裸多風險）: %w", filledQty, err)
	}

	logger.Info("✅ [%s] 正向對沖已建立 spot=%.8f fut_short=%.8f rate=%.5f", s.symbol, filledQty, futQty, rate)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"direction": "forward", "spot_qty": filledQty, "fut_qty": futQty,
		"funding_rate": rate, "message": fmt.Sprintf("正向開倉: %s spot=%.6f short=%.6f rate=%.5f", s.symbol, filledQty, futQty, rate),
	})
	return nil
}

// ---------------------------------------------------------------------------
// Reverse: open reverse hedge (borrow + sell spot + long futures)
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) openReverseHedge(ctx context.Context, futPx, spotPx, rate float64) error {
	if s.marginEx == nil {
		return fmt.Errorf("反向套利需要保證金帳戶，但 marginEx 為 nil")
	}

	cap := s.capitalUSDT()
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小", cap)
	}
	legNotional := cap / 2

	s.ensureFuturesMargin(ctx, legNotional)

	base := s.spot.GetBaseAsset()
	borrowQty := legNotional / spotPx
	borrowQty = s.roundQty(borrowQty, s.spot.GetQuantityDecimals())
	if borrowQty <= 0 {
		return fmt.Errorf("借幣數量太小")
	}

	// Step 1: 借幣
	_, err := s.marginEx.Borrow(ctx, base, borrowQty)
	if err != nil {
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "margin_borrow", "error": err.Error(), "message": "借幣失敗",
		})
		return fmt.Errorf("借幣 %s: %w", base, err)
	}

	// Step 2: 保證金帳戶賣出（用 marginEx，它 embed 了 IExchange）
	sellPrice := spotPx * 0.995
	sellPrice = s.roundPrice(sellPrice, s.spot.GetPriceDecimals())
	sellOrder, err := s.marginEx.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeLimit,
		Quantity:      borrowQty,
		Price:         sellPrice,
		PriceDecimals: s.spot.GetPriceDecimals(),
		StrategyType:  "funding_carry_reverse",
	})
	if err != nil {
		// 賣出失敗，歸還借幣
		_, _ = s.marginEx.Repay(ctx, base, borrowQty)
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "margin_sell", "error": err.Error(), "message": "借幣後賣出失敗，已歸還",
		})
		return fmt.Errorf("保證金賣出: %w", err)
	}

	filledQty, fillErr := s.waitOrderFill(ctx, s.marginEx, sellOrder.OrderID, orderWaitTimeout)
	if fillErr != nil || filledQty <= 0 {
		_ = s.marginEx.CancelOrder(ctx, s.symbol, sellOrder.OrderID)
		_, _ = s.marginEx.Repay(ctx, base, borrowQty)
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"side": "margin_sell", "message": "保證金賣出超時，已撤單歸還",
		})
		return fmt.Errorf("保證金賣出超時未成交")
	}

	// Step 3: 合約開多
	futQty := s.roundQty(filledQty, s.fut.GetQuantityDecimals())
	if futQty <= 0 {
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "已賣出借幣但合約精度截斷為 0", "sold_qty": filledQty,
		})
		return fmt.Errorf("合約精度截斷，借幣已賣出 %.8f 但無法對沖", filledQty)
	}

	_, err = s.fut.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideBuy,
		Type:          exchange.OrderTypeMarket,
		Quantity:      futQty,
		Price:         0,
		PriceDecimals: s.fut.GetPriceDecimals(),
		StrategyType:  "funding_carry_reverse",
	})
	if err != nil {
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"side": "futures_buy", "error": err.Error(), "sold_qty": filledQty,
			"message": "合約開多失敗！借幣已賣出，存在裸空風險",
		})
		return fmt.Errorf("合約開多失敗（借幣已賣出 %.8f，裸空風險）: %w", filledQty, err)
	}

	logger.Info("✅ [%s] 反向對沖已建立 borrow=%.8f fut_long=%.8f rate=%.5f", s.symbol, filledQty, futQty, rate)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"direction": "reverse", "borrow_qty": filledQty, "fut_qty": futQty,
		"funding_rate": rate, "message": fmt.Sprintf("反向開倉: %s borrow=%.6f long=%.6f rate=%.5f", s.symbol, filledQty, futQty, rate),
	})
	return nil
}

// ---------------------------------------------------------------------------
// Close positions
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) closeAll(ctx context.Context, reason string) error {
	var errs []error

	pos, err := s.fut.GetPositions(ctx, s.symbol)
	if err != nil {
		errs = append(errs, fmt.Errorf("fut.GetPositions: %w", err))
	} else {
		for _, p := range pos {
			if p == nil || p.Size >= 0 {
				continue
			}
			q := math.Abs(p.Size)
			_, err = s.fut.PlaceOrder(ctx, &exchange.OrderRequest{
				Symbol: s.symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket,
				Quantity: q, ReduceOnly: true, PriceDecimals: s.fut.GetPriceDecimals(),
				StrategyType: "funding_carry",
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("合約平空: %w", err))
				s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
					"side": "futures_close", "error": err.Error(), "message": "合約平空失敗",
				})
			}
		}
	}

	base := s.spot.GetBaseAsset()
	bal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		errs = append(errs, fmt.Errorf("spot.GetBalance: %w", err))
	} else {
		qty := s.roundQty(bal, s.spot.GetQuantityDecimals())
		if qty > 0 {
			sellPrice := 0.0
			if px, e := s.spot.GetLatestPrice(ctx, s.symbol); e == nil {
				sellPrice = px * 0.99
			}
			sellPrice = s.roundPrice(sellPrice, s.spot.GetPriceDecimals())
			if sellPrice > 0 {
				_, err = s.spot.PlaceOrder(ctx, &exchange.OrderRequest{
					Symbol: s.symbol, Side: exchange.SideSell, Type: exchange.OrderTypeLimit,
					Quantity: qty, Price: sellPrice, PriceDecimals: s.spot.GetPriceDecimals(),
					StrategyType: "funding_carry",
				})
				if err != nil {
					errs = append(errs, fmt.Errorf("現貨賣出: %w", err))
					s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
						"side": "spot_sell", "error": err.Error(), "message": "現貨賣出失敗",
					})
				}
			}
		}
	}

	if len(errs) == 0 {
		logger.Info("✅ [%s] 正向平倉完成 reason=%s", s.symbol, reason)
		s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
			"direction": "forward", "reason": reason,
			"message": fmt.Sprintf("正向平倉: %s (%s)", s.symbol, reason),
		})
	} else {
		logger.Warn("⚠️ [%s] 正向平倉部分失敗 reason=%s errors=%d", s.symbol, reason, len(errs))
	}
	return combineErrors(errs)
}

func (s *FundingCarryStrategy) closeReverse(ctx context.Context, reason string) error {
	if s.marginEx == nil {
		return fmt.Errorf("marginEx is nil, cannot close reverse")
	}
	var errs []error

	// 1. 合約平多
	pos, err := s.fut.GetPositions(ctx, s.symbol)
	if err != nil {
		errs = append(errs, fmt.Errorf("fut.GetPositions: %w", err))
	} else {
		for _, p := range pos {
			if p == nil || p.Size <= 0 {
				continue
			}
			_, err = s.fut.PlaceOrder(ctx, &exchange.OrderRequest{
				Symbol: s.symbol, Side: exchange.SideSell, Type: exchange.OrderTypeMarket,
				Quantity: p.Size, ReduceOnly: true, PriceDecimals: s.fut.GetPriceDecimals(),
				StrategyType: "funding_carry_reverse",
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("合約平多: %w", err))
				s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
					"side": "futures_close_long", "error": err.Error(), "message": "合約平多失敗",
				})
			}
		}
	}

	// 2. 保證金帳戶買入 base asset
	s.mu.RLock()
	debt := s.marginDebt
	s.mu.RUnlock()
	if debt > 0 {
		buyQty := s.roundQty(debt*1.002, s.spot.GetQuantityDecimals()) // 多買 0.2% 覆蓋利息
		if buyQty > 0 {
			buyPrice := 0.0
			if px, e := s.spot.GetLatestPrice(ctx, s.symbol); e == nil {
				buyPrice = px * 1.005
			}
			buyPrice = s.roundPrice(buyPrice, s.spot.GetPriceDecimals())
			if buyPrice > 0 {
				buyOrder, err := s.marginEx.PlaceOrder(ctx, &exchange.OrderRequest{
					Symbol: s.symbol, Side: exchange.SideBuy, Type: exchange.OrderTypeLimit,
					Quantity: buyQty, Price: buyPrice, PriceDecimals: s.spot.GetPriceDecimals(),
					StrategyType: "funding_carry_reverse",
				})
				if err != nil {
					errs = append(errs, fmt.Errorf("保證金買入: %w", err))
				} else {
					filledQty, _ := s.waitOrderFill(ctx, s.marginEx, buyOrder.OrderID, orderWaitTimeout)
					if filledQty <= 0 {
						_ = s.marginEx.CancelOrder(ctx, s.symbol, buyOrder.OrderID)
						errs = append(errs, fmt.Errorf("保證金買入超時"))
					}
				}
			}
		}

		// 3. 還幣
		base := s.spot.GetBaseAsset()
		_, err = s.marginEx.Repay(ctx, base, debt)
		if err != nil {
			errs = append(errs, fmt.Errorf("還幣: %w", err))
			s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
				"side": "margin_repay", "error": err.Error(), "debt": debt,
				"message": "還幣失敗，借幣利息持續計算！",
			})
		}
	}

	if len(errs) == 0 {
		logger.Info("✅ [%s] 反向平倉完成 reason=%s", s.symbol, reason)
		s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
			"direction": "reverse", "reason": reason,
			"message": fmt.Sprintf("反向平倉: %s (%s)", s.symbol, reason),
		})
	} else {
		logger.Warn("⚠️ [%s] 反向平倉部分失敗 reason=%s errors=%d", s.symbol, reason, len(errs))
	}
	return combineErrors(errs)
}

// ---------------------------------------------------------------------------
// Order helpers
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) waitOrderFill(ctx context.Context, ex exchange.IExchange, orderID int64, timeout time.Duration) (float64, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(orderPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline:
			o, err := ex.GetOrder(ctx, s.symbol, orderID)
			if err == nil && o.ExecutedQty > 0 {
				return o.ExecutedQty, nil
			}
			return 0, nil
		case <-ticker.C:
			o, err := ex.GetOrder(ctx, s.symbol, orderID)
			if err != nil {
				continue
			}
			switch o.Status {
			case exchange.OrderStatusFilled:
				return o.ExecutedQty, nil
			case exchange.OrderStatusPartiallyFilled:
				continue
			case exchange.OrderStatusCanceled, exchange.OrderStatusRejected, exchange.OrderStatusExpired:
				if o.ExecutedQty > 0 {
					return o.ExecutedQty, nil
				}
				return 0, fmt.Errorf("訂單狀態 %s", o.Status)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Event & math helpers
// ---------------------------------------------------------------------------

func (s *FundingCarryStrategy) publishEvent(eventType event.EventType, data map[string]interface{}) {
	s.mu.RLock()
	bus := s.eventBus
	s.mu.RUnlock()
	if bus == nil {
		return
	}
	if data == nil {
		data = make(map[string]interface{})
	}
	data["strategy"] = "funding_carry"
	if _, ok := data["symbol"]; !ok {
		data["symbol"] = s.symbol
	}
	bus.Publish(&event.Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	})
}

func (s *FundingCarryStrategy) roundQty(q float64, decimals int) float64 {
	step := math.Pow10(-decimals)
	if step <= 0 {
		return q
	}
	return math.Floor(q/step) * step
}

func (s *FundingCarryStrategy) roundPrice(p float64, decimals int) float64 {
	factor := math.Pow10(decimals)
	return math.Round(p*factor) / factor
}

func combineErrors(errs []error) error {
	if len(errs) == 0 {
		return nil
	}
	msg := ""
	for i, e := range errs {
		if i > 0 {
			msg += "; "
		}
		msg += e.Error()
	}
	return fmt.Errorf("%s", msg)
}
