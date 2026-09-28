package strategy

import (
	"encoding/json"
	"fmt"
	"strings"

	"quantmesh/config"
	"quantmesh/position"
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
			o.FillProgress.Quantity > o.Quantity+entryQtyEpsilon {
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

func signalRuntimeStateDecisionError(strategyName string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s runtime state is not durable; new decisions are paused: %w", strategyName, err)
}
