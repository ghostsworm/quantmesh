package web

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
)

func TestConfigurationSavedButHotReloadFailureIsReported(t *testing.T) {
	for _, format := range []string{"json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			setupBotCreateTestEnv(t)
			const botID = "report-config-fixture"
			seedBotForMergeTest(t, botID)
			cfg, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			hr := config.NewHotReloader(cfg)
			hr.RegisterCallback(func(*config.Config, *config.Config, []config.ConfigChange) error {
				return errors.New("private-hot-reload-fixture")
			})
			previousHR := configHotReloader
			configHotReloader = hr
			t.Cleanup(func() { configHotReloader = previousHR })
			previousProvider := symbolManagerProvider
			probe := &reportUpdateProbe{report: TradingParamsUpdateReport{Verified: true, Applied: []string{}, Failed: map[string]string{}, NotRunning: []string{}}}
			symbolManagerProvider = probe
			t.Cleanup(func() { symbolManagerProvider = previousProvider })
			botCfgByID(cfg, botID).Name = "saved-before-runtime-failure"
			data, err := yaml.Marshal(cfg)
			if err != nil {
				t.Fatal(err)
			}
			body := data
			if format == "json" {
				var values map[string]interface{}
				if err := yaml.Unmarshal(data, &values); err != nil {
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
			c.Request.Header.Set("Content-Type", "application/json")
			if format == "json" {
				updateConfigHandler(c)
			} else {
				updateConfigYAMLHandler(c)
			}
			if w.Code != http.StatusConflict || strings.Contains(w.Body.String(), "private-hot-reload-fixture") {
				t.Fatalf("reload failure hidden or leaked: %d", w.Code)
			}
			var response struct {
				Saved  bool `json:"config_saved"`
				Failed bool `json:"hot_reload_failed"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			latest, err := GetLatestConfig()
			if !response.Saved || !response.Failed || err != nil || botCfgByID(latest, botID).Name != "saved-before-runtime-failure" {
				t.Fatal("saved versus applied readback incorrect")
			}
		})
	}
}

func TestJSONConfigurationReportsPerBotApplicationFailure(t *testing.T) {
	setupBotCreateTestEnv(t)
	const botID = "json-runtime-report"
	seedBotForMergeTest(t, botID)
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	botCfgByID(cfg, botID).Name = "saved-with-partial-application"
	previousHR, previousProvider := configHotReloader, symbolManagerProvider
	configHotReloader = nil
	probe := &reportUpdateProbe{report: TradingParamsUpdateReport{Verified: true, Applied: []string{"other-bot"}, Failed: map[string]string{botID: "risk_apply_failed"}, NotRunning: []string{}}}
	symbolManagerProvider = probe
	t.Cleanup(func() { configHotReloader, symbolManagerProvider = previousHR, previousProvider })
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
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	updateConfigHandler(c)
	var response struct {
		Saved  bool                      `json:"config_saved"`
		Report TradingParamsUpdateReport `json:"runtime_update"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	latest, err := GetLatestConfig()
	if w.Code != http.StatusConflict || !response.Saved || response.Report.Failed[botID] != "risk_apply_failed" || len(response.Report.Applied) != 1 || err != nil || botCfgByID(latest, botID).Name != "saved-with-partial-application" {
		t.Fatal("partial JSON runtime application hidden or saved config lost")
	}
}
