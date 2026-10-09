package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"
)

type constructorBorrowReceiptVenue struct {
	*cleanConstructorVenue
	queries    atomic.Int32
	wrongID    bool
	repayment  bool
	afterQuery func()
}

func (v *constructorBorrowReceiptVenue) GetMarginTransactionByID(_ context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	v.queries.Add(1)
	expectedKind, expectedID, timestamp := "BORROW", int64(42), int64(1000)
	if v.repayment {
		expectedKind, expectedID, timestamp = "REPAY", 7, 2000
	}
	if v.market != "spot_margin" || asset != "BTC" || kind != expectedKind || id != expectedID {
		return exchange.MarginBorrowRecord{}, errors.New("receipt query attribution mismatch")
	}
	if v.wrongID {
		id++
	}
	if v.afterQuery != nil {
		v.afterQuery()
	}
	return exchange.MarginBorrowRecord{TransferID: id, Asset: asset, Status: "CONFIRMED", Amount: 0.4, Principal: 0.4, Timestamp: timestamp}, nil
}

func TestFundingCarryFullConstructorBorrowReceiptRetainsCapitalAndOwnership(t *testing.T) {
	testFundingCarryConstructorReceipt(t, false)
}

func TestFundingCarryFullConstructorRepaymentReceiptRetainsCapitalAndOwnership(t *testing.T) {
	testFundingCarryConstructorReceipt(t, true)
}

func testFundingCarryConstructorReceipt(t *testing.T, repayment bool) {
	testFundingCarryConstructorReceiptWithStorage(t, repayment, newEnableStateStorage)
}

func testFundingCarryConstructorReceiptWithStorage(t *testing.T, repayment bool, newStorage func(*testing.T) *BotManager) {
	for _, name := range []string{"verified_receipt", "wrong_identity", "changed_checkpoint"} {
		wrongID := name == "wrong_identity"
		t.Run(name, func(t *testing.T) {
			bm := newStorage(t)
			cfg := bm.cfg
			cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
			cfg.Timing.PriceSendInterval = 100
			const botID = "borrow-receipt-constructor"
			scope := equityAccountScopeID("binance", cfg.Exchanges["binance"])
			seed := map[string]interface{}{"strategy": "funding_carry", "futures_exchange": "binance", "spot_exchange": "binance", "symbol": "BTCUSDT",
				"margin_account_scope": scope, "ownership_ready": true, "intent_in_flight": true, "exposure_unknown": true, "direction": 0, "margin_borrow_transfer_id": 42}
			if repayment {
				seed["direction"], seed["margin_debt"], seed["margin_borrowed_at"] = 2, 0.4, "1970-01-01T00:00:01Z"
				seed["margin_debt_events"] = []map[string]interface{}{{"action": "borrow", "transfer_id": 42, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": scope, "occurred_at": "1970-01-01T00:00:01Z"}}
				seed["margin_repay_intent"] = map[string]interface{}{"asset": "BTC", "account_scope": scope, "amount": 0.4, "expected_remaining": 0, "borrow_transfer_id": 42, "transfer_id": 7}
			}
			original, err := json.Marshal(seed)
			if err != nil {
				t.Fatal(err)
			}
			stateStore := bm.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
			expectedPayload := string(original)
			if err := stateStore.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 7, Payload: string(original)}); err != nil {
				t.Fatal(err)
			}
			provider := &runtimeLeaseTestLock{}
			venues := map[string]*constructorBorrowReceiptVenue{}
			deps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
				},
				newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
					v := &constructorBorrowReceiptVenue{cleanConstructorVenue: &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}, wrongID: wrongID, repayment: repayment}
					if market == "spot_margin" && name == "changed_checkpoint" {
						v.afterQuery = func() {
							expectedPayload = strings.Replace(string(original), `"margin_borrow_transfer_id":42`, `"margin_borrow_transfer_id":43`, 1)
							if repayment {
								expectedPayload = strings.Replace(string(original), `"transfer_id":7`, `"transfer_id":8`, 1)
							}
							if expectedPayload == string(original) {
								t.Fatal("fixture did not change borrow identity")
							}
							if err := stateStore.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 7, Payload: expectedPayload}); err != nil {
								t.Fatal(err)
							}
						}
					}
					venues[market] = v
					return v, nil
				},
			}
			ctx, cancel := context.WithCancel(context.Background())
			t.Cleanup(cancel)
			sym := config.SymbolConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
				Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
			rt, startErr := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, nil, bm.storageService, provider, nil, nil, deps)
			var retained *fundingCarryStartupRetentionError
			if rt != nil || !errors.As(startErr, &retained) || fundingCarryReconciliationOnlyError(startErr) {
				t.Fatalf("borrow recovery released trading or concealed retained claims: %v", startErr)
			}
			saved, err := stateStore.GetStrategyRuntimeState(botID, "funding_carry")
			if err != nil || saved == nil {
				t.Fatalf("durable checkpoint missing: %v", err)
			}
			if wrongID || name == "changed_checkpoint" {
				if saved.Payload != expectedPayload {
					t.Fatal("invalid or stale receipt changed actual SQL state")
				}
			} else {
				var state struct {
					Direction   int                      `json:"direction"`
					Debt        float64                  `json:"margin_debt"`
					Unknown     bool                     `json:"exposure_unknown"`
					Pending     bool                     `json:"intent_in_flight"`
					Events      []map[string]interface{} `json:"margin_debt_events"`
					RepayIntent json.RawMessage          `json:"margin_repay_intent"`
				}
				if err := json.Unmarshal([]byte(saved.Payload), &state); err != nil {
					t.Fatal(err)
				}
				expectedDebt, expectedEvents := 0.4, 1
				if repayment {
					expectedDebt, expectedEvents = 0, 2
				}
				if state.Direction != 2 || state.Debt != expectedDebt || !state.Unknown || !state.Pending || len(state.Events) != expectedEvents {
					t.Fatal("SQL receipt checkpoint did not preserve debt and uncertainty")
				}
				if repayment && len(state.RepayIntent) != 0 && string(state.RepayIntent) != "null" {
					t.Fatal("confirmed repayment intent remained after conditional cleanup")
				}
			}
			assertConstructorRecoveryEvidence(t, bm.storageService, botID, saved.Payload)
			if venues["spot_margin"].queries.Load() != 1 {
				t.Fatal("constructor omitted exact margin receipt query")
			}
			for _, v := range venues {
				if v.mutations.Load() != 0 || v.stops.Load() != 1 {
					t.Fatal("failed startup mutated finances or omitted stream cleanup")
				}
			}
			provider.mu.Lock()
			retainedLeases := 0
			for key := range provider.held {
				if strings.HasPrefix(key, "runtime-owner:") {
					retainedLeases++
				}
			}
			provider.mu.Unlock()
			if retainedLeases != 3 {
				t.Fatalf("retained ownership legs=%d want=3", retainedLeases)
			}
		})
	}
}
