package web

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/storage"
)

type mockFixBotManager struct{}

func (m *mockFixBotManager) ListBots() []BotResponse { return nil }
func (m *mockFixBotManager) GetBot(botID string) (*BotDetailResponse, bool) {
	if botID != "binance:BTCUSDT:futures" {
		return nil, false
	}
	return &BotDetailResponse{
		BotResponse: BotResponse{
			BotID:      botID,
			Exchange:   "binance",
			Symbol:     "BTCUSDT",
			MarketType: "futures",
			Running:    true,
		},
	}, true
}
func (m *mockFixBotManager) StartBot(ctx context.Context, botCfg config.BotConfig) error { return nil }
func (m *mockFixBotManager) StopBot(botID string) error                                  { return nil }
func (m *mockFixBotManager) EnableBot(botID string) error                                { return nil }

func TestGetFixSessions(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_fix_sessions.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	if err := st.UpsertFixSessionState(&storage.FixSessionState{
		SessionID:       "FIX.4.4:SELLER->BUYER",
		Role:            "acceptor",
		BeginString:     "FIX.4.4",
		SenderCompID:    "SELLER",
		TargetCompID:    "BUYER",
		NextSenderSeq:   11,
		NextTargetSeq:   22,
		IsLoggedOn:      true,
		LastLogonAt:     &now,
		LastHeartbeatAt: &now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("保存 FIX 会话失败: %v", err)
	}

	origStorageProvider := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(origStorageProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/fix/sessions?limit=20&offset=0", nil)

	getFixSessions(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Sessions []struct {
			SessionID string `json:"session_id"`
			Role      string `json:"role"`
		} `json:"sessions"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Sessions) != 1 {
		t.Fatalf("期望 1 条会话，实际 %d", len(resp.Sessions))
	}
	if resp.Sessions[0].SessionID != "FIX.4.4:SELLER->BUYER" || resp.Sessions[0].Role != "acceptor" {
		t.Fatalf("响应字段异常: %+v", resp.Sessions[0])
	}
}

func TestGetFixOrderLinks(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_fix_orders.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	if err := st.UpsertFixOrderLink(&storage.FixOrderLink{
		SessionID:       "FIX.4.4:SELLER->BUYER",
		ClOrdID:         "A-1001",
		BotID:           "binance:BTCUSDT:futures",
		Exchange:        "binance",
		Symbol:          "BTCUSDT",
		Side:            "BUY",
		InternalOrderID: 10001,
		LastExecID:      "exec-1",
		OrdStatus:       "NEW",
		CumQty:          0,
		LeavesQty:       1,
		AvgPx:           0,
		CreatedAt:       now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("保存 FIX 订单映射失败: %v", err)
	}

	origStorageProvider := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(origStorageProvider) })

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/fix/orders?session_id=FIX.4.4:SELLER->BUYER&ord_status=NEW", nil)

	getFixOrderLinks(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp struct {
		Orders []struct {
			ClOrdID   string `json:"cl_ord_id"`
			OrdStatus string `json:"ord_status"`
		} `json:"orders"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if len(resp.Orders) != 1 {
		t.Fatalf("期望 1 条订单映射，实际 %d", len(resp.Orders))
	}
	if resp.Orders[0].ClOrdID != "A-1001" || resp.Orders[0].OrdStatus != "NEW" {
		t.Fatalf("响应字段异常: %+v", resp.Orders[0])
	}
}

func TestFixLogonAndHeartbeat(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_fix_logon_heartbeat.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLiteStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	origStorageProvider := storageServiceProvider
	origBotProvider := botManagerProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	RegisterBotManagerProvider(&mockFixBotManager{})
	t.Cleanup(func() {
		SetStorageServiceProvider(origStorageProvider)
		RegisterBotManagerProvider(origBotProvider)
	})

	logonBody := []byte(`{
		"session_id":"FIX.4.4:BUYER->SELLER",
		"bot_id":"binance:BTCUSDT:futures",
		"role":"acceptor",
		"begin_string":"FIX.4.4",
		"sender_comp_id":"SELLER",
		"target_comp_id":"BUYER"
	}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/fix/sessions/logon", bytes.NewReader(logonBody))
	c.Request.Header.Set("Content-Type", "application/json")
	fixLogonSession(c)
	if w.Code != http.StatusOK {
		t.Fatalf("logon expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	state, err := st.GetFixSessionState("FIX.4.4:BUYER->SELLER")
	if err != nil || state == nil {
		t.Fatalf("logon 后应有会话状态: err=%v state=%+v", err, state)
	}
	if !state.IsLoggedOn {
		t.Fatalf("会话绑定异常: %+v", state)
	}

	heartbeatBody := []byte(`{"session_id":"FIX.4.4:BUYER->SELLER"}`)
	w2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(w2)
	c2.Request = httptest.NewRequest(http.MethodPost, "/api/fix/sessions/heartbeat", bytes.NewReader(heartbeatBody))
	c2.Request.Header.Set("Content-Type", "application/json")
	fixHeartbeat(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("heartbeat expected 200, got %d body=%s", w2.Code, w2.Body.String())
	}
}
