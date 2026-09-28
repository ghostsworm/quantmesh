package binance

import "testing"

func TestConnectivityIdentityStableAndDifferentAcrossAccounts(t *testing.T) {
	var events []ConnectivityEvent
	SetConnectivityEventHandler(func(e ConnectivityEvent) { events = append(events, e) })
	t.Cleanup(func() { SetConnectivityEventHandler(nil) })
	a, b := &WebSocketManager{symbol: "BTCUSDT"}, &WebSocketManager{symbol: "BTCUSDT"}
	a.emitConnectivity(ConnectivityDisconnected, "test")
	b.emitConnectivity(ConnectivityReconnected, "test")
	a.emitConnectivity(ConnectivityStopped, "test")
	if len(events) != 3 || events[0].ConnectionID == "" || events[0].ConnectionID == events[1].ConnectionID || events[0].ConnectionID != events[2].ConnectionID {
		t.Fatal("connection identity is missing, unstable, or shared")
	}
}
