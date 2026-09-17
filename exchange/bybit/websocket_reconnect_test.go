package bybit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

const (
	testAuthAckDelay = 150 * time.Millisecond
	testWaitTimeout  = 5 * time.Second
)

type wsFrame struct {
	Op string `json:"op"`
}

// fakeBybitPrivateServer 模擬 Bybit 私有 WS：延遲回認證確認並校驗訂閱在確認之後；
// 第 1 條連接推送後主動斷開，第 2 條連接推送後保持。
type fakeBybitPrivateServer struct {
	conns          atomic.Int32
	earlySubscribe atomic.Bool
	authFail       bool
	category       string // 推送的 category，為空時按 linear
}

func (f *fakeBybitPrivateServer) pushCategory() string {
	if f.category != "" {
		return f.category
	}
	return bybitCategoryLinear
}

func (f *fakeBybitPrivateServer) handle(w http.ResponseWriter, r *http.Request) {
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
			var fr wsFrame
			_ = json.Unmarshal(msg, &fr)
			if fr.Op == bybitOpPing {
				continue
			}
			frames <- fr
		}
	}()

	if fr, ok := <-frames; !ok || fr.Op != bybitOpAuth {
		return
	}
	time.Sleep(testAuthAckDelay)
	select {
	case fr, ok := <-frames:
		if ok && fr.Op == bybitOpSubscribe {
			f.earlySubscribe.Store(true)
		}
	default:
	}
	if f.authFail {
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"success":false,"ret_msg":"Invalid apikey","op":"auth"}`))
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"success":true,"ret_msg":"","op":"auth"}`))
	if fr, ok := <-frames; !ok || fr.Op != bybitOpSubscribe {
		return
	}
	_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"success":true,"ret_msg":"","op":"subscribe"}`))
	push := `{"topic":"order","data":[{"category":"` + f.pushCategory() + `","symbol":"BTCUSDT","orderId":"` + strconv.Itoa(int(n)) +
		`","side":"Buy","orderType":"Limit","orderStatus":"Filled"}]}`
	_ = conn.WriteMessage(websocket.TextMessage, []byte(push))
	if n == 1 {
		return // 模擬斷線
	}
	for range frames {
	}
}

func TestBybitPrivateWSReconnectsAfterAuthAck(t *testing.T) {
	fake := &fakeBybitPrivateServer{}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()

	w := NewWebSocketManager("k", "s", false)
	w.privateURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	w.reconnectInitialBackoff = 10 * time.Millisecond
	w.reconnectMaxBackoff = 20 * time.Millisecond

	updates := make(chan OrderUpdate, 4)
	if err := w.Start(context.Background(), "BTCUSDT", func(u OrderUpdate) { updates <- u }); err != nil {
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
		t.Fatal("在認證確認之前就發送了訂閱")
	}

	stopped := make(chan struct{})
	go func() { w.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(testWaitTimeout):
		t.Fatal("Stop 未在超時內返回（協程未退出）")
	}
}

func TestBybitPrivateWSStartFailsOnAuthError(t *testing.T) {
	fake := &fakeBybitPrivateServer{authFail: true}
	srv := httptest.NewServer(http.HandlerFunc(fake.handle))
	defer srv.Close()

	w := NewWebSocketManager("k", "s", false)
	w.privateURL = "ws" + strings.TrimPrefix(srv.URL, "http")
	err := w.Start(context.Background(), "BTCUSDT", func(OrderUpdate) {})
	if err == nil || !strings.Contains(err.Error(), "Invalid apikey") {
		t.Fatalf("期望認證失敗錯誤，得到 %v", err)
	}
	if w.isRunning.Load() {
		t.Fatal("認證失敗後不應處於運行狀態")
	}
}
