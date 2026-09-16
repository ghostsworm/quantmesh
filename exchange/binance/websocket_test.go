package binance

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func TestWebSocketManager_OnAccountUpdate(t *testing.T) {
	w := NewWebSocketManager("test_key", "test_secret", false)

	var called int32
	w.SetOnAccountUpdate(func() {
		atomic.StoreInt32(&called, 1)
	})

	// 模擬 ACCOUNT_UPDATE 事件
	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeAccountUpdate,
	}
	w.handleUserDataEvent(event)

	if atomic.LoadInt32(&called) != 1 {
		t.Error("OnAccountUpdate 回調應被調用")
	}
}

func TestWebSocketManager_OnAccountUpdate_NilCallback(t *testing.T) {
	w := NewWebSocketManager("test_key", "test_secret", false)
	// 未設置回調時不應 panic
	event := &futures.WsUserDataEvent{
		Event: futures.UserDataEventTypeAccountUpdate,
	}
	w.handleUserDataEvent(event)
}

// fakeUserStream 注入式的 listenKey / WS 連接模擬
type fakeUserStream struct {
	mu        sync.Mutex
	keySeq    int
	served    []string
	keepFails bool
	// dropFirst 為 true 時第一次連接立即斷開
	dropFirst bool
}

func (f *fakeUserStream) install(w *WebSocketManager) {
	w.reconnectDelay = time.Millisecond
	w.maxReconnectDelay = 5 * time.Millisecond
	w.closeTimeout = time.Second
	w.startUserStream = func(ctx context.Context) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.keySeq++
		return fmt.Sprintf("key%d", f.keySeq), nil
	}
	w.keepAliveStream = func(ctx context.Context, key string) error {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.keepFails {
			return errors.New("listenKey does not exist")
		}
		return nil
	}
	w.serveUserData = func(key string, h futures.WsUserDataHandler, eh futures.ErrHandler) (chan struct{}, chan struct{}, error) {
		f.mu.Lock()
		first := len(f.served) == 0
		f.served = append(f.served, key)
		f.mu.Unlock()
		doneC := make(chan struct{})
		stopC := make(chan struct{})
		if first && f.dropFirst {
			close(doneC)
			return doneC, stopC, nil
		}
		go func() {
			<-stopC
			close(doneC)
		}()
		return doneC, stopC, nil
	}
}

func (f *fakeUserStream) servedKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.served...)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("等待條件超時")
}

func TestWebSocketManager_RenewsListenKeyAfterDisconnect(t *testing.T) {
	w := NewWebSocketManager("k", "s", false)
	w.keepAliveInterval = time.Hour
	f := &fakeUserStream{dropFirst: true}
	f.install(w)

	if err := w.Start(context.Background(), func(OrderUpdate) {}); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	waitFor(t, func() bool { return len(f.servedKeys()) >= 2 })
	keys := f.servedKeys()
	if keys[0] != "key1" || keys[1] != "key2" {
		t.Fatalf("斷線後應使用新 listenKey 重連, served=%v", keys)
	}
}

func TestWebSocketManager_RenewsListenKeyAfterKeepaliveFailure(t *testing.T) {
	w := NewWebSocketManager("k", "s", false)
	w.keepAliveInterval = 2 * time.Millisecond
	f := &fakeUserStream{keepFails: true}
	f.install(w)

	if err := w.Start(context.Background(), func(OrderUpdate) {}); err != nil {
		t.Fatal(err)
	}
	defer w.Stop()

	waitFor(t, func() bool { return len(f.servedKeys()) >= 2 })
	keys := f.servedKeys()
	if keys[1] == keys[0] {
		t.Fatalf("保活失敗後應重建 listenKey, served=%v", keys)
	}
}

func TestWebSocketManager_StopThenStartDoesNotPanic(t *testing.T) {
	w := NewWebSocketManager("k", "s", false)
	w.keepAliveInterval = time.Hour
	f := &fakeUserStream{}
	f.install(w)

	for i := 0; i < 2; i++ {
		if err := w.Start(context.Background(), func(OrderUpdate) {}); err != nil {
			t.Fatalf("第 %d 次 Start 失敗: %v", i+1, err)
		}
		waitFor(t, func() bool { return len(f.servedKeys()) >= i+1 })
		w.Stop()
	}
}
