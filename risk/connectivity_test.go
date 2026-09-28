package risk

import (
	"testing"

	"quantmesh/event"
)

func TestConnectivityRecoveryIsScopedToOneStream(t *testing.T) {
	gcb := &GlobalCircuitBreaker{}
	gcb.ReportWebSocketDisconnect("account-a-stream-1")
	first := gcb.lastWSDisconnect
	gcb.ReportWebSocketDisconnect("account-a-stream-1")
	if !gcb.lastWSDisconnect.Equal(first) {
		t.Fatal("duplicate disconnect reset deadline")
	}
	gcb.ReportWebSocketDisconnect("account-b-stream-1")
	gcb.ReportWebSocketReconnected("account-b-stream-1")
	if !gcb.lastWSDisconnect.Equal(first) {
		t.Fatal("B recovery cleared A")
	}
	gcb.ReportWebSocketStopped("account-b-stream-1")
	gcb.ReportWebSocketReconnected()
	if !gcb.lastWSDisconnect.Equal(first) {
		t.Fatal("B stop or legacy recovery cleared A")
	}
	gcb.ReportWebSocketStopped("account-a-stream-1")
	if !gcb.lastWSDisconnect.IsZero() {
		t.Fatal("stopped stream retained deadline")
	}
}

func TestConnectivityIdentityUsesStreamInstance(t *testing.T) {
	a := &event.Event{Data: map[string]interface{}{"connection_id": "a", "symbol": "BTCUSDT"}}
	b := &event.Event{Data: map[string]interface{}{"connection_id": "b", "symbol": "BTCUSDT"}}
	if connectivityEventKey(a) == connectivityEventKey(b) {
		t.Fatal("same symbol conflated two connections")
	}
}
