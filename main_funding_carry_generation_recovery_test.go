package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/storage"
)

type generationRecoveryVenue struct {
	*constructorRecoveryVenue
	claimStale func()
}

func (v *generationRecoveryVenue) GetOrderByClientOrderID(_ context.Context, symbol, clientID string) (*exchange.Order, error) {
	if symbol != "BTCUSDT" || clientID != "partial-7" {
		return nil, errors.New("fixture cover identity mismatch")
	}
	if v.claimStale != nil {
		v.claimStale()
	}
	return &exchange.Order{OrderID: 7, ClientOrderID: clientID, Symbol: symbol, Side: exchange.SideBuy,
		Type: exchange.OrderTypeLimit, Price: 50000, Quantity: 0.401, ExecutedQty: 0.2,
		AvgPrice: 50000, Status: exchange.OrderStatusCanceled, CreatedAt: time.UnixMilli(1700000000000).UTC()}, nil
}

func (v *generationRecoveryVenue) GetOrder(_ context.Context, symbol string, id int64) (*exchange.Order, error) {
	if symbol != "BTCUSDT" || id != 7 {
		return nil, errors.New("fixture order identity mismatch")
	}
	return &exchange.Order{OrderID: id, ClientOrderID: "partial-7", Symbol: symbol, Side: exchange.SideBuy,
		Type: exchange.OrderTypeLimit, Price: 50000, Quantity: 0.401, ExecutedQty: 0.2,
		AvgPrice: 50000, Status: exchange.OrderStatusCanceled, CreatedAt: time.UnixMilli(1700000000000).UTC()}, nil
}

func TestFundingCarryConstructorRecoveryCheckpointUsesPersistentGeneration(t *testing.T) {
	for _, mode := range []string{"current", "stale", "write_error"} {
		t.Run(mode, func(t *testing.T) {
			cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "isolated-generation-fixture"}}}
			cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
			cfg.Storage.Path = filepath.Join(t.TempDir(), "funding-carry-generation.db")
			cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
			cfg.Timing.PriceSendInterval = 100
			service, err := storage.NewStorageService(cfg, t.Context())
			if err != nil {
				t.Fatal("open isolated SQLite storage:", err)
			}
			t.Cleanup(func() { service.Stop() })
			const botID = "generation-recovery-fixture"
			account := equityAccountScopeID("binance", cfg.Exchanges["binance"])
			payloadBytes, err := json.Marshal(map[string]interface{}{
				"strategy": "funding_carry", "futures_exchange": "binance", "spot_exchange": "binance", "symbol": "BTCUSDT",
				"margin_account_scope": account, "ownership_ready": true, "intent_in_flight": true, "exposure_unknown": true,
				"direction": 2, "margin_debt": 0.4, "margin_borrow_transfer_id": 42,
				"margin_borrowed_at": "2023-11-14T22:13:20Z",
				"margin_debt_events": []map[string]interface{}{{"action": "borrow", "transfer_id": 42, "asset": "BTC", "amount": 0.4,
					"principal": 0.4, "account_scope": account, "occurred_at": "2023-11-14T22:13:20Z"}},
				"margin_cover_intent": map[string]interface{}{"client_order_id": "partial-7", "symbol": "BTCUSDT", "asset": "BTC",
					"account_scope": account, "quantity": 0.401, "price": 50000, "debt_to_cover": 0.4, "prepared_at": "2023-11-14T22:13:20Z"},
			})
			if err != nil {
				t.Fatal("encode seeded recovery checkpoint:", err)
			}
			initialPayload := string(payloadBytes)
			stateStore := service.GetStorage().(storage.StrategyRuntimeStateStore)
			if err := stateStore.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 7, Payload: initialPayload}); err != nil {
				t.Fatal("seed recovery checkpoint:", err)
			}
			if mode == "write_error" {
				db, err := sql.Open("sqlite3", cfg.Storage.Path+"?_journal_mode=WAL&_synchronous=NORMAL")
				if err != nil {
					t.Fatal("open trigger connection:", err)
				}
				t.Cleanup(func() { _ = db.Close() })
				if _, err := db.Exec(`CREATE TRIGGER reject_funding_carry_recovery BEFORE UPDATE ON strategy_runtime_states WHEN OLD.bot_id = 'generation-recovery-fixture' AND OLD.strategy_name = 'funding_carry' AND NEW.payload LIKE '%"verified":true%' BEGIN SELECT RAISE(ABORT, 'injected recovery CAS failure'); END`); err != nil {
					t.Fatal("install isolated persistence fault:", err)
				}
			}

			venueByMarket := map[string]*generationRecoveryVenue{}
			deps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
				},
				newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
					venue := &generationRecoveryVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}
					venueByMarket[market] = venue
					return venue, nil
				},
			}
			sym := config.SymbolConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry,
				TotalAllocatedCapital: 100,
				Strategies:            []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
			var staleClaimErr error
			if mode == "stale" {
				// The production constructor creates the venue before invoking strategy recovery.
				// This callback takes over exactly its complete scope set between source read and CAS.
				deps.newExchange = func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
					venue := &generationRecoveryVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}
					if market == "spot_margin" {
						venue.claimStale = func() {
							if staleClaimErr != nil {
								return
							}
							scopes, scopeErr := fundingCarryRuntimeOwnershipScopes(cfg, sym.Exchange, sym.Symbol, true)
							if scopeErr != nil {
								staleClaimErr = scopeErr
								return
							}
							keys := make([]string, 0, len(scopes))
							for _, scope := range scopes {
								key, keyErr := scope.Key()
								if keyErr != nil {
									staleClaimErr = keyErr
									return
								}
								keys = append(keys, key)
							}
							_, staleClaimErr = service.GetStorage().(storage.FundingCarryRuntimeGenerationStore).ClaimFundingCarryRuntimeGeneration(t.Context(), keys)
						}
					}
					venueByMarket[market] = venue
					return venue, nil
				}
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			rt, startErr := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, nil, service, lock.NewNopLock(), nil, nil, deps)
			if mode == "current" {
				if startErr != nil || rt == nil {
					t.Fatalf("current generation recovery startup: runtime=%v err=%v", rt, startErr)
				}
				saved, err := stateStore.GetStrategyRuntimeState(botID, "funding_carry")
				if err != nil || saved == nil || saved.Payload == initialPayload || !strings.Contains(saved.Payload, `"verified":true`) {
					t.Fatalf("current-generation recovery checkpoint was not durably advanced: state=%+v err=%v", saved, err)
				}
				if err := rt.StopWithError(); err == nil {
					t.Fatal("partial close evidence was reported as fully reconciled")
				}
				return
			}
			if rt != nil || startErr == nil {
				t.Fatalf("%s recovery unexpectedly produced managed runtime: runtime=%v err=%v", mode, rt, startErr)
			}
			if mode == "stale" && (staleClaimErr != nil || !errors.Is(startErr, storage.ErrFundingCarryRuntimeGenerationLost)) {
				t.Fatalf("stale-generation checkpoint was not rejected at the production recovery CAS: claim=%v err=%v", staleClaimErr, startErr)
			}
			if mode == "write_error" && !strings.Contains(startErr.Error(), "injected recovery CAS failure") {
				t.Fatalf("recovery write-error case did not reach the injected CAS failure: %v", startErr)
			}
			saved, err := stateStore.GetStrategyRuntimeState(botID, "funding_carry")
			if err != nil || saved == nil {
				t.Fatalf("%s failure lost financial checkpoint: state=%+v err=%v", mode, saved, err)
			}
			if mode == "stale" && saved.Payload != initialPayload {
				t.Fatalf("stale-generation failure changed the pre-CAS financial checkpoint: state=%+v", saved)
			}
			if mode == "write_error" {
				var checkpoint struct {
					IntentInFlight         bool    `json:"intent_in_flight"`
					ExposureUnknown        bool    `json:"exposure_unknown"`
					Direction              int     `json:"direction"`
					MarginDebt             float64 `json:"margin_debt"`
					MarginBorrowTransferID int64   `json:"margin_borrow_transfer_id"`
					MarginCoverOrders      []struct {
						OrderID       int64  `json:"order_id"`
						ClientOrderID string `json:"client_order_id"`
						Verified      bool   `json:"verified"`
					} `json:"margin_cover_orders"`
				}
				if err := json.Unmarshal([]byte(saved.Payload), &checkpoint); err != nil {
					t.Fatalf("decode persisted unresolved checkpoint: %v", err)
				}
				if !checkpoint.IntentInFlight || !checkpoint.ExposureUnknown || checkpoint.Direction != 2 || checkpoint.MarginDebt != 0.4 || checkpoint.MarginBorrowTransferID != 42 ||
					len(checkpoint.MarginCoverOrders) != 1 || checkpoint.MarginCoverOrders[0].OrderID != 7 || checkpoint.MarginCoverOrders[0].ClientOrderID != "partial-7" || checkpoint.MarginCoverOrders[0].Verified {
					t.Fatalf("failed recovery CAS falsely resolved or discarded financial exposure: %+v", checkpoint)
				}
			}
			claimsReader := service.GetStorage().(storage.AccountWalletCapitalReservationReader)
			claims, err := claimsReader.ListAccountWalletCapitalReservations(t.Context(), "", "", 10)
			if err != nil || len(claims) != 3 {
				t.Fatalf("%s recovery failure did not retain all account capital reservations: claims=%d err=%v", mode, len(claims), err)
			}
			markets := make(map[string]bool, len(claims))
			for _, claim := range claims {
				if !claim.Mapped || claim.Exchange != "binance" || claim.Symbol != "BTCUSDT" {
					t.Fatalf("%s recovery failure retained an unrelated/unmapped capital reservation: %+v", mode, claim)
				}
				markets[claim.Market] = true
			}
			if !markets["futures"] || !markets["spot"] || !markets["spot_margin"] {
				t.Fatalf("%s recovery failure retained incomplete market reservations: %v", mode, markets)
			}
			for market, venue := range venueByMarket {
				if venue.mutations.Load() != 0 {
					t.Fatalf("%s recovery failure issued financial RPCs on %s", mode, market)
				}
			}
		})
	}
}
