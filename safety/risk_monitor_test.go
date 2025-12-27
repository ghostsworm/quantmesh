package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/exchange"
	"testing"
)

// MockRiskExchange 模拟风控所需的交易所方法
type MockRiskExchange struct {
	exchange.IExchange
	HistoricalKlines []*exchange.Candle
}

func (m *MockRiskExchange) GetHistoricalKlines(ctx context.Context, symbol, interval string, limit int) ([]*exchange.Candle, error) {
	return m.HistoricalKlines, nil
}

func (m *MockRiskExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback exchange.CandleUpdateCallback) error {
	return nil
}

func TestRiskMonitor_IsTriggered(t *testing.T) {
	cfg := &config.Config{}
	cfg.RiskControl.Enabled = true
	cfg.RiskControl.MonitorSymbols = []string{"BTCUSDT"}
	cfg.RiskControl.AverageWindow = 5
	cfg.RiskControl.VolumeMultiplier = 2.0
	cfg.RiskControl.RecoveryThreshold = 1

	// 构造历史 K 线数据
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

	// 模拟初始化加载历史数据
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

	// 场景 2: 触发风控（价格下跌且成交量放大）
	rm.onCandleUpdate(&exchange.Candle{
		Symbol:   "BTCUSDT",
		Close:    90.0,    // 均价 100
		Volume:   3000.0,  // 均量 1000, 阈值 2000
		IsClosed: true,
	})
	if !rm.IsTriggered() {
		t.Error("价格大跌且成交量激增时应触发风控")
	}

	// 场景 3: 恢复行情
	// 需要连续的正常 K 线来将均值拉回
	rm.onCandleUpdate(&exchange.Candle{
		Symbol:   "BTCUSDT",
		Close:    110.0,
		Volume:   500.0,
		IsClosed: true,
	})
	if rm.IsTriggered() {
		t.Error("行情恢复后应解除风控")
	}
}

