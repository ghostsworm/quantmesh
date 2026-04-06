package strategy

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/config"
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

	minFundingRate float64
	exitFundingRate float64
	maxBasisPct  float64
	tickInterval time.Duration

	mu       sync.RWMutex
	ctx      context.Context
	cancel   context.CancelFunc
	eventBus EventBus

	positionOpen bool
}

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
		maxBasisPct:  maxBasis,
		tickInterval: time.Duration(intervalSec) * time.Second,
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

func (s *FundingCarryStrategy) Stop() error {
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
	open := s.positionOpen
	s.mu.RUnlock()
	return map[string]interface{}{
		"type":            "funding_carry",
		"position_open": open,
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
				logger.Warn("⚠️ [%s] funding_carry tick: %v", s.symbol, err)
			}
		}
	}
}

func (s *FundingCarryStrategy) tick() error {
	ctx, cancel := context.WithTimeout(s.ctx, 60*time.Second)
	defer cancel()

	rate, err := s.fut.GetFundingRate(ctx, s.symbol)
	if err != nil {
		return fmt.Errorf("GetFundingRate: %w", err)
	}

	s.mu.Lock()
	open := s.positionOpen
	s.mu.Unlock()

	if open {
		if rate < s.exitFundingRate {
			logger.Info("📉 [%s] 資金費低於退出閾值 %.5f < %.5f，平倉", s.symbol, rate, s.exitFundingRate)
			return s.closeAll(ctx)
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

	return s.openHedge(ctx, futPx)
}

func (s *FundingCarryStrategy) capitalUSDT() float64 {
	c := s.symCfg.TotalAllocatedCapital
	if c <= 0 {
		c = s.symCfg.OrderQuantity
	}
	return c
}

func (s *FundingCarryStrategy) openHedge(ctx context.Context, refPrice float64) error {
	cap := s.capitalUSDT()
	if cap < 200 {
		return fmt.Errorf("分配資金 %.2f USDT 過小，合約單邊最小名義通常需 ≥100 USDT，建議 ≥200 USDT", cap)
	}
	legNotional := cap / 2
	if legNotional < 100 {
		return fmt.Errorf("單腿名義 %.2f USDT 低於合約最小要求（約 100 USDT）", legNotional)
	}

	qty := legNotional / refPrice
	qty = s.futRoundQty(qty)

	// 現貨限價買入（略高於市價）
	buyPrice := refPrice * 1.003
	_, err := s.spot.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideBuy,
		Type:          exchange.OrderTypeLimit,
		Quantity:      qty,
		Price:         buyPrice,
		PriceDecimals: s.spot.GetPriceDecimals(),
		StrategyType:  "funding_carry",
	})
	if err != nil {
		return fmt.Errorf("現貨買入: %w", err)
	}

	// 合約開空
	_, err = s.fut.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol:        s.symbol,
		Side:          exchange.SideSell,
		Type:          exchange.OrderTypeMarket,
		Quantity:      qty,
		Price:         0,
		PriceDecimals: s.fut.GetPriceDecimals(),
		StrategyType:  "funding_carry",
	})
	if err != nil {
		return fmt.Errorf("合約開空: %w", err)
	}

	s.mu.Lock()
	s.positionOpen = true
	s.mu.Unlock()
	logger.Info("✅ [%s] 已建立期現對沖 數量≈%.8f 名義腿≈%.2f USDT", s.symbol, qty, legNotional)
	return nil
}

func (s *FundingCarryStrategy) closeAll(ctx context.Context) error {
	base := s.spot.GetBaseAsset()
	bal, err := s.spot.GetBalance(ctx, base)
	if err != nil {
		return fmt.Errorf("現貨餘額: %w", err)
	}
	qty := s.futRoundQty(bal)
	if qty > 0 {
		sellPrice := 0.0
		if px, e := s.spot.GetLatestPrice(ctx, s.symbol); e == nil {
			sellPrice = px * 0.997
		}
		_, err = s.spot.PlaceOrder(ctx, &exchange.OrderRequest{
			Symbol:        s.symbol,
			Side:          exchange.SideSell,
			Type:          exchange.OrderTypeLimit,
			Quantity:      qty,
			Price:         sellPrice,
			PriceDecimals: s.spot.GetPriceDecimals(),
			ReduceOnly:    false,
			StrategyType:  "funding_carry",
		})
		if err != nil {
			logger.Warn("⚠️ [%s] 現貨賣出平倉: %v", s.symbol, err)
		}
	}

	pos, err := s.fut.GetPositions(ctx, s.symbol)
	if err == nil && len(pos) > 0 {
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
				logger.Warn("⚠️ [%s] 合約平空: %v", s.symbol, err)
			}
		}
	}

	s.mu.Lock()
	s.positionOpen = false
	s.mu.Unlock()
	return nil
}

func (s *FundingCarryStrategy) futRoundQty(q float64) float64 {
	step := math.Pow10(-s.fut.GetQuantityDecimals())
	if step <= 0 {
		return q
	}
	return math.Floor(q/step) * step
}
