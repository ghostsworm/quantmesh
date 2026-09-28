package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"quantmesh/config"
	"strings"
)

const legacyManualCloseBlock = "legacy_manual_close"

// The legacy endpoint is account/symbol scoped, unlike a Bot-owned liquidation.
// Keep its temporary opening barrier separate from manual/risk pause owners.
// Keep the legacy endpoint only as a compatibility adapter: execution still
// has to pass through the sole Bot owner's executor and durable intent journal.
func (sm *SymbolManager) closeLegacyPositions(ctx context.Context, rt *SymbolRuntime) (int, int, error) {
	if sm == nil || sm.botManager == nil || rt == nil || rt.Exchange == nil || rt.AccountScope == "" {
		return 0, 0, fmt.Errorf("account identity and runtime required for verified manual close")
	}
	if !sm.legacyCloseMu.TryLock() {
		return 0, 0, fmt.Errorf("manual close already in progress")
	}
	defer sm.legacyCloseMu.Unlock()
	peers := []*SymbolRuntime{rt}
	key := shutdownRuntimeScopeKey(rt)
	for _, other := range sm.List() {
		if other != nil && other != rt && shutdownRuntimeScopeKey(other) == key {
			peers = append(peers, other)
		}
	}
	if len(peers) > 1 {
		return 0, len(peers), fmt.Errorf("account-level net position is shared by %d Bot runtimes; manual close cannot safely assign fills to strategy owners", len(peers))
	}
	for _, peer := range peers {
		if peer.SuperPositionManager == nil {
			return 0, 0, fmt.Errorf("manual close requires an initialized opening barrier")
		}
		if reason := peer.shutdownCloseUnverifiedReason(); reason != "" {
			return 0, 0, fmt.Errorf("previous account close remains unverified: %w", errShutdownCloseUnverified)
		}
	}
	for _, peer := range peers {
		peer.SuperPositionManager.OpeningGate().Block(legacyManualCloseBlock)
	}
	defer func() {
		for _, peer := range peers {
			peer.SuperPositionManager.OpeningGate().Unblock(legacyManualCloseBlock)
		}
	}()
	for _, peer := range peers {
		if err := peer.SuperPositionManager.OpeningGate().Drain(ctx); err != nil {
			return 0, 0, fmt.Errorf("manual close opening requests not drained: %w", err)
		}
	}
	if rt.ExchangeExecutor == nil {
		return 0, 0, fmt.Errorf("manual close requires the Bot-owned execution journal; direct account-level submission is disabled")
	}
	venuePositions, err := queryShutdownPositions(ctx, rt.Exchange, rt.Config.Symbol)
	if err != nil {
		return 0, 0, fmt.Errorf("manual close ownership preflight: %w", err)
	}
	var accountGross float64
	for _, p := range venuePositions {
		accountGross += math.Abs(p.Size)
	}
	ownedGross, _, _, _ := rt.SuperPositionManager.GetPositionExposure(1)
	tolerance := math.Pow10(-rt.Exchange.GetQuantityDecimals()) / 2
	if rt.Exchange.GetQuantityDecimals() < 0 || tolerance <= 0 || math.IsNaN(tolerance) {
		tolerance = 1e-8
	}
	if math.Abs(accountGross-ownedGross) > tolerance {
		return 0, 1, fmt.Errorf("manual close refused: exchange gross position %.12g does not match this Bot's owned inventory %.12g; reconcile account ownership first", accountGross, ownedGross)
	}
	botID := rt.Config.ID
	botConfig := config.SymbolConfigToBotConfig(rt.Config, false)
	if botID == "" {
		botID = config.BotIDOrGenerate(botConfig)
	}
	bot := &BotRuntime{BotID: botID, Config: botConfig, Inner: rt}
	if err := bot.CloseAllPositions(ctx, "manual", 20); err != nil {
		for _, peer := range peers {
			peer.ExchangeExecutor.InvalidateExposure("manual owner-scoped liquidation requires reconciliation")
			peer.markShutdownCloseUnverified("Bot 所有权平仓未完成，需核实订单、策略槽位与账户持仓后再操作")
		}
		return 0, 1, errors.Join(errShutdownCloseUnverified, err)
	}
	remaining, err := queryShutdownPositions(ctx, rt.Exchange, rt.Config.Symbol)
	if err != nil || len(remaining) != 0 {
		if err == nil {
			err = fmt.Errorf("account still has %d unassigned position legs after Bot close", len(remaining))
		}
		return markManualCloseUnverified(peers, err)
	}
	openOrders, err := rt.Exchange.GetOpenOrders(ctx, rt.Config.Symbol)
	if err != nil {
		return markManualCloseUnverified(peers, fmt.Errorf("verify remaining open orders: %w", err))
	}
	for _, order := range openOrders {
		if order == nil || order.Symbol == "" || strings.EqualFold(order.Symbol, rt.Config.Symbol) {
			return markManualCloseUnverified(peers, fmt.Errorf("account still has an unverified open order after Bot close"))
		}
	}
	return 1, 0, nil
}

func markManualCloseUnverified(peers []*SymbolRuntime, cause error) (int, int, error) {
	for _, peer := range peers {
		peer.ExchangeExecutor.InvalidateExposure("manual owner-scoped liquidation requires reconciliation")
		peer.markShutdownCloseUnverified("Bot 所有权平仓结果未核实，需核对账户持仓、活动委托与成交账本")
	}
	return 0, 1, errors.Join(errShutdownCloseUnverified, cause)
}
