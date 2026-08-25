package position

import (
	"math"
	"sync"
	"testing"

	"quantmesh/config"
)

func newAnchorTestManager() *SuperPositionManager {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	cfg.Trading.BuyWindowSize = 2
	return NewSuperPositionManager(cfg, &MockExecutor{}, &MockExchange{}, 2, 4)
}

// TestAnchorPrice_ConcurrentAccess 回歸測試：
// 修復前 anchorPrice 是裸 float64 欄位，ShiftGrid 持 spm.mu 寫入，
// 但 findNearestGridPrice / GridAutoRebuilder 的定時 goroutine 在鎖外讀，
// 構成數據競態。本測試在 -race 下並發讀寫，修復前必然報 DATA RACE。
func TestAnchorPrice_ConcurrentAccess(t *testing.T) {
	spm := newAnchorTestManager()
	spm.setAnchorPrice(1000)
	spm.lastMarketPrice.Store(1000.0)

	const goroutines = 8
	const iterations = 200

	var wg sync.WaitGroup

	// 寫者：模擬 Web API 觸發的網格上下移
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(up bool) {
			defer wg.Done()
			direction := "down"
			if up {
				direction = "up"
			}
			for j := 0; j < iterations; j++ {
				spm.ShiftGrid(direction, 1)
			}
		}(i%2 == 0)
	}

	// 讀者：模擬網格計算與自動重建定時器
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				_ = spm.anchorPrice()
				_ = spm.GetAnchorPrice()
				_ = spm.findNearestGridPrice(1000)
			}
		}()
	}

	wg.Wait()

	// 上移與下移次數相等，錨點應回到起點（未被 clamp 到 0 的前提下）
	if got := spm.anchorPrice(); got < 0 {
		t.Errorf("錨點不應為負，實得 %.2f", got)
	}
}

// TestShiftGrid_ReadModifyWriteIsAtomic 驗證讀-改-寫沒有丟失更新：
// 單向並發上移 N 次，錨點必須精確增加 N*step。
// 若把 ShiftGrid 的 += 拆成鎖外讀、鎖外寫，這裡就會少加。
func TestShiftGrid_ReadModifyWriteIsAtomic(t *testing.T) {
	spm := newAnchorTestManager()
	spm.setAnchorPrice(1000)

	const goroutines = 8
	const iterations = 100
	const step = 1.0

	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				spm.ShiftGrid("up", step)
			}
		}()
	}
	wg.Wait()

	want := 1000 + float64(goroutines*iterations)*step
	if got := spm.anchorPrice(); math.Abs(got-want) > 1e-6 {
		t.Errorf("並發上移後錨點 = %.4f，期望 %.4f（丟失了 %.0f 次更新）",
			got, want, (want-got)/step)
	}
}
