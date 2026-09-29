package inspector

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	"quantmesh/monitor"
	"quantmesh/storage"
	"quantmesh/utils"
)

// SnapshotSource 提供單個交易對的當前快照（與 monitor.RuntimeSnapshotSource 一致）
type SnapshotSource interface {
	Exchange() string
	Symbol() string
	Account() string
	CurrentSnapshot() (currentPrice, unrealizedPnL, totalPositionValue float64)
}

type scopedPnLSnapshotSource interface {
	AccountScope() string
	MarketType() string
	PnLAsset() string
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
	GetSnapshotSources func() []SnapshotSource
	Storage            storage.Storage
	GetNewsRisk        NewsRiskProvider
	IsRiskTriggered    RiskTriggerProvider
	GetPrice           PriceProvider
	GetAccountSummary  AccountSummaryProvider
	GetGoldAnalysis    GoldAnalysisProvider
}

// Collect 採集當前快照
func (c *Collector) Collect(ctx context.Context) *InspectionSnapshot {
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now()
	snap := &InspectionSnapshot{
		Timestamp:  now,
		NewsRisk:   make(map[string]*monitor.NewsRiskAssessment),
		MarketData: make(map[string]MarketInfo),
	}

	sources := c.GetSnapshotSources()
	if sources == nil {
		return snap
	}

	// 彙總持倉；账户权益只允许在单一明确账户下展示。
	seenAccount := make(map[string]bool)
	accountCount := 0
	for _, src := range sources {
		ex, sym, account := src.Exchange(), src.Symbol(), src.Account()
		price, unrealized, totalVal := src.CurrentSnapshot()
		position := PositionInfo{
			Exchange:      ex,
			Symbol:        sym,
			CurrentPrice:  price,
			UnrealizedPnL: unrealized,
			PositionValue: totalVal,
		}
		if scoped, ok := src.(scopedPnLSnapshotSource); ok {
			position.PnLAsset = strings.ToUpper(strings.TrimSpace(scoped.PnLAsset()))
		}
		snap.Positions = append(snap.Positions, position)
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
		if !seenAccount[key] {
			seenAccount[key] = true
			accountCount++
		}
		if accountCount == 1 && !seenAccount[key+":queried"] && c.GetAccountSummary != nil {
			seenAccount[key+":queried"] = true
			acc, err := c.GetAccountSummary(ctx, ex, account)
			if err == nil {
				snap.AccountSummary = acc
			}
		}
	}

	if accountCount != 1 {
		snap.AccountSummary = AccountSummary{}
	}

	// 已實現盈亏只有在精確凭据作用域与统一计价币均可证明时才汇总。
	if c.Storage != nil && len(sources) > 0 {
		snap.PnLSummary = collectScopedPnLSummary(c.Storage, sources, now)
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

type inspectorPnLScope struct {
	exchange string
	scope    string
	asset    string
}

func collectScopedPnLSummary(st storage.Storage, sources []SnapshotSource, now time.Time) PnLSummary {
	reader, ok := st.(interface {
		GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
	})
	if !ok {
		return PnLSummary{}
	}
	scopes, asset, ok := inspectorPnLScopes(sources)
	if !ok {
		return PnLSummary{}
	}
	windows := buildInspectorPnLTimeWindows(now)
	var result PnLSummary
	result.PnLAsset = asset
	for _, scope := range scopes {
		values, err := queryInspectorPnLWindows(reader, scope, windows)
		if err != nil {
			return PnLSummary{}
		}
		result.TodayRealized += values.today
		result.TodayTrades += values.todayTrades
		result.WeekRealized += values.week
		result.WeekTrades += values.weekTrades
		result.MonthRealized += values.month
		result.MonthTrades += values.monthTrades
		result.TotalRealized += values.total
	}
	if !finiteInspectorPnL(result) {
		return PnLSummary{}
	}
	result.Verified = true
	return result
}

func inspectorPnLScopes(sources []SnapshotSource) ([]inspectorPnLScope, string, bool) {
	unique := make(map[string]inspectorPnLScope)
	asset := ""
	for _, source := range sources {
		scoped, ok := source.(scopedPnLSnapshotSource)
		if !ok || strings.TrimSpace(source.Exchange()) == "" || strings.TrimSpace(scoped.AccountScope()) == "" || strings.TrimSpace(scoped.MarketType()) == "" {
			return nil, "", false
		}
		sourceAsset := strings.ToUpper(strings.TrimSpace(scoped.PnLAsset()))
		if sourceAsset == "" || (asset != "" && sourceAsset != asset) {
			return nil, "", false
		}
		asset = sourceAsset
		scope := inspectorPnLScope{exchange: strings.ToLower(strings.TrimSpace(source.Exchange())), scope: strings.TrimSpace(scoped.AccountScope()), asset: sourceAsset}
		unique[scope.exchange+"\x00"+scope.scope+"\x00"+scope.asset] = scope
	}
	if len(unique) == 0 {
		return nil, "", false
	}
	scopes := make([]inspectorPnLScope, 0, len(unique))
	for _, scope := range unique {
		scopes = append(scopes, scope)
	}
	return scopes, asset, true
}

type inspectorPnLTimeWindows struct {
	today, week, month, total time.Time
	end                       time.Time
}

func buildInspectorPnLTimeWindows(now time.Time) inspectorPnLTimeWindows {
	localNow := now.In(utils.NowConfiguredTimezone().Location())
	today := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, localNow.Location())
	week := today.AddDate(0, 0, -int(localNow.Weekday()))
	if localNow.Weekday() == 0 {
		week = today.AddDate(0, 0, -6)
	}
	month := time.Date(localNow.Year(), localNow.Month(), 1, 0, 0, 0, 0, localNow.Location())
	return inspectorPnLTimeWindows{today: today.UTC(), week: week.UTC(), month: month.UTC(), total: time.Unix(0, 0).UTC(), end: now.UTC()}
}

type inspectorPnLValues struct {
	today, week, month, total            float64
	todayTrades, weekTrades, monthTrades int
}

func queryInspectorPnLWindows(reader interface {
	GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
}, scope inspectorPnLScope, windows inspectorPnLTimeWindows) (inspectorPnLValues, error) {
	var values inspectorPnLValues
	for _, window := range []struct {
		start  time.Time
		target *float64
		trades *int
	}{{windows.today, &values.today, &values.todayTrades}, {windows.week, &values.week, &values.weekTrades}, {windows.month, &values.month, &values.monthTrades}, {windows.total, &values.total, nil}} {
		rows, err := reader.GetPnLByAccountScopeAndAsset(scope.exchange, scope.scope, scope.asset, window.start, windows.end)
		if err != nil {
			return inspectorPnLValues{}, fmt.Errorf("query inspector scoped PnL: %w", err)
		}
		for _, row := range rows {
			if row == nil || !strings.EqualFold(row.PnLAsset, scope.asset) || math.IsNaN(row.TotalPnL) || math.IsInf(row.TotalPnL, 0) {
				return inspectorPnLValues{}, fmt.Errorf("inspector PnL row has incomplete asset or amount evidence")
			}
			*window.target += row.TotalPnL
			if window.trades != nil {
				*window.trades += row.TotalTrades
			}
		}
	}
	return values, nil
}

func finiteInspectorPnL(summary PnLSummary) bool {
	for _, value := range []float64{summary.TodayRealized, summary.WeekRealized, summary.MonthRealized, summary.TotalRealized} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func displayAsset(asset string) string {
	if strings.TrimSpace(asset) == "" {
		return "未核實計價幣"
	}
	return strings.ToUpper(strings.TrimSpace(asset))
}
