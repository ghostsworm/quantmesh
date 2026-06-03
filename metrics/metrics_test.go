package metrics

import (
	"testing"
	"time"
)

func TestMetricsCollectorRecordsLatestValues(t *testing.T) {
	collector := NewMetricsCollector()
	initial := collector.GetMetrics().LastUpdate

	duration := 123 * time.Millisecond
	collector.RecordOrderExecution(duration)
	if got := collector.GetMetrics().OrderExecutionTime; got != duration {
		t.Fatalf("OrderExecutionTime = %v, want %v", got, duration)
	}
	if !collector.GetMetrics().LastUpdate.After(initial) && collector.GetMetrics().LastUpdate != initial {
		t.Fatalf("LastUpdate moved unexpectedly: initial=%v got=%v", initial, collector.GetMetrics().LastUpdate)
	}

	collector.RecordPnL(42.5)
	if got := collector.GetMetrics().AveragePnL; got != 42.5 {
		t.Fatalf("AveragePnL = %v, want 42.5", got)
	}

	before := collector.GetMetrics().LastUpdate
	collector.RecordOrderResult(true)
	if collector.GetMetrics().LastUpdate.Before(before) {
		t.Fatalf("LastUpdate moved backwards: before=%v after=%v", before, collector.GetMetrics().LastUpdate)
	}
}

func TestGetPrometheusMetricsReturnsSingleton(t *testing.T) {
	first := GetPrometheusMetrics()
	second := GetPrometheusMetrics()
	if first == nil || second == nil {
		t.Fatal("expected non-nil prometheus metrics")
	}
	if first != second {
		t.Fatalf("GetPrometheusMetrics returned different instances: %p vs %p", first, second)
	}
}

func TestPrometheusMetricsRecordersDoNotPanic(t *testing.T) {
	pm := NewPrometheusMetrics()

	pm.RecordOrder("binance", "BTCUSDT", "buy", "submitted")
	pm.RecordOrderSuccess("binance", "BTCUSDT", "buy", 10*time.Millisecond)
	pm.RecordOrderFailure("binance", "BTCUSDT", "sell", "timeout")
	pm.RecordOrderDuration("binance", "BTCUSDT", "buy", 5*time.Millisecond)
	pm.RecordTrade("binance", "BTCUSDT", "buy", 0.1, 6000)
	pm.SetPnL("binance", "BTCUSDT", 12.5)
	pm.RecordRealizedPnL("binance", "BTCUSDT", 3.5)
	pm.SetWinRate("binance", "BTCUSDT", 55)
	pm.SetRiskControlStatus("binance", "BTCUSDT", true)
	pm.RecordRiskControlTrigger("binance", "BTCUSDT", "drawdown")
	pm.SetMarginUsageRatio("binance", "BTCUSDT", 0.2)
	pm.SetPositionRisk("binance", "BTCUSDT", 0.4)
	pm.SetPositionSize("binance", "BTCUSDT", 0.3)
	pm.SetPositionValue("binance", "BTCUSDT", 18000)
	pm.SetActiveOrdersCount("binance", "BTCUSDT", "buy", 2)
	pm.SetGoroutineCount(12)
	pm.RecordGCPause(time.Millisecond)
	pm.SetMemoryAlloc(1024)
	pm.AddMemoryTotalAlloc(2048)
	pm.SetWebSocketStatus("binance", "ticker", true)
	pm.RecordWebSocketReconnect("binance", "ticker")
	pm.RecordAPICall("binance", "/orders", "ok", 20*time.Millisecond)
	pm.RecordAPIRateLimitHit("binance")
	pm.RecordFixSessionLogon("session-1", "bot-1")
	pm.RecordFixOrder("session-1", "new", "ok")
	pm.RecordFixSessionTimeout("session-1")
	pm.SetCurrentPrice("binance", "BTCUSDT", 60000)
	pm.RecordPriceUpdate("binance", "BTCUSDT")
	pm.RecordReconciliation("binance", "BTCUSDT")
	pm.RecordReconciliationDiff("binance", "BTCUSDT", "missing_order")
	pm.RecordLockAcquire("bot-lock", "success")
	pm.RecordLockConflict("bot-lock")
	pm.RecordLockHoldDuration("bot-lock", 30*time.Millisecond)
	pm.SetNewsRiskScore(66)
	pm.SetBitcoinCrashProbability(0.12)
	pm.SetHighRiskNewsCount(3)
	pm.SetNewsPredictionProbability("24h", "down", 10, 0.25)
	pm.SetNewsRecommendation("reduce_position")
	pm.SetNewsRecommendation("stop_trading")
	pm.SetNewsRecommendation("unknown")
	pm.SetNewsLastAnalysisTimestamp(123456789)
	pm.SetNewsCollectedCount(7)
}
