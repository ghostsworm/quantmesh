package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/exchange"
	"testing"
)

// MockRiskExchange 模拟风控所需的交易所方法。
// 勿只嵌入 exchange.IExchange 而不赋值：未覆蓋的方法會转发到 nil 接口並在運行時 panic。
type MockRiskExchange struct {
	exchange.IExchange
	HistoricalKlines []*exchange.Candle
}

func (m *MockRiskExchange) GetName() string { return "mock" }

func (m *MockRiskExchange) GetHistoricalKlines(ctx context.Context, symbol, interval string, limit int) ([]*exchange.Candle, error) {
	return m.HistoricalKlines, nil
}

func (m *MockRiskExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback exchange.CandleUpdateCallback) error {
	return nil
}

func (m *MockRiskExchange) StopKlineStream() error { return nil }

func TestRiskMonitor_IsTriggered(t *testing.T) {
	cfg := &config.Config{}
	cfg.RiskControl.Enabled = true
	cfg.RiskControl.MonitorSymbols = []string{"BTCUSDT"}
	cfg.RiskControl.AverageWindow = 5
	cfg.RiskControl.VolumeMultiplier = 2.0
	cfg.RiskControl.RecoveryThreshold = 1

	// 構造历史 K 線數據
	historical := make([]*exchange.Candle, 0)
	for i := 0; i < 10; i++ {
		historical = append(historical, &exchange.Candle{
			Symbol:   "BTCUSDT",
			Close:    100.0,
			Volume:   1000.0,
			IsClosed: true,
		})
	}

	ex := &MockRiskExchange{HistoricalKlines: historical}
	rm := NewRiskMonitor(cfg, ex)

	// 模拟初始化加載历史數據
	for _, symbol := range cfg.RiskControl.MonitorSymbols {
		rm.symbolDataMap[symbol].candles = historical
	}

	// 场景 1: 正常行情
	rm.onCandleUpdate(&exchange.Candle{
		Symbol:   "BTCUSDT",
		Close:    101.0,
		Volume:   1100.0,
		IsClosed: true,
	})
	if rm.IsTriggered() {
		t.Error("正常行情下不应触发风控")
	}

	// 场景 2: 触发风控（價格下跌且成交量放大）
	rm.onCandleUpdate(&exchange.Candle{
		Symbol:   "BTCUSDT",
		Close:    90.0,   // 均價 100
		Volume:   3000.0, // 均量 1000, 阈值 2000
		IsClosed: true,
	})
	if !rm.IsTriggered() {
		t.Error("價格大跌且成交量激增時应触发风控")
	}

	// 场景 3: 恢複行情
	// 需要连续的正常 K 線来將均值拉回
	rm.onCandleUpdate(&exchange.Candle{
		Symbol:   "BTCUSDT",
		Close:    110.0,
		Volume:   500.0,
		IsClosed: true,
	})
	if rm.IsTriggered() {
		t.Error("行情恢複后应解除风控")
	}
}
