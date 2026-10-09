package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
)

type guardedReportProbe struct {
	SymbolManagerProvider
	calls, applications int
}

func TestJSONSaveUsesGuardedReport(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(cfg, id).Name = "fresh-json-save"
	data, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var values map[string]interface{}
	if err := yaml.Unmarshal(data, &values); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(values)
	if err != nil {
		t.Fatal(err)
	}
	previous, previousHR := symbolManagerProvider, configHotReloader
	probe := &guardedReportProbe{}
	symbolManagerProvider, configHotReloader = probe, nil
	t.Cleanup(func() { symbolManagerProvider, configHotReloader = previous, previousHR })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	updateConfigHandler(c)
	if w.Code != http.StatusOK || probe.calls != 1 || probe.applications != len(cfg.Bots) {
		t.Fatalf("fresh JSON configuration rejected by snapshot guard: status=%d applications=%d", w.Code, probe.applications)
	}
}

func (p *guardedReportProbe) UpdateTradingParamsWithGuardedReport(cfg *config.Config, current func() bool) TradingParamsUpdateReport {
	p.calls++
	report := TradingParamsUpdateReport{Verified: true, Applied: []string{}, Failed: map[string]string{}, NotRunning: []string{}}
	for _, bot := range cfg.Bots {
		id := config.BotIDOrGenerate(bot)
		if !current() {
			report.Failed[id] = "runtime_configuration_changed"
			continue
		}
		p.applications++
		report.Applied = append(report.Applied, id)
	}
	return report
}

func TestPersistedHotReportRejectsSupersededSnapshot(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	previous := symbolManagerProvider
	probe := &guardedReportProbe{}
	symbolManagerProvider = probe
	t.Cleanup(func() { symbolManagerProvider = previous })
	old, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	newer, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(newer, id).SmartOrder.MaxOpenOrders = 3
	if err := fileConfigManager.UpdateConfig(newer); err != nil {
		t.Fatal(err)
	}
	report := applyTradingParamsWithReport(old)
	if probe.applications != 0 || len(report.Applied) != 0 || report.Failed[id] != "runtime_configuration_changed" {
		t.Fatalf("superseded persisted snapshot applied: %+v", report)
	}
	report = applyTradingParamsWithReport(newer)
	if probe.calls != 2 || probe.applications != len(newer.Bots) || len(report.Applied) != len(newer.Bots) || len(report.Failed) != 0 {
		t.Fatalf("latest snapshot application removed: %+v", report)
	}
}

func TestStrategySaveUsesGuardedReport(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	previous := symbolManagerProvider
	probe := &guardedReportProbe{}
	symbolManagerProvider = probe
	t.Cleanup(func() { symbolManagerProvider = previous })
	w := callStrategyMutation(id, `{"smart_order_max_open_orders":2}`)
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || probe.calls != 1 || probe.applications != len(latest.Bots) {
		t.Fatalf("actual saved strategy snapshot failed freshness verification: status=%d applications=%d", w.Code, probe.applications)
	}
}
