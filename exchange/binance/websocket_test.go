package binance

import (
	"sync/atomic"
	"testing"

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
