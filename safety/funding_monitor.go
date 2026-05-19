package safety

import (
	"context"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"sync"
	"time"
)

// FundingRateRecord 資金費率歷史記錄
type FundingRateRecord struct {
	Rate      float64
	Timestamp time.Time
}

// FundingRateMonitor 資金費率監控器
// 負責監控資金費率變化，提供費率偏向計算，以及告警服務
type FundingRateMonitor struct {
	cfg             *config.Config
	futuresExchange exchange.IExchange
	symbol          string

	// 緩存
	currentRate     float64
	nextFundingTime time.Time
	markPrice       float64
	indexPrice      float64
	rateHistory     []FundingRateRecord

	// 狀態
	mu         sync.RWMutex
	lastUpdate time.Time
	running    bool
	stopCh     chan struct{}

	// 告警狀態
	lastAlertTime time.Time
	alertCooldown time.Duration
}

// NewFundingRateMonitor 創建資金費率監控器
func NewFundingRateMonitor(cfg *config.Config, futuresExchange exchange.IExchange, symbol string) *FundingRateMonitor {
	return &FundingRateMonitor{
		cfg:             cfg,
		futuresExchange: futuresExchange,
		symbol:          symbol,
		rateHistory:     make([]FundingRateRecord, 0, 100),
		stopCh:          make(chan struct{}),
		alertCooldown:   5 * time.Minute, // 告警冷卻時間
	}
}

// Start 啟動監控
func (f *FundingRateMonitor) Start(ctx context.Context) {
	if !f.cfg.FundingRate.Enabled {
		logger.Info("⚠️ 資金費率監控未啟用")
		return
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if f.futuresExchange == nil {
		logger.Warn("⚠️ 資金費率監控交易所为空，跳过启动")
		return
	}

	f.mu.Lock()
	if f.running {
		f.mu.Unlock()
		return
	}
	stopCh := make(chan struct{})
	f.stopCh = stopCh
	f.running = true
	f.mu.Unlock()

	logger.Info("💰 啟動資金費率監控 (交易對: %s, 間隔: %d秒)", f.symbol, f.cfg.FundingRate.MonitorInterval)

	// 立即獲取一次
	if err := f.fetchFundingRate(ctx); err != nil {
		logger.Warn("⚠️ 初始獲取資金費率失敗: %v", err)
	} else {
		f.logCurrentRate()
	}

	// 啟動定期監控
	go f.monitorLoop(ctx, stopCh)
}

// Stop 停止監控
func (f *FundingRateMonitor) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()

	if !f.running {
		return
	}

	f.running = false
	close(f.stopCh)
	f.stopCh = nil
	logger.Info("💰 資金費率監控已停止")
}

// monitorLoop 監控循環
func (f *FundingRateMonitor) monitorLoop(ctx context.Context, stopCh chan struct{}) {
	interval := time.Duration(f.cfg.FundingRate.MonitorInterval) * time.Second
	if interval <= 0 {
		interval = 60 * time.Second
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			f.markStopped(stopCh)
			return
		case <-stopCh:
			return
		case <-ticker.C:
			if err := f.fetchFundingRate(ctx); err != nil {
				logger.Warn("⚠️ 獲取資金費率失敗: %v", err)
				continue
			}

			// 檢查是否需要告警
			f.checkAlert()
		}
	}
}

func (f *FundingRateMonitor) markStopped(stopCh chan struct{}) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.stopCh != stopCh {
		return
	}
	f.running = false
	f.stopCh = nil
}

// fetchFundingRate 獲取資金費率
func (f *FundingRateMonitor) fetchFundingRate(ctx context.Context) error {
	rate, err := f.futuresExchange.GetFundingRate(ctx, f.symbol)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.currentRate = rate
	f.lastUpdate = time.Now()

	// 記錄歷史
	f.rateHistory = append(f.rateHistory, FundingRateRecord{
		Rate:      rate,
		Timestamp: time.Now(),
	})

	// 保留最近 100 條記錄
	if len(f.rateHistory) > 100 {
		f.rateHistory = f.rateHistory[len(f.rateHistory)-100:]
	}

	return nil
}

// FetchFundingInfo 獲取完整的資金費率信息（包括下次結算時間）
// 這是一個輔助方法，用於需要完整信息的場景
func (f *FundingRateMonitor) FetchFundingInfo(ctx context.Context) error {
	// 嘗試調用交易所的 GetFundingInfo 方法（如果支持）
	// 由於接口限制，我們先獲取基本費率，然後嘗試獲取更多信息

	rate, err := f.futuresExchange.GetFundingRate(ctx, f.symbol)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	f.currentRate = rate
	f.lastUpdate = time.Now()

	// 記錄歷史
	f.rateHistory = append(f.rateHistory, FundingRateRecord{
		Rate:      rate,
		Timestamp: time.Now(),
	})

	// 保留最近 100 條記錄
	if len(f.rateHistory) > 100 {
		f.rateHistory = f.rateHistory[len(f.rateHistory)-100:]
	}

	// 計算預估的下次結算時間（幣安為每 8 小時：00:00, 08:00, 16:00 UTC）
	now := time.Now().UTC()
	hour := now.Hour()
	var nextHour int
	if hour < 8 {
		nextHour = 8
	} else if hour < 16 {
		nextHour = 16
	} else {
		nextHour = 24 // 明天 00:00
	}

	nextTime := time.Date(now.Year(), now.Month(), now.Day(), nextHour%24, 0, 0, 0, time.UTC)
	if nextHour == 24 {
		nextTime = nextTime.AddDate(0, 0, 1)
	}
	f.nextFundingTime = nextTime

	return nil
}

// logCurrentRate 記錄當前費率
func (f *FundingRateMonitor) logCurrentRate() {
	f.mu.RLock()
	rate := f.currentRate
	f.mu.RUnlock()

	ratePercent := rate * 100
	biasText := f.getBiasDescription()

	logger.Info("💰 當前資金費率: %.4f%% (%s)", ratePercent, biasText)
}

// getBiasDescription 獲取偏向描述
func (f *FundingRateMonitor) getBiasDescription() string {
	bias := f.GetBuyBias()
	switch {
	case bias >= 1.2:
		return "負費率，有利於多頭"
	case bias >= 1.0:
		return "正常費率"
	case bias >= 0.7:
		return "略高費率，減少買入"
	case bias >= 0.3:
		return "高費率，大幅減少買入"
	default:
		return "極高費率，暫停買入"
	}
}

// checkAlert 檢查是否需要告警
func (f *FundingRateMonitor) checkAlert() {
	f.mu.RLock()
	rate := f.currentRate
	lastAlert := f.lastAlertTime
	f.mu.RUnlock()

	alertThreshold := f.cfg.FundingRate.AlertThreshold
	if alertThreshold <= 0 {
		alertThreshold = 0.001 // 默認 0.1%
	}

	// 檢查冷卻時間
	if time.Since(lastAlert) < f.alertCooldown {
		return
	}

	// 高費率告警
	if rate > alertThreshold {
		f.mu.Lock()
		f.lastAlertTime = time.Now()
		f.mu.Unlock()

		ratePercent := rate * 100
		thresholdPercent := alertThreshold * 100
		logger.Warn("⚠️ [資金費率告警] 當前費率 %.4f%% 超過閾值 %.4f%%", ratePercent, thresholdPercent)
	}

	// 極端負費率告警（有利於多頭但可能預示市場異常）
	if rate < -alertThreshold {
		f.mu.Lock()
		f.lastAlertTime = time.Now()
		f.mu.Unlock()

		ratePercent := rate * 100
		logger.Info("📈 [資金費率提示] 當前負費率 %.4f%%，有利於多頭持倉", ratePercent)
	}
}

// GetCurrentRate 獲取當前資金費率
func (f *FundingRateMonitor) GetCurrentRate() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.currentRate
}

// GetNextFundingTime 獲取下次結算時間
func (f *FundingRateMonitor) GetNextFundingTime() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.nextFundingTime
}

// SetNextFundingTime 設置下次結算時間（由外部調用更新）
func (f *FundingRateMonitor) SetNextFundingTime(t time.Time) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextFundingTime = t
}

// GetMarkPrice 獲取標記價格
func (f *FundingRateMonitor) GetMarkPrice() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.markPrice
}

// SetMarkPrice 設置標記價格
func (f *FundingRateMonitor) SetMarkPrice(price float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markPrice = price
}

// GetIndexPrice 獲取指數價格
func (f *FundingRateMonitor) GetIndexPrice() float64 {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.indexPrice
}

// SetIndexPrice 設置指數價格
func (f *FundingRateMonitor) SetIndexPrice(price float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.indexPrice = price
}

// IsHighRate 判斷是否為高費率
func (f *FundingRateMonitor) IsHighRate() bool {
	f.mu.RLock()
	rate := f.currentRate
	f.mu.RUnlock()

	threshold := f.cfg.FundingRate.HighRateThreshold
	if threshold <= 0 {
		threshold = 0.001 // 默認 0.1%
	}

	return rate > threshold
}

// IsNegativeRate 判斷是否為負費率
func (f *FundingRateMonitor) IsNegativeRate() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.currentRate < 0
}

// GetBuyBias 獲取買入偏向係數
// 返回 0-1.2 的值：
// - 1.2: 負費率，可以略微增加買入
// - 1.0: 正常費率
// - 0.7: 略高費率，減少 30%
// - 0.3: 高費率，減少 70%
// - 0.0: 極高費率，暫停買入
func (f *FundingRateMonitor) GetBuyBias() float64 {
	// 如果費率偏向策略未啟用，返回 1.0（正常）
	if !f.cfg.FundingRate.BiasEnabled {
		return 1.0
	}

	f.mu.RLock()
	rate := f.currentRate
	f.mu.RUnlock()

	// 獲取閾值配置
	highThreshold := f.cfg.FundingRate.HighRateThreshold
	pauseThreshold := f.cfg.FundingRate.PauseBuyThreshold

	if highThreshold <= 0 {
		highThreshold = 0.001 // 默認 0.1%
	}
	if pauseThreshold <= 0 {
		pauseThreshold = 0.0015 // 默認 0.15%
	}

	// 計算中間閾值
	midThreshold := highThreshold / 2 // 0.05%
	lowMidThreshold := midThreshold   // 0.05%

	// 費率偏向計算
	switch {
	case rate <= 0:
		// 負費率：有利於多頭，可以略微增加買入
		return 1.2

	case rate <= lowMidThreshold:
		// 0 < rate <= 0.05%：正常
		return 1.0

	case rate <= highThreshold:
		// 0.05% < rate <= 0.1%：減少 30%
		return 0.7

	case rate <= pauseThreshold:
		// 0.1% < rate <= 0.15%：減少 70%
		return 0.3

	default:
		// rate > 0.15%：暫停買入
		return 0.0
	}
}

// ShouldPauseBuying 判斷是否應該暫停買入
func (f *FundingRateMonitor) ShouldPauseBuying() bool {
	return f.GetBuyBias() == 0
}

// GetRateHistory 獲取費率歷史
func (f *FundingRateMonitor) GetRateHistory() []FundingRateRecord {
	f.mu.RLock()
	defer f.mu.RUnlock()

	// 返回副本
	history := make([]FundingRateRecord, len(f.rateHistory))
	copy(history, f.rateHistory)
	return history
}

// GetLastUpdate 獲取最後更新時間
func (f *FundingRateMonitor) GetLastUpdate() time.Time {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.lastUpdate
}

// IsRunning 判斷監控是否正在運行
func (f *FundingRateMonitor) IsRunning() bool {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.running
}

// GetStatus 獲取監控狀態（用於 API 展示）
type FundingMonitorStatus struct {
	Enabled         bool      `json:"enabled"`
	Running         bool      `json:"running"`
	Symbol          string    `json:"symbol"`
	CurrentRate     float64   `json:"current_rate"`
	CurrentRatePct  float64   `json:"current_rate_pct"`
	NextFundingTime time.Time `json:"next_funding_time"`
	MarkPrice       float64   `json:"mark_price"`
	IndexPrice      float64   `json:"index_price"`
	BuyBias         float64   `json:"buy_bias"`
	BiasDescription string    `json:"bias_description"`
	IsHighRate      bool      `json:"is_high_rate"`
	IsNegativeRate  bool      `json:"is_negative_rate"`
	LastUpdate      time.Time `json:"last_update"`
}

// GetStatus 獲取監控狀態
func (f *FundingRateMonitor) GetStatus() FundingMonitorStatus {
	f.mu.RLock()
	defer f.mu.RUnlock()

	return FundingMonitorStatus{
		Enabled:         f.cfg.FundingRate.Enabled,
		Running:         f.running,
		Symbol:          f.symbol,
		CurrentRate:     f.currentRate,
		CurrentRatePct:  f.currentRate * 100,
		NextFundingTime: f.nextFundingTime,
		MarkPrice:       f.markPrice,
		IndexPrice:      f.indexPrice,
		BuyBias:         f.GetBuyBias(),
		BiasDescription: f.getBiasDescription(),
		IsHighRate:      f.currentRate > f.cfg.FundingRate.HighRateThreshold,
		IsNegativeRate:  f.currentRate < 0,
		LastUpdate:      f.lastUpdate,
	}
}

// TimeUntilNextFunding 獲取距離下次結算的時間
func (f *FundingRateMonitor) TimeUntilNextFunding() time.Duration {
	f.mu.RLock()
	nextTime := f.nextFundingTime
	f.mu.RUnlock()

	if nextTime.IsZero() {
		return 0
	}

	return time.Until(nextTime)
}

// IsNearFundingTime 判斷是否接近結算時間
// minutes: 結算前多少分鐘算「接近」
func (f *FundingRateMonitor) IsNearFundingTime(minutes int) bool {
	timeUntil := f.TimeUntilNextFunding()
	if timeUntil <= 0 {
		return false
	}

	return timeUntil <= time.Duration(minutes)*time.Minute
}
