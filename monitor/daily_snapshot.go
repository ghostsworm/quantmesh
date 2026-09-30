package monitor

import (
	"context"
	"math"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/storage"
	"quantmesh/utils"
)

// accountEquitySampler 可選：由 snapshotRuntimeAdapter 實現，小時任務中調用交易所 GetAccount
type accountEquitySampler interface {
	AccountEquityUSDT(ctx context.Context) (float64, bool)
}

type spotInventorySampler interface {
	SpotInventoryQty(ctx context.Context) (float64, bool)
}

type marketTypeSnapshotSource interface {
	MarketType() string
}

type pnlAssetSnapshotSource interface {
	PnLAsset() string
}

type accountScopeSnapshotSource interface {
	AccountScope() string
}

type marketAwareEquityStorage interface {
	QueryHourlyEquityRecordsByMarketType(exchange, marketType, symbol, account string, startTime, endTime time.Time) ([]*storage.HourlyEquityRecord, error)
}

type scopedEquityStorage interface {
	QueryHourlyEquityRecordsByScope(exchange, marketType, symbol, accountScope string, startTime, endTime time.Time) ([]*storage.HourlyEquityRecord, error)
}

type accountEquityRecordWriter interface {
	SaveAccountEquityRecord(*storage.AccountEquityRecord) error
}

type accountEquityRecordCleaner interface {
	DeleteAccountEquityRecordsBefore(time.Time) error
}

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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	sampledAccounts := make(map[string]bool)
	legacyAccountEquity := make(map[string]float64)
	accountEquityStore, hasAccountEquityStore := r.storage.(accountEquityRecordWriter)
	for _, rt := range runtimes {
		marketPrice, unrealized, totalVal := rt.CurrentSnapshot()
		marketType := snapshotMarketType(rt)
		accountScope := snapshotAccountScope(rt)
		pnlAsset := snapshotPnLAsset(rt)
		var spotPositionQty *float64
		if strings.EqualFold(marketType, "spot") {
			if sampler, ok := rt.(spotInventorySampler); ok {
				if qty, available := sampler.SpotInventoryQty(ctx); available && !math.IsNaN(qty) && !math.IsInf(qty, 0) && qty >= 0 {
					spotPositionQty = &qty
				}
			}
		}
		equity := totalVal // 持倉市值（與日內回撤計算一致）
		rec := &storage.HourlyEquityRecord{
			Exchange:           rt.Exchange(),
			MarketType:         marketType,
			AccountScope:       accountScope,
			Symbol:             rt.Symbol(),
			Account:            rt.Account(),
			Timestamp:          ts,
			Equity:             equity,
			UnrealizedPnL:      unrealized,
			UnrealizedPnLAsset: pnlAsset,
			TotalPositionValue: totalVal,
			MarketPrice:        marketPrice,
			SpotPositionQty:    spotPositionQty,
		}
		accountIdentity := accountScope
		if accountIdentity == "" {
			accountIdentity = "legacy:" + rt.Account()
		}
		accountKey := rt.Exchange() + "\x00" + marketType + "\x00" + accountIdentity
		if sampler, ok := rt.(accountEquitySampler); ok && !sampledAccounts[accountKey] {
			sampledAccounts[accountKey] = true
			if value, available := sampler.AccountEquityUSDT(ctx); available {
				if hasAccountEquityStore {
					if err := accountEquityStore.SaveAccountEquityRecord(&storage.AccountEquityRecord{Exchange: rt.Exchange(), MarketType: marketType, AccountScope: accountScope, Account: rt.Account(), Timestamp: ts, AccountEquity: value}); err != nil {
						logger.Warn("⚠️ 保存账户级权益记录失败 %s: %v", rt.Exchange(), err)
					}
				} else {
					// Legacy storage fallback: keep one sample only, never duplicate it per symbol.
					legacyAccountEquity[accountKey] = value
				}
			}
		}
		if value, ok := legacyAccountEquity[accountKey]; ok {
			rec.AccountEquity = &value
			delete(legacyAccountEquity, accountKey)
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
	endOfDay := startOfDay.AddDate(0, 0, 1)

	runtimes := r.getRuntimes()
	for _, rt := range runtimes {
		exchange, symbol, account := rt.Exchange(), rt.Symbol(), rt.Account()
		marketType := snapshotMarketType(rt)
		accountScope := snapshotAccountScope(rt)
		var records []*storage.HourlyEquityRecord
		var err error
		if scopedStorage, ok := r.storage.(scopedEquityStorage); ok && accountScope != "" {
			records, err = scopedStorage.QueryHourlyEquityRecordsByScope(exchange, marketType, symbol, accountScope, startOfDay, endOfDay)
		} else if marketStorage, ok := r.storage.(marketAwareEquityStorage); ok {
			records, err = marketStorage.QueryHourlyEquityRecordsByMarketType(exchange, marketType, symbol, account, startOfDay, endOfDay)
		} else {
			records, err = r.storage.QueryHourlyEquityRecords(exchange, symbol, account, startOfDay, endOfDay)
		}
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

		// 收盤時刻的未實現盈虧與持倉價值取當日最后一條市場小時記錄。
		last := records[len(records)-1]
		pnlAsset := consistentSnapshotPnLAsset(records)
		snap := &storage.DailySnapshot{
			Exchange:               exchange,
			MarketType:             marketType,
			AccountScope:           accountScope,
			Symbol:                 symbol,
			Account:                account,
			Date:                   startOfDay,
			UnrealizedPnL:          last.UnrealizedPnL,
			UnrealizedPnLAsset:     pnlAsset,
			TotalPositionValue:     last.TotalPositionValue,
			ClosingPrice:           last.MarketPrice,
			SpotPositionQty:        last.SpotPositionQty,
			IntradayMaxDrawdown:    maxDrawdown,
			IntradayMaxDrawdownPct: maxDrawdownPct,
			IntradayPeakEquity:     peakEquity,
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	accountEquityStore, hasAccountEquityStore := r.storage.(accountEquityRecordWriter)
	sampledAccounts := make(map[string]bool)
	legacyAccountEquity := make(map[string]float64)
	for _, rt := range runtimes {
		marketType := snapshotMarketType(rt)
		accountKey := rt.Exchange() + "\x00" + marketType + "\x00" + snapshotIdentity(rt)
		if sampledAccounts[accountKey] {
			continue
		}
		sampler, ok := rt.(accountEquitySampler)
		if !ok {
			continue
		}
		sampledAccounts[accountKey] = true
		if value, available := sampler.AccountEquityUSDT(ctx); available {
			if hasAccountEquityStore {
				if err := accountEquityStore.SaveAccountEquityRecord(&storage.AccountEquityRecord{Exchange: rt.Exchange(), MarketType: marketType, AccountScope: snapshotAccountScope(rt), Account: rt.Account(), Timestamp: ts, AccountEquity: value}); err != nil {
					logger.Warn("⚠️ 保存 0 点账户级权益记录失败 %s: %v", rt.Exchange(), err)
				}
			} else {
				legacyAccountEquity[accountKey] = value
			}
		}
	}
	for _, rt := range runtimes {
		exchange, symbol, account := rt.Exchange(), rt.Symbol(), rt.Account()
		marketType := snapshotMarketType(rt)
		pnlAsset := snapshotPnLAsset(rt)
		marketPrice, unrealized, totalVal := rt.CurrentSnapshot()
		var spotPositionQty *float64
		if strings.EqualFold(marketType, "spot") {
			if sampler, ok := rt.(spotInventorySampler); ok {
				if qty, available := sampler.SpotInventoryQty(ctx); available && !math.IsNaN(qty) && !math.IsInf(qty, 0) && qty >= 0 {
					spotPositionQty = &qty
				}
			}
		}
		snap := &storage.DailySnapshot{
			Exchange:               exchange,
			MarketType:             marketType,
			AccountScope:           snapshotAccountScope(rt),
			Symbol:                 symbol,
			Account:                account,
			Date:                   today,
			UnrealizedPnL:          unrealized,
			UnrealizedPnLAsset:     pnlAsset,
			TotalPositionValue:     totalVal,
			ClosingPrice:           marketPrice,
			SpotPositionQty:        spotPositionQty,
			IntradayMaxDrawdown:    0,
			IntradayMaxDrawdownPct: 0,
			IntradayPeakEquity:     totalVal,
			SnapshotTime:           ts,
		}
		accountKey := exchange + "\x00" + marketType + "\x00" + snapshotIdentity(rt)
		if value, ok := legacyAccountEquity[accountKey]; ok {
			snap.AccountEquity = &value
			delete(legacyAccountEquity, accountKey)
		}
		if err := r.storage.SaveDailySnapshot(snap); err != nil {
			logger.Warn("⚠️ 保存 0 點未實現快照失敗 %s:%s %s: %v", exchange, symbol, today.Format("2006-01-02"), err)
		}
	}
}

func snapshotMarketType(rt RuntimeSnapshotSource) string {
	if source, ok := rt.(marketTypeSnapshotSource); ok {
		return source.MarketType()
	}
	return ""
}

func snapshotPnLAsset(rt RuntimeSnapshotSource) string {
	source, ok := rt.(pnlAssetSnapshotSource)
	if !ok {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(source.PnLAsset()))
}

func consistentSnapshotPnLAsset(records []*storage.HourlyEquityRecord) string {
	asset := ""
	for _, record := range records {
		if record == nil {
			return ""
		}
		current := strings.ToUpper(strings.TrimSpace(record.UnrealizedPnLAsset))
		if current == "" || (asset != "" && current != asset) {
			return ""
		}
		asset = current
	}
	return asset
}

func snapshotAccountScope(rt RuntimeSnapshotSource) string {
	if source, ok := rt.(accountScopeSnapshotSource); ok {
		return source.AccountScope()
	}
	return ""
}

func snapshotIdentity(rt RuntimeSnapshotSource) string {
	if scope := snapshotAccountScope(rt); scope != "" {
		return scope
	}
	return "legacy:" + rt.Account()
}

// cleanupOldHourlyData 刪除超過保留天數的小時級數據
func (r *DailySnapshotRunner) cleanupOldHourlyData() {
	cutoff := time.Now().AddDate(0, 0, -r.cleanupRetentionDays)
	if err := r.storage.DeleteHourlyEquityRecordsBefore(cutoff); err != nil {
		logger.Warn("⚠️ 清理過期小時數據失敗: %v", err)
	}
	if cleaner, ok := r.storage.(accountEquityRecordCleaner); ok {
		if err := cleaner.DeleteAccountEquityRecordsBefore(cutoff); err != nil {
			logger.Warn("⚠️ 清理過期账户权益数据失败: %v", err)
		}
	}
}
