package web

import (
	"encoding/json"
	"net/http"
	"quantmesh/config"
	"testing"
)

type reportUpdateProbe struct {
	SymbolManagerProvider
	report                   TradingParamsUpdateReport
	reportCalls, legacyCalls int
	beforeReport             func()
}

func (p *reportUpdateProbe) UpdateTradingParamsWithReport(*config.Config) TradingParamsUpdateReport {
	if p.beforeReport != nil {
		p.beforeReport()
	}
	p.reportCalls++
	return p.report
}

func TestStrategyHotApplicationRunsOutsidePersistenceLifecycle(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	coordinator := &configMutationTestProvider{}
	previousManager := botManagerProvider()
	RegisterBotManagerProvider(coordinator)
	t.Cleanup(func() { RegisterBotManagerProvider(previousManager) })
	previous := symbolManagerProvider
	probe := &reportUpdateProbe{report: TradingParamsUpdateReport{Verified: true, Applied: []string{id}, Failed: map[string]string{}, NotRunning: []string{}}}
	probe.beforeReport = func() {
		if coordinator.inside {
			t.Error("runtime application re-enters the lifecycle lock still held by persistence")
		}
		cfg, err := GetLatestConfig()
		if err != nil || botCfgByID(cfg, id).SmartOrder.MaxOpenOrders != 2 {
			t.Fatal("runtime application preceded persisted configuration")
		}
	}
	symbolManagerProvider = probe
	t.Cleanup(func() { symbolManagerProvider = previous })
	w := callStrategyMutation(id, `{"smart_order_max_open_orders":2}`)
	if w.Code != http.StatusOK || probe.reportCalls != 1 {
		t.Fatalf("saved hot update not applied once: status=%d calls=%d", w.Code, probe.reportCalls)
	}
}
func (p *reportUpdateProbe) UpdateTradingParams(*config.Config) []string {
	p.legacyCalls++
	return []string{"not-proof"}
}

func TestStrategyUpdateReportsSavedButRuntimeFailure(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	previous := symbolManagerProvider
	probe := &reportUpdateProbe{report: TradingParamsUpdateReport{Verified: true, Applied: []string{"another-bot"}, Failed: map[string]string{id: "risk_apply_failed"}, NotRunning: []string{}}}
	symbolManagerProvider = probe
	t.Cleanup(func() { symbolManagerProvider = previous })
	w := callStrategyMutation(id, `{"smart_order_max_open_orders":2}`)
	if w.Code != http.StatusConflict || probe.reportCalls != 1 || probe.legacyCalls != 0 {
		t.Fatalf("runtime failure hidden or double dispatched: %d", w.Code)
	}
	var response struct {
		OK     bool                      `json:"ok"`
		Saved  bool                      `json:"config_saved"`
		Report TradingParamsUpdateReport `json:"runtime_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.OK || !response.Saved || !response.Report.Verified || response.Report.Failed[id] != "risk_apply_failed" || len(response.Report.Applied) != 1 {
		t.Fatal("partial application report lost")
	}
	cfg, err := GetLatestConfig()
	if err != nil || botCfgByID(cfg, id).SmartOrder.MaxOpenOrders != 2 {
		t.Fatal("saved configuration not read back")
	}
}

func TestStrategyUpdateLegacyAndAbsentProviderRemainUnverified(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		t.Run(map[bool]string{true: "legacy", false: "absent"}[legacy], func(t *testing.T) {
			_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
			previous := symbolManagerProvider
			symbolManagerProvider = nil
			probe := &strategyHotUpdateProbe{}
			if legacy {
				symbolManagerProvider = probe
			}
			t.Cleanup(func() { symbolManagerProvider = previous })
			w := callStrategyMutation(id, `{"smart_order_max_open_orders":2}`)
			var response struct {
				OK     bool                      `json:"ok"`
				Saved  bool                      `json:"config_saved"`
				Report TradingParamsUpdateReport `json:"runtime_update"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if w.Code != http.StatusOK || response.OK || !response.Saved || response.Report.Verified || len(response.Report.Applied) != 0 {
				t.Fatal("unknown application claimed success")
			}
			if legacy && probe.calls != 1 {
				t.Fatal("legacy dispatch was removed")
			}
		})
	}
}
