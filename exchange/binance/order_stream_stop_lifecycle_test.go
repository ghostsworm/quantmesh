package binance

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	binancesdk "github.com/adshao/go-binance/v2"
	"github.com/adshao/go-binance/v2/futures"
)

type orderStopFixture struct {
	start           func(context.Context, func(OrderUpdate)) error
	stop            func() error
	running         func() bool
	retained        func() bool
	startREST       func(func(context.Context) (string, error))
	keepalive       func(func(context.Context, string) error)
	serve           func(func(func()) (chan struct{}, chan struct{}, error))
	setCloseTimeout func(time.Duration)
}

const confirmedRestartStopTimeout = time.Second

func newOrderStopFixture(t *testing.T, kind string) orderStopFixture {
	if kind == "futures" {
		w := NewWebSocketManager("fixture", "fixture", false)
		w.closeTimeout, w.keepAliveInterval = 10*time.Millisecond, time.Millisecond
		w.startUserStream = func(context.Context) (string, error) { return "fixture", nil }
		w.keepAliveStream = func(context.Context, string) error { return nil }
		a := newGuardAdapter(t, &fakeFuturesServer{}) // Real mode preflight, loopback HTTP only.
		a.wsManager = w
		return orderStopFixture{start: func(ctx context.Context, cb func(OrderUpdate)) error {
			return a.StartOrderStream(ctx, func(interface{}) { cb(OrderUpdate{}) })
		}, stop: a.StopOrderStream,
			running:   func() bool { w.mu.RLock(); defer w.mu.RUnlock(); return w.isRunning },
			retained:  func() bool { return a.wsManager == w },
			startREST: func(f func(context.Context) (string, error)) { w.startUserStream = f },
			keepalive: func(f func(context.Context, string) error) { w.keepAliveStream = f },
			serve: func(f func(func()) (chan struct{}, chan struct{}, error)) {
				w.serveUserData = func(_ string, h futures.WsUserDataHandler, _ futures.ErrHandler) (chan struct{}, chan struct{}, error) {
					return f(func() { h(&futures.WsUserDataEvent{Event: futures.UserDataEventTypeOrderTradeUpdate}) })
				}
			},
			setCloseTimeout: func(timeout time.Duration) { w.closeTimeout = timeout },
		}
	}
	w := NewSpotUserDataWebSocketManager(nil, false)
	w.closeTimeout, w.keepAliveInterval = 10*time.Millisecond, time.Millisecond
	w.startUserStream = func(context.Context) (string, error) { return "fixture", nil }
	w.keepAliveStream = func(context.Context, string) error { return nil }
	a := &BinanceSpotAdapter{orderWS: w}
	return orderStopFixture{start: func(ctx context.Context, cb func(OrderUpdate)) error {
		return a.StartOrderStream(ctx, func(interface{}) { cb(OrderUpdate{}) })
	}, stop: a.StopOrderStream,
		running:   func() bool { w.mu.Lock(); defer w.mu.Unlock(); return w.isRunning },
		retained:  func() bool { return a.orderWS == w },
		startREST: func(f func(context.Context) (string, error)) { w.startUserStream = f },
		keepalive: func(f func(context.Context, string) error) { w.keepAliveStream = f },
		serve: func(f func(func()) (chan struct{}, chan struct{}, error)) {
			w.serveUserData = func(_ string, h binancesdk.WsUserDataHandler, _ binancesdk.ErrHandler) (chan struct{}, chan struct{}, error) {
				return f(func() { h(&binancesdk.WsUserDataEvent{Event: binancesdk.UserDataEventTypeExecutionReport}) })
			}
		},
		setCloseTimeout: func(timeout time.Duration) { w.closeTimeout = timeout },
	}
}

func requireOrderStopSignal(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("offline stream fixture signal timed out")
	}
}

func requireUnconfirmedOrderStop(t *testing.T, f orderStopFixture) {
	t.Helper()
	if err := f.stop(); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unconfirmed adapter stop acknowledged or lost deadline: %v", err)
	}
	if !f.running() || !f.retained() {
		t.Fatal("unconfirmed stop discarded lifecycle or adapter reference")
	}
	if err := f.start(t.Context(), func(OrderUpdate) {}); err == nil {
		t.Fatal("unconfirmed stream admitted restart")
	}
}

func TestBinanceOrderStreamStopWaitsForEveryWorkerAndRetriesSameGeneration(t *testing.T) {
	for _, kind := range []string{"futures", "spot"} {
		for _, mode := range []string{"connection", "callback", "keepalive", "startup", "connect_startup", "external_cancel", "concurrent_stop"} {
			t.Run(kind+"_"+mode, func(t *testing.T) { testOrderStopLifecycle(t, kind, mode) })
		}
	}
}

func testOrderStopLifecycle(t *testing.T, kind, mode string) {
	f := newOrderStopFixture(t, kind)
	ctx, cancel := context.WithCancel(t.Context())
	release, entered, connectionClosed, restartedConnection := make(chan struct{}), make(chan struct{}), make(chan struct{}), make(chan struct{})
	var once sync.Once
	var restartedOnce sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		cancel()
		if err := f.stop(); err != nil {
			t.Error(err)
		}
	})
	var requests, connections, callbacks atomic.Int32
	f.startREST(func(ctx context.Context) (string, error) {
		requests.Add(1)
		if mode == "startup" && requests.Load() == 1 {
			close(entered)
			<-ctx.Done()
			<-release
			return "", ctx.Err()
		}
		return "fixture", nil
	})
	if mode == "keepalive" {
		var keepaliveEntered sync.Once
		f.keepalive(func(ctx context.Context, _ string) error {
			if requests.Load() == 1 {
				keepaliveEntered.Do(func() { close(entered) })
				<-ctx.Done()
				<-release
				return ctx.Err()
			}
			return nil
		})
	}
	restartedGeneration := int32(2)
	if mode == "startup" {
		// The interrupted first start never reaches serveUserData; its restart is
		// therefore the first connection fixture generation.
		restartedGeneration = 1
	}
	f.serve(func(callback func()) (chan struct{}, chan struct{}, error) {
		generation := connections.Add(1)
		if generation == restartedGeneration {
			restartedOnce.Do(func() { close(restartedConnection) })
		}
		if mode == "connect_startup" && generation == 1 {
			close(entered)
			<-release
		}
		done, stop := make(chan struct{}), make(chan struct{})
		go func() {
			if generation == 1 && mode == "callback" {
				callback()
			}
			<-stop
			if generation == 1 && (mode == "connection" || mode == "external_cancel" || mode == "concurrent_stop") {
				<-release
			}
			close(done)
			if generation == 1 {
				close(connectionClosed)
			}
		}()
		if generation == 1 && (mode == "connection" || mode == "external_cancel" || mode == "concurrent_stop") {
			close(entered)
		}
		return done, stop, nil
	})
	startResult := make(chan error, 1)
	cb := func(OrderUpdate) { callbacks.Add(1); close(entered); <-release }
	go func() { startResult <- f.start(ctx, cb) }()
	requireOrderStopSignal(t, entered)
	if mode != "startup" {
		select {
		case err := <-startResult:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("start hung")
		}
	}
	if mode == "external_cancel" {
		cancel()
	}
	if mode == "concurrent_stop" {
		var stops sync.WaitGroup
		for i := 0; i < 6; i++ {
			stops.Add(1)
			go func() {
				defer stops.Done()
				if err := f.stop(); !errors.Is(err, context.DeadlineExceeded) {
					t.Errorf("concurrent unconfirmed stop: %v", err)
				}
			}()
		}
		stops.Wait()
	}
	requireUnconfirmedOrderStop(t, f)
	requireUnconfirmedOrderStop(t, f)
	if requests.Load() != 1 {
		t.Fatal("pending stop replayed listenKey startup")
	}
	if mode == "callback" && callbacks.Load() != 1 {
		t.Fatal("actual SDK callback was not exercised")
	}
	unblock()
	if mode == "startup" {
		select {
		case err := <-startResult:
			if !errors.Is(err, context.Canceled) {
				t.Fatal("startup cancel not propagated", err)
			}
		case <-time.After(time.Second):
			t.Fatal("startup did not finish")
		}
		if connections.Load() != 0 {
			t.Fatal("cancelled startup created connection")
		}
	} else {
		requireOrderStopSignal(t, connectionClosed)
	}
	waitFor(t, func() bool { return !f.running() })
	if err := f.stop(); err != nil {
		t.Fatal("same generation retry did not finish", err)
	}
	if !f.retained() {
		t.Fatal("adapter discarded confirmed reusable manager")
	}
	// The 10ms deadline above deliberately verifies an unconfirmed stop. Once
	// that generation is confirmed closed, use a production-like budget for the
	// restarted generation so package-level race load does not turn scheduler
	// delay into a false worker-leak failure.
	f.setCloseTimeout(confirmedRestartStopTimeout)
	if err := f.start(t.Context(), func(OrderUpdate) {}); err != nil {
		t.Fatal("confirmed stop prevented restart", err)
	}
	requireOrderStopSignal(t, restartedConnection)
	if err := f.stop(); err != nil {
		t.Fatal("restarted generation did not stop", err)
	}
	if requests.Load() != 2 {
		t.Fatal("restart did not use exactly one fresh listenKey")
	}
}
