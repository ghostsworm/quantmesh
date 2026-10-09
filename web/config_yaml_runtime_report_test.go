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

func TestYAMLSaveUsesGuardedReport(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(cfg, id).Name = "yaml-guarded"
	body, err := yaml.Marshal(cfg)
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
	updateConfigYAMLHandler(c)
	if w.Code != http.StatusOK || probe.calls != 1 || probe.applications != len(cfg.Bots) {
		t.Fatalf("YAML save omitted guarded runtime application: status=%d calls=%d applications=%d", w.Code, probe.calls, probe.applications)
	}
	var response struct {
		OK     bool                      `json:"ok"`
		Saved  bool                      `json:"config_saved"`
		Report TradingParamsUpdateReport `json:"runtime_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.OK || !response.Saved || !response.Report.Verified || len(response.Report.Applied) != len(cfg.Bots) {
		t.Fatal("YAML application report lost")
	}
}

func TestYAMLSaveRejectsSnapshotSupersededBeforeRuntimeDispatch(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(cfg, id).Name = "older-yaml"
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	previous, previousHR := symbolManagerProvider, configHotReloader
	probe := &guardedReportProbe{}
	hr := config.NewHotReloader(cfg)
	hr.RegisterCallback(func(_, _ *config.Config, _ []config.ConfigChange) error {
		newer, err := GetLatestConfig()
		if err != nil {
			return err
		}
		botCfgByID(newer, id).Name = "newer-save"
		return fileConfigManager.UpdateConfig(newer)
	})
	symbolManagerProvider, configHotReloader = probe, hr
	t.Cleanup(func() { symbolManagerProvider, configHotReloader = previous, previousHR })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body))
	updateConfigYAMLHandler(c)
	var response struct {
		Saved  bool                      `json:"config_saved"`
		Report TradingParamsUpdateReport `json:"runtime_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || !response.Saved || probe.applications != 0 || response.Report.Failed[id] != "runtime_configuration_changed" || botCfgByID(latest, id).Name != "newer-save" {
		t.Fatal("overtaken YAML snapshot applied or replaced newer persisted data")
	}
}

func TestYAMLSaveReportsRuntimeFailureAfterPersistence(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(cfg, id).Name = "yaml-saved-before-rejection"
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	previous, previousHR := symbolManagerProvider, configHotReloader
	probe := &reportUpdateProbe{report: TradingParamsUpdateReport{Verified: true, Applied: []string{}, Failed: map[string]string{id: "runtime_restart_required"}, NotRunning: []string{}}}
	symbolManagerProvider, configHotReloader = probe, nil
	t.Cleanup(func() { symbolManagerProvider, configHotReloader = previous, previousHR })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body))
	updateConfigYAMLHandler(c)
	var response struct {
		OK     bool                      `json:"ok"`
		Saved  bool                      `json:"config_saved"`
		Error  string                    `json:"error"`
		Report TradingParamsUpdateReport `json:"runtime_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusConflict || response.OK || !response.Saved || response.Error != "runtime_configuration_apply_failed" || response.Report.Failed[id] != "runtime_restart_required" || botCfgByID(latest, id).Name != "yaml-saved-before-rejection" {
		t.Fatal("YAML hid runtime rejection or lost saved configuration")
	}
}

func TestYAMLSaveAbsentRuntimeProviderRemainsUnverified(t *testing.T) {
	_, _, _ = seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	body, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	previous, previousHR := symbolManagerProvider, configHotReloader
	symbolManagerProvider, configHotReloader = nil, nil
	t.Cleanup(func() { symbolManagerProvider, configHotReloader = previous, previousHR })
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body))
	updateConfigYAMLHandler(c)
	var response struct {
		OK     bool                      `json:"ok"`
		Saved  bool                      `json:"config_saved"`
		Report TradingParamsUpdateReport `json:"runtime_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || response.OK || !response.Saved || response.Report.Verified || len(response.Report.Applied) != 0 {
		t.Fatal("YAML saved snapshot claimed absent provider applied it")
	}
}
