package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/utils"
)

const signalRuntimeStateSchemaVersion = 1

type signalRuntimeState struct {
	BotID         string             `json:"bot_id"`
	StrategyName  string             `json:"strategy_name"`
	Symbol        string             `json:"symbol"`
	Position      *Position          `json:"position,omitempty"`
	EntryPrice    float64            `json:"entry_price"`
	ActiveOrder   *Order             `json:"active_order,omitempty"`
	OrderAlias    string             `json:"order_alias,omitempty"`
	PendingAction string             `json:"pending_action,omitempty"`
	Statistics    StrategyStatistics `json:"statistics"`
	IsPaused      bool               `json:"is_paused"`
}

func signalStrategyBotID(cfg *config.Config, exchange position.IExchange, symbol string) string {
	if cfg != nil && strings.TrimSpace(cfg.Trading.BotID) != "" {
		return strings.TrimSpace(cfg.Trading.BotID)
	}
	exchangeName := "binance"
	if exchange != nil && strings.TrimSpace(exchange.GetName()) != "" {
		exchangeName = strings.ToLower(strings.TrimSpace(exchange.GetName()))
	}
	marketType := "futures"
	if cfg != nil && strings.TrimSpace(cfg.Trading.MarketType) != "" {
		marketType = strings.ToLower(strings.TrimSpace(cfg.Trading.MarketType))
	}
	return config.GenerateBotID(exchangeName, symbol, marketType)
}

func saveSignalRuntimeState(store RuntimeStateStore, cfg *config.Config, exchange position.IExchange, strategyName, symbol string,
	holding *Position, entryPrice float64, active *Order, action string, stats *StrategyStatistics, paused bool) error {
	if store == nil {
		return nil
	}
	state := signalRuntimeState{
		BotID: signalStrategyBotID(cfg, exchange, symbol), StrategyName: strategyName, Symbol: symbol,
		EntryPrice: entryPrice, PendingAction: action, IsPaused: paused,
	}
	if holding != nil {
		copyHolding := *holding
		state.Position = &copyHolding
	}
	if active != nil {
		copyOrder := *active
		state.ActiveOrder = &copyOrder
		state.OrderAlias = active.clientOrderAlias
	}
	if stats != nil {
		state.Statistics = *stats
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode signal strategy state %s: %w", strategyName, err)
	}
	if err := store.SaveRuntimeState(strategyName, signalRuntimeStateSchemaVersion, string(payload)); err != nil {
		return fmt.Errorf("persist signal strategy state %s: %w", strategyName, err)
	}
	return nil
}

func loadSignalRuntimeState(store RuntimeStateStore, cfg *config.Config, exchange position.IExchange, strategyName, symbol string) (*signalRuntimeState, bool, error) {
	if store == nil {
		return nil, false, nil
	}
	version, payload, found, err := store.LoadRuntimeState(strategyName)
	if err != nil {
		return nil, false, fmt.Errorf("load signal strategy state %s: %w", strategyName, err)
	}
	if !found {
		return nil, false, nil
	}
	if version != signalRuntimeStateSchemaVersion {
		return nil, false, fmt.Errorf("unsupported signal strategy state schema version %d for %s", version, strategyName)
	}
	var state signalRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return nil, false, fmt.Errorf("decode signal strategy state %s: %w", strategyName, err)
	}
	if state.BotID != signalStrategyBotID(cfg, exchange, symbol) || state.StrategyName != strategyName || state.Symbol != symbol {
		return nil, false, fmt.Errorf("signal strategy state identity mismatch for %s", strategyName)
	}
	if !signalFinite(state.EntryPrice) || !signalFinite(state.Statistics.TotalPnL) || !signalFinite(state.Statistics.TotalVolume) ||
		!signalFinite(state.Statistics.WinRate) || state.Statistics.WinRate < 0 || state.Statistics.WinRate > 1 {
		return nil, false, fmt.Errorf("signal strategy state contains invalid statistics")
	}
	if state.Position != nil {
		p := state.Position
		if p.Symbol != symbol || p.Size <= 0 || !signalFinite(p.Size) || p.EntryPrice <= 0 || !signalFinite(p.EntryPrice) ||
			p.OpeningFee < 0 || !signalFinite(p.OpeningFee) || !signalFinite(p.CurrentPrice) || !signalFinite(p.PnL) ||
			state.EntryPrice <= 0 || !signalFinite(state.EntryPrice) || absSignal(state.EntryPrice-p.EntryPrice) > maxSignal(1e-8, p.EntryPrice*1e-8) {
			return nil, false, fmt.Errorf("signal strategy state contains invalid inventory")
		}
	} else if state.EntryPrice != 0 {
		return nil, false, fmt.Errorf("signal strategy state has entry price without inventory")
	}
	if state.ActiveOrder == nil {
		if state.PendingAction != "" || state.OrderAlias != "" {
			return nil, false, fmt.Errorf("signal strategy state has orphaned pending order metadata")
		}
	} else {
		o := state.ActiveOrder
		if state.PendingAction != signalActionOpenLong && state.PendingAction != signalActionCloseLong {
			return nil, false, fmt.Errorf("signal strategy state has invalid pending action")
		}
		if o.Symbol != symbol || o.Quantity <= 0 || o.Price <= 0 || !signalFinite(o.Quantity) || !signalFinite(o.Price) ||
			o.FillProgress.Quantity < 0 || o.FillProgress.Notional < 0 || !signalFinite(o.FillProgress.Quantity) || !signalFinite(o.FillProgress.Notional) ||
			o.FillProgress.Quantity > o.Quantity+entryQtyEpsilon || o.FeeVerifiedQty < 0 || !signalFinite(o.FeeVerifiedQty) ||
			o.FeeProgress < 0 || !signalFinite(o.FeeProgress) ||
			math.Abs(o.FeeVerifiedQty-o.FillProgress.Quantity) > entryQtyEpsilon {
			return nil, false, fmt.Errorf("signal strategy state contains invalid active order")
		}
		if state.PendingAction == signalActionCloseLong && state.Position == nil {
			return nil, false, fmt.Errorf("signal close intent has no persisted inventory")
		}
		o.clientOrderAlias = state.OrderAlias
	}
	return &state, true, nil
}

func absSignal(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

func maxSignal(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func updateSignalPositionMark(holding *Position, price float64) error {
	if !signalFinite(price) || price <= 0 {
		return fmt.Errorf("signal strategy received invalid mark price %v", price)
	}
	if holding != nil {
		holding.CurrentPrice = price
		holding.PnL = (price-holding.EntryPrice)*holding.Size - holding.OpeningFee
	}
	return nil
}

func reconcileSignalRuntimeOrder(ctx context.Context, ex position.IExchange, symbol string, active *Order,
	apply func(*position.OrderUpdate) error) error {
	if active == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ex == nil || active.OrderID <= 0 || active.Quantity <= 0 {
		return fmt.Errorf("active signal order lacks an exchange, order ID, or requested quantity")
	}
	raw, err := ex.GetOrder(ctx, symbol, active.OrderID)
	if err != nil {
		return fmt.Errorf("query active signal order %d: %w", active.OrderID, err)
	}
	order, ok := dcaExchangeOrder(raw)
	if !ok || order == nil {
		return fmt.Errorf("exchange returned unsupported or missing signal order evidence (%T)", raw)
	}
	wantSide := active.Side
	if order.ClientOrderID != "" && active.ClientOrderID != "" && order.ClientOrderID != active.ClientOrderID &&
		order.ClientOrderID != active.clientOrderAlias && utils.RemoveBrokerPrefix(strings.ToLower(ex.GetName()), order.ClientOrderID) != active.ClientOrderID {
		return fmt.Errorf("exchange signal order client identity conflicts with persisted state")
	}
	if order.OrderID != active.OrderID || !strings.EqualFold(order.Symbol, symbol) || !strings.EqualFold(string(order.Side), wantSide) ||
		!finiteNumber(order.Quantity) || math.Abs(order.Quantity-active.Quantity) > math.Max(entryQtyEpsilon, active.Quantity*1e-8) ||
		!finiteNumber(order.ExecutedQty) || order.ExecutedQty < 0 || order.ExecutedQty > active.Quantity+entryQtyEpsilon ||
		order.ExecutedQty+entryQtyEpsilon < active.FillProgress.Quantity {
		return fmt.Errorf("exchange signal order identity or quantity conflicts with persisted state")
	}
	status := strings.ToUpper(strings.TrimSpace(string(order.Status)))
	switch status {
	case "NEW", "PARTIALLY_FILLED", "FILLED", "FULLY_FILLED", "CLOSED", "CANCELED", "CANCELLED", "REJECTED", "EXPIRED", "FAILED":
	default:
		return fmt.Errorf("exchange signal order has unrecognized status %q", order.Status)
	}
	if status == "NEW" && order.ExecutedQty > entryQtyEpsilon || status == "PARTIALLY_FILLED" && order.ExecutedQty <= 0 ||
		(signalOrderStatusFilled(status) && order.ExecutedQty <= 0) {
		return fmt.Errorf("exchange signal order status conflicts with cumulative execution")
	}
	update := &position.OrderUpdate{OrderID: order.OrderID, ClientOrderID: active.ClientOrderID,
		Symbol: order.Symbol, Side: string(order.Side), Status: status, ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice}
	if order.ExecutedQty > 0 || active.FeeVerifiedQty > 0 {
		fee, avgPrice, err := reconcileSignalFills(ctx, ex, symbol, active, order)
		if err != nil {
			return err
		}
		update.Commission, update.CommissionAsset, update.AvgPrice = fee, ex.GetQuoteAsset(), avgPrice
	}
	if err := apply(update); err != nil {
		return fmt.Errorf("apply reconciled signal order %d: %w", order.OrderID, err)
	}
	return nil
}

func reconcileSignalFills(ctx context.Context, ex position.IExchange, symbol string, active *Order, order *exchange.Order) (float64, float64, error) {
	raw, err := ex.GetOrderFills(ctx, symbol, order.OrderID)
	if err != nil {
		return 0, 0, fmt.Errorf("query signal order fills: %w", err)
	}
	fills, ok := dcaExchangeOrderFills(raw)
	if !ok || len(fills) == 0 {
		return 0, 0, fmt.Errorf("signal order execution has no supported fill evidence")
	}
	sort.Slice(fills, func(i, j int) bool {
		if fills[i] == nil {
			return false
		}
		if fills[j] == nil {
			return true
		}
		if fills[i].TradeTime != fills[j].TradeTime {
			return fills[i].TradeTime < fills[j].TradeTime
		}
		return fills[i].TradeID < fills[j].TradeID
	})
	seen := make(map[string]struct{}, len(fills))
	var qty, notional, prefixQty, prefixNotional, prefixFee, addedFee float64
	for _, fill := range fills {
		if fill == nil || strings.TrimSpace(fill.TradeID) == "" || fill.OrderID != 0 && fill.OrderID != order.OrderID ||
			fill.Symbol != "" && !strings.EqualFold(fill.Symbol, symbol) || fill.Side != "" && !strings.EqualFold(string(fill.Side), active.Side) ||
			!finiteNumber(fill.Price) || fill.Price <= 0 || !finiteNumber(fill.Quantity) || fill.Quantity <= 0 ||
			!finiteNumber(fill.Commission) || fill.BaseFeeQty != 0 {
			return 0, 0, fmt.Errorf("signal order returned invalid or unsupported fill evidence")
		}
		if _, exists := seen[fill.TradeID]; exists {
			return 0, 0, fmt.Errorf("signal order returned duplicate trade ID %q", fill.TradeID)
		}
		seen[fill.TradeID] = struct{}{}
		fee, known := 0.0, false
		if fill.CommissionQuoteKnown {
			fee, known = fill.CommissionQuote, finiteNumber(fill.CommissionQuote) && fill.CommissionQuote >= 0
		} else if fill.Commission == 0 && strings.TrimSpace(fill.CommissionAsset) == "" {
			return 0, 0, fmt.Errorf("signal fill %s has no verifiable commission evidence", fill.TradeID)
		} else {
			fee, known = commissionInQuote(ex, fill.Commission, fill.CommissionAsset, fill.Price)
		}
		if !known {
			return 0, 0, fmt.Errorf("signal fill %s commission cannot be valued in quote asset", fill.TradeID)
		}
		qty += fill.Quantity
		notional += fill.Price * fill.Quantity
		if prefixQty < active.FillProgress.Quantity-entryQtyEpsilon {
			if prefixQty+fill.Quantity > active.FillProgress.Quantity+entryQtyEpsilon {
				return 0, 0, fmt.Errorf("persisted signal progress splits an exchange fill")
			}
			prefixQty += fill.Quantity
			prefixNotional += fill.Price * fill.Quantity
			prefixFee += fee
			continue
		}
		addedFee += fee
	}
	tol := math.Max(entryQtyEpsilon, order.ExecutedQty*1e-8)
	if math.Abs(qty-order.ExecutedQty) > tol || math.Abs(prefixQty-active.FillProgress.Quantity) > tol ||
		active.FillProgress.Quantity > 0 && math.Abs(prefixNotional-active.FillProgress.Notional) > math.Max(1e-8, active.FillProgress.Notional*1e-8) ||
		math.Abs(prefixFee-active.FeeProgress) > math.Max(1e-8, active.FeeProgress*1e-8) {
		return 0, 0, fmt.Errorf("signal order fill history does not reconcile with persisted quantity, notional, or fee cursor")
	}
	avg := notional / qty
	if !finiteNumber(avg) || avg <= 0 || !finiteNumber(order.AvgPrice) || order.AvgPrice > 0 && math.Abs(avg-order.AvgPrice) > math.Max(1e-8, order.AvgPrice*1e-8) {
		return 0, 0, fmt.Errorf("signal order fill notional does not match exchange order")
	}
	return addedFee, avg, nil
}

func (tfs *TrendFollowingStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	tfs.mu.Lock()
	defer tfs.mu.Unlock()
	tfs.runtimeStateStore = store
}

func (tfs *TrendFollowingStrategy) persistRuntimeStateLocked() error {
	err := saveSignalRuntimeState(tfs.runtimeStateStore, tfs.cfg, tfs.exchange, tfs.name,
		signalStrategySymbol(tfs.cfg, tfs.strategyCfg), tfs.position, tfs.entryPrice, tfs.activeOrder,
		tfs.pendingAction, tfs.stats, tfs.isPaused)
	tfs.runtimeStateErr = err
	return err
}

func (tfs *TrendFollowingStrategy) restoreRuntimeState() error {
	state, found, err := loadSignalRuntimeState(tfs.runtimeStateStore, tfs.cfg, tfs.exchange, tfs.name, signalStrategySymbol(tfs.cfg, tfs.strategyCfg))
	if err != nil || !found {
		return err
	}
	tfs.mu.Lock()
	defer tfs.mu.Unlock()
	tfs.position, tfs.entryPrice = state.Position, state.EntryPrice
	tfs.activeOrder, tfs.pendingAction = state.ActiveOrder, state.PendingAction
	tfs.stats, tfs.isPaused = &state.Statistics, state.IsPaused
	return nil
}

func (tfs *TrendFollowingStrategy) reconcileRuntimeOrder(ctx context.Context) error {
	tfs.mu.RLock()
	var active *Order
	if tfs.activeOrder != nil {
		copied := *tfs.activeOrder
		active = &copied
	}
	tfs.mu.RUnlock()
	return reconcileSignalRuntimeOrder(ctx, tfs.exchange, signalStrategySymbol(tfs.cfg, tfs.strategyCfg), active, tfs.OnOrderUpdate)
}

func (mrs *MeanReversionStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	mrs.mu.Lock()
	defer mrs.mu.Unlock()
	mrs.runtimeStateStore = store
}

func (mrs *MeanReversionStrategy) persistRuntimeStateLocked() error {
	err := saveSignalRuntimeState(mrs.runtimeStateStore, mrs.cfg, mrs.exchange, mrs.name,
		signalStrategySymbol(mrs.cfg, mrs.strategyCfg), mrs.position, mrs.entryPrice, mrs.activeOrder,
		mrs.pendingAction, mrs.stats, mrs.isPaused)
	mrs.runtimeStateErr = err
	return err
}

func (mrs *MeanReversionStrategy) restoreRuntimeState() error {
	state, found, err := loadSignalRuntimeState(mrs.runtimeStateStore, mrs.cfg, mrs.exchange, mrs.name, signalStrategySymbol(mrs.cfg, mrs.strategyCfg))
	if err != nil || !found {
		return err
	}
	mrs.mu.Lock()
	defer mrs.mu.Unlock()
	mrs.position, mrs.entryPrice = state.Position, state.EntryPrice
	mrs.activeOrder, mrs.pendingAction = state.ActiveOrder, state.PendingAction
	mrs.stats, mrs.isPaused = &state.Statistics, state.IsPaused
	return nil
}

func (mrs *MeanReversionStrategy) reconcileRuntimeOrder(ctx context.Context) error {
	mrs.mu.RLock()
	var active *Order
	if mrs.activeOrder != nil {
		copied := *mrs.activeOrder
		active = &copied
	}
	mrs.mu.RUnlock()
	return reconcileSignalRuntimeOrder(ctx, mrs.exchange, signalStrategySymbol(mrs.cfg, mrs.strategyCfg), active, mrs.OnOrderUpdate)
}

func (ms *MomentumStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.runtimeStateStore = store
}

func (ms *MomentumStrategy) persistRuntimeStateLocked() error {
	err := saveSignalRuntimeState(ms.runtimeStateStore, ms.cfg, ms.exchange, ms.name,
		signalStrategySymbol(ms.cfg, ms.strategyCfg), ms.position, ms.entryPrice, ms.activeOrder,
		ms.pendingAction, ms.stats, ms.isPaused)
	ms.runtimeStateErr = err
	return err
}

func (ms *MomentumStrategy) restoreRuntimeState() error {
	state, found, err := loadSignalRuntimeState(ms.runtimeStateStore, ms.cfg, ms.exchange, ms.name, signalStrategySymbol(ms.cfg, ms.strategyCfg))
	if err != nil || !found {
		return err
	}
	ms.mu.Lock()
	defer ms.mu.Unlock()
	ms.position, ms.entryPrice = state.Position, state.EntryPrice
	ms.activeOrder, ms.pendingAction = state.ActiveOrder, state.PendingAction
	ms.stats, ms.isPaused = &state.Statistics, state.IsPaused
	return nil
}

func (ms *MomentumStrategy) reconcileRuntimeOrder(ctx context.Context) error {
	ms.mu.RLock()
	var active *Order
	if ms.activeOrder != nil {
		copied := *ms.activeOrder
		active = &copied
	}
	ms.mu.RUnlock()
	return reconcileSignalRuntimeOrder(ctx, ms.exchange, signalStrategySymbol(ms.cfg, ms.strategyCfg), active, ms.OnOrderUpdate)
}

func signalRuntimeStateDecisionError(strategyName string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s runtime state is not durable; new decisions are paused: %w", strategyName, err)
}
