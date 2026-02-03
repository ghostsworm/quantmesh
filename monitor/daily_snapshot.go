package monitor

import (
	"context"
	"math"
	"time"

	"quantmesh/logger"
	"quantmesh/storage"
	"quantmesh/utils"
)

// RuntimeSnapshotSource 提供單個交易對的當前快照數據（由 main 注入）
type RuntimeSnapshotSource interface {
	Exchange() string
	Symbol() string
	Account() string
	CurrentSnapshot() (currentPrice, unrealizedPnL, totalPositionValue float64)
}

// DailySnapshotRunner 每日快照與小時權益記錄任務
type DailySnapshotRunner struct {
	storage              storage.Storage
	getRuntimes          func() []RuntimeSnapshotSource
	dailySchedule        string // "23:59"
	cleanupRetentionDays int    // 90
	ctx                  context.Context
	cancel               context.CancelFunc
}

// NewDailySnapshotRunner 創建快照任務
func NewDailySnapshotRunner(
	st storage.Storage,
	getRuntimes func() []RuntimeSnapshotSource,
	dailySchedule string,
	cleanupRetentionDays int,
) *DailySnapshotRunner {
	if dailySchedule == "" {
		dailySchedule = "23:59"
	}
	if cleanupRetentionDays <= 0 {
		cleanupRetentionDays = 90
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &DailySnapshotRunner{
		storage:              st,
		getRuntimes:          getRuntimes,
		dailySchedule:        dailySchedule,
		cleanupRetentionDays: cleanupRetentionDays,
		ctx:                  ctx,
		cancel:               cancel,
	}
}

// Start 啟動小時記錄與每日彙總循環
func (r *DailySnapshotRunner) Start() {
	if r.storage == nil || r.getRuntimes == nil {
		logger.Info("ℹ️ 每日快照未啟用（storage 或 getRuntimes 為空）")
		return
	}
	go r.hourlyLoop()
	logger.Info("✅ 每日快照任務已啟動（小時記錄 + 日終彙總 %s，清理保留 %d 天）", r.dailySchedule, r.cleanupRetentionDays)
}

// Stop 停止任務
func (r *DailySnapshotRunner) Stop() {
	if r.cancel != nil {
		r.cancel()
	}
}

// hourlyLoop 每小時寫入權益記錄，並在日終執行每日快照與清理
func (r *DailySnapshotRunner) hourlyLoop() {
	loc := utils.GlobalLocation
	if loc == nil {
		loc = time.Local
	}

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	// 首次延遲到下一整點
	now := time.Now().In(loc)
	nextHour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour()+1, 0, 0, 0, loc)
	if nextHour.Before(now) || nextHour.Equal(now) {
		nextHour = nextHour.Add(1 * time.Hour)
	}
	firstDelay := time.Until(nextHour)
	if firstDelay > 0 && firstDelay < 1*time.Hour {
		time.Sleep(firstDelay)
	}

	for {
		select {
		case <-r.ctx.Done():
			return
		case t := <-ticker.C:
			runAt := t.In(loc)
			r.recordHourlyForAll(runAt)
			// 每天 00:00 執行昨日日終彙總、今日 0 點未實現快照、與清理
			if runAt.Hour() == 0 && runAt.Minute() < 5 {
				yesterday := runAt.AddDate(0, 0, -1)
				r.aggregateDaily(yesterday)
				r.recordMidnightSnapshot(runAt) // 0 點當下的未實現盈虧快照
				r.cleanupOldHourlyData()
			}
		}
	}
}

// recordHourlyForAll 為所有 runtime 寫入當前小時權益記錄
func (r *DailySnapshotRunner) recordHourlyForAll(ts time.Time) {
	runtimes := r.getRuntimes()
	for _, rt := range runtimes {
		_, unrealized, totalVal := rt.CurrentSnapshot()
		equity := totalVal // 權益即當前持倉市值
		rec := &storage.HourlyEquityRecord{
			Exchange:           rt.Exchange(),
			Symbol:             rt.Symbol(),
			Account:            rt.Account(),
			Timestamp:          ts,
			Equity:             equity,
			UnrealizedPnL:      unrealized,
			TotalPositionValue: totalVal,
		}
		if err := r.storage.SaveHourlyEquityRecord(rec); err != nil {
			logger.Warn("⚠️ 保存小時權益記錄失敗 %s:%s: %v", rt.Exchange(), rt.Symbol(), err)
			continue
		}
	}
}

// aggregateDaily 彙總指定日期的日內最大回撤並寫入每日快照
func (r *DailySnapshotRunner) aggregateDaily(date time.Time) {
	loc := utils.GlobalLocation
	if loc == nil {
		loc = time.Local
	}
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, loc)
	endOfDay := startOfDay.Add(24 * time.Hour)

	runtimes := r.getRuntimes()
	for _, rt := range runtimes {
		exchange, symbol, account := rt.Exchange(), rt.Symbol(), rt.Account()
		records, err := r.storage.QueryHourlyEquityRecords(exchange, symbol, account, startOfDay, endOfDay)
		if err != nil {
			logger.Warn("⚠️ 查詢小時權益失敗 %s:%s: %v", exchange, symbol, err)
			continue
		}
		if len(records) == 0 {
			continue
		}

		var peakEquity float64 = -1
		var maxDrawdown float64
		var maxDrawdownPct float64
		for _, rec := range records {
			if rec.Equity > peakEquity {
				peakEquity = rec.Equity
			}
			if peakEquity <= 0 {
				continue
			}
			dd := peakEquity - rec.Equity
			if dd > maxDrawdown {
				maxDrawdown = dd
			}
			pct := (dd / peakEquity) * 100
			if pct > maxDrawdownPct {
				maxDrawdownPct = pct
			}
		}
		if math.IsInf(maxDrawdownPct, 0) || math.IsNaN(maxDrawdownPct) {
			maxDrawdownPct = 0
		}

		// 收盤時刻的未實現盈虧與持倉價值取當日最后一條小時記錄
		last := records[len(records)-1]
		snap := &storage.DailySnapshot{
			Exchange:               exchange,
			Symbol:                 symbol,
			Account:                account,
			Date:                   startOfDay,
			UnrealizedPnL:          last.UnrealizedPnL,
			TotalPositionValue:     last.TotalPositionValue,
			IntradayMaxDrawdown:    maxDrawdown,
			IntradayMaxDrawdownPct: maxDrawdownPct,
			IntradayPeakEquity:     peakEquity,
			ClosingPrice:           0, // 可從 K 線或 API 補齊
			SnapshotTime:           last.Timestamp,
		}
		if err := r.storage.SaveDailySnapshot(snap); err != nil {
			logger.Warn("⚠️ 保存每日快照失敗 %s:%s %s: %v", exchange, symbol, date.Format("2006-01-02"), err)
		}
	}
}

// recordMidnightSnapshot 在 0 點記錄當日的未實現盈虧快照（供收益統計日曆/每日統計使用）
func (r *DailySnapshotRunner) recordMidnightSnapshot(ts time.Time) {
	loc := utils.GlobalLocation
	if loc == nil {
		loc = time.Local
	}
	today := time.Date(ts.Year(), ts.Month(), ts.Day(), 0, 0, 0, 0, loc)
	runtimes := r.getRuntimes()
	for _, rt := range runtimes {
		exchange, symbol, account := rt.Exchange(), rt.Symbol(), rt.Account()
		_, unrealized, totalVal := rt.CurrentSnapshot()
		snap := &storage.DailySnapshot{
			Exchange:               exchange,
			Symbol:                 symbol,
			Account:                account,
			Date:                   today,
			UnrealizedPnL:          unrealized,
			TotalPositionValue:     totalVal,
			IntradayMaxDrawdown:    0,
			IntradayMaxDrawdownPct: 0,
			IntradayPeakEquity:     totalVal,
			ClosingPrice:           0,
			SnapshotTime:           ts,
		}
		if err := r.storage.SaveDailySnapshot(snap); err != nil {
			logger.Warn("⚠️ 保存 0 點未實現快照失敗 %s:%s %s: %v", exchange, symbol, today.Format("2006-01-02"), err)
		}
	}
}

// cleanupOldHourlyData 刪除超過保留天數的小時級數據
func (r *DailySnapshotRunner) cleanupOldHourlyData() {
	cutoff := time.Now().AddDate(0, 0, -r.cleanupRetentionDays)
	if err := r.storage.DeleteHourlyEquityRecordsBefore(cutoff); err != nil {
		logger.Warn("⚠️ 清理過期小時數據失敗: %v", err)
	}
}
