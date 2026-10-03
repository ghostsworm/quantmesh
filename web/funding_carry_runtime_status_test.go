package web

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"quantmesh/config"
	"quantmesh/storage"
	"testing"
	"time"
)

type fundingDashboardStore struct{ storage.Storage }

func (fundingDashboardStore) GetFundingPaymentsSumByScope(string, string, string, string, string, time.Time, time.Time) (float64, error) {
	return 0, nil
}
func (fundingDashboardStore) GetDailyFundingPaymentsByAccountScopeAndAsset(string, string, string, string, time.Time, time.Time) (map[string]float64, error) {
	return map[string]float64{}, nil
}

type fundingDashboardStorageProvider struct{}

func (fundingDashboardStorageProvider) GetStorage() storage.Storage { return fundingDashboardStore{} }

func TestFundingCarryDashboardHTTPCountsOnlyVerifiedTrading(t *testing.T) {
	oldConfig, oldStorage, oldBots := fileConfigManager, storageServiceProvider, botManagerProvider()
	t.Cleanup(func() {
		fileConfigManager = oldConfig
		storageServiceProvider = oldStorage
		RegisterBotManagerProvider(oldBots)
	})
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"fake": {APIKey: "isolated-fixture-not-a-credential"}}}
	bots := []BotResponse{
		{BotID: "running", Running: true, FundingCarryRuntime: &FundingCarryRuntimeStatus{TradingRunning: true}},
		{BotID: "reconciling", Running: true, FundingCarryRuntime: &FundingCarryRuntimeStatus{ReconciliationRequired: true}},
		{BotID: "unknown", Running: true},
		{BotID: "stopped"},
	}
	for _, bot := range bots {
		cfg.Bots = append(cfg.Bots, config.BotConfig{ID: bot.BotID, Exchange: "fake", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 10})
	}
	fileConfigManager = NewFileConfigManager("")
	if err := fileConfigManager.SetRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	storageServiceProvider = fundingDashboardStorageProvider{}
	RegisterBotManagerProvider(&mockBotManagerForGetBotsTest{bots: bots})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest("GET", "/api/funding-carry/dashboard", nil)
	getFundingCarryDashboard(ctx)
	var body struct {
		Overview struct {
			ActiveBots int `json:"active_bots"`
		}
		Symbols []fundingCarryDashboardSymbolInfo
	}
	if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &body) != nil || body.Overview.ActiveBots != 1 || len(body.Symbols) != 4 {
		t.Fatalf("incorrect dashboard: %s", recorder.Body.String())
	}
	want := []string{"running", "reconciliation_required", "unknown", "stopped"}
	for i, sym := range body.Symbols {
		if sym.Status != want[i] {
			t.Fatalf("status %s want %s", sym.Status, want[i])
		}
	}
}

type fundingStatusProvider struct {
	BotManagerProvider
	detail *BotDetailResponse
}

func (p fundingStatusProvider) GetBot(string) (*BotDetailResponse, bool) { return p.detail, true }

func TestFundingCarryStatusHTTPDoesNotReportManagedReconciliationAsRunning(t *testing.T) {
	previous := botManagerProvider()
	t.Cleanup(func() { RegisterBotManagerProvider(previous) })
	RegisterBotManagerProvider(fundingStatusProvider{detail: &BotDetailResponse{BotResponse: BotResponse{
		BotID: "bot-a", Running: true, FundingCarryRuntime: &FundingCarryRuntimeStatus{ReconciliationRequired: true},
	}}})
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Params = gin.Params{{Key: "botId", Value: "bot-a"}}
	getFundingCarryStatus(ctx)
	var body struct {
		Managed bool
		Running bool
		Status  string
	}
	if recorder.Code != 200 || json.Unmarshal(recorder.Body.Bytes(), &body) != nil || !body.Managed || body.Running || body.Status != "reconciliation_required" {
		t.Fatalf("incorrect HTTP state: %s", recorder.Body.String())
	}
}

func TestFundingCarryDashboardSeparatesManagedAndTrading(t *testing.T) {
	for _, tc := range []struct {
		name    string
		managed bool
		runtime *FundingCarryRuntimeStatus
		want    string
	}{
		{"unmanaged", false, nil, "stopped"},
		{"managed_without_evidence", true, nil, "unknown"},
		{"managed_paused", true, &FundingCarryRuntimeStatus{}, "stopped"},
		{"managed_reconciliation", true, &FundingCarryRuntimeStatus{ReconciliationRequired: true}, "reconciliation_required"},
		{"running_but_unverified", true, &FundingCarryRuntimeStatus{TradingRunning: true, ReconciliationRequired: true}, "reconciliation_required"},
		{"verified_loop", true, &FundingCarryRuntimeStatus{TradingRunning: true}, "running"},
		{"removed_with_old_evidence", false, &FundingCarryRuntimeStatus{TradingRunning: true}, "stopped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := fundingCarryDashboardStatus(BotResponse{Running: tc.managed, FundingCarryRuntime: tc.runtime}); got != tc.want {
				t.Fatalf("status=%s want=%s", got, tc.want)
			}
		})
	}
}
