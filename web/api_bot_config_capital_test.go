package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/storage"
)

type configMutationTestProvider struct {
	mockBotManagerForDeleteGroupTest
	inside  bool
	before  func()
	running bool
}

func (p *configMutationTestProvider) GetBot(botID string) (*BotDetailResponse, bool) {
	return &BotDetailResponse{BotResponse: BotResponse{BotID: botID, Running: p.running}}, true
}

func (p *configMutationTestProvider) WithBotConfigurationLock(_ string, persist func() error) error {
	p.inside = true
	defer func() { p.inside = false }()
	if p.before != nil {
		p.before()
	}
	return persist()
}

func TestBotConfigFileCannotDiscardRecoveryWithCapitalClaim(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			setupBotCreateTestEnv(t)
			const botID = "config-recovery"
			seedBotForMergeTest(t, botID)
			original := &config.BotConfigFile{BotID: botID, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", Name: "recovery-original"}
			if err := saveBotConfigUnified(original, "fixture", "fixture"); err != nil {
				t.Fatal(err)
			}
			store := primaryStorageForAppConfig.(*storage.SQLStorage)
			before, err := store.GetBotConfigDocument(context.Background(), botID)
			if err != nil || before == nil {
				t.Fatalf("seed document: %v", err)
			}
			claim := seedConfigRecoveryClaim(t, store, botID)
			provider := &configMutationTestProvider{}
			previous := botManagerProvider()
			RegisterBotManagerProvider(provider)
			t.Cleanup(func() { RegisterBotManagerProvider(previous) })
			w := callConfigFileMutation(method, botID)
			if w.Code != http.StatusConflict {
				t.Fatalf("stopped Bot with recovery claim must reject %s: status=%d body=%s", method, w.Code, w.Body.String())
			}
			after, err := store.GetBotConfigDocument(context.Background(), botID)
			if err != nil || after == nil || after.Content != before.Content {
				t.Fatalf("durable recovery configuration changed: %v", err)
			}
			if found, err := store.HasAccountWalletCapitalReservation(context.Background(), botID); err != nil || !found {
				t.Fatalf("claim changed after rejected mutation: %v", err)
			}
			rows, err := store.ListAccountWalletCapitalReservations(context.Background(), "", "", 10)
			if err != nil || len(rows) != 1 || rows[0].Amount != claim.Amount {
				t.Fatalf("wallet budget changed: %v", err)
			}
			if b := findBotInLatest(t, botID); b.Name != "old" || b.Symbol != "BTCUSDT" {
				t.Fatal("primary Bot config changed after rejection")
			}
		})
	}
}

func seedConfigRecoveryClaim(t *testing.T, store *storage.SQLStorage, botID string) storage.AccountWalletCapitalClaim {
	t.Helper()
	claim := storage.AccountWalletCapitalClaim{WalletKey: strings.Repeat("a", 64), ReservationToken: strings.Repeat("b", 64), Amount: 50, Available: 100,
		Exchange: "binance", Market: "futures", QuoteAsset: "USDT", Symbol: "BTCUSDT"}
	seq, err := store.BeginAccountWalletBalanceObservation(context.Background(), claim.WalletKey)
	if err != nil {
		t.Fatal(err)
	}
	claim.ObservationSequence = seq
	if err := store.ReserveAccountWalletCapital(context.Background(), botID, []storage.AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	return claim
}

func callConfigFileMutation(method, botID string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: botID}}
	c.Request = httptest.NewRequest(method, "/api/bots/"+botID+"/config-file", strings.NewReader(`{"name":"replacement","exchange":"binance","symbol":"ETHUSDT","market_type":"futures"}`))
	c.Request.Header.Set("Content-Type", "application/json")
	if method == http.MethodPut {
		putBotConfigFile(c)
	} else {
		deleteBotConfigFile(c)
	}
	return w
}

func TestBotConfigMutationRechecksAfterLifecycleAdmission(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			setupBotCreateTestEnv(t)
			store := primaryStorageForAppConfig.(*storage.SQLStorage)
			provider := &configMutationTestProvider{}
			provider.before = func() {
				if !provider.inside {
					t.Fatal("new claim not injected inside lifecycle lock")
				}
				seedConfigRecoveryClaim(t, store, "late-claim")
			}
			previous := botManagerProvider()
			RegisterBotManagerProvider(provider)
			t.Cleanup(func() { RegisterBotManagerProvider(previous) })
			w := callConfigFileMutation(method, "late-claim")
			if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "bot_capital_reservation_not_released") {
				t.Fatalf("claim established during admission bypassed guard: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestBotConfigMutationFailsClosedWithoutVerificationOrCoordination(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		for _, failure := range []string{"missing_coordinator", "storage_read_failed", "runtime_started"} {
			t.Run(method+"/"+failure, func(t *testing.T) {
				setupBotCreateTestEnv(t)
				provider := &configMutationTestProvider{}
				previous := botManagerProvider()
				t.Cleanup(func() { RegisterBotManagerProvider(previous) })
				want := http.StatusServiceUnavailable
				switch failure {
				case "missing_coordinator":
					RegisterBotManagerProvider(&mockBotManagerForDeleteGroupTest{})
				case "storage_read_failed":
					store := primaryStorageForAppConfig.(*storage.SQLStorage)
					provider.before = func() {
						if err := store.Close(); err != nil {
							t.Fatal(err)
						}
					}
					RegisterBotManagerProvider(provider)
				case "runtime_started":
					provider.before = func() { provider.running = true }
					RegisterBotManagerProvider(provider)
					want = http.StatusConflict
				}
				w := callConfigFileMutation(method, "guarded-bot")
				if w.Code != want {
					t.Fatalf("unsafe mutation admitted: %d %s", w.Code, w.Body.String())
				}
			})
		}
	}
}

func TestBotConfigMutationAllowsConfirmedUnreservedStoppedBot(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			setupBotCreateTestEnv(t)
			const botID = "unreserved-config"
			seedBotForMergeTest(t, botID)
			if err := saveBotConfigUnified(&config.BotConfigFile{BotID: botID, Name: "old", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}, "fixture", "fixture"); err != nil {
				t.Fatal(err)
			}
			previous := botManagerProvider()
			RegisterBotManagerProvider(&configMutationTestProvider{})
			t.Cleanup(func() { RegisterBotManagerProvider(previous) })
			w := callConfigFileMutation(method, botID)
			if w.Code != http.StatusOK {
				t.Fatalf("confirmed unreserved stopped Bot rejected: %d %s", w.Code, w.Body.String())
			}
			store := primaryStorageForAppConfig.(*storage.SQLStorage)
			doc, err := store.GetBotConfigDocument(context.Background(), botID)
			if err != nil {
				t.Fatal(err)
			}
			if method == http.MethodDelete && doc != nil {
				t.Fatal("successful delete retained document")
			}
			if method == http.MethodPut && (doc == nil || !strings.Contains(doc.Content, "ETHUSDT")) {
				t.Fatal("successful update did not persist new document")
			}
		})
	}
}
