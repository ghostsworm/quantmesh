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

	st, err := storage.NewSQLStorage(dbPath)
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

	st, err := storage.NewSQLStorage(dbPath)
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

	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	origStorageProvider := storageServiceProvider
	origBotProvider := botManagerProvider()
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
	if state.BotID != "binance:BTCUSDT:futures" {
		t.Fatalf("BotID 应持久化到存储: got %q", state.BotID)
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

func TestFixSessionTimeout(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_fix_timeout.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	// 创建超时会话：last_heartbeat_at 超过 120 秒前
	oldTime := time.Now().Add(-150 * time.Second)
	state := &storage.FixSessionState{
		SessionID:       "FIX.4.4:TIMEOUT_SESSION",
		BotID:           "binance:BTCUSDT:futures",
		Role:            "acceptor",
		BeginString:     "FIX.4.4",
		NextSenderSeq:   1,
		NextTargetSeq:   1,
		IsLoggedOn:      true,
		LastLogonAt:     &oldTime,
		LastHeartbeatAt: &oldTime,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := st.UpsertFixSessionState(state); err != nil {
		t.Fatalf("创建超时会话失败: %v", err)
	}
	setFixSessionBotBinding(state.SessionID, state.BotID)

	origStorageProvider := storageServiceProvider
	origBotProvider := botManagerProvider()
	SetStorageServiceProvider(&testStorageProvider{st: st})
	RegisterBotManagerProvider(&mockFixBotManager{})
	t.Cleanup(func() {
		SetStorageServiceProvider(origStorageProvider)
		RegisterBotManagerProvider(origBotProvider)
	})

	// fixNewOrder 应因超时被拒
	newOrderBody := []byte(`{
		"session_id":"FIX.4.4:TIMEOUT_SESSION",
		"cl_ord_id":"T-O-1",
		"side":"BUY",
		"price":50000,
		"order_qty":0.001
	}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/fix/orders/new", bytes.NewReader(newOrderBody))
	c.Request.Header.Set("Content-Type", "application/json")
	fixNewOrder(c)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超时会话应拒单 400, got %d body=%s", w.Code, w.Body.String())
	}
	// 验证会话已被标记失活
	updated, _ := st.GetFixSessionState(state.SessionID)
	if updated != nil && updated.IsLoggedOn {
		t.Fatalf("超时后会话应被标记 is_logged_on=false")
	}
}

func TestFixLogoutSession(t *testing.T) {
	gin.SetMode(gin.TestMode)

	dbPath := "./test_api_fix_logout.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC()
	if err := st.UpsertFixSessionState(&storage.FixSessionState{
		SessionID:       "FIX.4.4:LOGOUT_TEST",
		Role:            "acceptor",
		BeginString:     "FIX.4.4",
		SenderCompID:    "SELLER",
		TargetCompID:    "BUYER",
		NextSenderSeq:   1,
		NextTargetSeq:   1,
		IsLoggedOn:      true,
		LastLogonAt:     &now,
		LastHeartbeatAt: &now,
		UpdatedAt:       now,
	}); err != nil {
		t.Fatalf("保存 FIX 会话失败: %v", err)
	}
	setFixSessionBotBinding("FIX.4.4:LOGOUT_TEST", "binance:BTCUSDT:futures")

	origStorageProvider := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: st})
	t.Cleanup(func() { SetStorageServiceProvider(origStorageProvider) })

	logoutBody := []byte(`{"session_id":"FIX.4.4:LOGOUT_TEST"}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/fix/sessions/logout", bytes.NewReader(logoutBody))
	c.Request.Header.Set("Content-Type", "application/json")
	fixLogoutSession(c)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}
	var resp struct {
		OK        bool   `json:"ok"`
		SessionID string `json:"session_id"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if !resp.OK || resp.SessionID != "FIX.4.4:LOGOUT_TEST" {
		t.Fatalf("响应异常: %+v", resp)
	}

	state, err := st.GetFixSessionState("FIX.4.4:LOGOUT_TEST")
	if err != nil || state == nil {
		t.Fatalf("登出后会话仍应存在: err=%v state=%+v", err, state)
	}
	if state.IsLoggedOn {
		t.Fatalf("登出后 is_logged_on 应为 false, got %+v", state)
	}
}

func TestFixDisabledReturns503(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origConfig := globalConfig
	t.Cleanup(func() { globalConfig = origConfig })

	// 设置 FIX 关闭
	globalConfig = &config.Config{}
	globalConfig.Fix.Enabled = config.BoolPtr(false)
	globalConfig.Fix.HeartbeatTimeoutSec = 120

	r := gin.New()
	api := r.Group("/api")
	protected := api.Group("")
	protected.Use(func(c *gin.Context) { c.Next() }) // 跳过认证
	fixGroup := protected.Group("/fix")
	fixGroup.Use(fixEnabledMiddleware())
	fixGroup.GET("/sessions", getFixSessions)

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/fix/sessions?limit=10&offset=0", nil)
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("FIX 关闭时应返回 503, got %d body=%s", w.Code, w.Body.String())
	}
}

func TestFixHeartbeatTimeoutConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)

	origConfig := globalConfig
	t.Cleanup(func() { globalConfig = origConfig })
	globalConfig = &config.Config{}
	globalConfig.Fix.Enabled = config.BoolPtr(true)
	globalConfig.Fix.HeartbeatTimeoutSec = 10 // 10 秒超时

	dbPath := "./test_api_fix_timeout_config.db"
	defer os.Remove(dbPath)
	defer os.Remove(dbPath + "-shm")
	defer os.Remove(dbPath + "-wal")

	st, err := storage.NewSQLStorage(dbPath)
	if err != nil {
		t.Fatalf("创建存储失败: %v", err)
	}
	defer st.Close()

	oldTime := time.Now().Add(-15 * time.Second) // 15 秒前
	state := &storage.FixSessionState{
		SessionID:       "FIX.4.4:TIMEOUT_CFG",
		BotID:           "binance:BTCUSDT:futures",
		Role:            "acceptor",
		BeginString:     "FIX.4.4",
		NextSenderSeq:   1,
		NextTargetSeq:   1,
		IsLoggedOn:      true,
		LastLogonAt:     &oldTime,
		LastHeartbeatAt: &oldTime,
		UpdatedAt:       time.Now().UTC(),
	}
	if err := st.UpsertFixSessionState(state); err != nil {
		t.Fatalf("创建超时会话失败: %v", err)
	}
	setFixSessionBotBinding(state.SessionID, state.BotID)

	origStorageProvider := storageServiceProvider
	origBotProvider := botManagerProvider()
	SetStorageServiceProvider(&testStorageProvider{st: st})
	RegisterBotManagerProvider(&mockFixBotManager{})
	t.Cleanup(func() {
		SetStorageServiceProvider(origStorageProvider)
		RegisterBotManagerProvider(origBotProvider)
	})

	newOrderBody := []byte(`{
		"session_id":"FIX.4.4:TIMEOUT_CFG",
		"cl_ord_id":"T-CFG-1",
		"side":"BUY",
		"price":50000,
		"order_qty":0.001
	}`)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/fix/orders/new", bytes.NewReader(newOrderBody))
	c.Request.Header.Set("Content-Type", "application/json")
	fixNewOrder(c)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("配置 10 秒超时、会话 15 秒未心跳应拒单 400, got %d body=%s", w.Code, w.Body.String())
	}
}
