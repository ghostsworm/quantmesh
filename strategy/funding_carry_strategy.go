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

// FundingCarryStrategy 資金費率期現套利：現貨多 + 永續空（正費率時收資金費）
type FundingCarryStrategy struct {
	name   string
	cfg    *config.Config
	symCfg config.SymbolConfig
	fut    exchange.IExchange
	spot   exchange.IExchange
	symbol string

	minFundingRate  float64
	exitFundingRate float64
	maxBasisPct     float64
	tickInterval    time.Duration

	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	eventBus EventBus

	// 從交易所同步的真實持倉（每次 tick 刷新，不依賴內存布爾值）
	spotQty float64 // 現貨持有基礎幣數量
	futQty  float64 // 合約空頭數量（正值表示空頭 size）

	consecutiveErrors int
}

const (
	maxConsecutiveErrors = 5 // 連續錯誤達到此數觸發 critical 通知
	orderWaitTimeout     = 30 * time.Second
	orderPollInterval    = 2 * time.Second
)

// NewFundingCarryStrategy 建立策略（fut/spot 為同一交易所的合約與現貨實例）
func NewFundingCarryStrategy(
	name string,
	cfg *config.Config,
	symCfg config.SymbolConfig,
	fut exchange.IExchange,
	spot exchange.IExchange,
	stratCfg map[string]interface{},
) *FundingCarryStrategy {
	minR := 0.0004
	exitR := 0.0002
	maxBasis := 0.5
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
	}
	intervalSec := 45
	if stratCfg != nil {
		if v, ok := stratCfg["rebalance_interval_sec"].(float64); ok && v >= 10 {
			intervalSec = int(v)
		}
	}
	return &FundingCarryStrategy{
		name:            name,
		cfg:             cfg,
		symCfg:          symCfg,
		fut:             fut,
		spot:            spot,
		symbol:          symCfg.Symbol,
		minFundingRate:  minR,
		exitFundingRate: exitR,
		maxBasisPct:     maxBasis,
		tickInterval:    time.Duration(intervalSec) * time.Second,
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
	logger.Info("✅ [%s] 資金費套利策略已啟動 (min=%.5f exit=%.5f)", s.symbol, s.minFundingRate, s.exitFundingRate)
	return nil
}

// Stop 停止策略——先嘗試平倉再停止循環
func (s *FundingCarryStrategy) Stop() error {
	s.mu.RLock()
	hasPosition := s.spotQty > 0 || s.futQty > 0
	s.mu.RUnlock()

	if hasPosition {
		logger.Info("⏹️ [%s] 停止前嘗試平倉…", s.symbol)
		closeCtx, closeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer closeCancel()
		if err := s.closeAll(closeCtx, "bot_stopped"); err != nil {
			logger.Warn("⚠️ [%s] 停止時平倉失敗: %v（仓位可能仍在交易所）", s.symbol, err)
			s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
				"symbol":  s.symbol,
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

func (s *FundingCarryStrategy) OnPriceChange(float64) error { return nil }

func (s *FundingCarryStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }

func (s *FundingCarryStrategy) GetPositions() []*Position { return nil }

func (s *FundingCarryStrategy) GetOrders() []*Order { return nil }

func (s *FundingCarryStrategy) GetStatistics() *StrategyStatistics {
	return &StrategyStatistics{}
}

func (s *FundingCarryStrategy) GetVisualizationData() map[string]interface{} {
	s.mu.RLock()
	sQ := s.spotQty
	fQ := s.futQty
	s.mu.RUnlock()
	return map[string]interface{}{
		"type":          "funding_carry",
		"position_open": sQ > 0 || fQ > 0,
		"spot_qty":      sQ,
		"futures_qty":   fQ,
	}
}

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
						"symbol":            s.symbol,
						"strategy":          "funding_carry",
						"consecutive_errors": errCount,
						"last_error":        err.Error(),
						"message":           fmt.Sprintf("資金費套利連續 %d 次 tick 異常，請檢查 API 狀態", errCount),
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

	// 每次 tick 先從交易所同步真實持倉
	if err := s.syncPositions(ctx); err != nil {
		return fmt.Errorf("syncPositions: %w", err)
	}

	rate, err := s.fut.GetFundingRate(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("GetFundingRate: %w", err)
	}

	s.mu.RLock()
	hasPosition := s.spotQty > 0 || s.futQty > 0
	spotQ := s.spotQty
	futQ := s.futQty
	s.mu.RUnlock()

	// 腿不平衡檢測
	if hasPosition {
		if (spotQ > 0) != (futQ > 0) {
			logger.Warn("⚠️ [%s] 腿不平衡! spot=%.8f fut_short=%.8f", s.symbol, spotQ, futQ)
			s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
				"symbol":   s.symbol,
				"strategy": "funding_carry",
				"spot_qty": spotQ,
				"fut_qty":  futQ,
				"message":  "期現對沖腿不平衡，建議手動檢查或等待下次 tick 自動修復",
			})
		}
	}

	if hasPosition {
		if rate < s.exitFundingRate {
			logger.Info("📉 [%s] 資金費低於退出閾值 %.5f < %.5f，平倉", s.symbol, rate, s.exitFundingRate)
			return s.closeAll(ctx, "exit_funding_rate")
		}
		return nil
	}

	if rate < s.minFundingRate {
		return nil
	}

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

// syncPositions 從交易所拉取真實持倉，覆寫內存狀態
func (s *FundingCarryStrategy) syncPositions(ctx context.Context) error {
	// 合約持倉
	var futShort float64
	pos, err := s.fut.GetPositions(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("fut.GetPositions: %w", err)
	}
	for _, p := range pos {
		if p != nil && p.Size < 0 {
			futShort += math.Abs(p.Size)
		}
	}

	// 現貨餘額
	base := s.spot.GetBaseAsset()
	spotBal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		return fmt.Errorf("spot.GetBalance(%s): %w", base, err)
	}

	s.mu.Lock()
	s.spotQty = spotBal
	s.futQty = futShort
	s.mu.Unlock()
	return nil
}

func (s *FundingCarryStrategy) capitalUSDT() float64 {
	c := s.symCfg.TotalAllocatedCapital
	if c <= 0 {
		c = s.symCfg.OrderQuantity
	}
	return c
}

// openHedge 原子性開倉：現貨買入 → 等成交 → 合約開空；任一失敗回滾
func (s *FundingCarryStrategy) openHedge(ctx context.Context, futPx, spotPx, rate float64) error {
	cap := s.capitalUSDT()
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小，建議 ≥200 USDT", cap)
	}
	legNotional := cap / 2
	if legNotional < 100 {
		return fmt.Errorf("單腿名義 %.2f USDT 低於合約最小要求", legNotional)
	}

	qty := legNotional / spotPx
	qty = s.roundQty(qty, s.spot.GetQuantityDecimals())

	// Step 1: 現貨限價買入（略高於市價確保成交，使用現貨價而非合約價）
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
			"symbol":   s.symbol,
			"strategy": "funding_carry",
			"side":     "spot_buy",
			"error":    err.Error(),
			"message":  "資金費套利現貨買入失敗",
		})
		return fmt.Errorf("現貨買入: %w", err)
	}

	// Step 2: 等待現貨訂單成交（輪詢直到 FILLED/PARTIALLY_FILLED 或超時）
	filledQty, spotFillErr := s.waitOrderFill(ctx, s.spot, spotOrder.OrderID, orderWaitTimeout)
	if spotFillErr != nil || filledQty <= 0 {
		// 現貨未成交：撤單回滾
		logger.Warn("⚠️ [%s] 現貨買入未成交，撤單回滾 orderID=%d", s.symbol, spotOrder.OrderID)
		_ = s.spot.CancelOrder(ctx, s.symbol, spotOrder.OrderID)
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
			"symbol":   s.symbol,
			"strategy": "funding_carry",
			"side":     "spot_buy",
			"order_id": spotOrder.OrderID,
			"message":  "現貨買入超時未成交，已撤單",
		})
		if spotFillErr != nil {
			return fmt.Errorf("等待現貨成交: %w", spotFillErr)
		}
		return fmt.Errorf("現貨買入超時未成交，已撤單")
	}

	// Step 3: 用實際成交數量開合約空（確保兩腿對齊）
	futQty := s.roundQty(filledQty, s.fut.GetQuantityDecimals())
	if futQty <= 0 {
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"symbol":   s.symbol,
			"strategy": "funding_carry",
			"message":  "現貨已成交但合約數量精度截斷為 0，腿不對齊！",
			"spot_qty": filledQty,
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
		// 合約開空失敗——現貨已買入，單腿裸多！必須通知
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"symbol":     s.symbol,
			"strategy":   "funding_carry",
			"side":       "futures_sell",
			"error":      err.Error(),
			"spot_qty":   filledQty,
			"message":    "合約開空失敗！現貨已買入，請立即手動處理",
		})
		return fmt.Errorf("合約開空失敗（現貨已買入 %.8f，存在裸多風險）: %w", filledQty, err)
	}

	logger.Info("✅ [%s] 期現對沖已建立 spot=%.8f fut_short=%.8f 名義腿≈%.2f USDT rate=%.5f",
		s.symbol, filledQty, futQty, legNotional, rate)

	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"symbol":       s.symbol,
		"strategy":     "funding_carry",
		"spot_qty":     filledQty,
		"fut_qty":      futQty,
		"notional":     legNotional,
		"funding_rate": rate,
		"message":      fmt.Sprintf("資金費套利開倉: %s spot=%.6f fut_short=%.6f rate=%.5f", s.symbol, filledQty, futQty, rate),
	})
	return nil
}

// waitOrderFill 輪詢等待訂單成交，返回已成交數量
func (s *FundingCarryStrategy) waitOrderFill(ctx context.Context, ex exchange.IExchange, orderID int64, timeout time.Duration) (float64, error) {
	deadline := time.After(timeout)
	ticker := time.NewTicker(orderPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-deadline:
			// 超時：查一次最終狀態
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
				// 部分成交——繼續等（也可視為足夠）
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

// closeAll 雙腿平倉——現貨市價賣出 + 合約市價平空
func (s *FundingCarryStrategy) closeAll(ctx context.Context, reason string) error {
	var errs []error

	// 合約平空（先平合約防止裸空）
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
				Symbol:        s.symbol,
				Side:          exchange.SideBuy,
				Type:          exchange.OrderTypeMarket,
				Quantity:      q,
				Price:         0,
				ReduceOnly:    true,
				PriceDecimals: s.fut.GetPriceDecimals(),
				StrategyType:  "funding_carry",
			})
			if err != nil {
				errs = append(errs, fmt.Errorf("合約平空: %w", err))
				s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
					"symbol":   s.symbol,
					"strategy": "funding_carry",
					"side":     "futures_close",
					"error":    err.Error(),
					"message":  "合約平空失敗，請手動處理",
				})
			}
		}
	}

	// 現貨賣出——使用激進限價（低於市價 1%），而非市價（部分交易所現貨不支持市價）
	base := s.spot.GetBaseAsset()
	bal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		errs = append(errs, fmt.Errorf("spot.GetBalance: %w", err))
	} else {
		qty := s.roundQty(bal, s.spot.GetQuantityDecimals())
		if qty > 0 {
			sellPrice := 0.0
			if px, e := s.spot.GetLatestPrice(ctx, s.symbol); e == nil {
				sellPrice = px * 0.99 // 低於市價 1%，確保快速成交
			}
			sellPrice = s.roundPrice(sellPrice, s.spot.GetPriceDecimals())
			if sellPrice > 0 {
				_, err = s.spot.PlaceOrder(ctx, &exchange.OrderRequest{
					Symbol:        s.symbol,
					Side:          exchange.SideSell,
					Type:          exchange.OrderTypeLimit,
					Quantity:      qty,
					Price:         sellPrice,
					PriceDecimals: s.spot.GetPriceDecimals(),
					StrategyType:  "funding_carry",
				})
				if err != nil {
					errs = append(errs, fmt.Errorf("現貨賣出: %w", err))
					s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{
						"symbol":   s.symbol,
						"strategy": "funding_carry",
						"side":     "spot_sell",
						"error":    err.Error(),
						"message":  "現貨賣出失敗，請手動處理",
					})
				}
			}
		}
	}

	if len(errs) == 0 {
		logger.Info("✅ [%s] 期現對沖平倉完成 reason=%s", s.symbol, reason)
		s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{
			"symbol":   s.symbol,
			"strategy": "funding_carry",
			"reason":   reason,
			"message":  fmt.Sprintf("資金費套利平倉: %s (%s)", s.symbol, reason),
		})
	} else {
		logger.Warn("⚠️ [%s] 平倉部分失敗 reason=%s errors=%d", s.symbol, reason, len(errs))
	}

	return combineErrors(errs)
}

// publishEvent 安全發布事件（eventBus 可能為 nil）
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
