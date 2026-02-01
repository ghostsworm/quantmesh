package inspector

import (
	"context"
	"time"

	"quantmesh/monitor"
	"quantmesh/storage"
)

// SnapshotSource 提供單個交易對的當前快照（與 monitor.RuntimeSnapshotSource 一致）
type SnapshotSource interface {
	Exchange() string
	Symbol() string
	Account() string
	CurrentSnapshot() (currentPrice, unrealizedPnL, totalPositionValue float64)
}

// AccountSummaryProvider 提供賬戶彙總（由 main 注入交易所調用）
type AccountSummaryProvider func(ctx context.Context, exchange, account string) (AccountSummary, error)

// NewsRiskProvider 提供新聞風險評估（由 main 注入 NewsMonitor）
type NewsRiskProvider func(symbol string) *monitor.NewsRiskAssessment

// RiskTriggerProvider 提供風控觸發狀態
type RiskTriggerProvider func() (triggered bool, message string)

// PriceProvider 提供當前價格
type PriceProvider func(symbol string) float64

// GoldAnalysisProvider 提供黃金專項分析（可選）
type GoldAnalysisProvider func() *GoldAnalysis

// Collector 智子巡檢數據收集器
type Collector struct {
	GetSnapshotSources   func() []SnapshotSource
	Storage              storage.Storage
	GetNewsRisk          NewsRiskProvider
	IsRiskTriggered      RiskTriggerProvider
	GetPrice             PriceProvider
	GetAccountSummary    AccountSummaryProvider
	GetGoldAnalysis      GoldAnalysisProvider
}

// Collect 採集當前快照
func (c *Collector) Collect(ctx context.Context) *InspectionSnapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	snap := &InspectionSnapshot{
		Timestamp:   now,
		NewsRisk:    make(map[string]*monitor.NewsRiskAssessment),
		MarketData:  make(map[string]MarketInfo),
	}

	sources := c.GetSnapshotSources()
	if sources == nil {
		return snap
	}

	// 彙總持倉與權益
	seenAccount := make(map[string]bool)
	for _, src := range sources {
		ex, sym, account := src.Exchange(), src.Symbol(), src.Account()
		price, unrealized, totalVal := src.CurrentSnapshot()
		snap.Positions = append(snap.Positions, PositionInfo{
			Exchange:      ex,
			Symbol:        sym,
			CurrentPrice:  price,
			UnrealizedPnL: unrealized,
			PositionValue: totalVal,
		})
		mi := MarketInfo{Symbol: sym, Exchange: ex, CurrentPrice: price, LastUpdated: now}
		if c.GetPrice != nil {
			mi.CurrentPrice = c.GetPrice(sym)
		}
		if c.Storage != nil {
			if rate, err := c.Storage.GetLatestFundingRate(sym, ex); err == nil {
				mi.FundingRate = rate
			}
		}
		snap.MarketData[sym] = mi
		key := ex + ":" + account
		if !seenAccount[key] && c.GetAccountSummary != nil {
			seenAccount[key] = true
			acc, err := c.GetAccountSummary(ctx, ex, account)
			if err == nil {
				snap.AccountSummary = acc
				// 若多賬戶則只保留第一個；可擴展為 []AccountSummary
				break
			}
		}
	}

	// 若無賬戶彙總，從持倉推算未實現與總權益
	if snap.AccountSummary.TotalBalance == 0 && len(snap.Positions) > 0 {
		var totalUnrealized, totalValue float64
		for _, p := range snap.Positions {
			totalUnrealized += p.UnrealizedPnL
			totalValue += p.PositionValue
		}
		snap.AccountSummary.UnrealizedPnL = totalUnrealized
		snap.AccountSummary.Currency = "USDT"
	}

	// 盈虧統計（今日/本週/本月）
	if c.Storage != nil && len(sources) > 0 {
		account := sources[0].Account()
		todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
		weekStart := todayStart.AddDate(0, 0, -int(now.Weekday()))
		if now.Weekday() == 0 {
			weekStart = todayStart.AddDate(0, 0, -6)
		}
		monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		pnlToday, _ := c.Storage.GetPnLByTimeRange(account, todayStart, now)
		pnlWeek, _ := c.Storage.GetPnLByTimeRange(account, weekStart, now)
		pnlMonth, _ := c.Storage.GetPnLByTimeRange(account, monthStart, now)
		for _, p := range pnlToday {
			snap.PnLSummary.TodayRealized += p.TotalPnL
			snap.PnLSummary.TodayTrades += p.TotalTrades
		}
		for _, p := range pnlWeek {
			snap.PnLSummary.WeekRealized += p.TotalPnL
			snap.PnLSummary.WeekTrades += p.TotalTrades
		}
		for _, p := range pnlMonth {
			snap.PnLSummary.MonthRealized += p.TotalPnL
			snap.PnLSummary.MonthTrades += p.TotalTrades
		}
		// 總已實現從統計表或累加
		stats, _ := c.Storage.GetStatisticsSummary(account)
		if stats != nil {
			snap.PnLSummary.TotalRealized = stats.TotalPnL
		}
		for _, p := range snap.Positions {
			snap.PnLSummary.UnrealizedPnL += p.UnrealizedPnL
		}
	}

	// 風控狀態
	if c.IsRiskTriggered != nil {
		triggered, msg := c.IsRiskTriggered()
		snap.RiskStatus = RiskStatus{
			Triggered:   triggered,
			Reason:      msg,
			TriggeredAt: now,
		}
	}

	// 新聞風險（按資產）
	if c.GetNewsRisk != nil {
		if a := c.GetNewsRisk("BTCUSDT"); a != nil {
			snap.NewsRisk["crypto_btc"] = a
		}
		if a := c.GetNewsRisk("PAXGUSDT"); a != nil {
			snap.NewsRisk["commodity_gold"] = a
		}
	}

	// 黃金專項
	if c.GetGoldAnalysis != nil {
		snap.GoldAnalysis = c.GetGoldAnalysis()
	}

	return snap
}
