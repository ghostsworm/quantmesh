package strategy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
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

	mu                sync.RWMutex
	ctx               context.Context
	cancel            context.CancelFunc
	runDone           chan struct{}
	started           bool
	stopMu            sync.Mutex
	stopped           bool
	stopErr           error
	ownershipReady    bool
	exposureUnknown   bool
	ownedA            float64
	ownedB            float64
	intentInFlight    bool
	runtimeStateStore RuntimeStateStore
	eventBus          EventBus

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

func (s *FundingPerpSpreadStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	s.runtimeStateStore = store
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) Start(ctx context.Context) error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("funding_perp_spread strategy already started")
	}
	s.started = true
	store := s.runtimeStateStore
	s.mu.Unlock()
	if store == nil {
		s.resetStartAfterFailure()
		return errors.New("funding_perp_spread requires durable runtime state storage")
	}
	checkCtx, checkCancel := context.WithTimeout(ctx, 15*time.Second)
	defer checkCancel()
	version, payload, found, err := store.LoadRuntimeState("funding_perp_spread")
	if err != nil {
		s.resetStartAfterFailure()
		return fmt.Errorf("load funding_perp_spread runtime state: %w", err)
	}
	var restored fundingPerpSpreadRuntimeState
	if found {
		restored, err = decodeFundingPerpSpreadRuntimeState(version, payload,
			s.legA.GetName(), s.symA, s.legB.GetName(), s.symB)
		if err != nil {
			s.resetStartAfterFailure()
			return err
		}
	}
	posA, err := s.readLegSnapshot(checkCtx, s.legA, s.symA)
	if err != nil {
		s.resetStartAfterFailure()
		return fmt.Errorf("legA position/order state cannot be safely reconciled: %w", err)
	}
	posB, err := s.readLegSnapshot(checkCtx, s.legB, s.symB)
	if err != nil {
		s.resetStartAfterFailure()
		return fmt.Errorf("legB position/order state cannot be safely reconciled: %w", err)
	}
	if found {
		if math.Abs(posA-restored.OwnedA) > s.legTolerance(s.legA) || math.Abs(posB-restored.OwnedB) > s.legTolerance(s.legB) {
			s.resetStartAfterFailure()
			return fmt.Errorf("runtime state ownership does not match exchange positions (A %.8f/%.8f, B %.8f/%.8f)", posA, restored.OwnedA, posB, restored.OwnedB)
		}
	} else if posA != 0 || posB != 0 {
		s.resetStartAfterFailure()
		return fmt.Errorf("unowned positions exist without a runtime state (A %.8f, B %.8f)", posA, posB)
	}
	s.mu.Lock()
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.runDone = make(chan struct{})
	s.ownershipReady = true
	s.exposureUnknown = false
	s.ownedA = 0
	s.ownedB = 0
	if found {
		s.ownedA, s.ownedB = restored.OwnedA, restored.OwnedB
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.cancel()
		s.ctx, s.cancel, s.runDone = nil, nil, nil
		s.started = false
		s.ownershipReady = false
		s.mu.Unlock()
		return fmt.Errorf("persist initial funding_perp_spread runtime state: %w", err)
	}
	go s.runLoop()
	s.mu.Unlock()
	return nil
}

func (s *FundingPerpSpreadStrategy) resetStartAfterFailure() {
	s.mu.Lock()
	s.started = false
	s.mu.Unlock()
}

func (s *FundingPerpSpreadStrategy) readLegSnapshot(ctx context.Context, ex exchange.IExchange, symbol string) (float64, error) {
	size, err := netFutSize(ctx, ex, symbol)
	if err != nil {
		return 0, fmt.Errorf("read positions: %w", err)
	}
	orders, err := ex.GetOpenOrders(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("read open orders: %w", err)
	}
	if len(orders) != 0 {
		return 0, fmt.Errorf("%d open order(s) exist", len(orders))
	}
	return size, nil
}

func (s *FundingPerpSpreadStrategy) Stop() error {
	s.stopMu.Lock()
	defer s.stopMu.Unlock()
	s.mu.RLock()
	cancel, runDone := s.cancel, s.runDone
	s.mu.RUnlock()
	if cancel == nil {
		return nil
	}
	cancel()
	if runDone != nil {
		select {
		case <-runDone:
		case <-time.After(20 * time.Second):
			return errors.New("funding_perp_spread run loop did not stop; positions left unchanged for safety")
		}
	}
	if s.stopped {
		return nil
	}
	ctx, stopClose := context.WithTimeout(context.Background(), 30*time.Second)
	defer stopClose()
	s.stopErr = s.closeAll(ctx, "strategy_stop")
	if s.stopErr == nil {
		s.stopped = true
	}
	return s.stopErr
}

func (s *FundingPerpSpreadStrategy) OnPriceChange(float64) error               { return nil }
func (s *FundingPerpSpreadStrategy) OnOrderUpdate(*position.OrderUpdate) error { return nil }
func (s *FundingPerpSpreadStrategy) GetPositions() []*Position                 { return nil }
func (s *FundingPerpSpreadStrategy) GetOrders() []*Order                       { return nil }
func (s *FundingPerpSpreadStrategy) GetStatistics() *StrategyStatistics        { return &StrategyStatistics{} }

func (s *FundingPerpSpreadStrategy) GetVisualizationData() map[string]interface{} {
	return map[string]interface{}{
		"type": "funding_perp_spread",
	}
}

func (s *FundingPerpSpreadStrategy) runLoop() {
	defer close(s.runDone)
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
	if err := s.verifyOwnedExposure(posA, posB); err != nil {
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
	var positive, negative bool
	for _, p := range pos {
		if p == nil {
			return 0, fmt.Errorf("position snapshot for %s contains a null entry", sym)
		}
		if math.IsNaN(p.Size) || math.IsInf(p.Size, 0) {
			return 0, fmt.Errorf("invalid position size for %s: %v", sym, p.Size)
		}
		positive = positive || p.Size > 0
		negative = negative || p.Size < 0
		sum += p.Size
	}
	if positive && negative {
		return 0, fmt.Errorf("both long and short positions exist for %s; net exposure cannot prove ownership", sym)
	}
	return sum, nil
}

func (s *FundingPerpSpreadStrategy) legTolerance(ex exchange.IExchange) float64 {
	decimals := ex.GetQuantityDecimals()
	if decimals < 0 {
		decimals = 0
	}
	return math.Pow10(-decimals) / 2
}

func (s *FundingPerpSpreadStrategy) exposureSnapshot(ctx context.Context) (float64, float64, error) {
	posA, err := s.readLegSnapshot(ctx, s.legA, s.symA)
	if err != nil {
		return 0, 0, fmt.Errorf("read legA exposure: %w", err)
	}
	posB, err := s.readLegSnapshot(ctx, s.legB, s.symB)
	if err != nil {
		return 0, 0, fmt.Errorf("read legB exposure: %w", err)
	}
	return posA, posB, nil
}

func (s *FundingPerpSpreadStrategy) verifyOwnedExposure(posA, posB float64) error {
	s.mu.Lock()
	if !s.ownershipReady || s.exposureUnknown || s.intentInFlight {
		s.mu.Unlock()
		return errors.New("funding_perp_spread ownership is not verified; trading is blocked")
	}
	validA := math.Abs(posA-s.ownedA) <= s.legTolerance(s.legA)
	validB := math.Abs(posB-s.ownedB) <= s.legTolerance(s.legB)
	if !validA || !validB {
		s.exposureUnknown = true
		ownedA, ownedB := s.ownedA, s.ownedB
		persistErr := s.persistRuntimeStateLocked()
		s.mu.Unlock()
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message":    "交易所實際敞口與本策略記賬不一致，已鎖定自動交易和平倉，需人工核對",
			"position_a": posA, "owned_a": ownedA,
			"position_b": posB, "owned_b": ownedB,
		})
		return errors.Join(fmt.Errorf("actual exposure differs from strategy ownership (legA %.8f/%.8f, legB %.8f/%.8f)", posA, ownedA, posB, ownedB), persistErr)
	}
	s.mu.Unlock()
	return nil
}

func (s *FundingPerpSpreadStrategy) recordOpenedLeg(ex exchange.IExchange, symbol string, actual, requested float64, side exchange.Side) error {
	if math.IsNaN(actual) || math.IsInf(actual, 0) || math.Abs(actual) > requested+s.legTolerance(ex) {
		s.mu.Lock()
		s.exposureUnknown = true
		s.mu.Unlock()
		return fmt.Errorf("opened exposure does not match requested leg size: actual=%.8f requested=%.8f", actual, requested)
	}
	if actual == 0 || (side == exchange.SideSell && actual > 0) || (side == exchange.SideBuy && actual < 0) {
		s.mu.Lock()
		s.exposureUnknown = true
		s.mu.Unlock()
		return fmt.Errorf("opened leg has no confirmed exposure in the requested direction: %.8f", actual)
	}
	s.mu.Lock()
	switch {
	case strings.EqualFold(ex.GetName(), s.legA.GetName()) && strings.EqualFold(symbol, s.symA):
		s.ownedA = actual
	case strings.EqualFold(ex.GetName(), s.legB.GetName()) && strings.EqualFold(symbol, s.symB):
		s.ownedB = actual
	default:
		s.exposureUnknown = true
		s.mu.Unlock()
		return errors.New("opened order does not match either configured leg")
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.exposureUnknown = true
		s.mu.Unlock()
		return fmt.Errorf("persist opened leg ownership: %w", err)
	}
	s.mu.Unlock()
	return nil
}

func (s *FundingPerpSpreadStrategy) beginOrderIntent() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ownershipReady || s.exposureUnknown || s.intentInFlight {
		return errors.New("funding_perp_spread execution state is unresolved; new order blocked")
	}
	s.intentInFlight = true
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.exposureUnknown = true
		return fmt.Errorf("persist order intent before exchange submission: %w", err)
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) finishOrderIntent() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intentInFlight = false
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.intentInFlight = true
		s.exposureUnknown = true
		return fmt.Errorf("persist reconciled order result: %w", err)
	}
	return nil
}

func (s *FundingPerpSpreadStrategy) markExposureUnknown() {
	s.mu.Lock()
	s.exposureUnknown = true
	_ = s.persistRuntimeStateLocked()
	s.mu.Unlock()
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
	currentA, err := s.readLegSnapshot(ctx, s.legA, s.symA)
	if err != nil {
		return fmt.Errorf("verify legA before opening: %w", err)
	}
	currentB, err := s.readLegSnapshot(ctx, s.legB, s.symB)
	if err != nil {
		return fmt.Errorf("verify legB before opening: %w", err)
	}
	if err := s.verifyOwnedExposure(currentA, currentB); err != nil {
		return fmt.Errorf("refuse new spread: %w", err)
	}
	if err := s.beginOrderIntent(); err != nil {
		return err
	}

	_, err = shortEx.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol: shortSym, Side: exchange.SideSell, Type: exchange.OrderTypeMarket,
		Quantity: qtyShort, Price: 0, PriceDecimals: shortEx.GetPriceDecimals(),
		StrategyType: "funding_perp_spread",
	})
	shortActual, readErr := s.readLegSnapshot(ctx, shortEx, shortSym)
	if readErr != nil {
		s.markExposureUnknown()
		return fmt.Errorf("open-short result cannot be reconciled: %w", readErr)
	}
	if shortActual != 0 {
		if captureErr := s.recordOpenedLeg(shortEx, shortSym, shortActual, qtyShort, exchange.SideSell); captureErr != nil {
			return fmt.Errorf("open short ownership unresolved: %w", captureErr)
		}
	}
	if err != nil {
		s.markExposureUnknown()
		s.publishEvent(event.EventTypeOrderFailed, map[string]interface{}{"leg": "short", "error": err.Error()})
		return fmt.Errorf("開空: %w", err)
	}
	if shortActual == 0 {
		s.markExposureUnknown()
		return errors.New("short order returned success but no position change was confirmed")
	}
	if err := s.finishOrderIntent(); err != nil {
		return err
	}
	if err := s.beginOrderIntent(); err != nil {
		return fmt.Errorf("short leg is open but long-leg intent could not be persisted: %w", err)
	}
	_, err = longEx.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol: longSym, Side: exchange.SideBuy, Type: exchange.OrderTypeMarket,
		Quantity: qtyLong, Price: 0, PriceDecimals: longEx.GetPriceDecimals(),
		StrategyType: "funding_perp_spread",
	})
	longActual, readErr := s.readLegSnapshot(ctx, longEx, longSym)
	if readErr != nil {
		s.markExposureUnknown()
		return fmt.Errorf("open-long result cannot be reconciled; short leg may remain open: %w", readErr)
	}
	if longActual != 0 {
		if captureErr := s.recordOpenedLeg(longEx, longSym, longActual, qtyLong, exchange.SideBuy); captureErr != nil {
			return fmt.Errorf("open long ownership unresolved; short leg may remain open: %w", captureErr)
		}
	}
	if err != nil {
		s.markExposureUnknown()
		s.publishEvent(event.EventTypeRiskTriggered, map[string]interface{}{
			"message": "開多失敗，空頭已成交，請手動處理", "error": err.Error(),
		})
		return fmt.Errorf("開多失敗（空頭已下單）: %w", err)
	}
	if longActual == 0 {
		s.markExposureUnknown()
		return errors.New("long order returned success but no position change was confirmed; short leg may remain open")
	}
	if err := s.finishOrderIntent(); err != nil {
		return fmt.Errorf("both leg positions are open but result-state persistence failed: %w", err)
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
	posA, posB, err := s.exposureSnapshot(ctx)
	if err != nil {
		return err
	}
	if err := s.verifyOwnedExposure(posA, posB); err != nil {
		return err
	}
	s.mu.RLock()
	ownedA, ownedB := s.ownedA, s.ownedB
	s.mu.RUnlock()
	if err := s.closeLeg(ctx, s.legA, s.symA, ownedA); err != nil {
		return err
	}
	if err := s.closeLeg(ctx, s.legB, s.symB, ownedB); err != nil {
		return err
	}
	logger.Info("✅ [funding_perp_spread] 平倉完成 reason=%s", reason)
	s.publishEvent(event.EventTypePositionClosed, map[string]interface{}{"reason": reason})
	return nil
}

func (s *FundingPerpSpreadStrategy) closeLeg(ctx context.Context, ex exchange.IExchange, sym string, owned float64) error {
	actual, err := s.readLegSnapshot(ctx, ex, sym)
	if err != nil {
		return fmt.Errorf("read %s position before close: %w", sym, err)
	}
	if math.Abs(actual-owned) > s.legTolerance(ex) {
		return fmt.Errorf("refusing to close %s: exchange exposure %.8f differs from strategy-owned %.8f", sym, actual, owned)
	}
	if owned == 0 {
		return nil
	}
	if err := s.beginOrderIntent(); err != nil {
		return err
	}
	side := exchange.SideBuy
	if owned > 0 {
		side = exchange.SideSell
	}
	_, orderErr := ex.PlaceOrder(ctx, &exchange.OrderRequest{
		Symbol: sym, Side: side, Type: exchange.OrderTypeMarket,
		Quantity: math.Abs(owned), ReduceOnly: true, PriceDecimals: ex.GetPriceDecimals(),
		StrategyType: "funding_perp_spread",
	})
	after, readErr := netFutSize(ctx, ex, sym)
	if readErr != nil {
		s.markExposureUnknown()
		return fmt.Errorf("close %s result cannot be reconciled: %w", sym, readErr)
	}
	orders, ordersErr := ex.GetOpenOrders(ctx, sym)
	if ordersErr != nil || len(orders) > 0 {
		s.markExposureUnknown()
		return fmt.Errorf("close %s remains unverified because open orders may remain (count=%d, error=%v)", sym, len(orders), ordersErr)
	}
	if math.Abs(after) <= s.legTolerance(ex) {
		if err := s.setOwnedLeg(ex, sym, 0); err != nil {
			s.markExposureUnknown()
			return err
		}
		if err := s.finishOrderIntent(); err != nil {
			return err
		}
		return nil
	}
	if err := s.setOwnedLeg(ex, sym, after); err != nil {
		s.markExposureUnknown()
		return err
	}
	if orderErr != nil {
		s.markExposureUnknown()
		return fmt.Errorf("close %s result remains uncertain at exposure %.8f (order error: %v)", sym, after, orderErr)
	}
	if err := s.finishOrderIntent(); err != nil {
		return err
	}
	if math.Abs(after) > s.legTolerance(ex) {
		return fmt.Errorf("close %s left residual exposure %.8f", sym, after)
	}
	return fmt.Errorf("close %s was acknowledged but exposure remains %.8f", sym, after)
}

func (s *FundingPerpSpreadStrategy) setOwnedLeg(ex exchange.IExchange, symbol string, size float64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if strings.EqualFold(ex.GetName(), s.legA.GetName()) && strings.EqualFold(symbol, s.symA) {
		s.ownedA = size
	} else if strings.EqualFold(ex.GetName(), s.legB.GetName()) && strings.EqualFold(symbol, s.symB) {
		s.ownedB = size
	} else {
		s.exposureUnknown = true
		return errors.New("cannot assign closed position to a configured strategy leg")
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.exposureUnknown = true
		return fmt.Errorf("persist updated owned position: %w", err)
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
