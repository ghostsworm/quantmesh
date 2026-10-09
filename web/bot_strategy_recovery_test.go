package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func TestBotStrategyPendingBorrowIdentity(t *testing.T) {
	for _, changed := range []string{"group_id", "symbol"} {
		t.Run(changed, func(t *testing.T) {
			setupBotCreateTestEnv(t)
			const id = "audit-pending-borrow"
			seedBotForMergeTest(t, id)
			cfg, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			bot := botCfgByID(cfg, id)
			bot.MarketType = "spot_margin"
			bot.Strategies = []config.StrategyInstance{{Type: "spot_short", Weight: 1, Config: map[string]interface{}{"group_id": "old-group", "symbol": "BTCUSDT"}}}
			if err := fileConfigManager.UpdateConfig(cfg); err != nil {
				t.Fatal(err)
			}
			const payload = `{"bot_id":"audit-pending-borrow","strategy":"spot_short","group_id":"old-group","symbol":"BTCUSDT","base_asset":"BTC","pending_repay":{},"consumed_repay_transfers":{},"pending_borrow":{"borrow-a":{"amount":0.4,"phase":"borrowed","borrow_transfer_id":42,"created_at_unix_milli":1}}}`
			oldBinding := strategy.HedgeRecoveryBinding{BotID: id, StrategyName: "spot_short", GroupID: "old-group", Symbol: "BTCUSDT", BaseAsset: "BTC"}
			if err := strategy.VerifySpotShortRecoveryConfigState(10, payload, oldBinding); !errors.Is(err, strategy.ErrRecoveryConfigRequired) {
				t.Fatalf("not valid unresolved journal: %v", err)
			}
			store := primaryStorageForAppConfig.(*storage.SQLStorage)
			if err := store.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: id, StrategyName: "spot_short", SchemaVersion: 10, Payload: payload}); err != nil {
				t.Fatal(err)
			}
			if held, err := store.HasAccountWalletCapitalReservation(t.Context(), id); err != nil || held {
				t.Fatal("not confirmed zero reservation")
			}
			if err := verifyBotRecoveryConfiguration(t.Context(), id); !errors.Is(err, strategy.ErrRecoveryConfigRequired) {
				t.Fatalf("production gate cannot detect pending: %v", err)
			}
			previous := botManagerProvider()
			RegisterBotManagerProvider(&configMutationTestProvider{})
			t.Cleanup(func() { RegisterBotManagerProvider(previous) })
			group, symbol := "old-group", "BTCUSDT"
			if changed == "group_id" {
				group = "new-group"
			} else {
				symbol = "ETHUSDT"
			}
			body := `{"strategies":[{"type":"spot_short","weight":1,"config":{"group_id":"` + group + `","symbol":"` + symbol + `"}}]}`
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "id", Value: id}}
			c.Request = httptest.NewRequest(http.MethodPut, "/api/bots/"+id+"/strategy", strings.NewReader(body))
			c.Request.Header.Set("Content-Type", "application/json")
			putBotStrategy(c)
			latest, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			saved, err := store.GetStrategyRuntimeState(id, "spot_short")
			if err != nil || saved == nil || saved.Payload != payload {
				t.Fatal("journal not preserved")
			}
			now := botCfgByID(latest, id)
			identityChanged := now.Strategies[0].Config[changed] != bot.Strategies[0].Config[changed]
			proofAfter := verifyBotRecoveryConfiguration(t.Context(), id)
			t.Logf("http=%d primary_identity_changed=%t journal_unchanged=true before=required after_unverified=%t", w.Code, identityChanged, errors.Is(proofAfter, strategy.ErrRecoveryConfigUnverified))
			if w.Code != http.StatusConflict || identityChanged {
				t.Fatalf("pending borrow recovery identity overwritten: http=%d changed=%t", w.Code, identityChanged)
			}
		})
	}
}

func callStrategyMutation(id, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/bots/"+id+"/strategy", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	putBotStrategy(c)
	return w
}

type strategyHotUpdateProbe struct {
	SymbolManagerProvider
	calls  int
	latest *config.Config
}

func (p *strategyHotUpdateProbe) UpdateTradingParams(cfg *config.Config) []string {
	p.calls++
	p.latest = cfg
	return nil
}

func TestBotStrategyKeepsManagedHotControls(t *testing.T) {
	store, id, payload := seedFinancialRecoveryFixture(t, "PUT_config", "pending")
	provider := &configMutationTestProvider{running: true}
	RegisterBotManagerProvider(provider)
	old := symbolManagerProvider
	probe := &strategyHotUpdateProbe{}
	symbolManagerProvider = probe
	t.Cleanup(func() { symbolManagerProvider = old })
	w := callStrategyMutation(id, `{"smart_order_enabled":true,"smart_order_max_open_orders":2}`)
	if w.Code != http.StatusOK || probe.calls != 1 || probe.latest == nil {
		t.Fatalf("hot controls blocked or not dispatched: %d", w.Code)
	}
	if botCfgByID(probe.latest, id).SmartOrder.MaxOpenOrders != 2 {
		t.Fatal("hot controls not persisted")
	}
	saved, err := store.GetStrategyRuntimeState(id, "funding_carry")
	if err != nil || saved == nil || saved.Payload != payload {
		t.Fatal("hot controls changed journal")
	}
}

func TestBotStrategyContractChangesUseRecoveryGate(t *testing.T) {
	for _, scenario := range []string{"pending", "wrong_scope", "old_unknown", "absent", "flat"} {
		t.Run(scenario, func(t *testing.T) {
			_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", scenario)
			before, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			w := callStrategyMutation(id, `{"strategies":[{"type":"funding_carry","weight":1,"config":{"fixture_limit":1}}]}`)
			want := http.StatusServiceUnavailable
			if scenario == "pending" {
				want = http.StatusConflict
			}
			if scenario == "absent" || scenario == "flat" {
				want = http.StatusOK
			}
			if w.Code != want {
				t.Fatalf("incorrect gate response: %d %s", w.Code, w.Body.String())
			}
			latest, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			if want != http.StatusOK && !reflect.DeepEqual(before, latest) {
				t.Fatal("rejected mutation changed configuration")
			}
		})
	}
}

func TestBotStrategyRefusesManagedContractChangeAndMissingCoordinator(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	provider := &configMutationTestProvider{running: true}
	RegisterBotManagerProvider(provider)
	w := callStrategyMutation(id, `{"direction":"SHORT"}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("managed recovery contract admitted: %d", w.Code)
	}
	RegisterBotManagerProvider(&mockBotManagerForCreateTest{})
	w = callStrategyMutation(id, `{"smart_order_max_open_orders":2}`)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing coordination admitted: %d", w.Code)
	}
}

type strategyConcurrentConfigProvider struct {
	configMutationTestProvider
	t       *testing.T
	changed bool
}

func (p *strategyConcurrentConfigProvider) GetBot(id string) (*BotDetailResponse, bool) {
	if !p.changed {
		p.changed = true
		if err := fileConfigManager.UpdateConfigUsing(func(cfg *config.Config) error { botCfgByID(cfg, id).Name = "concurrent-write"; return nil }); err != nil {
			p.t.Fatal(err)
		}
	}
	return p.configMutationTestProvider.GetBot(id)
}

func TestBotStrategyStaleSnapshotCannotOverwriteConcurrentConfig(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	RegisterBotManagerProvider(&strategyConcurrentConfigProvider{t: t})
	w := callStrategyMutation(id, `{"smart_order_max_open_orders":2}`)
	if w.Code != http.StatusConflict {
		t.Fatalf("stale snapshot admitted: %d", w.Code)
	}
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if botCfgByID(latest, id).Name != "concurrent-write" || botCfgByID(latest, id).SmartOrder.MaxOpenOrders == 2 {
		t.Fatal("concurrent configuration overwritten")
	}
}

func TestBotStrategyFailedPersistenceDoesNotPublishHotControls(t *testing.T) {
	for _, failure := range []string{"closed_storage", "canceled"} {
		t.Run(failure, func(t *testing.T) {
			store, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
			before, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			old := symbolManagerProvider
			probe := &strategyHotUpdateProbe{}
			symbolManagerProvider = probe
			t.Cleanup(func() { symbolManagerProvider = old })
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Params = gin.Params{{Key: "id", Value: id}}
			c.Request = httptest.NewRequest(http.MethodPut, "/fixture", strings.NewReader(`{"smart_order_max_open_orders":2}`))
			c.Request.Header.Set("Content-Type", "application/json")
			if failure == "closed_storage" {
				if err := store.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				ctx, cancel := context.WithCancel(c.Request.Context())
				cancel()
				c.Request = c.Request.WithContext(ctx)
			}
			putBotStrategy(c)
			latest, err := GetLatestConfig()
			if w.Code < http.StatusBadRequest || probe.calls != 0 || err != nil || !reflect.DeepEqual(before, latest) {
				t.Fatalf("failed persistence was published: status=%d calls=%d err=%v", w.Code, probe.calls, err)
			}
		})
	}
}
