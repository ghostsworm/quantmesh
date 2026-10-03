package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

const mergeTestCreatedAt = "2026-01-02T03:04:05Z"

// seedBotForMergeTest 在主配置中放入一個 Enabled=false、帶 CreatedAt 的 Bot
func seedBotForMergeTest(t *testing.T, botID string) {
	t.Helper()
	cfg, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	disabled := false
	cfg.Bots = append(cfg.Bots, config.BotConfig{
		ID:            botID,
		Name:          "old",
		CreatedAt:     mergeTestCreatedAt,
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		MarketType:    "futures",
		Enabled:       &disabled,
		PriceInterval: 100,
	})
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func findBotInLatest(t *testing.T, botID string) config.BotConfig {
	t.Helper()
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	var found []config.BotConfig
	for _, b := range latest.Bots {
		if b.ID == botID {
			found = append(found, b)
		}
	}
	if len(found) != 1 {
		t.Fatalf("expected exactly 1 bot %s, got %d", botID, len(found))
	}
	return found[0]
}

func assertMergedBot(t *testing.T, b config.BotConfig, wantName string, wantInterval float64) {
	t.Helper()
	if b.Enabled == nil || *b.Enabled {
		t.Fatalf("Enabled must survive update as false, got %v", b.Enabled)
	}
	if b.CreatedAt != mergeTestCreatedAt {
		t.Fatalf("CreatedAt must survive update, got %q", b.CreatedAt)
	}
	if b.Name != wantName || b.PriceInterval != wantInterval {
		t.Fatalf("updated fields not applied: name=%q interval=%v", b.Name, b.PriceInterval)
	}
}

func TestPutBotConfigFilePreservesEnabledAndCreatedAt(t *testing.T) {
	setupBotCreateTestEnv(t)
	previous := botManagerProvider()
	RegisterBotManagerProvider(&configMutationTestProvider{})
	t.Cleanup(func() { RegisterBotManagerProvider(previous) })
	const botID = "merge-b1"
	seedBotForMergeTest(t, botID)

	body := `{"name":"new","exchange":"binance","symbol":"BTCUSDT","market_type":"futures",
		"grid":{"price_interval":250,"order_quantity":50}}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Params = gin.Params{{Key: "id", Value: botID}}
	c.Request = httptest.NewRequest(http.MethodPut, "/api/bots/"+botID+"/config-file", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	putBotConfigFile(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	assertMergedBot(t, findBotInLatest(t, botID), "new", 250)
}

func TestSyncBotConfigToMainPreservesEnabledAndCreatedAt(t *testing.T) {
	setupBotCreateTestEnv(t)
	const botID = "merge-b2"
	seedBotForMergeTest(t, botID)

	syncBotConfigToMain(botID, &config.BotConfigFile{
		BotID: botID, Name: "synced", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures",
		Grid: config.GridConfig{PriceInterval: 300},
	})
	assertMergedBot(t, findBotInLatest(t, botID), "synced", 300)
}
