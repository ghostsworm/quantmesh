package web

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
	"quantmesh/strategy"
)

func callGlobalRecoverySave(t *testing.T, format string, cfg *config.Config) *httptest.ResponseRecorder {
	t.Helper()
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if format == "json" {
		var values map[string]interface{}
		if err := yaml.Unmarshal(body, &values); err != nil {
			t.Fatal(err)
		}
		body, err = json.Marshal(values)
		if err != nil {
			t.Fatal(err)
		}
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body))
	if format == "json" {
		updateConfigHandler(c)
	} else {
		updateConfigYAMLHandler(c)
	}
	return w
}

func TestGlobalRecoverySaveProtectsPendingOwner(t *testing.T) {
	cases := []struct {
		name   string
		change func(*config.Config, string)
	}{
		{"remove", func(cfg *config.Config, id string) {
			retained := []config.BotConfig{}
			for _, bot := range cfg.Bots {
				if config.BotIDOrGenerate(bot) != id {
					retained = append(retained, bot)
				}
			}
			cfg.Bots = retained
		}},
		{"remove-with-legacy-mirror", func(cfg *config.Config, id string) {
			cfg.Trading.Symbols = []config.SymbolConfig{config.BotConfigToSymbolConfig(*botCfgByID(cfg, id))}
			cfg.Trading.Symbols[0].OrderQuantity = 10
			cfg.Bots = []config.BotConfig{}
		}},
		{"direction", func(cfg *config.Config, id string) { botCfgByID(cfg, id).Direction = "SHORT" }},
		{"strategy", func(cfg *config.Config, id string) {
			botCfgByID(cfg, id).Strategies = []config.StrategyInstance{{Type: "grid", Weight: 1}}
		}},
		{"account", func(cfg *config.Config, _ string) {
			ec := cfg.Exchanges["binance"]
			ec.Testnet = !ec.Testnet
			cfg.Exchanges["binance"] = ec
		}},
		{"group", func(cfg *config.Config, id string) {
			cfg.BotGroups = []config.BotGroup{{ID: "changed-group", BotIDs: []string{id}}}
		}},
	}
	for _, format := range []string{"json", "yaml"} {
		for _, tc := range cases {
			t.Run(format+"/"+tc.name, func(t *testing.T) {
				_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "pending")
				before, err := GetLatestConfig()
				if err != nil {
					t.Fatal(err)
				}
				next, err := cloneConfigSnapshot(before)
				if err != nil {
					t.Fatal(err)
				}
				tc.change(next, id)
				w := callGlobalRecoverySave(t, format, next)
				current, err := GetLatestConfig()
				if err != nil {
					t.Fatal(err)
				}
				durable, err := loadConfigFromPrimaryDB()
				if err != nil {
					t.Fatal(err)
				}
				var receipt struct {
					Error string `json:"error"`
					Saved *bool  `json:"config_saved"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &receipt); err != nil {
					t.Fatal(err)
				}
				if w.Code != http.StatusConflict || receipt.Error != "bot_recovery_configuration_required" || receipt.Saved == nil || *receipt.Saved || !reflect.DeepEqual(current, before) || durable == nil || botCfgByID(durable, id) == nil {
					t.Fatalf("recovery admission failed: status=%d code=%s", w.Code, receipt.Error)
				}
			})
		}
	}
}

func TestGlobalRecoverySaveAllowsHotAndMetadataWithPendingState(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "pending")
			cfg, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			botCfgByID(cfg, id).Name = "display-only"
			botCfgByID(cfg, id).PriceInterval += 1
			previous, previousHR := symbolManagerProvider, configHotReloader
			symbolManagerProvider, configHotReloader = nil, nil
			t.Cleanup(func() { symbolManagerProvider, configHotReloader = previous, previousHR })
			w := callGlobalRecoverySave(t, format, cfg)
			durable, err := loadConfigFromPrimaryDB()
			if err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || botCfgByID(durable, id).Name != "display-only" || !errors.Is(verifyBotRecoveryConfiguration(t.Context(), id), strategy.ErrRecoveryConfigRequired) {
				t.Fatal("supported hot/display update was blocked or recovery owner lost")
			}
		})
	}
}

func TestGlobalRecoverySaveRejectsManagedAndReservedOwner(t *testing.T) {
	for _, kind := range []string{"managed", "reservation", "corrupt"} {
		t.Run(kind, func(t *testing.T) {
			scenario := "absent"
			if kind == "corrupt" {
				scenario = "corrupt"
			}
			store, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", scenario)
			if kind == "managed" {
				RegisterBotManagerProvider(&configMutationTestProvider{running: true})
			}
			if kind == "reservation" {
				seedConfigRecoveryClaim(t, store, id)
			}
			before, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			next, err := cloneConfigSnapshot(before)
			if err != nil {
				t.Fatal(err)
			}
			botCfgByID(next, id).Direction = "SHORT"
			err = fileConfigManager.updateConfigFromSnapshot(t.Context(), before, next, "fixture")
			var admission *globalRecoveryAdmissionError
			if !errors.As(err, &admission) {
				t.Fatal("unsafe owner admitted")
			}
			current, readErr := GetLatestConfig()
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !reflect.DeepEqual(current, before) {
				t.Fatal("rejected owner persisted")
			}
		})
	}
}

func TestGlobalRecoverySaveCancelsBeforeLifecycleAdmission(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	RegisterBotManagerProvider(&strategySaveContextProbe{wait: true})
	before, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cloneConfigSnapshot(before)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).Direction = "SHORT"
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	err = fileConfigManager.updateConfigFromSnapshot(ctx, before, next, "fixture")
	if !errors.Is(err, errConfigSaveCancelledBeforePersistence) {
		t.Fatal("cancelled wait not classified as prewrite")
	}
	current, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, before) {
		t.Fatal("cancelled wait persisted")
	}
}

type globalRecoveryLockProbe struct {
	configMutationTestProvider
	active []string
	order  []string
	check  func()
}

func (p *globalRecoveryLockProbe) WithBotStrategyConfigurationLock(id string, persist func(bool) error) error {
	p.order = append(p.order, id)
	p.active = append(p.active, id)
	defer func() { p.active = p.active[:len(p.active)-1] }()
	if p.check != nil {
		p.check()
	}
	return persist(false)
}

func TestGlobalRecoverySaveLocksAllOwnersSortedAndNotifiesAfterUnlock(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	before, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	second := *botCfgByID(before, id)
	second.ID = "a-owner"
	before.Bots = append(before.Bots, second)
	if err := fileConfigManager.UpdateConfig(before); err != nil {
		t.Fatal(err)
	}
	before, err = GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cloneConfigSnapshot(before)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).Direction = "SHORT"
	botCfgByID(next, second.ID).Direction = "SHORT"
	probe := &globalRecoveryLockProbe{}
	probe.check = func() {
		// The configuration lock must not already be held while acquiring Bot
		// locks; a read here also exercises the established Bot -> config order.
		if _, err := fileConfigManager.GetConfig(); err != nil {
			t.Fatal(err)
		}
	}
	RegisterBotManagerProvider(probe)
	oldNotify := newsMonitorRuntimeSync
	t.Cleanup(func() { newsMonitorRuntimeSync = oldNotify })
	notified := false
	newsMonitorRuntimeSync = func(*config.Config) {
		notified = true
		if len(probe.active) != 0 {
			t.Fatal("notification reentered under lifecycle locks")
		}
	}
	if err := fileConfigManager.updateConfigFromSnapshot(t.Context(), before, next, "fixture"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(probe.order, []string{second.ID, id}) || len(probe.active) != 0 || !notified {
		t.Fatal("sorted lock/notification boundary not preserved")
	}
	durable, err := loadConfigFromPrimaryDB()
	if err != nil {
		t.Fatal(err)
	}
	if botCfgByID(durable, id).Direction != "SHORT" || botCfgByID(durable, second.ID).Direction != "SHORT" {
		t.Fatal("verified cold change not saved")
	}
}

func TestGlobalRecoveryAffectedBotsRejectsAmbiguousIdentity(t *testing.T) {
	cfg := &config.Config{Bots: []config.BotConfig{{ID: "same"}, {ID: "same"}}}
	if _, err := globalRecoveryAffectedBots(cfg, cfg); err == nil {
		t.Fatal("duplicate identity silently overwritten")
	}
}

func TestGlobalRecoverySaveMissingCoordinatorDoesNotPersist(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	// A provider that can list Bots is not evidence of lifecycle coordination.
	RegisterBotManagerProvider(&mockBotManagerForDeleteGroupTest{})
	before, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	next, err := cloneConfigSnapshot(before)
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(next, id).Direction = "SHORT"
	w := callGlobalRecoverySave(t, "json", next)
	current, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusServiceUnavailable || !reflect.DeepEqual(current, before) {
		t.Fatal("missing lifecycle coordinator admitted cold mutation")
	}
}
