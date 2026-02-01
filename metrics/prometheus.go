package metrics

import (
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	once sync.Once
	// 订單指標
	orderTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_order_total",
			Help: "Total number of orders placed",
		},
		[]string{"exchange", "symbol", "side", "status"},
	)

	orderSuccessTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_order_success_total",
			Help: "Total number of successful orders",
		},
		[]string{"exchange", "symbol", "side"},
	)

	orderFailureTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_order_failure_total",
			Help: "Total number of failed orders",
		},
		[]string{"exchange", "symbol", "side", "reason"},
	)

	orderDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "quantmesh_order_duration_seconds",
			Help:    "Order execution duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0},
		},
		[]string{"exchange", "symbol", "side"},
	)

	// 交易指標
	tradeVolume = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_trade_volume_total",
			Help: "Total trading volume in base currency",
		},
		[]string{"exchange", "symbol", "side"},
	)

	tradeAmount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_trade_amount_total",
			Help: "Total trading amount in quote currency",
		},
		[]string{"exchange", "symbol", "side"},
	)

	tradeCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_trade_count_total",
			Help: "Total number of trades executed",
		},
		[]string{"exchange", "symbol", "side"},
	)

	// 盈亏指標
	pnlTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_pnl_total",
			Help: "Total profit and loss",
		},
		[]string{"exchange", "symbol"},
	)

	pnlRealized = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_pnl_realized_total",
			Help: "Total realized profit and loss",
		},
		[]string{"exchange", "symbol"},
	)

	winRate = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_win_rate",
			Help: "Win rate percentage (0-100)",
		},
		[]string{"exchange", "symbol"},
	)

	// 风控指標
	riskControlTriggered = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_risk_control_triggered",
			Help: "Risk control status (0=normal, 1=triggered)",
		},
		[]string{"exchange", "symbol"},
	)

	riskControlTriggerCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_risk_control_trigger_count_total",
			Help: "Total number of risk control triggers",
		},
		[]string{"exchange", "symbol", "reason"},
	)

	marginUsageRatio = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_margin_usage_ratio",
			Help: "Margin usage ratio (0-1)",
		},
		[]string{"exchange", "symbol"},
	)

	positionRisk = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_position_risk",
			Help: "Position risk score",
		},
		[]string{"exchange", "symbol"},
	)

	// 持倉指標
	positionSize = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_position_size",
			Help: "Current position size",
		},
		[]string{"exchange", "symbol"},
	)

	positionValue = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_position_value",
			Help: "Current position value in quote currency",
		},
		[]string{"exchange", "symbol"},
	)

	activeOrdersCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_active_orders_count",
			Help: "Number of active orders",
		},
		[]string{"exchange", "symbol", "side"},
	)

	// 系统指標
	goroutineCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_goroutine_count",
			Help: "Number of goroutines",
		},
	)

	gcPauseDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "quantmesh_gc_pause_duration_seconds",
			Help:    "GC pause duration in seconds",
			Buckets: []float64{0.0001, 0.0005, 0.001, 0.005, 0.01, 0.05, 0.1},
		},
	)

	// 分布式鎖指標
	lockAcquireTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_lock_acquire_total",
			Help: "Total number of lock acquisitions",
		},
		[]string{"key", "status"}, // status: success, failed, skipped
	)

	lockHoldDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "quantmesh_lock_hold_duration_seconds",
			Help:    "Lock hold duration in seconds",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1.0, 5.0, 10.0, 30.0},
		},
		[]string{"key"},
	)

	lockConflictTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_lock_conflict_total",
			Help: "Total number of lock conflicts",
		},
		[]string{"key"},
	)

	memoryAllocBytes = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_memory_alloc_bytes",
			Help: "Bytes of allocated heap objects",
		},
	)

	memoryTotalAllocBytes = promauto.NewCounter(
		prometheus.CounterOpts{
			Name: "quantmesh_memory_total_alloc_bytes",
			Help: "Cumulative bytes allocated for heap objects",
		},
	)

	// 交易所指標
	websocketConnected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_websocket_connected",
			Help: "WebSocket connection status (0=disconnected, 1=connected)",
		},
		[]string{"exchange", "stream_type"},
	)

	websocketReconnectCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_websocket_reconnect_count_total",
			Help: "Total number of WebSocket reconnections",
		},
		[]string{"exchange", "stream_type"},
	)

	apiCallTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_api_call_total",
			Help: "Total number of API calls",
		},
		[]string{"exchange", "endpoint", "status"},
	)

	apiCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "quantmesh_api_call_duration_seconds",
			Help:    "API call duration in seconds",
			Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1.0, 2.0, 5.0, 10.0},
		},
		[]string{"exchange", "endpoint"},
	)

	apiRateLimitHit = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_api_rate_limit_hit_total",
			Help: "Total number of API rate limit hits",
		},
		[]string{"exchange"},
	)

	// 價格指標
	currentPrice = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_current_price",
			Help: "Current market price",
		},
		[]string{"exchange", "symbol"},
	)

	priceUpdateCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_price_update_count_total",
			Help: "Total number of price updates received",
		},
		[]string{"exchange", "symbol"},
	)

	// 對账指標
	reconciliationCount = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_reconciliation_count_total",
			Help: "Total number of reconciliations performed",
		},
		[]string{"exchange", "symbol"},
	)

	reconciliationDiffFound = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "quantmesh_reconciliation_diff_found_total",
			Help: "Total number of reconciliation differences found",
		},
		[]string{"exchange", "symbol", "type"},
	)

	// 新聞監控指標
	newsRiskScore = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_news_risk_score",
			Help: "News-based risk score (0-100)",
		},
	)

	bitcoinCrashProbability = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_bitcoin_crash_probability",
			Help: "Bitcoin crash probability based on news (0-1)",
		},
	)

	highRiskNewsCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_high_risk_news_count",
			Help: "Number of high-risk news items in the last 24 hours",
		},
	)

	newsPredictionProbability = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "quantmesh_news_prediction_probability",
			Help: "News-based price prediction probability (0-1) by timeframe, direction, change_percent",
		},
		[]string{"timeframe", "direction", "change_percent"},
	)

	newsRecommendation = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_news_recommendation",
			Help: "Current recommendation: 0=normal, 1=caution, 2=reduce_position, 3=stop_trading",
		},
	)

	newsLastAnalysisTimestamp = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_news_last_analysis_timestamp",
			Help: "Unix timestamp of last news analysis",
		},
	)

	newsCollectedCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "quantmesh_news_collected_count",
			Help: "Number of news items in the 2-hour cache",
		},
	)
)

// PrometheusMetrics Prometheus 指標收集器
type PrometheusMetrics struct {
	mu sync.RWMutex
}

// NewPrometheusMetrics 創建 Prometheus 指標收集器
func NewPrometheusMetrics() *PrometheusMetrics {
	return &PrometheusMetrics{}
}

// 訂單相關指標記錄

// RecordOrder 記錄订單
func (pm *PrometheusMetrics) RecordOrder(exchange, symbol, side, status string) {
	orderTotal.WithLabelValues(exchange, symbol, side, status).Inc()
}

// RecordOrderSuccess 記錄订單成功
func (pm *PrometheusMetrics) RecordOrderSuccess(exchange, symbol, side string, duration time.Duration) {
	orderSuccessTotal.WithLabelValues(exchange, symbol, side).Inc()
	orderDuration.WithLabelValues(exchange, symbol, side).Observe(duration.Seconds())
}

// RecordOrderFailure 記錄订單失败
func (pm *PrometheusMetrics) RecordOrderFailure(exchange, symbol, side, reason string) {
	orderFailureTotal.WithLabelValues(exchange, symbol, side, reason).Inc()
}

// RecordOrderDuration 記錄訂單執行時长
func (pm *PrometheusMetrics) RecordOrderDuration(exchange, symbol, side string, duration time.Duration) {
	orderDuration.WithLabelValues(exchange, symbol, side).Observe(duration.Seconds())
}

// 交易相关指標記錄

// RecordTrade 記錄交易
func (pm *PrometheusMetrics) RecordTrade(exchange, symbol, side string, volume, amount float64) {
	tradeVolume.WithLabelValues(exchange, symbol, side).Add(volume)
	tradeAmount.WithLabelValues(exchange, symbol, side).Add(amount)
	tradeCount.WithLabelValues(exchange, symbol, side).Inc()
}

// 盈亏相关指標記錄

// SetPnL 設置盈亏
func (pm *PrometheusMetrics) SetPnL(exchange, symbol string, pnl float64) {
	pnlTotal.WithLabelValues(exchange, symbol).Set(pnl)
}

// RecordRealizedPnL 記錄已實現盈亏
func (pm *PrometheusMetrics) RecordRealizedPnL(exchange, symbol string, pnl float64) {
	pnlRealized.WithLabelValues(exchange, symbol).Add(pnl)
}

// SetWinRate 設置胜率
func (pm *PrometheusMetrics) SetWinRate(exchange, symbol string, rate float64) {
	winRate.WithLabelValues(exchange, symbol).Set(rate)
}

// 风控相关指標記錄

// SetRiskControlStatus 設置风控状態
func (pm *PrometheusMetrics) SetRiskControlStatus(exchange, symbol string, triggered bool) {
	value := 0.0
	if triggered {
		value = 1.0
	}
	riskControlTriggered.WithLabelValues(exchange, symbol).Set(value)
}

// RecordRiskControlTrigger 記錄风控触发
func (pm *PrometheusMetrics) RecordRiskControlTrigger(exchange, symbol, reason string) {
	riskControlTriggerCount.WithLabelValues(exchange, symbol, reason).Inc()
}

// SetMarginUsageRatio 設置保证金使用率
func (pm *PrometheusMetrics) SetMarginUsageRatio(exchange, symbol string, ratio float64) {
	marginUsageRatio.WithLabelValues(exchange, symbol).Set(ratio)
}

// SetPositionRisk 設置持倉风險
func (pm *PrometheusMetrics) SetPositionRisk(exchange, symbol string, risk float64) {
	positionRisk.WithLabelValues(exchange, symbol).Set(risk)
}

// 持倉相关指標記錄

// SetPositionSize 設置持倉大小
func (pm *PrometheusMetrics) SetPositionSize(exchange, symbol string, size float64) {
	positionSize.WithLabelValues(exchange, symbol).Set(size)
}

// SetPositionValue 設置持倉價值
func (pm *PrometheusMetrics) SetPositionValue(exchange, symbol string, value float64) {
	positionValue.WithLabelValues(exchange, symbol).Set(value)
}

// SetActiveOrdersCount 設置活跃订單數量
func (pm *PrometheusMetrics) SetActiveOrdersCount(exchange, symbol, side string, count int) {
	activeOrdersCount.WithLabelValues(exchange, symbol, side).Set(float64(count))
}

// 系统相关指標記錄

// SetGoroutineCount 設置 Goroutine 數量
func (pm *PrometheusMetrics) SetGoroutineCount(count int) {
	goroutineCount.Set(float64(count))
}

// RecordGCPause 記錄 GC 停顿時间
func (pm *PrometheusMetrics) RecordGCPause(duration time.Duration) {
	gcPauseDuration.Observe(duration.Seconds())
}

// SetMemoryAlloc 設置記憶體分配
func (pm *PrometheusMetrics) SetMemoryAlloc(bytes uint64) {
	memoryAllocBytes.Set(float64(bytes))
}

// AddMemoryTotalAlloc 增加累计記憶體分配
func (pm *PrometheusMetrics) AddMemoryTotalAlloc(bytes uint64) {
	memoryTotalAllocBytes.Add(float64(bytes))
}

// 交易所相关指標記錄

// SetWebSocketStatus 設置 WebSocket 连接状態
func (pm *PrometheusMetrics) SetWebSocketStatus(exchange, streamType string, connected bool) {
	value := 0.0
	if connected {
		value = 1.0
	}
	websocketConnected.WithLabelValues(exchange, streamType).Set(value)
}

// RecordWebSocketReconnect 記錄 WebSocket 重连
func (pm *PrometheusMetrics) RecordWebSocketReconnect(exchange, streamType string) {
	websocketReconnectCount.WithLabelValues(exchange, streamType).Inc()
}

// RecordAPICall 記錄 API 調用
func (pm *PrometheusMetrics) RecordAPICall(exchange, endpoint, status string, duration time.Duration) {
	apiCallTotal.WithLabelValues(exchange, endpoint, status).Inc()
	apiCallDuration.WithLabelValues(exchange, endpoint).Observe(duration.Seconds())
}

// RecordAPIRateLimitHit 記錄 API 限流
func (pm *PrometheusMetrics) RecordAPIRateLimitHit(exchange string) {
	apiRateLimitHit.WithLabelValues(exchange).Inc()
}

// 價格相关指標記錄

// SetCurrentPrice 設置當前價格
func (pm *PrometheusMetrics) SetCurrentPrice(exchange, symbol string, price float64) {
	currentPrice.WithLabelValues(exchange, symbol).Set(price)
}

// RecordPriceUpdate 記錄價格更新
func (pm *PrometheusMetrics) RecordPriceUpdate(exchange, symbol string) {
	priceUpdateCount.WithLabelValues(exchange, symbol).Inc()
}

// 對账相关指標記錄

// RecordReconciliation 記錄對账
func (pm *PrometheusMetrics) RecordReconciliation(exchange, symbol string) {
	reconciliationCount.WithLabelValues(exchange, symbol).Inc()
}

// RecordReconciliationDiff 記錄對账差异
func (pm *PrometheusMetrics) RecordReconciliationDiff(exchange, symbol, diffType string) {
	reconciliationDiffFound.WithLabelValues(exchange, symbol, diffType).Inc()
}

// 分布式鎖相关指標記錄

// RecordLockAcquire 記錄鎖獲取
func (pm *PrometheusMetrics) RecordLockAcquire(key, status string) {
	lockAcquireTotal.WithLabelValues(key, status).Inc()
}

// RecordLockConflict 記錄鎖冲突
func (pm *PrometheusMetrics) RecordLockConflict(key string) {
	lockConflictTotal.WithLabelValues(key).Inc()
}

// RecordLockHoldDuration 記錄鎖持有時长
func (pm *PrometheusMetrics) RecordLockHoldDuration(key string, duration time.Duration) {
	lockHoldDuration.WithLabelValues(key).Observe(duration.Seconds())
}

// 新聞監控相关指標記錄

// SetNewsRiskScore 設置新闻风險评分
func (pm *PrometheusMetrics) SetNewsRiskScore(score float64) {
	newsRiskScore.Set(score)
}

// SetBitcoinCrashProbability 設置比特币大跌概率
func (pm *PrometheusMetrics) SetBitcoinCrashProbability(probability float64) {
	bitcoinCrashProbability.Set(probability)
}

// SetHighRiskNewsCount 設置高风險新闻數量
func (pm *PrometheusMetrics) SetHighRiskNewsCount(count int) {
	highRiskNewsCount.Set(float64(count))
}

// SetNewsPredictionProbability 設置指定场景的概率預测
func (pm *PrometheusMetrics) SetNewsPredictionProbability(timeframe, direction string, changePercent float64, probability float64) {
	changeLabel := strconv.FormatFloat(changePercent, 'f', 0, 64)
	newsPredictionProbability.WithLabelValues(timeframe, direction, changeLabel).Set(probability)
}

// SetNewsRecommendation 設置新闻推荐操作
func (pm *PrometheusMetrics) SetNewsRecommendation(rec string) {
	v := 0.0
	switch rec {
	case "caution":
		v = 1
	case "reduce_position":
		v = 2
	case "stop_trading":
		v = 3
	}
	newsRecommendation.Set(v)
}

// SetNewsLastAnalysisTimestamp 設置最近分析時间戳
func (pm *PrometheusMetrics) SetNewsLastAnalysisTimestamp(ts int64) {
	newsLastAnalysisTimestamp.Set(float64(ts))
}

// SetNewsCollectedCount 設置已收集新闻數量
func (pm *PrometheusMetrics) SetNewsCollectedCount(count int) {
	newsCollectedCount.Set(float64(count))
}

// 全局實例
var globalPrometheusMetrics *PrometheusMetrics

// GetPrometheusMetrics 獲取全局 Prometheus 指標收集器
func GetPrometheusMetrics() *PrometheusMetrics {
	once.Do(func() {
		globalPrometheusMetrics = NewPrometheusMetrics()
	})
	return globalPrometheusMetrics
}
