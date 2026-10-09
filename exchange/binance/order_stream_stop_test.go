package binance

import (
	"context"
	"testing"
	"time"

	"github.com/adshao/go-binance/v2/futures"
)

func TestFuturesOrderStreamStopMustConfirmConnectionExit(t *testing.T) {
	w := NewWebSocketManager("fixture", "fixture", false)
	w.closeTimeout = 10 * time.Millisecond
	w.keepAliveInterval = time.Hour
	w.startUserStream = func(context.Context) (string, error) { return "fixture", nil }
	connected, stopped, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	w.serveUserData = func(string, futures.WsUserDataHandler, futures.ErrHandler) (chan struct{}, chan struct{}, error) {
		done, stop := make(chan struct{}), make(chan struct{})
		close(connected)
		go func() { <-stop; close(stopped); <-release; close(done) }()
		return done, stop, nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	t.Cleanup(func() { close(release); w.Stop() })
	if err := w.Start(ctx, func(OrderUpdate) {}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("fixture not connected")
	}
	a := &BinanceAdapter{wsManager: w}
	if err := a.StopOrderStream(); err == nil {
		t.Fatal("adapter acknowledged stop while underlying connection has not exited")
	}
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("stop not requested")
	}
	w.mu.RLock()
	running := w.isRunning
	w.mu.RUnlock()
	if !running {
		t.Fatal("unconfirmed stop was advertised as restartable")
	}
	if err := w.Start(ctx, func(OrderUpdate) {}); err == nil {
		t.Fatal("unconfirmed connection admitted restart")
	}
}
