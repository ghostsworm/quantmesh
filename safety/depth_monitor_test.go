package safety

import (
	"context"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

const (
	testDepthSymbolA   = "BTCUSDT"
	testDepthSymbolB   = "ETHUSDT"
	testNormalDepth    = 100000.0 // 單邊名義深度，總深度為其兩倍
	testCrashDepth     = 20000.0
	testWarmupSnapshot = 5
)

// mockDepthExchange 按交易對返回可配置深度的訂單簿
type mockDepthExchange struct {
	exchange.IExchange
	mu    sync.Mutex
	depth map[string]float64 // 單邊名義深度（USDT）
}

func (m *mockDepthExchange) setDepth(symbol string, notional float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.depth[symbol] = notional
}

func (m *mockDepthExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	m.mu.Lock()
	notional := m.depth[symbol]
	m.mu.Unlock()
	return &exchange.OrderBook{
		Symbol: symbol,
		Bids:   []exchange.OrderBookLevel{{Price: 1, Quantity: notional}},
		Asks:   []exchange.OrderBookLevel{{Price: 1, Quantity: notional}},
	}, nil
}

func newTestDepthMonitor() (*DepthMonitor, *mockDepthExchange) {
	cfg := &config.Config{}
	cfg.RiskControl.DepthMonitor.Enabled = true
	cfg.RiskControl.DepthMonitor.DepthLevels = 10
	cfg.RiskControl.DepthMonitor.DropThreshold = 0.5
	cfg.RiskControl.DepthMonitor.RecoveryThreshold = 0.7
	cfg.RiskControl.DepthMonitor.MinDepthUSDT = 10000
	ex := &mockDepthExchange{depth: map[string]float64{
		testDepthSymbolA: testNormalDepth,
		testDepthSymbolB: testNormalDepth,
	}}
	return NewDepthMonitor(cfg, ex), ex
}

func checkBoth(d *DepthMonitor) {
	ctx := context.Background()
	d.checkDepth(ctx, testDepthSymbolA)
	d.checkDepth(ctx, testDepthSymbolB)
}

// TestDepthMonitor_HealthySymbolDoesNotRecoverOther 驗證 C5：B 正常不會解除 A 的觸發
func TestDepthMonitor_HealthySymbolDoesNotRecoverOther(t *testing.T) {
	d, ex := newTestDepthMonitor()
	for i := 0; i < testWarmupSnapshot; i++ {
		checkBoth(d)
	}

	ex.setDepth(testDepthSymbolA, testCrashDepth)
	checkBoth(d)

	if !d.IsTriggered() || !d.IsSymbolTriggered(testDepthSymbolA) {
		t.Fatalf("A 深度崩塌後應觸發")
	}
	if d.IsSymbolTriggered(testDepthSymbolB) {
		t.Fatalf("B 深度正常不應觸發")
	}
	if score, _ := d.GetDepthRiskScore(testDepthSymbolA); score != 100 {
		t.Fatalf("A 風險分 = %v, want 100", score)
	}
	if score, _ := d.GetDepthRiskScore(testDepthSymbolB); score != 0 {
		t.Fatalf("B 風險分 = %v, want 0", score)
	}
}

// TestDepthMonitor_BaselineFrozenWhileTriggered 驗證 C5：持續低深度不會因基線被拉低而自動解除
func TestDepthMonitor_BaselineFrozenWhileTriggered(t *testing.T) {
	d, ex := newTestDepthMonitor()
	for i := 0; i < testWarmupSnapshot; i++ {
		checkBoth(d)
	}

	ex.setDepth(testDepthSymbolA, testCrashDepth)
	// 遠多於基線樣本數的崩盤後快照
	for i := 0; i < depthHistoryMaxSnapshots*2; i++ {
		checkBoth(d)
	}
	if !d.IsSymbolTriggered(testDepthSymbolA) {
		t.Fatalf("深度持續低迷時不應自動解除觸發")
	}
	if got := len(d.depthHistory[testDepthSymbolA]); got != testWarmupSnapshot {
		t.Fatalf("觸發期間歷史不應增長: len = %d, want %d", got, testWarmupSnapshot)
	}

	// 恢復到基線 70% 以上才解除
	ex.setDepth(testDepthSymbolA, testNormalDepth*0.8)
	checkBoth(d)
	if d.IsSymbolTriggered(testDepthSymbolA) || d.IsTriggered() {
		t.Fatalf("深度恢復到基線 80%% 後應解除觸發")
	}
}

// TestDepthMonitor_AnyTriggeredKeepsGlobalFlag 驗證全局 IsTriggered 在任一交易對觸發時為 true
func TestDepthMonitor_AnyTriggeredKeepsGlobalFlag(t *testing.T) {
	d, ex := newTestDepthMonitor()
	for i := 0; i < testWarmupSnapshot; i++ {
		checkBoth(d)
	}

	ex.setDepth(testDepthSymbolA, testCrashDepth)
	ex.setDepth(testDepthSymbolB, testCrashDepth)
	checkBoth(d)
	if !d.IsSymbolTriggered(testDepthSymbolA) || !d.IsSymbolTriggered(testDepthSymbolB) {
		t.Fatalf("A 與 B 均應觸發")
	}

	ex.setDepth(testDepthSymbolA, testNormalDepth)
	checkBoth(d)
	if d.IsSymbolTriggered(testDepthSymbolA) {
		t.Fatalf("A 恢復後應解除")
	}
	if !d.IsTriggered() {
		t.Fatalf("B 仍觸發時全局 IsTriggered 應為 true")
	}
	if msg := d.GetLastMsg(); msg == "" {
		t.Fatalf("仍有觸發交易對時 lastMsg 不應為空")
	}
}
