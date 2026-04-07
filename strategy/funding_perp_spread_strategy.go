package strategy

import (
	"context"
	"errors"
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

// FundingPerpSpreadStrategy 雙永续跨所資金費差：高費率所做空、低費率所做多，名義對齊
type FundingPerpSpreadStrategy struct {
	name   string
	cfg    *config.Config
	symCfg config.SymbolConfig
	fp     *config.FundingPerpSpreadConfig
	legA   exchange.IExchange
	legB   exchange.IExchange
	symA   string
	symB   string

	minSpread  float64
	exitSpread float64
	maxBasis   float64
	tickInt    time.Duration

	mu sync.RWMutex
	ctx context.Context
	cancel context.CancelFunc
	eventBus EventBus

	consecutiveErrors int
}

// NewFundingPerpSpreadStrategy 建立策略
func NewFundingPerpSpreadStrategy(
	name string,
	cfg *config.Config,
	symCfg config.SymbolConfig,
	legA, legB exchange.IExchange,
	fp *config.FundingPerpSpreadConfig,
	stratCfg map[string]interface{},
) *FundingPerpSpreadStrategy {
	minS := 0.0001
	exitS := 0.00005
	maxB := 1.0
	intervalSec := 45
	if fp != nil {
		if fp.MinFundingSpread > 0 {
			minS = fp.MinFundingSpread
		}
		if fp.ExitFundingSpread > 0 {
			exitS = fp.ExitFundingSpread
		}
		if fp.MaxBasisPct > 0 {
			maxB = fp.MaxBasisPct
		}
	}
	if stratCfg != nil {
		if v, ok := stratCfg["min_funding_spread"].(float64); ok && v > 0 {
			minS = v
		}
		if v, ok := stratCfg["exit_funding_spread"].(float64); ok && v > 0 {
			exitS = v
		}
		if v, ok := stratCfg["max_basis_pct"].(float64); ok && v > 0 {
			maxB = v
		}
		if v, ok := stratCfg["rebalance_interval_sec"].(float64); ok && v >= 10 {
			intervalSec = int(v)
		}
	}

	return &FundingPerpSpreadStrategy{
		name:       name,
		cfg:        cfg,
		symCfg:     symCfg,
		fp:         fp,
		legA:       legA,
		legB:       legB,
		symA:       fp.LegA.Symbol,
		symB:       fp.LegB.Symbol,
		minSpread:  minS,
		exitSpread: exitS,
		maxBasis:   maxB,
		tickInt:    time.Duration(intervalSec) * time.Second,
	}
}

func (s *FundingPerpSpreadStrategy) Name() string { return s.name }

func (s *FundingPerpSpreadStrategy) Initialize(*config.Config, position.OrderExecutorInterface, position.IExchange) error {
	return nil
}

func (s *FundingPerpSpreadStrategy) SetEventBus(bus EventBus) { s.eventBus = bus }

func (s *FundingPerpSpreadStrategy) Start(ctx context.Context) error {
	s.ctx, s.cancel = context.WithCancel(ctx)
	go s.runLoop()
	return nil
}

func (s *FundingPerpSpreadStrategy) Stop() error {
	if s.cancel != nil {
		s.cancel()
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) OnPriceChange(float64) error { return nil }
func (s *FundingPerpSpreadStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }
func (s *FundingPerpSpreadStrategy) GetPositions() []*Position { return nil }
func (s *FundingPerpSpreadStrategy) GetOrders() []*Order { return nil }
func (s *FundingPerpSpreadStrategy) GetStatistics() *StrategyStatistics { return &StrategyStatistics{} }

func (s *FundingPerpSpreadStrategy) GetVisualizationData() map[string]interface{} {
	return map[string]interface{}{
		"type": "funding_perp_spread",
	}
}

func (s *FundingPerpSpreadStrategy) runLoop() {
	ticker := time.NewTicker(s.tickInt)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			if err := s.tick(); err != nil {
				s.mu.Lock()
				s.consecutiveErrors++
				n := s.consecutiveErrors
				s.mu.Unlock()
				logger.Warn("⚠️ [funding_perp_spread] tick error (%d): %v", n, err)
			} else {
				s.mu.Lock()
				s.consecutiveErrors = 0
				s.mu.Unlock()
			}
		}
	}
}

func (s *FundingPerpSpreadStrategy) tick() error {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	rA, err := s.legA.GetFundingRate(ctx, s.symA)
	if err != nil {
		return fmt.Errorf("legA GetFundingRate: %w", err)
	}
	rB, err := s.legB.GetFundingRate(ctx, s.symB)
	if err != nil {
		return fmt.Errorf("legB GetFundingRate: %w", err)
	}
	spread := math.Abs(rA - rB)
	if rA == rB {
		spread = 0
	}

	pxA, err := s.legA.GetLatestPrice(ctx, s.symA)
	if err != nil {
		return err
	}
	pxB, err := s.legB.GetLatestPrice(ctx, s.symB)
	if err != nil {
		return err
	}
	basisPct := math.Abs(pxA-pxB) / math.Max(1e-12, (pxA+pxB)/2) * 100

	posA, err := netFutSize(ctx, s.legA, s.symA)
	if err != nil {
		return err
	}
	posB, err := netFutSize(ctx, s.legB, s.symB)
	if err != nil {
		return err
	}
	hasPos := posA != 0 || posB != 0

	if hasPos && posA != 0 && posB != 0 && posA*posB > 0 {
		logger.Warn("⚠️ [funding_perp_spread] 兩腿同向 posA=%.8f posB=%.8f", posA, posB)
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "雙永续兩腿同向，請手動檢查", "pos_a": posA, "pos_b": posB,
		})
	}

	if hasPos && spread < s.exitSpread {
		logger.Info("📉 [funding_perp_spread] 價差 %.6f < 退出 %.6f，平倉", spread, s.exitSpread)
		return s.closeAll(ctx, "exit_spread")
	}

	if hasPos {
		return nil
	}

	if spread < s.minSpread {
		return nil
	}
	if basisPct > s.maxBasis {
		logger.Warn("⚠️ [funding_perp_spread] 基差 %.4f%% > 上限 %.4f%%，暫不開倉", basisPct, s.maxBasis)
		return nil
	}

	// 高費率所做空，低費率所做多
	var shortEx exchange.IExchange
	var shortSym string
	var longEx exchange.IExchange
	var longSym string
	if rA >= rB {
		shortEx, shortSym, longEx, longSym = s.legA, s.symA, s.legB, s.symB
	} else {
		shortEx, shortSym, longEx, longSym = s.legB, s.symB, s.legA, s.symA
	}

	return s.openSpread(ctx, shortEx, shortSym, longEx, longSym, pxA, pxB, rA, rB)
}

func netFutSize(ctx context.Context, ex exchange.IExchange, sym string) (float64, error) {
	pos, err := ex.GetPositions(ctx, sym)
	if err != nil {
		return 0, err
	}
	var sum float64
	for _, p := range pos {
		if p != nil {
			sum += p.Size
		}
	}
	return sum, nil
}

func (s *FundingPerpSpreadStrategy) capitalUSDT() float64 {
	c := s.symCfg.TotalAllocatedCapital
	if c <= 0 {
		c = s.symCfg.OrderQuantity
	}
	return c
}

func (s *FundingPerpSpreadStrategy) openSpread(ctx context.Context, shortEx exchange.IExchange, shortSym string, longEx exchange.IExchange, longSym string, pxA, pxB, rA, rB float64) error {
	cap := s.capitalUSDT()
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小，建議 ≥200", cap)
	}
	legNotional := cap / 2
	if legNotional < 50 {
		return fmt.Errorf("單腿名義 %.2f USDT 過小", legNotional)
	}
	refPx := (pxA + pxB) / 2
	if refPx <= 0 {
		return fmt.Errorf("參考價無效")
	}
	qty := legNotional / refPx
	qtyShort := roundPerpQty(qty, shortEx.GetQuantityDecimals())
	qtyLong := roundPerpQty(qty, longEx.GetQuantityDecimals())
	if qtyShort <= 0 || qtyLong <= 0 {
		return fmt.Errorf("數量精度截斷為 0")
	}

	_, err := shortEx.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol: shortSym, Side: exchange.SideSell, Type: exchange.OrderTypeMarket,
		Quantity: qtyShort, Price: 0, PriceDecimals: shortEx.GetPriceDecimals(),
		StrategyType: "funding_perp_spread",
	})
	if err != nil {
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{"leg": "short", "error": err.Error()})
		return fmt.Errorf("開空: %w", err)
	}
	_, err = longEx.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol: longSym, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket,
		Quantity: qtyLong, Price: 0, PriceDecimals: longEx.GetPriceDecimals(),
		StrategyType: "funding_perp_spread",
	})
	if err != nil {
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "開多失敗，空頭已成交，請手動處理", "error": err.Error(),
		})
		return fmt.Errorf("開多失敗（空頭已下單）: %w", err)
	}
	logger.Info("✅ [funding_perp_spread] 已建倉 short=%s qty=%.8f long=%s qty=%.8f rA=%.6f rB=%.6f",
		shortSym, qtyShort, longSym, qtyLong, rA, rB)
	s.publishEvent(event.EventTypePositionOpened, map[string]interface{}{
		"short_symbol": shortSym, "long_symbol": longSym,
		"r_a": rA, "r_b": rB, "spread": math.Abs(rA - rB),
	})
	return nil
}

func (s *FundingPerpSpreadStrategy) closeAll(ctx context.Context, reason string) error {
	var errs []error
	if err := s.closeLeg(ctx, s.legA, s.symA); err != nil {
		errs = append(errs, err)
	}
	if err := s.closeLeg(ctx, s.legB, s.symB); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		logger.Info("✅ [funding_perp_spread] 平倉完成 reason=%s", reason)
		s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{"reason": reason})
	}
	return errors.Join(errs...)
}

func (s *FundingPerpSpreadStrategy) closeLeg(ctx context.Context, ex exchange.IExchange, sym string) error {
	pos, err := ex.GetPositions(ctx, sym)
	if err != nil {
		return err
	}
	for _, p := range pos {
		if p == nil || p.Size == 0 {
			continue
		}
		q := math.Abs(p.Size)
		side := exchange.SideBuy
		if p.Size > 0 {
			side = exchange.SideSell
		}
		_, err = ex.PlaceOrder(ctx, &exchange.OrderRequest{
			Symbol: sym, Side: side, Type: exchange.OrderTypeMarket,
			Quantity: q, ReduceOnly: true, PriceDecimals: ex.GetPriceDecimals(),
			StrategyType: "funding_perp_spread",
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) publishEvent(typ event.EventType, data map[string]interface{}) {
	if s.eventBus == nil {
		return
	}
	s.eventBus.Publish(&event.Event{Type: typ, Data: data})
}

func roundPerpQty(q float64, decimals int) float64 {
	if decimals <= 0 {
		return math.Round(q)
	}
	p := math.Pow10(decimals)
	return math.Round(q*p) / p
}
