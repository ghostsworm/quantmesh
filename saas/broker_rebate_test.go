package saas

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBrokerRebateManagerLifecycleAndHandlers(t *testing.T) {
	manager := NewBrokerRebateManager()
	if err := manager.AddBrokerConfig(&BrokerConfig{}); err == nil {
		t.Fatalf("invalid broker config should fail")
	}
	cfg := &BrokerConfig{Exchange: "binance", BrokerID: "broker-1", SecretKey: "secret", TotalRebateRate: 20, Enabled: true}
	if err := manager.AddBrokerConfig(cfg); err != nil {
		t.Fatalf("add config: %v", err)
	}
	if manager.GetBrokerConfig("binance") != cfg {
		t.Fatalf("config lookup mismatch")
	}
	for _, exchange := range []string{"binance", "okx", "bybit", "bitmex", "bitget", "gate", "custom"} {
		if _, err := manager.GenerateInviteLink(exchange, "user-"+exchange); exchange == "binance" && err != nil {
			t.Fatalf("binance invite link: %v", err)
		}
	}
	if _, err := manager.GenerateInviteLink("missing", "user"); err == nil {
		t.Fatalf("missing config should fail")
	}

	record, err := manager.RecordTrade(context.Background(), &TradeInfo{
		Exchange: "binance", UserID: "user-binance", TradeID: "trade-1", Symbol: "BTCUSDT", Side: "BUY", Volume: 1000, Commission: 10, RebateType: "api",
	})
	if err != nil || record == nil || record.RebateAmount != 2 {
		t.Fatalf("record=%#v err=%v", record, err)
	}
	if disabled, err := manager.RecordTrade(context.Background(), &TradeInfo{Exchange: "unknown"}); err != nil || disabled != nil {
		t.Fatalf("disabled rebate record=%#v err=%v", disabled, err)
	}
	if user := manager.GetUserRebate("user-binance"); user == nil || user.PendingRebate != 2 {
		t.Fatalf("user rebate=%#v", user)
	}
	if pending := manager.GetPendingRebates(); len(pending) != 1 {
		t.Fatalf("pending len=%d", len(pending))
	}
	if history := manager.GetUserRebateHistory("user-binance"); len(history) != 1 {
		t.Fatalf("history len=%d", len(history))
	}
	stats := manager.GetRebateStats()
	if stats.TradeCount != 1 || stats.UserCount == 0 || stats.ByExchange["binance"].Rebate != 2 {
		t.Fatalf("stats=%#v", stats)
	}
	if err := manager.SettleRebates(context.Background()); err != nil {
		t.Fatalf("settle: %v", err)
	}
	if user := manager.GetUserRebate("user-binance"); user.PaidRebate != 2 || user.PendingRebate != 0 {
		t.Fatalf("settled user=%#v", user)
	}
	if len(generateInviteCode("user", "broker")) != 8 || !strings.HasPrefix(generateRecordID(), "RB") || len(generateRandomString(5)) != 5 {
		t.Fatalf("id helpers returned unexpected values")
	}
	if manager.signRequest("a=1", "secret") != manager.signRequestHMAC("a=1", "secret") {
		t.Fatalf("sign helpers should match")
	}

	handler := NewBrokerRebateHandler(manager)
	w := httptest.NewRecorder()
	handler.HandleGetStats(w, httptest.NewRequest(http.MethodGet, "/stats", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("stats handler code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.HandleGetUserRebate(w, httptest.NewRequest(http.MethodGet, "/user", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("missing user code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.HandleGetUserRebate(w, httptest.NewRequest(http.MethodGet, "/user?user_id=user-binance", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("user handler code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.HandleGenerateInviteLink(w, httptest.NewRequest(http.MethodPost, "/invite", bytes.NewBufferString(`bad`)))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("bad invite code=%d", w.Code)
	}
	body, _ := json.Marshal(map[string]string{"exchange": "binance", "user_id": "handler-user"})
	w = httptest.NewRecorder()
	handler.HandleGenerateInviteLink(w, httptest.NewRequest(http.MethodPost, "/invite", bytes.NewReader(body)))
	if w.Code != http.StatusOK {
		t.Fatalf("invite handler code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.HandleGetPendingRebates(w, httptest.NewRequest(http.MethodGet, "/pending", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("pending handler code=%d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.HandleSettleRebates(w, httptest.NewRequest(http.MethodPost, "/settle", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("settle handler code=%d", w.Code)
	}
	if err := manager.FetchRebatesFromExchange(context.Background(), "missing"); err == nil {
		t.Fatalf("fetch missing should fail")
	}
}
