package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/storage"
)

type constructorCoverACKVenue struct {
	*cleanConstructorVenue
	queries    atomic.Int32
	wrongCID   bool
	afterQuery func()
}

func (v *constructorCoverACKVenue) coverOrder() *exchange.Order {
	cid := "cover-7"
	if v.wrongCID {
		cid = "foreign-cover"
	}
	return &exchange.Order{OrderID: 7, ClientOrderID: cid, Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Price: 50000, Quantity: 0.401, Status: exchange.OrderStatusNew, CreatedAt: time.UnixMilli(1000)}
}

func (v *constructorCoverACKVenue) GetOrderByClientOrderID(_ context.Context, symbol, cid string) (*exchange.Order, error) {
	v.queries.Add(1)
	if v.market != "spot_margin" || symbol != "BTCUSDT" || cid != "cover-7" {
		return nil, errors.New("fixture cover CID attribution mismatch")
	}
	if v.afterQuery != nil {
		v.afterQuery()
	}
	return v.coverOrder(), nil
}

func (v *constructorCoverACKVenue) GetOrder(_ context.Context, symbol string, id int64) (*exchange.Order, error) {
	if symbol != "BTCUSDT" || id != 7 {
		return nil, errors.New("fixture cover order identity mismatch")
	}
	return v.coverOrder(), nil
}

func TestFundingCarryFullConstructorCoverACKPreservesConcurrentEvidence(t *testing.T) {
	for _, mode := range []string{"verified_ack", "wrong_cid", "changed_checkpoint"} {
		t.Run(mode, func(t *testing.T) {
			bm := newEnableStateStorage(t)
			cfg := bm.cfg
			cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "fixture-account-identity"}}
			cfg.Timing.PriceSendInterval = 100
			const botID = "cover-ack-constructor"
			scope := equityAccountScopeID("binance", cfg.Exchanges["binance"])
			seed := map[string]interface{}{
				"strategy": "funding_carry", "futures_exchange": "binance", "spot_exchange": "binance", "symbol": "BTCUSDT", "margin_account_scope": scope,
				"ownership_ready": true, "intent_in_flight": true, "exposure_unknown": true, "direction": 2, "margin_debt": 0.4, "margin_borrow_transfer_id": 42, "margin_borrowed_at": "1970-01-01T00:00:01Z",
				"margin_debt_events":  []map[string]interface{}{{"action": "borrow", "transfer_id": 42, "asset": "BTC", "amount": 0.4, "principal": 0.4, "account_scope": scope, "occurred_at": "1970-01-01T00:00:01Z"}},
				"margin_cover_intent": map[string]interface{}{"client_order_id": "cover-7", "symbol": "BTCUSDT", "asset": "BTC", "account_scope": scope, "quantity": 0.401, "price": 50000, "debt_to_cover": 0.4, "prepared_at": "1970-01-01T00:00:01Z"},
			}
			encoded, err := json.Marshal(seed)
			if err != nil {
				t.Fatal(err)
			}
			store := bm.storageService.GetStorage().(storage.StrategyRuntimeStateStore)
			expected := string(encoded)
			save := func(payload string) {
				if err := store.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: "funding_carry", SchemaVersion: 7, Payload: payload}); err != nil {
					t.Fatal(err)
				}
			}
			save(expected)
			venues := map[string]*constructorCoverACKVenue{}
			deps := fundingCarryStartupDependencies{
				checkSetup: func(context.Context, *config.Config, string, string) (*exchange.FundingCarryPermissionResult, error) {
					return &exchange.FundingCarryPermissionResult{OK: true, SpotOK: true, FuturesOK: true}, nil
				},
				newExchange: func(_ *config.Config, _, _, market string) (exchange.IExchange, error) {
					v := &constructorCoverACKVenue{cleanConstructorVenue: &cleanConstructorVenue{constructorRecoveryVenue: &constructorRecoveryVenue{market: market}}, wrongCID: mode == "wrong_cid"}
					if market == "spot_margin" && mode == "changed_checkpoint" {
						v.afterQuery = func() { expected = strings.Replace(string(encoded), "cover-7", "cover-new", 1); save(expected) }
					}
					venues[market] = v
					return v, nil
				},
			}
			provider := &runtimeLeaseTestLock{}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			sym := config.SymbolConfig{ID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 100,
				Strategies: []config.StrategyInstance{{Type: "funding_carry", Weight: 1, Config: map[string]interface{}{"reverse_enabled": true}}}}
			rt, startErr := startFundingCarrySymbolRuntimeWithDependencies(ctx, cfg, sym, nil, bm.storageService, provider, nil, nil, deps)
			var retained *fundingCarryStartupRetentionError
			if rt != nil || !errors.As(startErr, &retained) || fundingCarryReconciliationOnlyError(startErr) {
				t.Fatalf("cover ACK admitted trading or managed recovery: %v", startErr)
			}
			saved, err := store.GetStrategyRuntimeState(botID, "funding_carry")
			if err != nil || saved == nil {
				t.Fatalf("SQL checkpoint missing: %v", err)
			}
			if mode != "verified_ack" {
				if saved.Payload != expected {
					t.Fatal("stale/foreign ACK overwrote SQL checkpoint")
				}
			} else {
				var state struct {
					Debt    float64         `json:"margin_debt"`
					Unknown bool            `json:"exposure_unknown"`
					Pending bool            `json:"intent_in_flight"`
					Intent  json.RawMessage `json:"margin_cover_intent"`
					Orders  []struct {
						ID        int64   `json:"order_id"`
						CID       string  `json:"client_order_id"`
						Requested float64 `json:"requested"`
						Price     float64 `json:"request_price"`
						Verified  bool    `json:"verified"`
					} `json:"margin_cover_orders"`
				}
				if err := json.Unmarshal([]byte(saved.Payload), &state); err != nil {
					t.Fatal(err)
				}
				if state.Debt != 0.4 || !state.Unknown || !state.Pending || len(state.Intent) != 0 || len(state.Orders) != 1 || state.Orders[0].ID != 7 || state.Orders[0].CID != "cover-7" || state.Orders[0].Requested != 0.401 || state.Orders[0].Price != 50000 || state.Orders[0].Verified {
					t.Fatal("SQL ACK checkpoint lost request attribution or invented fills")
				}
			}
			assertConstructorRecoveryEvidence(t, bm.storageService, botID, saved.Payload)
			if venues["spot_margin"].queries.Load() != 1 {
				t.Fatal("exact CID was not queried once")
			}
			for _, v := range venues {
				if v.mutations.Load() != 0 || v.stops.Load() != 1 || v.fillQueries.Load() != 0 {
					t.Fatal("ACK recovery traded, omitted cleanup or invented fill queries")
				}
			}
			provider.mu.Lock()
			leases := 0
			for key := range provider.held {
				if strings.HasPrefix(key, "runtime-owner:") {
					leases++
				}
			}
			provider.mu.Unlock()
			if leases != 3 {
				t.Fatalf("ownership legs=%d want=3", leases)
			}
		})
	}
}
