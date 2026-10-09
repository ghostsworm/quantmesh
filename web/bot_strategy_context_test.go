package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
)

type strategySaveContextProbe struct {
	configMutationTestProvider
	wait            bool
	cancelAfterSave context.CancelFunc
	contextCalls    int
}

func (p *strategySaveContextProbe) WithBotStrategyConfigurationContext(ctx context.Context, botID string, persist func(bool) error) error {
	p.contextCalls++
	if p.wait {
		<-ctx.Done()
		return ctx.Err()
	}
	if err := p.WithBotStrategyConfigurationLock(botID, persist); err != nil {
		return err
	}
	if p.cancelAfterSave != nil {
		p.cancelAfterSave()
		return ctx.Err()
	}
	return nil
}

type cancelledSaveReportProbe struct{ SymbolManagerProvider }

func (*cancelledSaveReportProbe) UpdateTradingParamsWithContext(ctx context.Context, cfg *config.Config, _ func() bool) TradingParamsUpdateReport {
	report := TradingParamsUpdateReport{Verified: true, Applied: []string{}, Failed: map[string]string{}, NotRunning: []string{}}
	if ctx.Err() != nil {
		for _, bot := range cfg.Bots {
			report.Failed[config.BotIDOrGenerate(bot)] = "runtime_application_cancelled"
		}
	}
	return report
}

func TestStrategySaveCancellationBeforePersistencePreservesConfig(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	before, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	previous := botManagerProvider()
	probe := &strategySaveContextProbe{wait: true}
	RegisterBotManagerProvider(probe)
	t.Cleanup(func() { RegisterBotManagerProvider(previous) })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	w := callStrategyWithContext(id, ctx)
	latest, err := GetLatestConfig()
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Saved bool   `json:"config_saved"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusRequestTimeout || response.Saved || response.Error != "bot_configuration_cancelled" || probe.contextCalls != 1 || !reflect.DeepEqual(before, latest) {
		t.Fatal("cancelled strategy request persisted or lost contextual coordination")
	}
}

func TestStrategyCancellationAfterPersistenceStillReportsSaved(t *testing.T) {
	_, id, _ := seedFinancialRecoveryFixture(t, "PUT_config", "absent")
	previous, previousSymbols := botManagerProvider(), symbolManagerProvider
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	probe := &strategySaveContextProbe{cancelAfterSave: cancel}
	RegisterBotManagerProvider(probe)
	symbolManagerProvider = &cancelledSaveReportProbe{}
	t.Cleanup(func() { RegisterBotManagerProvider(previous); symbolManagerProvider = previousSymbols })
	w := callStrategyWithContext(id, ctx)
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
	if w.Code != http.StatusConflict || !response.Saved || response.Report.Failed[id] != "runtime_application_cancelled" || botCfgByID(latest, id).SmartOrder.MaxOpenOrders != 2 {
		t.Fatal("saved data misreported as rolled back after cancellation")
	}
}

func callStrategyWithContext(id string, ctx context.Context) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPut, "/fixture", bytes.NewBufferString(`{"smart_order_max_open_orders":2}`)).WithContext(ctx)
	c.Params = gin.Params{{Key: "id", Value: id}}
	putBotStrategy(c)
	return w
}
