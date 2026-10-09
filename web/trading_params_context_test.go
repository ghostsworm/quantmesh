package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"gopkg.in/yaml.v3"
	"quantmesh/config"
)

type contextReportProbe struct {
	guardedReportProbe
	want         context.Context
	contextCalls int
	t            *testing.T
}

func (p *contextReportProbe) UpdateTradingParamsWithContext(ctx context.Context, cfg *config.Config, current func() bool) TradingParamsUpdateReport {
	p.contextCalls++
	if ctx != p.want {
		p.t.Error("request context lost before runtime dispatch")
	}
	return p.UpdateTradingParamsWithGuardedReport(cfg, current)
}

func TestConfigurationHandlersForwardApplicationRequestContext(t *testing.T) {
	for _, format := range []string{"strategy", "json", "yaml"} {
		t.Run(format, func(t *testing.T) {
			_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
			cfg, err := GetLatestConfig()
			if err != nil {
				t.Fatal(err)
			}
			body := []byte(`{"smart_order_max_open_orders":2}`)
			if format != "strategy" {
				body, err = yaml.Marshal(cfg)
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
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			previous, previousHR := symbolManagerProvider, configHotReloader
			probe := &contextReportProbe{want: ctx, t: t}
			symbolManagerProvider, configHotReloader = probe, nil
			t.Cleanup(func() { symbolManagerProvider, configHotReloader = previous, previousHR })
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/fixture", bytes.NewReader(body)).WithContext(ctx)
			c.Params = gin.Params{{Key: "id", Value: id}}
			switch format {
			case "strategy":
				putBotStrategy(c)
			case "json":
				updateConfigHandler(c)
			case "yaml":
				updateConfigYAMLHandler(c)
			}
			if w.Code != http.StatusOK || probe.contextCalls != 1 || probe.applications != len(cfg.Bots) {
				t.Fatalf("context application not forwarded: status=%d calls=%d", w.Code, probe.contextCalls)
			}
		})
	}
}
