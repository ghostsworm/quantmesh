package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

type reconciliationRuntimeVenue struct{ exchange.ISpotMarginExchange }

func (*reconciliationRuntimeVenue) GetName() string          { return "fake" }
func (*reconciliationRuntimeVenue) GetBaseAsset() string     { return "BTC" }
func (*reconciliationRuntimeVenue) GetQuoteAsset() string    { return "USDT" }
func (*reconciliationRuntimeVenue) GetQuantityDecimals() int { return 4 }
func (*reconciliationRuntimeVenue) GetPriceDecimals() int    { return 2 }

type reconciliationRuntimeStore struct {
	payload string
	writes  int
	onLoad  func()
}

func (s *reconciliationRuntimeStore) LoadRuntimeState(string) (int, string, bool, error) {
	if s.onLoad != nil {
		s.onLoad()
	}
	return 6, s.payload, true, nil
}
func (s *reconciliationRuntimeStore) SaveRuntimeState(_ string, _ int, payload string) error {
	s.payload, s.writes = payload, s.writes+1
	return nil
}

type reconciliationUnlockFailure struct{ lock.DistributedLock }

func (reconciliationUnlockFailure) Unlock(context.Context, string) error {
	return errors.New("injected wallet unlock failure")
}

func TestFundingCarryManagedStartupRetainsOnlyVerifiedReconciliation(t *testing.T) {
	for _, mode := range []string{"remaining", "invalid_ledger", "wrong_scope", "owner_lost", "cancelled", "cancelled_during_load", "unlock_failure", "gate_mismatch"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Strategies.Configs = map[string]config.StrategyConfig{"funding_carry": {Enabled: true, Type: "funding_carry", Weight: 1}}
			venue := &reconciliationRuntimeVenue{}
			fc := strategy.NewFundingCarryStrategy("funding_carry", cfg, config.SymbolConfig{Symbol: "BTCUSDT"}, venue, venue, venue, nil)
			state := map[string]interface{}{
				"strategy": "funding_carry", "futures_exchange": "fake", "spot_exchange": "fake", "symbol": "BTCUSDT", "margin_account_scope": "scope-a",
				"ownership_ready": true, "intent_in_flight": true, "exposure_unknown": true, "direction": 0,
				"margin_debt_events": []map[string]interface{}{
					{"action": "borrow", "transfer_id": 42, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": "scope-a", "occurred_at": "1970-01-01T00:00:01Z"},
					{"action": "repay", "transfer_id": 1, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": "scope-a", "occurred_at": "1970-01-01T00:00:02Z"},
				},
				"margin_cover_orders": []map[string]interface{}{{"order_id": 7, "asset": "BTC", "account_scope": "scope-a", "requested": 0.4008, "debt_to_cover": 0.4, "gross": 0.4008, "net": 0.4008, "verified": true, "repay_transfer_id": 1, "consumed": 0.4,
					"fills": []*exchange.OrderFill{{OrderID: 7, TradeID: "cover-7", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 50000, Quantity: 0.4008, CommissionAsset: "BTC", TradeTime: 1500}}}},
			}
			if mode == "invalid_ledger" {
				state["margin_debt"] = 1
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store := &reconciliationRuntimeStore{payload: string(payload)}
			fc.SetRuntimeStateStore(store)
			scope := "scope-a"
			if mode == "wrong_scope" {
				scope = "scope-other"
			}
			if err := fc.SetMarginAccountScope(scope); err != nil {
				t.Fatal(err)
			}
			var coordinator lock.DistributedLock = lock.NewNopLock()
			if mode == "unlock_failure" {
				coordinator = reconciliationUnlockFailure{coordinator}
			}
			if err := fc.SetAccountWalletCoordinationLock(coordinator, "funding_carry_wallet:"+t.Name()); err != nil {
				t.Fatal(err)
			}
			gate := &execution.OpeningGate{}
			gate.Block("manual_pause")
			fc.SetOpeningGate(gate)
			if mode == "owner_lost" {
				gate.Block("runtime_ownership_unverified")
			}
			if mode == "gate_mismatch" {
				fc.SetOpeningGate(&execution.OpeningGate{})
			}
			manager := strategy.NewStrategyManager(cfg, 100)
			manager.RegisterStrategy("funding_carry", fc, 1, 0)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			if mode == "cancelled_during_load" {
				store.onLoad = cancel
			}
			pending, err := startFundingCarryManagedStrategy(ctx, manager, fc, gate)
			if mode == "cancelled_during_load" && fc.GetVisualizationData()["margin_cover_remaining_qty"] != nil {
				t.Fatal("cancelled startup continued importing accounting under a detached manager context")
			}
			if mode == "remaining" {
				if err != nil || !pending || !gate.HasBlock(fundingCarryReconciliationBlock) || !gate.HasBlock("manual_pause") {
					t.Fatalf("verified accounting not retained under independent block: %v", err)
				}
				status := manager.GetStrategyStatus("funding_carry")
				registry := NewBotManager(cfg, nil, nil, nil, "")
				registry.AddRuntime(&BotRuntime{BotID: "bot-a", Inner: &SymbolRuntime{StrategyManager: manager}})
				registered, found := registry.Get("bot-a")
				if !found || registered == nil {
					t.Fatal("managed runtime missing from registry")
				}
				response := web.BotResponse{Running: true}
				attachFundingCarryRuntimeStatus(registered.Inner, &response)
				if !response.Running || response.FundingCarryRuntime == nil || response.FundingCarryRuntime.TradingRunning || !response.FundingCarryRuntime.ReconciliationRequired {
					t.Fatal("bot status confused registered accounting with trading or lost managed state")
				}
				if !status.IsEnabled || status.IsRunning || status.VisualizationData["margin_cover_remaining_qty"] != "0.0008" || status.VisualizationData["reconciliation_required"] != true {
					t.Fatal("managed strategy status hid accounting or falsely reported trading")
				}
				claims := &accountWalletCapitalReleaseSpy{}
				claim := storage.AccountWalletCapitalClaim{WalletKey: strings.Repeat("a", 64), Amount: 10, Available: 100}
				if err := verifyAndReleaseAccountWalletCapital(ctx, claims, "bot-a", []storage.AccountWalletCapitalClaim{claim}, fc.VerifyFlat); err == nil || claims.releases != 0 {
					t.Fatal("managed reconciliation released capital as flat")
				}
			} else if err == nil || pending || gate.HasBlock(fundingCarryReconciliationBlock) {
				t.Fatalf("ordinary startup failure admitted as managed recovery: %v", err)
			}
			if store.writes != 0 || store.payload != string(payload) || fc.IsRunning() {
				t.Fatal("managed admission rewrote evidence or started a loop")
			}
		})
	}
}
