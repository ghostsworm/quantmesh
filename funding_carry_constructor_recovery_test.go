package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

type constructorRecoveryVenue struct {
	exchange.ISpotMarginExchange
	market    string
	mutations atomic.Int32
	streamCtx context.Context
	stops     atomic.Int32
}

func TestFundingCarryRetentionCannotAdmitReconciliation(t *testing.T) {
	pending := &strategy.FundingCarryReconciliationRequiredError{}
	retained := &fundingCarryStartupRetentionError{Cause: pending}
	for _, err := range []error{retained, fmt.Errorf("startup: %w", retained), errors.Join(pending, retained)} {
		if !errors.Is(err, pending) {
			t.Fatal("original cause no longer traceable")
		}
		if fundingCarryReconciliationOnlyError(err) {
			t.Fatal("retained cleanup failure admitted as reconciliation-only")
		}
	}
}

func (*constructorRecoveryVenue) GetName() string          { return "binance" }
func (v *constructorRecoveryVenue) GetMarketType() string  { return v.market }
func (*constructorRecoveryVenue) GetBaseAsset() string     { return "BTC" }
func (*constructorRecoveryVenue) GetQuoteAsset() string    { return "USDT" }
func (*constructorRecoveryVenue) GetQuantityDecimals() int { return 4 }
func (*constructorRecoveryVenue) GetPriceDecimals() int    { return 2 }
func (*constructorRecoveryVenue) GetAccount(context.Context) (*exchange.Account, error) {
	return &exchange.Account{BalanceAsset: "USDT", TotalWalletBalance: 1000, TotalMarginBalance: 1000, AvailableBalance: 1000}, nil
}
func (v *constructorRecoveryVenue) StartPriceStream(ctx context.Context, _ string, callback func(float64)) error {
	v.streamCtx = ctx
	callback(50000)
	return nil
}
func (v *constructorRecoveryVenue) StopOrderStream() error           { v.stops.Add(1); return nil }
func (*constructorRecoveryVenue) SupportsFundingIncomeHistory() bool { return false }
func (*constructorRecoveryVenue) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return []*exchange.Order{}, nil
}
func (*constructorRecoveryVenue) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return []*exchange.Position{}, nil
}
func (v *constructorRecoveryVenue) PlaceOrder(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
	v.mutations.Add(1)
	return nil, errors.New("fixture rejects financial RPC")
}
func (v *constructorRecoveryVenue) CancelOrder(context.Context, string, int64) error {
	v.mutations.Add(1)
	return errors.New("fixture rejects financial RPC")
}
func (v *constructorRecoveryVenue) Borrow(context.Context, string, float64) (int64, error) {
	v.mutations.Add(1)
	return 0, errors.New("fixture rejects financial RPC")
}
func (v *constructorRecoveryVenue) Repay(context.Context, string, float64) (int64, error) {
	v.mutations.Add(1)
	return 0, errors.New("fixture rejects financial RPC")
}
func (v *constructorRecoveryVenue) InternalTransfer(context.Context, string, string, string, float64) (string, error) {
	v.mutations.Add(1)
	return "", errors.New("fixture rejects financial RPC")
}

func constructorRecoveryPayload(t *testing.T, scope string) string {
	t.Helper()
	state := map[string]interface{}{
		"strategy": "funding_carry", "futures_exchange": "binance", "spot_exchange": "binance", "symbol": "BTCUSDT", "margin_account_scope": scope,
		"ownership_ready": true, "intent_in_flight": true, "exposure_unknown": true, "direction": 0,
		"margin_debt_events": []map[string]interface{}{
			{"action": "borrow", "transfer_id": 42, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": scope, "occurred_at": "1970-01-01T00:00:01Z"},
			{"action": "repay", "transfer_id": 1, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": scope, "occurred_at": "1970-01-01T00:00:02Z"},
		},
		"margin_cover_orders": []map[string]interface{}{{"order_id": 7, "asset": "BTC", "account_scope": scope, "requested": 0.4008, "debt_to_cover": 0.4, "gross": 0.4008, "net": 0.4008, "verified": true, "repay_transfer_id": 1, "consumed": 0.4,
			"fills": []*exchange.OrderFill{{OrderID: 7, TradeID: "cover-7", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 50000, Quantity: 0.4008, CommissionAsset: "BTC", TradeTime: 1500}}}},
	}
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	return string(payload)
}

func TestFundingCarryFullConstructorRetainsRemainingAssetRecovery(t *testing.T) {
	testFundingCarryFullConstructorRecovery(t, false)
}

func TestFundingCarryFullConstructorReportsRetainedClaimsOnWrongScope(t *testing.T) {
	testFundingCarryFullConstructorRecovery(t, true)
}

func testFundingCarryFullConstructorRecovery(t *testing.T, wrongScope bool) {
	t.Helper()
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}}
	cfg.Storage.Enabled, cfg.Storage.Type, cfg.Storage.Path = true, "sqlite", filepath.Join(t.TempDir(), "constructor.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	cfg.Timing.PriceSendInterval = 100
	service, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(service.Stop)
	const botID = "constructor-recovery"
	scope := equityAccountScopeID("binance", cfg.Exchanges["binance"])
	if wrongScope {
		scope = "unrelated-fixture-account"
	}
	payload := constructorRecoveryPayload(t, scope)
	stateStore := service.GetStorage().(storage.StrategyRuntimeStateStore)
	if err := stateStore.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 6, Payload: payload}); err != nil {
		t.Fatal(err)
	}
	venues := map[string]*constructorRecoveryVenue{}
	deps := fundingCarryStartupDependencies{
		checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
			return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
		},
		newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
			v := &constructorRecoveryVenue{market: market}
			venues[market] = v
			return v, nil
		},
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	sym := config.SymbolConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
		Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
	rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, nil, service, lock.NewNopLock(), nil, nil, deps)
	if wrongScope {
		if rt != nil || err == nil || !strings.Contains(err.Error(), "account scope") {
			t.Fatalf("wrong-scope recovery accepted or wrong failure: %v", err)
		}
		assertConstructorRecoveryEvidence(t, service, botID, payload)
		assertConstructorRecoveryStopped(t, venues)
		if !strings.Contains(err.Error(), "capital reservation release is unverified") {
			t.Fatalf("real constructor retained claims but caller lost that diagnostic: %v", err)
		}
		var retained *fundingCarryStartupRetentionError
		if !errors.As(err, &retained) || retained.Cause == nil {
			t.Fatal("retained claims are not structurally observable")
		}
		if fundingCarryReconciliationOnlyError(err) {
			t.Fatal("cleanup failure admitted as managed recovery")
		}
		return
	}
	if err != nil || rt == nil {
		t.Fatalf("complete constructor lost recovery: %v", err)
	}
	t.Cleanup(func() {
		if err := rt.StopWithError(); err != nil {
			t.Logf("fixture retained expected unresolved state: %v", err)
		}
	})
	if !rt.OpeningGate.HasBlock(fundingCarryReconciliationBlock) || rt.shutdownCloseUnverifiedReason() == "" {
		t.Fatal("constructor did not preserve independent recovery/close blocks")
	}
	status := rt.StrategyManager.GetStrategyStatus("funding_carry")
	if status.IsRunning || status.VisualizationData["margin_cover_remaining_qty"] != "0.0008" {
		t.Fatal("constructor lost historical accounting or started trading")
	}
	registry := NewBotManager(cfg, nil, service, lock.NewNopLock(), "")
	registry.AddRuntime(&BotRuntime{BotID: botID, Inner: rt})
	registered, found := registry.Get(botID)
	if !found || registered.Inner != rt {
		t.Fatal("constructed runtime lost in registry")
	}
	response := web.BotResponse{Running: true}
	attachFundingCarryRuntimeStatus(registered.Inner, &response)
	if response.FundingCarryRuntime == nil || response.FundingCarryRuntime.TradingRunning || !response.FundingCarryRuntime.ReconciliationRequired {
		t.Fatal("registered API state falsely reports trading")
	}
	assertConstructorRecoveryEvidence(t, service, botID, payload)
	if err := rt.StopWithError(); err == nil {
		t.Fatal("unreconciled remaining assets were reported stopped/flat")
	}
	assertConstructorRecoveryEvidence(t, service, botID, payload)
	assertConstructorRecoveryStopped(t, venues)
	for _, canceled := range []bool{false, true} {
		t.Run(fmt.Sprintf("failed_retry_canceled_%v", canceled), func(t *testing.T) {
			retryCtx, cancelRetry := context.WithCancel(context.Background())
			defer cancelRetry()
			cause := errors.New("fixture preflight unavailable")
			if canceled {
				cancelRetry()
				cause = context.Canceled
			}
			factoryCalls := 0
			retryDeps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					return nil, cause
				},
				newExchange: func(*config.Config, string, string, string) (exchange.IExchange, error) {
					factoryCalls++
					return nil, errors.New("unexpected connection")
				},
			}
			retry, retryErr := startFundingCarrySymbolRuntimeWithDependencies(retryCtx, cfg, sym, nil, service, lock.NewNopLock(), nil, nil, retryDeps)
			var retention *fundingCarryStartupRetentionError
			if retry != nil || !errors.Is(retryErr, cause) || factoryCalls != 0 {
				t.Fatalf("failed retry lost original cause or created resources: %v", retryErr)
			}
			if !errors.As(retryErr, &retention) {
				t.Fatalf("pre-existing SQL claims invisible after early failure: %v", retryErr)
			}
			if fundingCarryReconciliationOnlyError(retryErr) {
				t.Fatal("failed preflight admitted as reconciliation runtime")
			}
			assertConstructorRecoveryEvidence(t, service, botID, payload)
			assertConstructorRecoveryStopped(t, venues)
		})
	}
}

func assertConstructorRecoveryEvidence(t *testing.T, service *storage.StorageService, botID, payload string) {
	t.Helper()
	checker := service.GetStorage().(storage.AccountWalletCapitalReservationBotChecker)
	if held, err := checker.HasAccountWalletCapitalReservation(context.Background(), botID); err != nil || !held {
		t.Fatalf("lost persistent claims: %v", err)
	}
	reader := service.GetStorage().(storage.AccountWalletCapitalReservationReader)
	claims, err := reader.ListAccountWalletCapitalReservations(context.Background(), "", "", 10)
	if err != nil || len(claims) != 3 {
		t.Fatalf("expected all three persistent claims, got %d: %v", len(claims), err)
	}
	markets := map[string]bool{}
	for _, claim := range claims {
		if claim.Amount != 50 || claim.QuoteAsset != "USDT" || claim.Symbol != "BTCUSDT" {
			t.Fatal("claim amount or attribution changed")
		}
		markets[claim.Market] = true
	}
	if !markets["futures"] || !markets["spot"] || !markets["spot_margin"] {
		t.Fatal("a wallet claim disappeared")
	}
	saved, err := service.GetStorage().(storage.StrategyRuntimeStateStore).GetStrategyRuntimeState(botID, "funding_carry")
	if err != nil || saved == nil || saved.Payload != payload {
		t.Fatalf("constructor/stop changed durable financial evidence: %v", err)
	}
}

func assertConstructorRecoveryStopped(t *testing.T, venues map[string]*constructorRecoveryVenue) {
	t.Helper()
	for market, venue := range venues {
		if venue.mutations.Load() != 0 || venue.stops.Load() == 0 {
			t.Fatalf("%s mutated assets or leaked order stream", market)
		}
	}
	if venues["futures"].streamCtx == nil || venues["futures"].streamCtx.Err() == nil {
		t.Fatal("price lifecycle leaked after failed stop")
	}
}
