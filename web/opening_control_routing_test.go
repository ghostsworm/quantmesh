package web

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

func TestOpeningEndpointsRejectAmbiguousMissingAndMismatchedTargets(t *testing.T) {
	oldFCM, oldProvider := fileConfigManager, symbolManagerProvider
	t.Cleanup(func() { fileConfigManager, symbolManagerProvider = oldFCM, oldProvider })
	for _, endpoint := range []struct {
		name    string
		handler gin.HandlerFunc
		read    bool
	}{
		{"status", getOpeningControlStatus, true}, {"config", getOpeningControlConfig, true},
		{"pause", pauseOpening, false}, {"resume", resumeOpening, false},
	} {
		for _, scenario := range []string{"missing_id", "mismatched_symbol", "ambiguous", "invalid_market", "missing_provider", "missing_config", "runtime_mismatch", "stopped"} {
			t.Run(endpoint.name+"/"+scenario, func(t *testing.T) {
				cfg := newRiskPersistenceConfig()
				fileConfigManager = &FileConfigManager{currentConfig: cfg}
				symbolManagerProvider = openingTestProvider{}
				query := "exchange=binance&symbol=BTCUSDT&bot_id=risk-test"
				want := http.StatusConflict
				switch scenario {
				case "missing_id":
					query = "exchange=binance&symbol=BTCUSDT&bot_id=missing"
				case "mismatched_symbol":
					query = "exchange=binance&symbol=ETHUSDT&bot_id=risk-test"
				case "ambiguous":
					other := cfg.Bots[0]
					other.ID = "second"
					cfg.Bots = append(cfg.Bots, other)
					query = "exchange=binance&symbol=BTCUSDT"
				case "invalid_market":
					query += "&market_type=typo"
					want = http.StatusBadRequest
				case "missing_provider":
					symbolManagerProvider = nil
					want = http.StatusServiceUnavailable
				case "missing_config":
					fileConfigManager = nil
					want = http.StatusServiceUnavailable
				case "runtime_mismatch":
					symbolManagerProvider = openingTestProvider{runtime: &struct{ Config config.SymbolConfig }{Config: config.SymbolConfig{Exchange: "binance", Symbol: "ETHUSDT"}}}
				case "stopped":
					if endpoint.read {
						want = http.StatusOK
					}
				}
				w := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(w)
				c.Request = httptest.NewRequest(http.MethodGet, "/?"+query, nil)
				endpoint.handler(c)
				if w.Code != want {
					t.Fatalf("got %d want %d: %s", w.Code, want, w.Body)
				}
				decoder := json.NewDecoder(w.Body)
				var response map[string]interface{}
				if err := decoder.Decode(&response); err != nil {
					t.Fatalf("missing JSON response: %v", err)
				}
				if err := decoder.Decode(&response); err != io.EOF {
					t.Fatalf("appended second response: %v", err)
				}
				if scenario == "stopped" && endpoint.name == "status" && response["pause_reason"] != "bot_stopped" {
					t.Fatal("stopped state not explicit")
				}
			})
		}
	}
}
