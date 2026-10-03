package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func seedFinancialRecoveryFixture(t *testing.T, route, scenario string) (*storage.SQLStorage, string, string) {
	t.Helper()
	setupBotCreateTestEnv(t)
	const botID = "financial-recovery"
	seedBotForMergeTest(t, botID)
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	owner := botCfgByID(cfg, botID)
	if owner == nil {
		t.Fatal("fixture owner missing")
	}
	owner.MarketType = config.MarketTypeFundingCarry
	owner.Strategies = []config.StrategyInstance{{Type: "funding_carry", Weight: 1}}
	if route == "DELETE_group" {
		cfg.BotGroups = []config.BotGroup{{ID: "financial-group", BotIDs: []string{botID}}}
	}
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveBotConfigUnified(&config.BotConfigFile{BotID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry}, "fixture", "fixture"); err != nil {
		t.Fatal(err)
	}
	store := primaryStorageForAppConfig.(*storage.SQLStorage)
	payload := ""
	if scenario != "absent" {
		kind := "pending"
		if scenario == "remaining" {
			kind = "remaining"
		}
		bytes, err := os.ReadFile("testdata/recovery_config_" + kind + ".json")
		if err != nil {
			t.Fatal(err)
		}
		scope := config.AccountScopeID("binance", cfg.Exchanges["binance"])
		payload = strings.ReplaceAll(string(bytes), "FIXTURE_SCOPE", scope)
		if scenario == "flat" {
			payload = `{"strategy":"funding_carry","futures_exchange":"binance","spot_exchange":"binance","symbol":"BTCUSDT","margin_account_scope":"` + scope + `","ownership_ready":true,"intent_in_flight":false,"exposure_unknown":false,"direction":0,"owned_spot":0,"owned_futures":0,"margin_debt":0}`
		}
		if scenario == "wrong_scope" {
			payload = strings.ReplaceAll(payload, scope, "other-account")
		}
		if scenario == "corrupt" {
			payload = "{}"
		}
		key := "funding_carry"
		if scenario == "old_unknown" {
			key = "removed-custom-strategy"
		}
		version := 6
		if scenario == "old_schema" {
			version = 5
		}
		if err := store.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: key, SchemaVersion: version, Payload: payload}); err != nil {
			t.Fatal(err)
		}
	}
	previous := botManagerProvider()
	RegisterBotManagerProvider(&configMutationTestProvider{})
	t.Cleanup(func() { RegisterBotManagerProvider(previous) })
	return store, botID, payload
}

func callFinancialRecoveryMutation(route, botID string) *httptest.ResponseRecorder {
	switch route {
	case "PUT_config":
		return callConfigFileMutation(http.MethodPut, botID)
	case "DELETE_config":
		return callConfigFileMutation(http.MethodDelete, botID)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	id := botID
	if route == "DELETE_group" {
		id = "financial-group"
	}
	c.Params = gin.Params{{Key: "id", Value: id}}
	c.Request = httptest.NewRequest(http.MethodDelete, "/fixture/"+id, nil)
	if route == "DELETE_bot" {
		deleteBot(c)
	} else {
		deleteBotGroup(c)
	}
	return w
}

func TestRecoveryConfigurationFourEntrypointsPreserveFinancialEvidence(t *testing.T) {
	for _, scenario := range []string{"pending", "remaining", "wrong_scope", "corrupt", "old_unknown", "old_schema"} {
		for _, route := range []string{"PUT_config", "DELETE_config", "DELETE_bot", "DELETE_group"} {
			t.Run(scenario+"/"+route, func(t *testing.T) {
				store, botID, payload := seedFinancialRecoveryFixture(t, route, scenario)
				before, err := store.GetBotConfigDocument(t.Context(), botID)
				if err != nil || before == nil {
					t.Fatal("missing document")
				}
				if scenario == "pending" || scenario == "remaining" {
					cfg, err := GetLatestConfig()
					if err != nil {
						t.Fatal(err)
					}
					err = strategy.VerifyFundingCarryRecoveryConfigState(6, payload, strategy.FundingCarryRecoveryBinding{FuturesExchange: "binance", SpotExchange: "binance", Symbol: "BTCUSDT", BaseAsset: "BTC", MarginAccountScope: config.AccountScopeID("binance", cfg.Exchanges["binance"])})
					if !errors.Is(err, strategy.ErrRecoveryConfigRequired) {
						t.Fatalf("fixture is not valid unresolved economics: %v", err)
					}
				}
				w := callFinancialRecoveryMutation(route, botID)
				want := http.StatusServiceUnavailable
				if scenario == "pending" || scenario == "remaining" {
					want = http.StatusConflict
				}
				if w.Code != want {
					t.Fatalf("unresolved mutation admitted: %d %s", w.Code, w.Body.String())
				}
				after, err := store.GetBotConfigDocument(t.Context(), botID)
				if err != nil || after == nil || after.Content != before.Content {
					t.Fatal("recovery document changed")
				}
				cfg, err := GetLatestConfig()
				if err != nil || botCfgByID(cfg, botID) == nil || botCfgByID(cfg, botID).Symbol != "BTCUSDT" {
					t.Fatal("primary Bot identity changed")
				}
				if route == "DELETE_group" && len(cfg.BotGroups) != 1 {
					t.Fatal("group partially removed")
				}
				key := "funding_carry"
				if scenario == "old_unknown" {
					key = "removed-custom-strategy"
				}
				saved, err := store.GetStrategyRuntimeState(botID, key)
				if err != nil || saved == nil || saved.Payload != payload {
					t.Fatal("financial journal changed")
				}
				if held, err := store.HasAccountWalletCapitalReservation(t.Context(), botID); err != nil || held {
					t.Fatal("fixture no longer has confirmed zero claim")
				}
			})
		}
	}
}

func TestRecoveryConfigurationFourEntrypointsAllowKnownFlatOrAbsent(t *testing.T) {
	for _, scenario := range []string{"flat", "absent"} {
		for _, route := range []string{"PUT_config", "DELETE_config", "DELETE_bot", "DELETE_group"} {
			t.Run(scenario+"/"+route, func(t *testing.T) {
				store, botID, payload := seedFinancialRecoveryFixture(t, route, scenario)
				w := callFinancialRecoveryMutation(route, botID)
				if w.Code != http.StatusOK {
					t.Fatalf("safe journal change refused: %d %s", w.Code, w.Body.String())
				}
				doc, err := store.GetBotConfigDocument(t.Context(), botID)
				if err != nil {
					t.Fatal(err)
				}
				if route == "PUT_config" {
					if doc == nil || !strings.Contains(doc.Content, "ETHUSDT") {
						t.Fatal("replacement not persisted")
					}
				} else if doc != nil {
					t.Fatal("deleted document remains")
				}
				if scenario == "flat" {
					saved, err := store.GetStrategyRuntimeState(botID, "funding_carry")
					if err != nil || saved == nil || saved.Payload != payload {
						t.Fatal("proof deleted historical journal")
					}
				}
			})
		}
	}
}

func TestRecoveryConfigurationRechecksLateStateInsideLifecycleLock(t *testing.T) {
	store, botID, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	provider := &configMutationTestProvider{}
	provider.before = func() {
		if !provider.inside {
			t.Fatal("not inside lifecycle lock")
		}
		if err := store.SetStrategyRuntimeState(&storage.StrategyRuntimeState{BotID: botID, StrategyName: "removed-strategy", SchemaVersion: 1, Payload: "{}"}); err != nil {
			t.Fatal(err)
		}
	}
	RegisterBotManagerProvider(provider)
	w := callFinancialRecoveryMutation("PUT_config", botID)
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("late old state bypassed guard: %d", w.Code)
	}
}

func TestRecoveryConfigurationReadFailureAndCancellationFailClosed(t *testing.T) {
	store, botID, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := verifyBotRecoveryConfiguration(ctx, botID); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatal("cancellation treated as absence")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := verifyBotRecoveryConfiguration(t.Context(), botID); !errors.Is(err, strategy.ErrRecoveryConfigUnverified) {
		t.Fatal("read failure treated as absence")
	}
}

func TestRecoveryConfigurationAmbiguousOwnerFailsClosed(t *testing.T) {
	for _, route := range []string{"PUT_config", "DELETE_config", "DELETE_bot", "DELETE_group"} {
		t.Run(route, func(t *testing.T) {
			store, botID, payload := seedFinancialRecoveryFixture(t, route, "flat")
			// Fault injection into the authoritative in-memory snapshot. No
			// production config file is changed or normalization bypassed by API.
			fileConfigManager.mu.Lock()
			duplicate := *botCfgByID(fileConfigManager.currentConfig, botID)
			duplicate.Symbol = "ETHUSDT"
			fileConfigManager.currentConfig.Bots = append(fileConfigManager.currentConfig.Bots, duplicate)
			fileConfigManager.mu.Unlock()
			before, err := store.GetBotConfigDocument(t.Context(), botID)
			if err != nil || before == nil {
				t.Fatal("missing document")
			}
			w := callFinancialRecoveryMutation(route, botID)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("ambiguous owner admitted: %d", w.Code)
			}
			after, err := store.GetBotConfigDocument(t.Context(), botID)
			if err != nil || after == nil || before.Content != after.Content {
				t.Fatal("ambiguous owner changed recovery document")
			}
			saved, err := store.GetStrategyRuntimeState(botID, "funding_carry")
			if err != nil || saved == nil || saved.Payload != payload {
				t.Fatal("ambiguous owner changed financial journal")
			}
		})
	}
}

func TestRecoveryConfigurationGroupDoesNotPartiallyRemoveVerifiedMember(t *testing.T) {
	store, botID, payload := seedFinancialRecoveryFixture(t, "DELETE_group", "pending")
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	member := *botCfgByID(cfg, botID)
	member.ID = "flat-member"
	member.Symbol = "ETHUSDT"
	cfg.Bots = append(cfg.Bots, member)
	cfg.BotGroups[0].BotIDs = []string{"flat-member", botID}
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if err := saveBotConfigUnified(&config.BotConfigFile{BotID: member.ID, Exchange: "binance", Symbol: member.Symbol, MarketType: config.MarketTypeFundingCarry}, "fixture", "fixture"); err != nil {
		t.Fatal(err)
	}
	before, err := GetLatestConfig()
	if err != nil || botCfgByID(before, member.ID) == nil || botCfgByID(before, botID) == nil || len(before.BotGroups) != 1 {
		t.Fatal("two-member group fixture was not persisted")
	}
	w := callFinancialRecoveryMutation("DELETE_group", botID)
	if w.Code != http.StatusConflict {
		t.Fatalf("pending second member accepted: %d", w.Code)
	}
	latest, err := GetLatestConfig()
	if err != nil || len(latest.Bots) != len(before.Bots) || len(latest.BotGroups) != 1 {
		t.Fatal("group primary configuration partially removed")
	}
	for _, id := range []string{member.ID, botID} {
		doc, err := store.GetBotConfigDocument(t.Context(), id)
		if err != nil || doc == nil {
			t.Fatal("member recovery document removed")
		}
	}
	saved, err := store.GetStrategyRuntimeState(botID, "funding_carry")
	if err != nil || saved == nil || saved.Payload != payload {
		t.Fatal("pending journal changed")
	}
}
