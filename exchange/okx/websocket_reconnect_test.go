package okx

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testLoginAckDelay = 150 * time.Millisecond
	testWaitTimeout   = 5 * time.Second
)

type wsFrame struct {
	Op string `json:"op"`
}

// fakeOKXPrivateServer 模擬 OKX 私有 WS：延遲回登錄確認，並校驗訂閱只在確認之後到達；
// 第 1 條連接推送一條訂單後主動斷開，第 2 條連接推送後保持。
type fakeOKXPrivateServer struct {
	t              *testing.T
	conns          atomic.Int32
	earlySubscribe atomic.Bool
	loginFail      bool
}

func (f *fakeOKXPrivateServer) handle(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{}
	conn, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	n := f.conns.Add(1)

	frames := make(chan wsFrame, 8)
	go func() {
		defer close(frames)
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if string(msg) == okxPingMessage {
				continue
			}
			var fr wsFrame
			_ = json.Unmarshal(msg, &fr)
			frames <- fr
		}
	}()

	if fr, ok := <-frames; !ok || fr.Op != "login" {
		return
	}
	time.Sleep(testLoginAckDelay)
	select {
	case fr, ok := <-frames:
		if ok && fr.Op == "subscribe" {
			f.earlySubscribe.Store(true)
		}
	default:
	}
	if f.loginFail {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"error","code":"60009","msg":"Login failed."}`))
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"login","code":"0","msg":""}`))
	if fr, ok := <-frames; !ok || fr.Op != "subscribe" {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"event":"subscribe","arg":{"channel":"orders","instType":"SWAP","instId":"ETH-USDT-SWAP"}}`))
	push := `{"arg":{"channel":"orders","instId":"ETH-USDT-SWAP"},"data":[{"instId":"ETH-USDT-SWAP","ordId":"` +
		string(rune('0'+n)) + `","side":"buy","ordType":"limit","state":"filled","sz":"1","accFillSz":"1"}]}`
	_ = conn.WriteMessage(websocket.TextMessage, []byte(push))
	if n == 1 {
		return // 模擬斷線
	}
	for range frames {
	}
}

func newFakeOKXWS(t *testing.T, f *fakeOKXPrivateServer) (*httptest.Server, string) {
	srv := httptest.NewServer(http.HandlerFunc(f.handle))
	return srv, "ws" + strings.TrimPrefix(srv.URL, "http")
}

func TestPrivateWSReconnectsAndResubscribesAfterLoginAck(t *testing.T) {
	fake := &fakeOKXPrivateServer{t: t}
	srv, url := newFakeOKXWS(t, fake)
	defer srv.Close()

	w := NewWebSocketManager("k", "s", "p", false)
	w.privateURL = url
	w.reconnectInitialBackoff = 10 * time.Millisecond
	w.reconnectMaxBackoff = 20 * time.Millisecond

	updates := make(chan OrderUpdate, 4)
	if err := w.Start(context.Background(), "ETH-USDT-SWAP", func(u OrderUpdate) { updates <- u }); err != nil {
		t.Fatalf("Start: %v", err)
	}

	seen := map[int64]bool{}
	deadline := time.After(testWaitTimeout)
	for len(seen) < 2 {
		select {
		case u := <-updates:
			seen[u.OrderID] = true
		case <-deadline:
			t.Fatalf("重連後未收到推送，已收到 %v，連接數 %d", seen, fake.conns.Load())
		}
	}
	if fake.earlySubscribe.Load() {
		t.Fatal("在登錄確認之前就發送了訂閱")
	}

	stopped := make(chan struct{})
	go func() { w.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(testWaitTimeout):
		t.Fatal("Stop 未在超時內返回（協程未退出）")
	}
	if err := w.Start(context.Background(), "ETH-USDT-SWAP", func(OrderUpdate) {}); err != nil {
		t.Fatalf("Stop 後應可再次 Start: %v", err)
	}
	w.Stop()
}

func TestPrivateWSStartFailsOnLoginError(t *testing.T) {
	fake := &fakeOKXPrivateServer{t: t, loginFail: true}
	srv, url := newFakeOKXWS(t, fake)
	defer srv.Close()

	w := NewWebSocketManager("k", "s", "p", false)
	w.privateURL = url
	err := w.Start(context.Background(), "ETH-USDT-SWAP", func(OrderUpdate) {})
	if err == nil || !strings.Contains(err.Error(), "60009") {
		t.Fatalf("期望登錄失敗錯誤，得到 %v", err)
	}
	if w.isRunning.Load() {
		t.Fatal("登錄失敗後不應處於運行狀態")
	}
}
