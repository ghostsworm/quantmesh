package monitor

import (
	"context"
	"sync"
	"testing"
	"time"

	"quantmesh/exchange"
)

// fakePriceExchange 只實現價格流相關方法，其餘方法未實現（調用會 panic）
type fakePriceExchange struct {
	exchange.IExchange
	mu       sync.Mutex
	callback func(float64)
}

func (f *fakePriceExchange) StartPriceStream(_ context.Context, _ string, cb func(float64)) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callback = cb
	return nil
}

func (f *fakePriceExchange) GetName() string { return "fake" }

func (f *fakePriceExchange) push(price float64) {
	f.mu.Lock()
	cb := f.callback
	f.mu.Unlock()
	if cb != nil {
		cb(price)
	}
}

func TestPriceMonitorStalenessExposed(t *testing.T) {
	ex := &fakePriceExchange{}
	pm := NewPriceMonitor(ex, "BTCUSDT", 1)

	if !pm.GetLastPriceTime().IsZero() {
		t.Fatal("未收到價格前時間應為零值")
	}
	if !pm.IsStale(time.Minute) {
		t.Fatal("從未收到價格應視為過期")
	}
	if pm.IsStale(0) {
		t.Fatal("maxAge<=0 表示不檢查")
	}

	pm.updatePrice(100)
	if pm.IsStale(time.Minute) {
		t.Fatal("剛收到價格不應過期")
	}
	pm.lastPriceTime.Store(time.Now().Add(-2 * time.Minute))
	if !pm.IsStale(time.Minute) {
		t.Fatal("超過 maxAge 未更新應過期")
	}
}

func TestPriceMonitorStopDoesNotRaceWithSender(t *testing.T) {
	for i := 0; i < 20; i++ {
		ex := &fakePriceExchange{}
		pm := NewPriceMonitor(ex, "BTCUSDT", 1) // 1ms 發送間隔，放大競態窗口
		if err := pm.Start(); err != nil {
			t.Fatal(err)
		}
		sub := pm.Subscribe()

		stopPush := make(chan struct{})
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			p := 100.0
			for {
				select {
				case <-stopPush:
					return
				default:
					p++
					ex.push(p)
				}
			}
		}()

		time.Sleep(3 * time.Millisecond)
		pm.Stop()
		pm.Stop() // 重複 Stop 不應 panic
		close(stopPush)
		wg.Wait()

		// 訂閱方應能正常退出
		timeout := time.After(time.Second)
		for drained := false; !drained; {
			select {
			case _, ok := <-sub:
				if !ok {
					drained = true
				}
			case <-timeout:
				t.Fatal("Stop 後訂閱 channel 未關閉")
			}
		}
	}
}

func TestPriceMonitorStopWithoutStart(t *testing.T) {
	pm := NewPriceMonitor(&fakePriceExchange{}, "BTCUSDT", 1)
	pm.Stop()
	if _, ok := <-pm.priceChangeCh; ok {
		t.Fatal("未 Start 直接 Stop 也應關閉 channel")
	}
}
