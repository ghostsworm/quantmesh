package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/storage"
)

// Offline venue only. Every order still traverses the production executor,
// shared intent journal, strategy ledger and SQL runtime-state adapter.
type constructorFinalVerificationVenue struct {
	*cleanConstructorVenue
	mu              sync.Mutex
	last            *exchange.Order
	futuresQty      float64
	repaid          bool
	failFinal       atomic.Bool
	failCleanup     atomic.Bool
	placements      atomic.Int32
	repayments      atomic.Int32
	beforeLiability func()
}

func (v *constructorFinalVerificationVenue) StopOrderStream() error {
	err := v.constructorRecoveryVenue.StopOrderStream()
	if v.failCleanup.Load() {
		return errors.Join(err, errConstructorStreamCleanup)
	}
	return err
}

func (*constructorFinalVerificationVenue) GetLatestPrice(context.Context, string) (float64, error) {
	return 50000, nil
}

func (v *constructorFinalVerificationVenue) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.market == "futures" && v.futuresQty > 0 {
		return []*exchange.Position{{Symbol: "BTCUSDT", Size: v.futuresQty, EntryPrice: 50000, MarkPrice: 50000}}, nil
	}
	return []*exchange.Position{}, nil
}

func (v *constructorFinalVerificationVenue) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if req.Symbol != "BTCUSDT" || req.ClientOrderID == "" ||
		(v.market == "futures" && (req.Side != exchange.SideSell || !req.ReduceOnly || req.Quantity != v.futuresQty)) ||
		(v.market == "spot_margin" && (req.Side != exchange.SideBuy || req.Quantity != 0.4008)) || v.market == "spot" {
		return nil, errors.New("fixture rejects unrelated financial request")
	}
	v.placements.Add(1)
	price := req.Price
	if price == 0 {
		price = 50000
	}
	v.last = &exchange.Order{OrderID: 71, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Type: req.Type,
		Price: price, AvgPrice: price, Quantity: req.Quantity, ExecutedQty: req.Quantity, Status: exchange.OrderStatusFilled, CreatedAt: time.Now()}
	if v.market == "futures" {
		v.futuresQty = 0
	}
	copy := *v.last
	return &copy, nil
}

func (v *constructorFinalVerificationVenue) GetOrder(_ context.Context, symbol string, id int64) (*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.last == nil || id != v.last.OrderID || symbol != v.last.Symbol {
		return nil, errors.New("fixture order identity mismatch")
	}
	copy := *v.last
	return &copy, nil
}

func (v *constructorFinalVerificationVenue) GetOrderFills(ctx context.Context, symbol string, id int64) ([]*exchange.OrderFill, error) {
	o, err := v.GetOrder(ctx, symbol, id)
	if err != nil {
		return nil, err
	}
	fee := 0.0
	if v.market == "spot_margin" {
		fee = 0.0008 // Exact net cover 0.4; no unowned remainder is discarded.
	}
	return []*exchange.OrderFill{{OrderID: id, TradeID: fmt.Sprintf("%s-%d", v.market, id), Symbol: symbol, Side: o.Side,
		Price: o.AvgPrice, Quantity: o.ExecutedQty, CommissionAsset: "BTC", Commission: fee, BaseFeeQty: fee, TradeTime: o.CreatedAt.UnixMilli()}}, nil
}

func (v *constructorFinalVerificationVenue) GetMarginLiability(_ context.Context, asset string) (float64, float64, error) {
	if v.beforeLiability != nil {
		v.beforeLiability()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if asset != "BTC" {
		return 0, 0, errors.New("fixture liability asset mismatch")
	}
	if v.repaid {
		if v.failFinal.Load() {
			return 0, 0, errors.New("fixture final liability query unavailable")
		}
		return 0, 0, nil
	}
	return 0.4, 0, nil
}

func (v *constructorFinalVerificationVenue) GetMarginRepaymentFunds(_ context.Context, asset string) (float64, float64, float64, error) {
	if asset != "BTC" {
		return 0, 0, 0, errors.New("fixture repayment asset mismatch")
	}
	return 0.4, 0, 0.4, nil
}

func (v *constructorFinalVerificationVenue) Repay(_ context.Context, asset string, amount float64) (int64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.market != "spot_margin" || asset != "BTC" || amount != 0.4 || v.repaid {
		return 0, errors.New("fixture rejects unrelated or repeated repayment")
	}
	v.repaid = true
	v.repayments.Add(1)
	return 81, nil
}

func (*constructorFinalVerificationVenue) GetMarginTransactionByID(_ context.Context, asset, kind string, id int64) (exchange.MarginBorrowRecord, error) {
	if asset != "BTC" || kind != "REPAY" || id != 81 {
		return exchange.MarginBorrowRecord{}, errors.New("fixture repayment receipt identity mismatch")
	}
	return exchange.MarginBorrowRecord{TransferID: id, Asset: asset, Status: "CONFIRMED", Amount: 0.4, Principal: 0.4, Timestamp: time.Now().UnixMilli()}, nil
}

func TestFundingCarryFullConstructorFinalVerificationReleasesOnlyAfterReadonlyRetry(t *testing.T) {
	testFundingCarryConstructorFinalVerificationWithStorage(t, newEnableStateStorage)
}

func TestFundingCarryFullConstructorStreamCleanupAndFinalVerificationRecoverWithoutFinancialReplay(t *testing.T) {
	testFundingCarryConstructorFinalVerificationWithFaults(t, newEnableStateStorage, []string{"cleanup"})
}

func TestFundingCarryCloseVerificationRejectsStaleRuntimeGeneration(t *testing.T) {
	testFundingCarryConstructorFinalVerificationWithFaults(t, newEnableStateStorage, []string{"stale_generation"})
}

func testFundingCarryConstructorFinalVerificationWithStorage(t *testing.T, newStorage func(*testing.T) *BotManager) {
	testFundingCarryConstructorFinalVerificationWithFaults(t, newStorage, []string{"none"})
}

func testFundingCarryConstructorFinalVerificationWithFaults(t *testing.T, newStorage func(*testing.T) *BotManager, faults []string) {
	for _, all := range []bool{false, true} {
		for _, faultMode := range faults {
			name := map[bool]string{false: "StopBot", true: "StopAll"}[all]
			if faultMode != "none" {
				name += "_" + faultMode
			}
			t.Run(name, func(t *testing.T) {
				bm := newStorage(t)
				bm.eventBus = event.NewEventBus(8)
				t.Cleanup(bm.eventBus.Close)
				cfg := bm.cfg
				cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
				cfg.Timing.PriceSendInterval = 100
				account := equityAccountScopeID("binance", cfg.Exchanges["binance"])
				seed := map[string]interface{}{"strategy": "funding_carry", "futures_exchange": "binance", "spot_exchange": "binance", "symbol": "BTCUSDT",
					"margin_account_scope": account, "ownership_ready": true, "direction": 2, "owned_futures": 0.4, "margin_debt": 0.4,
					"margin_borrow_transfer_id": 42, "margin_borrowed_at": "1970-01-01T00:00:01Z",
					"margin_debt_events": []map[string]interface{}{{"action": "borrow", "transfer_id": 42, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": account, "occurred_at": "1970-01-01T00:00:01Z"}}}
				payload, err := json.Marshal(seed)
				if err != nil {
					t.Fatal(err)
				}
				store := bm.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
				if err := store.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: "owner", StrategyName: "funding_carry", SchemaVersion: 7, Payload: string(payload)}); err != nil {
					t.Fatal(err)
				}
				provider := &runtimeLeaseTestLock{}
				venues := map[string]*constructorFinalVerificationVenue{}
				deps := fundingCarryStartupDependencies{
					checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
						return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
					},
					newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
						v := &constructorFinalVerificationVenue{cleanConstructorVenue: &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}}
						if market == "futures" {
							v.futuresQty = 0.4
						}
						v.failFinal.Store(true)
						venues[market] = v
						return v, nil
					},
				}
				var capitalFault *constructorCapitalCommitFailure
				if isConstructorCapitalCommitMode(faultMode) {
					capitalFault = newConstructorCapitalCommitFailure(t, bm, faultMode)
					deps.releaseCapital = capitalFault.release
				}
				ctx, cancel := context.WithCancel(t.Context())
				t.Cleanup(cancel)
				sym := config.SymbolConfig{ID: "owner", Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
					OpenPositionControl: config.OpenPositionControl{PauseOpening: true}, Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
				rt, err := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, bm.eventBus, bm.storageService, provider, nil, nil, deps)
				if err != nil || rt == nil {
					t.Fatalf("owned reverse full constructor failed: %v", err)
				}
				br := &BotRuntime{BotID: "owner", Config: config.SymbolConfigToBotConfig(sym, false), Inner: rt}
				bm.AddRuntime(br) // Register the actual carry contract, not a legacy fixture scope.
				if faultMode == "cleanup" {
					venues["spot"].failCleanup.Store(true)
				}
				stop := func() error { return bm.StopBot("owner") }
				if all {
					stop = bm.StopAll
				}
				if err := stop(); (faultMode == "cleanup" && (!isRetryableRuntimeStopCleanup(err) || !br.stopCleanupPending.Load())) || (faultMode != "cleanup" && (!isRetryableRuntimeStopVerification(err) || !br.stopVerificationPending.Load())) {
					t.Fatalf("actual financial final failure was not retained as readonly pending: %v", err)
				}
				original := rt.shutdownCloseUnverified.Load()
				saved := assertConstructorFinalVerificationCheckpoint(t, store, account, true)
				if err := stop(); (faultMode == "cleanup" && !isRetryableRuntimeStopCleanup(err)) || (faultMode != "cleanup" && !isRetryableRuntimeStopVerification(err)) || rt.shutdownCloseUnverified.Load() != original {
					t.Fatalf("actual failed readonly retry changed cached evidence: %v", err)
				}
				assertConstructorRecoveryEvidence(t, bm.storageService, "owner", saved.Payload)
				for _, market := range []string{"futures", "spot", "spot_margin"} {
					key, err := runtimeOwnershipScope(account, "binance", market, "BTCUSDT").Key()
					if err != nil {
						t.Fatal(err)
					}
					provider.mu.Lock()
					held := provider.held["runtime-owner:"+key]
					provider.mu.Unlock()
					if !held {
						t.Fatalf("failed proof surrendered %s ownership", market)
					}
				}
				claimsReader := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader)
				claims, err := claimsReader.ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
				if err != nil || len(claims) != 3 {
					t.Fatalf("failed proof released real SQL capital: %v claims=%d", err, len(claims))
				}
				if faultMode == "cleanup" {
					venues["spot"].failCleanup.Store(false)
					if err := stop(); !isRetryableRuntimeStopVerification(err) || rt.shutdownCloseUnverified.Load() != original {
						t.Fatal("cleanup completion erased or bypassed still-failing financial verification", err)
					}
					assertConstructorRecoveryEvidence(t, bm.storageService, "owner", saved.Payload)
					assertConstructorFinalVerificationCheckpoint(t, store, account, true)
				}
				venues["spot_margin"].failFinal.Store(false)
				if faultMode == "stale_generation" {
					var claimErr error
					claimed := false
					venues["spot_margin"].beforeLiability = func() {
						if claimed {
							return
						}
						claimed = true
						scopes, scopeErr := fundingCarryRuntimeOwnershipScopes(cfg, "binance", "BTCUSDT", true)
						if scopeErr != nil {
							claimErr = scopeErr
							return
						}
						keys := make([]string, 0, len(scopes))
						for _, scope := range scopes {
							key, keyErr := scope.Key()
							if keyErr != nil {
								claimErr = keyErr
								return
							}
							keys = append(keys, key)
						}
						_, claimErr = bm.storageService.GetStorage().(storage.FundingCarryRuntimeGenerationStore).ClaimFundingCarryRuntimeGeneration(t.Context(), keys)
					}
					if err := stop(); err == nil || !errors.Is(err, storage.ErrFundingCarryRuntimeGenerationLost) || claimErr != nil {
						t.Fatalf("close verification did not fail closed on generation takeover: claim=%v err=%v", claimErr, err)
					}
					if !claimed || rt.shutdownCloseUnverified.Load() != original || !br.stopVerificationPending.Load() {
						t.Fatal("stale close verification cleared its pending stop evidence")
					}
					assertConstructorRecoveryEvidence(t, bm.storageService, "owner", saved.Payload)
					claims, err := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
					if err != nil || len(claims) != 3 {
						t.Fatalf("stale close verification released capital reservations: %v claims=%d", err, len(claims))
					}
					if venues["spot_margin"].repayments.Load() != 1 || venues["futures"].placements.Load() != 1 {
						t.Fatal("stale close verification replayed financial operations")
					}
					return
				}
				var fault *constructorCommittedCASFailure
				if capitalFault != nil {
					injectConstructorCapitalCommitFailure(t, bm, rt, br, store, account, provider, venues, stop, original, capitalFault)
				} else if faultMode != "none" && faultMode != "cleanup" {
					fault = injectConstructorCommittedCASFailure(t, bm, rt, br, store, account, provider, stop, original, faultMode)
				}
				if err := stop(); err != nil {
					t.Fatalf("actual readonly recovery failed: %v", err)
				}
				if fault != nil && (fault.fired.Load() != 1 || fault.savedSame.Load() != 1) {
					t.Fatal("recovery did not make exactly one successful identical-source SQL CAS")
				}
				if capitalFault != nil && (capitalFault.fired.Load() != 1 || capitalFault.calls.Load() != 2) {
					t.Fatal("capital retry did not make exactly one idempotent SQL release after fresh proof")
				}
				assertConstructorFinalVerificationCheckpoint(t, store, account, false)
				for _, market := range []string{"futures", "spot_margin"} {
					key, err := (execution.IntentScope{Account: account, Exchange: "binance", Market: market, Symbol: "BTCUSDT", Bot: "owner"}).Key()
					if err != nil {
						t.Fatal(err)
					}
					rows, err := bm.storageService.GetStorage().(execution.IntentJournal).LoadExecutionIntents(t.Context(), key, 0, 10)
					if err != nil || len(rows) != 1 {
						t.Fatalf("%s SQL intent attribution missing: %v", market, err)
					}
					var intent struct{ Settled, Unknown bool }
					if err := json.Unmarshal(rows[0].Payload, &intent); err != nil || !intent.Settled || intent.Unknown {
						t.Fatal("actual SQL execution intent not economically settled", err)
					}
				}
				state, err := bm.storageService.GetStorage().GetBotState("owner")
				if err != nil || (!all && (state == nil || state.Enabled)) || (all && state != nil) {
					t.Fatal("actual stop changed persistent enable semantics: StopBot disables, StopAll preserves absent state", err)
				}
				journal, err := bm.readStopJournal("owner")
				if err != nil || journal != nil {
					t.Fatal("actual Bot stop failed to retire durable stop journal", err)
				}
				claims, err = claimsReader.ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
				if err != nil || len(claims) != 0 || rt.shutdownCloseUnverified.Load() != nil {
					t.Fatal("complete proof did not release SQL capital or owned stop failure", err)
				}
				if _, ok := bm.Get("owner"); ok {
					t.Fatal("verified actual runtime retained controller")
				}
				provider.mu.Lock()
				retained := len(provider.held)
				provider.mu.Unlock()
				if retained != 0 {
					t.Fatal("verified actual runtime retained wallet/runtime locks")
				}
				for market, v := range venues {
					want := int32(1)
					if market == "spot" {
						want = 0
					}
					wantStops := int32(1)
					if market == "spot" && faultMode == "cleanup" {
						wantStops = 3
					}
					if v.placements.Load() != want || v.stops.Load() != wantStops {
						t.Fatalf("%s replayed financial or stream stage", market)
					}
				}
				if venues["spot_margin"].repayments.Load() != 1 {
					t.Fatal("readonly retry replayed repayment")
				}
			})
		}
	}
}

func assertConstructorFinalVerificationCheckpoint(t *testing.T, store storage.StrategyRuntimeStateStore, account string, pending bool) *storage.StrategyRuntimeState {
	t.Helper()
	saved, err := store.GetStrategyRuntimeState("owner", "funding_carry")
	if err != nil || saved == nil || saved.SchemaVersion != 8 {
		t.Fatal("actual SQL strategy checkpoint missing", err)
	}
	var state struct {
		Scope     string  `json:"margin_account_scope"`
		Known     bool    `json:"ownership_ready"`
		Pending   bool    `json:"intent_in_flight"`
		Unknown   bool    `json:"exposure_unknown"`
		Marker    bool    `json:"margin_close_verification_pending"`
		Direction int     `json:"direction"`
		Debt      float64 `json:"margin_debt"`
		Futures   float64 `json:"owned_futures"`
		Spot      float64 `json:"owned_spot"`
		BorrowID  int64   `json:"margin_borrow_transfer_id"`
		Events    []struct {
			Action string `json:"action"`
			ID     int64  `json:"transfer_id"`
		} `json:"margin_debt_events"`
		Covers []struct {
			ID       int64   `json:"order_id"`
			Net      float64 `json:"net"`
			Consumed float64 `json:"consumed"`
			Verified bool    `json:"verified"`
		} `json:"margin_cover_orders"`
	}
	if err := json.Unmarshal([]byte(saved.Payload), &state); err != nil {
		t.Fatal(err)
	}
	if state.Scope != account || !state.Known || state.Pending != pending || state.Unknown != pending || state.Marker != pending || state.Debt != 0 || state.Futures != 0 || state.Spot != 0 ||
		len(state.Events) != 2 || state.Events[0].Action != "borrow" || state.Events[0].ID != 42 || state.Events[1].Action != "repay" || state.Events[1].ID != 81 ||
		len(state.Covers) != 1 || state.Covers[0].ID != 71 || state.Covers[0].Net != 0.4 || state.Covers[0].Consumed != 0.4 || !state.Covers[0].Verified {
		t.Fatal("actual SQL checkpoint discarded financial provenance or falsely completed proof")
	}
	if pending && (state.Direction != 2 || state.BorrowID != 42) || !pending && (state.Direction != 0 || state.BorrowID != 0) {
		t.Fatal("actual SQL checkpoint lost recovery identity or retained incomplete close")
	}
	return saved
}
