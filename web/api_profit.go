package web

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/profit"
	"quantmesh/storage"
	"quantmesh/utils"

	"github.com/gin-gonic/gin"
)

// ProfitSummary 盈利彙總
type ProfitSummary struct {
	ExchangeID                  string   `json:"exchangeId,omitempty"`
	TotalProfit                 float64  `json:"totalProfit"` // 淨利潤（毛利 - 手續費 + 資金費淨額）
	GrossProfit                 float64  `json:"grossProfit"` // 毛利（價差盈虧，未扣手續費）
	TotalFee                    float64  `json:"totalFee"`    // 手續費合計
	FundingNet                  float64  `json:"fundingNet"`  // 資金費淨額（正=淨收入，負=淨支出）
	TodayProfit                 float64  `json:"todayProfit"`
	WeekProfit                  float64  `json:"weekProfit"`
	MonthProfit                 float64  `json:"monthProfit"`
	UnrealizedProfit            float64  `json:"unrealizedProfit"` // 未實現盈利（根據當前倉位和價格計算）
	UnrealizedProfitVerified    bool     `json:"unrealizedProfitVerified"`
	ExchangeProfit              *float64 `json:"exchangeProfit,omitempty"` // 僅在當前憑據作用域可核驗時返回
	WithdrawnProfit             float64  `json:"withdrawnProfit"`
	AvailableToWithdraw         float64  `json:"availableToWithdraw"`
	AvailableToWithdrawVerified bool     `json:"availableToWithdrawVerified"`
	PriceDeviationLoss          float64  `json:"priceDeviationLoss"` // 🔥 價格偏差導致的總損失（USDT）
	BuyPriceDeviation           float64  `json:"buyPriceDeviation"`  // 🔥 買入價格偏差總和（USDT）
	SellPriceDeviation          float64  `json:"sellPriceDeviation"` // 🔥 賣出價格偏差總和（USDT）
	LastUpdated                 string   `json:"lastUpdated"`
}

// StrategyProfit 策略盈利
type StrategyProfit struct {
	ExchangeID                  string  `json:"exchangeId"`
	StrategyID                  string  `json:"strategyId"`
	MarketType                  string  `json:"marketType"`
	PnLAsset                    string  `json:"pnlAsset"`
	StrategyName                string  `json:"strategyName"`
	StrategyType                string  `json:"strategyType"`
	TotalProfit                 float64 `json:"totalProfit"`         // 网格方式盈亏
	ExchangeTotalProfit         float64 `json:"exchangeTotalProfit"` // 交易所方式盈亏
	TodayProfit                 float64 `json:"todayProfit"`
	UnrealizedProfit            float64 `json:"unrealizedProfit"`
	UnrealizedProfitVerified    bool    `json:"unrealizedProfitVerified"`
	RealizedProfit              float64 `json:"realizedProfit"`
	WithdrawnProfit             float64 `json:"withdrawnProfit"`
	AvailableToWithdraw         float64 `json:"availableToWithdraw"`
	AvailableToWithdrawVerified bool    `json:"availableToWithdrawVerified"`
	TradeCount                  int     `json:"tradeCount"`
	WinRate                     float64 `json:"winRate"`         // 网格方式胜率
	ExchangeWinRate             float64 `json:"exchangeWinRate"` // 交易所方式胜率
	AvgProfitPerTrade           float64 `json:"avgProfitPerTrade"`
	LastTradeAt                 string  `json:"lastTradeAt,omitempty"`
}

type verifiedUnrealizedPnLProvider interface {
	GetVerifiedUnrealizedPnL(currentPrice float64) (float64, bool)
}

type verifiedAssetUnrealizedPnLProvider interface {
	GetVerifiedUnrealizedPnLForAsset(currentPrice float64, asset string) (float64, bool)
}

func verifiedProviderPnL(provider PositionManagerProvider, slots []SlotInfo, exchange, asset string, currentPrice float64) (float64, bool) {
	if provider == nil || strings.TrimSpace(asset) == "" || !isFiniteNumber(currentPrice) || currentPrice <= 0 {
		return 0, false
	}
	if len(slots) == 0 {
		return 0, false
	}
	symbol := strings.TrimSpace(slots[0].Symbol)
	if symbol == "" {
		return 0, false
	}
	for _, slot := range slots {
		if exchange != "" && !strings.EqualFold(slot.Exchange, exchange) {
			return 0, false
		}
		if !strings.EqualFold(strings.TrimSpace(slot.Symbol), symbol) {
			return 0, false
		}
	}
	verifiedProvider, ok := provider.(verifiedUnrealizedPnLProvider)
	assetProvider, assetOK := provider.(verifiedAssetUnrealizedPnLProvider)
	if !ok || !assetOK {
		return 0, false
	}
	pnl, verified := verifiedProvider.GetVerifiedUnrealizedPnL(currentPrice)
	assetPnL, assetVerified := assetProvider.GetVerifiedUnrealizedPnLForAsset(currentPrice, asset)
	if !assetVerified || assetPnL != pnl {
		return 0, false
	}
	if !verified || !isFiniteNumber(pnl) {
		return 0, false
	}
	return pnl, true
}

func strategyProfitKey(exchange, marketType, symbol, asset string) string {
	return strings.ToLower(strings.TrimSpace(exchange)) + ":" + strings.ToLower(strings.TrimSpace(marketType)) + ":" + strings.ToLower(strings.TrimSpace(symbol)) + ":" + strings.ToLower(strings.TrimSpace(asset))
}

func validateStrategyPnLStream(stream *storage.PnLBySymbol) error {
	if stream == nil || strings.TrimSpace(stream.Exchange) == "" || strings.TrimSpace(stream.MarketType) == "" || strings.TrimSpace(stream.Symbol) == "" || strings.TrimSpace(stream.PnLAsset) == "" {
		return errors.New("strategy PnL identity is incomplete")
	}
	if stream.TotalTrades < 0 || stream.TotalVolume < 0 || stream.WinRate < 0 || stream.WinRate > 1 || stream.ExchangeWinRate < 0 || stream.ExchangeWinRate > 1 {
		return errors.New("strategy PnL statistics are outside valid ranges")
	}
	for _, value := range []float64{stream.TotalPnL, stream.ExchangePnL, stream.TotalVolume, stream.WinRate, stream.ExchangeWinRate} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("strategy PnL contains a non-finite value")
		}
	}
	return nil
}

// ProfitWithdrawRule 提取规则
type ProfitWithdrawRule struct {
	ID              string  `json:"id"`
	ExchangeID      string  `json:"exchangeId"`
	StrategyID      string  `json:"strategyId"`
	Type            string  `json:"type"`        // percentage, fixed, threshold
	TriggerType     string  `json:"triggerType"` // auto, manual, scheduled
	Threshold       float64 `json:"threshold"`
	Amount          float64 `json:"amount"`
	Percentage      float64 `json:"percentage"`
	TargetAddress   string  `json:"targetAddress"`
	Currency        string  `json:"currency"`
	IsEnabled       bool    `json:"enabled"` // 前端使用 enabled
	LastTriggeredAt string  `json:"lastTriggeredAt,omitempty"`
	CreatedAt       string  `json:"createdAt"`
	UpdatedAt       string  `json:"updatedAt"`
	// 前端額外需要的字段
	TriggerAmount     float64 `json:"triggerAmount,omitempty"` // 触发金額（對应 Threshold）
	WithdrawRatio     float64 `json:"withdrawRatio,omitempty"` // 提取比例 0-1（對应 Percentage/100）
	Frequency         string  `json:"frequency,omitempty"`     // immediate, daily, weekly
	Destination       string  `json:"destination,omitempty"`   // account, wallet
	MinWithdrawAmount float64 `json:"minWithdrawAmount,omitempty"`
}

// WithdrawRecord 提取記錄
type WithdrawRecord struct {
	ID            string  `json:"id"`
	ExchangeID    string  `json:"exchangeId"`
	StrategyID    string  `json:"strategyId"`
	StrategyName  string  `json:"strategyName"`
	Amount        float64 `json:"amount"`
	Fee           float64 `json:"fee"`
	NetAmount     float64 `json:"netAmount"`
	Currency      string  `json:"currency"`
	Type          string  `json:"type"`        // auto, manual
	Status        string  `json:"status"`      // pending, processing, completed, failed, cancelled
	Destination   string  `json:"destination"` // account, wallet
	WalletAddress string  `json:"walletAddress,omitempty"`
	TargetAddress string  `json:"targetAddress"` // 兼容舊版
	TxHash        string  `json:"txHash,omitempty"`
	CreatedAt     string  `json:"createdAt"`
	CompletedAt   string  `json:"completedAt,omitempty"`
	FailedReason  string  `json:"failedReason,omitempty"`
	Note          string  `json:"note,omitempty"`
	AccountScope  string  `json:"-"`
}

// ProfitTrendPoint 盈利趋势点
type ProfitTrendPoint struct {
	Timestamp string  `json:"timestamp"`
	Profit    float64 `json:"profit"`
	CumProfit float64 `json:"cumProfit"`
}

// FundingPaymentItem 資金費用記錄（API 返回）
type FundingPaymentItem struct {
	ID            int64   `json:"id"`
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	IncomeType    string  `json:"incomeType"`
	Income        float64 `json:"income"` // 正=收入，負=支出
	Asset         string  `json:"asset"`
	TransactionID int64   `json:"transactionId"`
	TradeTime     string  `json:"tradeTime"`
	CreatedAt     string  `json:"createdAt"`
}

func fundingPaymentItemFromRecord(payment *storage.FundingPayment, expectedScope, expectedExchange string) (FundingPaymentItem, error) {
	if payment == nil || strings.TrimSpace(expectedScope) == "" || strings.TrimSpace(expectedExchange) == "" {
		return FundingPaymentItem{}, errors.New("funding payment or expected account scope is missing")
	}
	if payment.AccountScope != expectedScope || !strings.EqualFold(strings.TrimSpace(payment.Exchange), strings.TrimSpace(expectedExchange)) {
		return FundingPaymentItem{}, errors.New("funding payment escaped the requested account or exchange scope")
	}
	if strings.TrimSpace(payment.Asset) == "" || math.IsNaN(payment.Income) || math.IsInf(payment.Income, 0) {
		return FundingPaymentItem{}, errors.New("funding payment identity or amount is invalid")
	}
	income, ok := roundProfitToPrecision(payment.Income, 1e8)
	if !ok {
		return FundingPaymentItem{}, errors.New("funding payment amount cannot be safely rounded")
	}
	return FundingPaymentItem{
		ID:            payment.ID,
		Exchange:      payment.Exchange,
		Symbol:        payment.Symbol,
		IncomeType:    payment.IncomeType,
		Income:        income,
		Asset:         payment.Asset,
		TransactionID: payment.TransactionID,
		TradeTime:     payment.TradeTime.Format(time.RFC3339),
		CreatedAt:     payment.CreatedAt.Format(time.RFC3339),
	}, nil
}

type fundingProfitSumReader interface {
	GetFundingPaymentsSum(account, exchange string, startTime, endTime time.Time) (float64, error)
}

type profitAccountScope struct {
	exchange string
	scope    string
}

type scopedProfitSummaryReader interface {
	GetStatisticsSummaryByAccountScope(exchange, accountScope, asset string) (*storage.Statistics, error)
	QueryDailyPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startDate, endDate time.Time) ([]*storage.DailyStatisticsWithTradeCount, error)
	GetFundingPaymentsSumByAccountScopeAndAsset(exchange, asset, accountScope string, startTime, endTime time.Time) (float64, error)
}

func resolveProfitAccountScopes(exchangeID string) ([]profitAccountScope, error) {
	cfg, err := GetLatestConfig()
	if err != nil {
		return nil, fmt.Errorf("load account configuration for profit scope: %w", err)
	}
	if cfg == nil {
		return nil, fmt.Errorf("account configuration for profit scope is unavailable")
	}
	exchanges := make([]string, 0, len(cfg.Exchanges))
	if exchangeID != "" {
		exchanges = append(exchanges, strings.TrimSpace(exchangeID))
	} else {
		for exchange, exchangeConfig := range cfg.Exchanges {
			if strings.TrimSpace(exchangeConfig.APIKey) != "" {
				exchanges = append(exchanges, exchange)
			}
		}
	}
	sort.Strings(exchanges)
	scopes := make([]profitAccountScope, 0, len(exchanges))
	for _, exchange := range exchanges {
		scope := accountIDForExchange(cfg, exchange)
		if scope != "" {
			scopes = append(scopes, profitAccountScope{exchange: exchange, scope: scope})
		}
	}
	if len(scopes) == 0 {
		return nil, fmt.Errorf("no configured credential scope is available for profit summary")
	}
	return scopes, nil
}

func readScopedFundingProfitTotals(reader scopedProfitSummaryReader, scopes []profitAccountScope, lifetimeStart, todayStart, weekStart, monthStart, end time.Time) (fundingProfitTotals, error) {
	var totals fundingProfitTotals
	queries := []struct {
		label string
		start time.Time
		dest  *float64
	}{
		{label: "累計", start: lifetimeStart, dest: &totals.Total},
		{label: "今日", start: todayStart, dest: &totals.Today},
		{label: "本週", start: weekStart, dest: &totals.Week},
		{label: "本月", start: monthStart, dest: &totals.Month},
	}
	for _, scope := range scopes {
		for _, query := range queries {
			value, err := reader.GetFundingPaymentsSumByAccountScopeAndAsset(scope.exchange, profitSummaryAsset, scope.scope, query.start, end)
			if err != nil {
				return fundingProfitTotals{}, fmt.Errorf("query %s scoped funding total for %s: %w", query.label, scope.exchange, err)
			}
			total, ok := addFiniteProfitValues(*query.dest, value)
			if !ok {
				return fundingProfitTotals{}, fmt.Errorf("query %s scoped funding total for %s is not finite", query.label, scope.exchange)
			}
			*query.dest = total
		}
	}
	return totals, nil
}

const profitSummaryAsset = "USDT"

type fundingProfitTotals struct {
	Total float64
	Today float64
	Week  float64
	Month float64
}

func addFiniteProfitValues(values ...float64) (float64, bool) {
	var total float64
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return 0, false
		}
		total += value
		if math.IsNaN(total) || math.IsInf(total, 0) {
			return 0, false
		}
	}
	return total, true
}

func addFiniteProfitToMap(totals map[string]float64, key string, value float64) error {
	if totals == nil {
		return errors.New("profit totals map is missing")
	}
	total, ok := addFiniteProfitValues(totals[key], value)
	if !ok {
		return errors.New("profit trend contains a non-finite or overflowing amount")
	}
	totals[key] = total
	return nil
}

func roundProfitToCents(value float64) (float64, bool) {
	return roundProfitToPrecision(value, 100)
}

func roundProfitToPrecision(value, precision float64) (float64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}
	if precision <= 0 || math.IsNaN(precision) || math.IsInf(precision, 0) {
		return 0, false
	}
	if math.Abs(value) > math.MaxFloat64/precision {
		return value, true
	}
	rounded := math.Round(value*precision) / precision
	return rounded, !math.IsNaN(rounded) && !math.IsInf(rounded, 0)
}

func roundStrategyProfitAmounts(profit *StrategyProfit) error {
	if profit == nil {
		return errors.New("strategy profit response is missing")
	}
	amounts := []*float64{
		&profit.TotalProfit, &profit.ExchangeTotalProfit, &profit.TodayProfit, &profit.UnrealizedProfit,
		&profit.RealizedProfit, &profit.WithdrawnProfit, &profit.AvailableToWithdraw, &profit.AvgProfitPerTrade,
	}
	for _, amount := range amounts {
		rounded, ok := roundProfitToCents(*amount)
		if !ok {
			return errors.New("strategy profit contains a non-finite amount")
		}
		*amount = rounded
	}
	return nil
}

func mergeProfitStatistics(target, source *storage.Statistics) error {
	if target == nil || source == nil || target.TotalTrades < 0 || source.TotalTrades < 0 ||
		source.TotalVolume < 0 || source.WinRate < 0 || source.WinRate > 1 {
		return errors.New("profit statistics are missing or outside valid ranges")
	}
	for _, value := range []float64{target.TotalVolume, target.TotalPnL, target.GrossPnL, target.TotalFee, target.TotalBuyDeviation, target.TotalSellDeviation,
		source.TotalVolume, source.TotalPnL, source.GrossPnL, source.TotalFee, source.TotalBuyDeviation, source.TotalSellDeviation, source.WinRate} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("profit statistics contain a non-finite value")
		}
	}
	maxInt := int(^uint(0) >> 1)
	if source.TotalTrades > maxInt-target.TotalTrades {
		return errors.New("profit trade count overflow")
	}
	merged := *target
	merged.TotalTrades += source.TotalTrades
	var ok bool
	if merged.TotalVolume, ok = addFiniteProfitValues(target.TotalVolume, source.TotalVolume); !ok {
		return errors.New("profit volume overflow")
	}
	if merged.TotalPnL, ok = addFiniteProfitValues(target.TotalPnL, source.TotalPnL); !ok {
		return errors.New("net profit overflow")
	}
	if merged.GrossPnL, ok = addFiniteProfitValues(target.GrossPnL, source.GrossPnL); !ok {
		return errors.New("gross profit overflow")
	}
	if merged.TotalFee, ok = addFiniteProfitValues(target.TotalFee, source.TotalFee); !ok {
		return errors.New("fee total overflow")
	}
	if merged.TotalBuyDeviation, ok = addFiniteProfitValues(target.TotalBuyDeviation, source.TotalBuyDeviation); !ok {
		return errors.New("buy price deviation overflow")
	}
	if merged.TotalSellDeviation, ok = addFiniteProfitValues(target.TotalSellDeviation, source.TotalSellDeviation); !ok {
		return errors.New("sell price deviation overflow")
	}
	*target = merged
	return nil
}

func validateProfitStatisticsSnapshot(summary *storage.Statistics) error {
	if summary == nil || summary.TotalTrades < 0 || summary.TotalVolume < 0 || summary.WinRate < 0 || summary.WinRate > 1 {
		return errors.New("profit statistics are missing or outside valid ranges")
	}
	for _, value := range []float64{summary.TotalVolume, summary.TotalPnL, summary.GrossPnL, summary.TotalFee,
		summary.TotalBuyDeviation, summary.TotalSellDeviation, summary.WinRate} {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return errors.New("profit statistics contain a non-finite value")
		}
	}
	return nil
}

const withdrawProfitHistoryLimit = 1000

type withdrawProfitStream struct {
	exchange string
	symbol   string
	asset    string
}

type withdrawProfitAmounts struct {
	withdrawn float64
	reserved  float64
}

type withdrawProfitLedger map[withdrawProfitStream]withdrawProfitAmounts

type withdrawProfitAvailability struct {
	amount   float64
	verified bool
}

type verifiedWithdrawProfit map[withdrawProfitStream]withdrawProfitAvailability

func classifyWithdrawProfitMarket(marketType string) (relevant, verified bool) {
	switch strings.ToLower(strings.TrimSpace(marketType)) {
	case "spot":
		return false, false
	case "futures":
		return true, true
	default:
		return true, false
	}
}

func newWithdrawProfitStream(exchangeID, symbol string, assets ...string) withdrawProfitStream {
	asset := ""
	if len(assets) > 0 {
		asset = assets[0]
	}
	return withdrawProfitStream{
		exchange: strings.ToUpper(strings.TrimSpace(exchangeID)),
		symbol:   strings.ToUpper(strings.TrimSpace(symbol)),
		asset:    strings.ToUpper(strings.TrimSpace(asset)),
	}
}

func readWithdrawProfitLedger(st storage.Storage, accountID string) (withdrawProfitLedger, error) {
	records, err := st.GetWithdrawRecords(accountID, withdrawProfitHistoryLimit)
	if err != nil {
		return nil, fmt.Errorf("read withdrawal ledger: %w", err)
	}
	if len(records) >= withdrawProfitHistoryLimit {
		return nil, fmt.Errorf("withdrawal ledger reached verification limit of %d records", withdrawProfitHistoryLimit)
	}
	return aggregateWithdrawProfitLedger(records)
}

func aggregateWithdrawProfitLedger(records []*storage.ProfitWithdrawRecord) (withdrawProfitLedger, error) {
	ledger := make(withdrawProfitLedger)
	for _, record := range records {
		if record == nil {
			return nil, errors.New("withdrawal ledger contains a nil record")
		}
		if record.Status == "failed" || record.Status == "cancelled" {
			continue
		}
		if record.Amount < 0 || math.IsNaN(record.Amount) || math.IsInf(record.Amount, 0) {
			return nil, fmt.Errorf("withdrawal ledger contains an invalid amount for record %s", record.ID)
		}
		if strings.TrimSpace(record.Currency) == "" {
			return nil, fmt.Errorf("withdrawal ledger record %s has no currency", record.ID)
		}
		key := newWithdrawProfitStream(record.ExchangeID, record.StrategyID, record.Currency)
		amounts := ledger[key]
		if record.Status == "completed" {
			amounts.withdrawn += record.Amount
		} else {
			// Unknown states are conservatively reserved until explicitly resolved.
			amounts.reserved += record.Amount
		}
		if math.IsNaN(amounts.withdrawn) || math.IsInf(amounts.withdrawn, 0) || math.IsNaN(amounts.reserved) || math.IsInf(amounts.reserved, 0) {
			return nil, fmt.Errorf("withdrawal ledger total is not finite for %s %s %s", key.exchange, key.symbol, key.asset)
		}
		ledger[key] = amounts
	}
	return ledger, nil
}

func (ledger withdrawProfitLedger) amountsFor(exchangeID, symbol string) (withdrawProfitAmounts, error) {
	return ledger.amountsForAsset(exchangeID, symbol, "")
}

func (ledger withdrawProfitLedger) amountsForAsset(exchangeID, symbol, asset string) (withdrawProfitAmounts, error) {
	exchangeFilter := strings.ToUpper(strings.TrimSpace(exchangeID))
	symbolFilter := strings.ToUpper(strings.TrimSpace(symbol))
	assetFilter := strings.ToUpper(strings.TrimSpace(asset))
	var total withdrawProfitAmounts
	for key, amounts := range ledger {
		if exchangeFilter != "" && key.exchange != exchangeFilter {
			continue
		}
		if symbolFilter != "" && key.symbol != symbolFilter {
			continue
		}
		if assetFilter != "" && key.asset != assetFilter {
			continue
		}
		var ok bool
		if total.withdrawn, ok = addFiniteProfitValues(total.withdrawn, amounts.withdrawn); !ok {
			return withdrawProfitAmounts{}, errors.New("withdrawn amount aggregation is non-finite or overflowed")
		}
		if total.reserved, ok = addFiniteProfitValues(total.reserved, amounts.reserved); !ok {
			return withdrawProfitAmounts{}, errors.New("reserved amount aggregation is non-finite or overflowed")
		}
	}
	return total, nil
}

func sumVerifiedWithdrawProfit(available verifiedWithdrawProfit, exchangeID, symbol string) (float64, bool, error) {
	exchangeFilter := strings.ToUpper(strings.TrimSpace(exchangeID))
	symbolFilter := strings.ToUpper(strings.TrimSpace(symbol))
	var total float64
	matched := false
	for key, amount := range available {
		if exchangeFilter != "" && key.exchange != exchangeFilter {
			continue
		}
		if symbolFilter != "" && key.symbol != symbolFilter {
			continue
		}
		matched = true
		if !amount.verified {
			return 0, false, nil
		}
		var ok bool
		if total, ok = addFiniteProfitValues(total, amount.amount); !ok {
			return 0, false, errors.New("verified withdrawal total is non-finite or overflowed")
		}
	}
	if !matched {
		return 0, false, nil
	}
	rounded, ok := roundProfitToCents(total)
	if !ok {
		return 0, false, errors.New("verified withdrawal total cannot be safely rounded")
	}
	return rounded, true, nil
}

func readVerifiedWithdrawProfit(st storage.Storage, accountID, exchangeID string, now time.Time) (verifiedWithdrawProfit, error) {
	scopes, err := resolveProfitAccountScopes(exchangeID)
	if err != nil {
		return nil, fmt.Errorf("resolve withdrawal PnL account scopes: %w", err)
	}
	reader, ok := st.(interface {
		GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
	})
	if !ok {
		return nil, errors.New("storage does not support scope- and asset-verified PnL streams")
	}
	available := make(verifiedWithdrawProfit)
	for _, scope := range scopes {
		streams, queryErr := reader.GetPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, time.Date(1970, 1, 1, 0, 0, 0, 0, time.UTC), now)
		if queryErr != nil {
			return nil, fmt.Errorf("read verified USDT PnL streams for %s: %w", scope.exchange, queryErr)
		}
		for _, stream := range streams {
			if stream == nil {
				return nil, fmt.Errorf("read verified USDT PnL streams for %s: result contains a missing row", scope.exchange)
			}
			if !strings.EqualFold(stream.PnLAsset, profitSummaryAsset) {
				continue
			}
			if strings.TrimSpace(stream.Exchange) == "" || strings.TrimSpace(stream.Symbol) == "" {
				return nil, fmt.Errorf("read verified USDT PnL streams for %s: result has incomplete exchange/symbol identity", scope.exchange)
			}
			relevant, marketVerified := classifyWithdrawProfitMarket(stream.MarketType)
			if !relevant {
				continue
			}
			key := newWithdrawProfitStream(stream.Exchange, stream.Symbol)
			if _, exists := available[key]; exists {
				available[key] = withdrawProfitAvailability{}
				continue
			}
			available[key] = withdrawProfitAvailability{}
			if !marketVerified {
				continue
			}
			_, _, _, amount, verifyErr := manualWithdrawWindow(st, accountID, scope.scope, stream.Exchange, stream.Symbol, 0, now)
			if verifyErr == nil && !math.IsNaN(amount) && !math.IsInf(amount, 0) {
				if amount < 0 {
					amount = 0
				}
				available[key] = withdrawProfitAvailability{amount: amount, verified: true}
			}
		}
	}
	return available, nil
}

func readFundingProfitTotals(reader fundingProfitSumReader, account, exchange string, lifetimeStart, todayStart, weekStart, monthStart, end time.Time) (fundingProfitTotals, error) {
	var totals fundingProfitTotals
	queries := []struct {
		label string
		start time.Time
		dest  *float64
	}{
		{label: "累計", start: lifetimeStart, dest: &totals.Total},
		{label: "今日", start: todayStart, dest: &totals.Today},
		{label: "本週", start: weekStart, dest: &totals.Week},
		{label: "本月", start: monthStart, dest: &totals.Month},
	}
	for _, query := range queries {
		value, err := reader.GetFundingPaymentsSum(account, exchange, query.start, end)
		if err != nil {
			return fundingProfitTotals{}, fmt.Errorf("query %s funding total: %w", query.label, err)
		}
		total, ok := addFiniteProfitValues(*query.dest, value)
		if !ok {
			return fundingProfitTotals{}, fmt.Errorf("query %s funding total is not finite", query.label)
		}
		*query.dest = total
	}
	return totals, nil
}

// 獲取盈利彙總
func getProfitSummaryHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()
	profitScopes, err := resolveProfitAccountScopes(exchangeID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法验证当前账户的盈利数据作用域: " + err.Error()})
		return
	}
	scopedProfitReader, ok := st.(scopedProfitSummaryReader)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储未提供按账户作用域和计价币核验的盈利查询"})
		return
	}

	// 1. 獲取累计盈利
	summaryStats := &storage.Statistics{}
	for _, scope := range profitScopes {
		part, queryErr := scopedProfitReader.GetStatisticsSummaryByAccountScope(scope.exchange, scope.scope, profitSummaryAsset)
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "盈利摘要未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		if err := mergeProfitStatistics(summaryStats, part); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "盈利摘要包含无效或溢出的统计数值: " + err.Error()})
			return
		}
	}

	// 2. 獲取今日/本周/本月盈利（按配置時區）
	now := utils.NowConfiguredTimezone()
	// 今日开始
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utils.GlobalLocation)
	// 本周开始（周一）
	offset := int(now.Weekday()) - 1
	if offset < 0 {
		offset = 6
	}
	weekStart := todayStart.AddDate(0, 0, -offset)
	// 本月开始
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, utils.GlobalLocation)

	dailyStatsByDate := make(map[string]float64)
	for _, scope := range profitScopes {
		dailyStats, queryErr := scopedProfitReader.QueryDailyPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, monthStart, now)
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "每日盈利未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		for _, stat := range dailyStats {
			if stat == nil || stat.Date.IsZero() {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "每日盈利统计包含缺失记录或日期"})
				return
			}
			day := stat.Date.Format("2006-01-02")
			total, ok := addFiniteProfitValues(dailyStatsByDate[day], stat.TotalPnL)
			if !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "每日盈利统计包含非有限数值或累加溢出"})
				return
			}
			dailyStatsByDate[day] = total
		}
	}

	todayProfit := 0.0
	weekProfit := 0.0
	monthProfit := 0.0

	todayKey, weekKey := todayStart.Format("2006-01-02"), weekStart.Format("2006-01-02")
	for day, pnl := range dailyStatsByDate {
		var ok bool
		if monthProfit, ok = addFiniteProfitValues(monthProfit, pnl); !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "月度盈利统计累加溢出"})
			return
		}
		if day >= todayKey {
			if todayProfit, ok = addFiniteProfitValues(todayProfit, pnl); !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "当日盈利统计累加溢出"})
				return
			}
		}
		if day >= weekKey {
			if weekProfit, ok = addFiniteProfitValues(weekProfit, pnl); !ok {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "本周盈利统计累加溢出"})
				return
			}
		}
	}

	// 3. 獲取未實現盈利 (Unrealized Profit)
	unrealizedProfit := 0.0
	unrealizedProfitVerified := false
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)

	if pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		currentPrice := 0.0
		if priceProv != nil {
			currentPrice = priceProv.GetLastPrice()
		}

		unrealizedProfit, unrealizedProfitVerified = verifiedProviderPnL(pmProvider, slots, exchangeID, profitSummaryAsset, currentPrice)
	}

	// 資金費用淨額（正=淨收入，負=淨支出）
	startAll := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	fundingTotals, err := readScopedFundingProfitTotals(scopedProfitReader, profitScopes, startAll, todayStart, weekStart, monthStart, now)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢資金費統計失敗: " + err.Error()})
		return
	}
	fundingSum, todayFunding, weekFunding, monthFunding := fundingTotals.Total, fundingTotals.Today, fundingTotals.Week, fundingTotals.Month
	netWithFunding, netOK := addFiniteProfitValues(summaryStats.TotalPnL, fundingSum)
	todayProfitWithFunding, todayOK := addFiniteProfitValues(todayProfit, todayFunding)
	weekProfitWithFunding, weekOK := addFiniteProfitValues(weekProfit, weekFunding)
	monthProfitWithFunding, monthOK := addFiniteProfitValues(monthProfit, monthFunding)

	// 🔥 计算价格偏差导致的损失
	// 买入价格偏差：如果实际买入价格高于委托价格，会导致成本增加（负值表示损失）
	// 卖出价格偏差：如果实际卖出价格低于委托价格，会导致收益减少（负值表示损失）
	// 总偏差损失 = 买入偏差（通常为负）+ 卖出偏差（通常为负）
	priceDeviationLoss, deviationOK := addFiniteProfitValues(summaryStats.TotalBuyDeviation, summaryStats.TotalSellDeviation)
	if !netOK || !todayOK || !weekOK || !monthOK || !deviationOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "盈利摘要最终合并数值无效或溢出"})
		return
	}

	// orders.realized_pnl 沒有記錄計價資產，不能與 USDT 損益混合後對外呈現。
	// exchangeProfit 保留 API 欄位相容性，但在訂單幣種證據補齊前不返回。
	var exchangeProfit *float64

	withdrawLedger, err := readWithdrawProfitLedger(st, accountID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法完整核验已提取及待处理金额: " + err.Error()})
		return
	}
	withdrawnAmounts, err := withdrawLedger.amountsForAsset(exchangeID, "", profitSummaryAsset)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "已提取或待处理金额汇总无效: " + err.Error()})
		return
	}
	verifiedAvailable, err := readVerifiedWithdrawProfit(st, accountID, exchangeID, now)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法完整核验可提现利润: " + err.Error()})
		return
	}
	availableToWithdraw, availableToWithdrawVerified, availabilityErr := sumVerifiedWithdrawProfit(verifiedAvailable, exchangeID, "")
	if availabilityErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "可提取金额汇总无效: " + availabilityErr.Error()})
		return
	}

	roundOK := true
	roundCents := func(value float64) float64 {
		rounded, ok := roundProfitToCents(value)
		if !ok {
			roundOK = false
			return 0
		}
		return rounded
	}
	summary := ProfitSummary{
		ExchangeID:                  exchangeID,
		TotalProfit:                 roundCents(netWithFunding),
		GrossProfit:                 roundCents(summaryStats.GrossPnL),
		TotalFee:                    roundCents(summaryStats.TotalFee),
		FundingNet:                  roundCents(fundingSum),
		TodayProfit:                 roundCents(todayProfitWithFunding),
		WeekProfit:                  roundCents(weekProfitWithFunding),
		MonthProfit:                 roundCents(monthProfitWithFunding),
		UnrealizedProfit:            roundCents(unrealizedProfit),
		UnrealizedProfitVerified:    unrealizedProfitVerified,
		ExchangeProfit:              exchangeProfit,
		WithdrawnProfit:             roundCents(withdrawnAmounts.withdrawn),
		AvailableToWithdraw:         availableToWithdraw,
		AvailableToWithdrawVerified: availableToWithdrawVerified,
		PriceDeviationLoss:          roundCents(priceDeviationLoss),
		BuyPriceDeviation:           roundCents(summaryStats.TotalBuyDeviation),
		SellPriceDeviation:          roundCents(summaryStats.TotalSellDeviation),
		LastUpdated:                 time.Now().Format(time.RFC3339),
	}
	if !roundOK {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "盈利摘要包含无法安全舍入的非有限数值"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"summary": summary,
	})
}

// 獲取資金費用明細
func getFundingHistoryHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")
	startStr := c.Query("start_time")
	endStr := c.Query("end_time")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	fundingExchange := strings.TrimSpace(exchangeID)
	if fundingExchange == "" {
		if cfg, cfgErr := GetLatestConfig(); cfgErr == nil && cfg != nil {
			fundingExchange = strings.TrimSpace(cfg.App.CurrentExchange)
		}
	}
	accountScope := accountScopeForExchange(fundingExchange)
	stWithFunding, ok := st.(interface {
		GetFundingPaymentsByAccountScope(accountScope, exchange string, startTime, endTime time.Time) ([]*storage.FundingPayment, error)
	})
	if !ok || accountScope == "" || fundingExchange == "" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "無法驗證資金費歷史帳戶作用域", "records": []FundingPaymentItem{}})
		return
	}

	now := utils.NowConfiguredTimezone()
	// 默認最近 30 天
	endTime := now
	startTime := now.AddDate(0, 0, -30)
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = t
		}
	}
	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = t
		}
	}
	if startTime.After(endTime) {
		startTime, endTime = endTime, startTime
	}

	list, err := stWithFunding.GetFundingPaymentsByAccountScope(accountScope, fundingExchange, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢資金費用失敗: " + err.Error(), "records": []FundingPaymentItem{}})
		return
	}

	records := make([]FundingPaymentItem, 0, len(list))
	for _, p := range list {
		item, itemErr := fundingPaymentItemFromRecord(p, accountScope, fundingExchange)
		if itemErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "资金费历史账本行核验失败: " + itemErr.Error(), "records": []FundingPaymentItem{}})
			return
		}
		records = append(records, item)
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "records": records})
}

// 按策略獲取盈利
func getStrategyProfitsHandler(c *gin.Context) {
	exchangeID := c.Query("exchange_id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	// 獲取當前账戶標识
	accountID := GetCurrentAccountID()
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := utils.NowConfiguredTimezone()
	scopes, err := resolveProfitAccountScopes(exchangeID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法验证策略盈利账户作用域: " + err.Error()})
		return
	}
	pnlReader, ok := st.(interface {
		GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储未提供按账户作用域和计价币核验的策略盈亏查询"})
		return
	}
	withdrawLedger, err := readWithdrawProfitLedger(st, accountID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法完整核验已提取及待处理金额: " + err.Error()})
		return
	}
	verifiedAvailable, err := readVerifiedWithdrawProfit(st, accountID, exchangeID, now)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法完整核验可提现利润: " + err.Error()})
		return
	}

	// 查詢所有時间的盈亏（按币种和交易所分组）
	var pnlList []*storage.PnLBySymbol
	for _, scope := range scopes {
		streams, queryErr := pnlReader.GetPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, startTime, now)
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略盈利未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		pnlList = append(pnlList, streams...)
	}

	// 獲取今日盈亏用於计算 TodayProfit（按配置時區）
	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, utils.GlobalLocation)
	var todayPnlList []*storage.PnLBySymbol
	for _, scope := range scopes {
		streams, queryErr := pnlReader.GetPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, todayStart, now)
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "今日策略盈利未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		todayPnlList = append(todayPnlList, streams...)
	}
	todayPnlMap := make(map[string]float64)
	for _, p := range todayPnlList {
		if p == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "今日策略盈亏包含缺失记录"})
			return
		}
		key := strategyProfitKey(p.Exchange, p.MarketType, p.Symbol, p.PnLAsset)
		total, ok := addFiniteProfitValues(todayPnlMap[key], p.TotalPnL)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "今日策略盈亏聚合溢出或包含非有限数值"})
			return
		}
		todayPnlMap[key] = total
	}

	// 獲取未實現盈亏
	unrealizedPnlMap := make(map[string]float64)
	unrealizedPnlVerifiedMap := make(map[string]bool)
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)
	if pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		currentPrice := 0.0
		if priceProv != nil {
			currentPrice = priceProv.GetLastPrice()
		}

		if len(slots) > 0 {
			marketTypes := make(map[string]struct{})
			for _, pnl := range pnlList {
				if pnl != nil && strings.EqualFold(pnl.Exchange, slots[0].Exchange) && strings.EqualFold(pnl.Symbol, slots[0].Symbol) && strings.EqualFold(pnl.PnLAsset, profitSummaryAsset) {
					marketTypes[strings.ToLower(strings.TrimSpace(pnl.MarketType))] = struct{}{}
				}
			}
			if len(marketTypes) == 1 {
				for marketType := range marketTypes {
					key := strategyProfitKey(slots[0].Exchange, marketType, slots[0].Symbol, profitSummaryAsset)
					unrealizedPnlMap[key], unrealizedPnlVerifiedMap[key] = verifiedProviderPnL(pmProvider, slots, slots[0].Exchange, profitSummaryAsset, currentPrice)
				}
			}
		}
	}

	profits := make([]StrategyProfit, 0)
	for _, p := range pnlList {
		if err := validateStrategyPnLStream(p); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略盈亏记录无效: " + err.Error()})
			return
		}
		// 如果指定了交易所且不匹配，跳過
		if exchangeID != "" && p.Exchange != exchangeID {
			continue
		}

		key := strategyProfitKey(p.Exchange, p.MarketType, p.Symbol, p.PnLAsset)

		// 暂時將 symbol 作為 strategyId
		strategyID := strings.ToLower(p.Symbol)
		if strings.Contains(strategyID, "usdt") {
			strategyID = strings.ReplaceAll(strategyID, "usdt", "")
		}
		withdrawnAmounts, amountErr := withdrawLedger.amountsForAsset(p.Exchange, p.Symbol, p.PnLAsset)
		if amountErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略提取金额汇总无效: " + amountErr.Error()})
			return
		}
		verifiedAmount := 0.0
		verifiedAmountOK := false
		if strings.EqualFold(p.MarketType, "futures") {
			if strings.EqualFold(p.PnLAsset, profitSummaryAsset) {
				availability := verifiedAvailable[newWithdrawProfitStream(p.Exchange, p.Symbol)]
				verifiedAmount = availability.amount
				verifiedAmountOK = availability.verified
			}
		}

		profit := StrategyProfit{
			ExchangeID:                  p.Exchange,
			StrategyID:                  p.Symbol, // 使用 Symbol 作為唯一標识
			MarketType:                  p.MarketType,
			PnLAsset:                    p.PnLAsset,
			StrategyName:                p.Symbol + " 策略",
			StrategyType:                "grid",        // 默认為网格，實際应從配置獲取
			TotalProfit:                 p.TotalPnL,    // 网格方式盈亏
			ExchangeTotalProfit:         p.ExchangePnL, // 交易所方式盈亏
			TodayProfit:                 todayPnlMap[key],
			UnrealizedProfit:            unrealizedPnlMap[key],
			UnrealizedProfitVerified:    unrealizedPnlVerifiedMap[key],
			RealizedProfit:              p.TotalPnL,
			WithdrawnProfit:             withdrawnAmounts.withdrawn,
			AvailableToWithdraw:         verifiedAmount,
			AvailableToWithdrawVerified: verifiedAmountOK,
			TradeCount:                  p.TotalTrades,
			WinRate:                     math.Round(p.WinRate*100) / 100,         // 网格方式胜率
			ExchangeWinRate:             math.Round(p.ExchangeWinRate*100) / 100, // 交易所方式胜率
			AvgProfitPerTrade:           0,                                       // 可计算
		}
		if err := roundStrategyProfitAmounts(&profit); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略盈利金额无法安全舍入: " + err.Error()})
			return
		}
		profits = append(profits, profit)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profits": profits,
	})
}

// 獲取單個策略盈利详情
func getStrategyProfitDetailHandler(c *gin.Context) {
	strategyID := c.Param("id") // 實際上这里傳的是 Symbol

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	// 查詢該幣種的所有盈亏
	startTime := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	accountID := GetCurrentAccountID()
	exchangeFilter := strings.ToLower(strings.TrimSpace(c.Query("exchange_id")))
	marketFilter := strings.ToLower(strings.TrimSpace(c.Query("market_type")))
	if (exchangeFilter == "") != (marketFilter == "") {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "exchange_id 与 market_type 必须同时提供"})
		return
	}
	scopes, err := resolveProfitAccountScopes(exchangeFilter)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法验证策略详情账户作用域: " + err.Error()})
		return
	}
	pnlReader, ok := st.(interface {
		GetPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startTime, endTime time.Time) ([]*storage.PnLBySymbol, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储未提供按账户作用域和计价币核验的策略盈亏查询"})
		return
	}
	var matches []*storage.PnLBySymbol
	for _, scope := range scopes {
		streams, queryErr := pnlReader.GetPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, startTime, now)
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略详情未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		for _, stream := range streams {
			if stream == nil || !strings.EqualFold(stream.Symbol, strategyID) {
				continue
			}
			if exchangeFilter != "" && (!strings.EqualFold(stream.Exchange, exchangeFilter) || !strings.EqualFold(stream.MarketType, marketFilter)) {
				continue
			}
			matches = append(matches, stream)
		}
	}
	if len(matches) > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "该交易对存在多个交易所或市场账本，请提供 exchange_id 与 market_type"})
		return
	}
	summary := &storage.PnLBySymbol{Symbol: strategyID, PnLAsset: profitSummaryAsset}
	if len(matches) == 1 {
		summary = matches[0]
	}
	if summary == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "策略盈亏查询结果无效"})
		return
	}
	if len(matches) == 1 {
		if err := validateStrategyPnLStream(summary); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略详情盈亏记录无效: " + err.Error()})
			return
		}
	}
	withdrawLedger, err := readWithdrawProfitLedger(st, accountID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法完整核验已提取及待处理金额: " + err.Error()})
		return
	}
	verifiedAvailable, err := readVerifiedWithdrawProfit(st, accountID, exchangeFilter, now)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法完整核验可提现利润: " + err.Error()})
		return
	}
	withdrawnAmounts, amountErr := withdrawLedger.amountsForAsset(summary.Exchange, strategyID, summary.PnLAsset)
	if amountErr != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略提取金额汇总无效: " + amountErr.Error()})
		return
	}

	// 獲取未實現盈亏
	unrealizedPnL := 0.0
	unrealizedPnLVerified := false
	pmProvider := PickPositionProvider(c)
	priceProv := PickPriceProvider(c)
	if pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		currentPrice := 0.0
		if priceProv != nil {
			currentPrice = priceProv.GetLastPrice()
		}

		if len(slots) > 0 && strings.EqualFold(slots[0].Symbol, strategyID) && strings.EqualFold(slots[0].Exchange, summary.Exchange) {
			unrealizedPnL, unrealizedPnLVerified = verifiedProviderPnL(pmProvider, slots, slots[0].Exchange, summary.PnLAsset, currentPrice)
		}
	}

	profit := StrategyProfit{
		ExchangeID:               summary.Exchange,
		StrategyID:               strategyID,
		MarketType:               summary.MarketType,
		PnLAsset:                 summary.PnLAsset,
		StrategyName:             strategyID + " 策略",
		StrategyType:             "grid",
		TotalProfit:              summary.TotalPnL,
		TodayProfit:              0, // 需要額外查詢
		UnrealizedProfit:         unrealizedPnL,
		UnrealizedProfitVerified: unrealizedPnLVerified,
		RealizedProfit:           summary.TotalPnL,
		WithdrawnProfit:          withdrawnAmounts.withdrawn,
		AvailableToWithdraw:      0,
		WinRate:                  math.Round(summary.WinRate*100) / 100, // 保持小數形式（0-1），前端會轉换為百分比
		TradeCount:               summary.TotalTrades,
		AvgProfitPerTrade:        0,
		LastTradeAt:              now.Format(time.RFC3339),
	}
	if strings.EqualFold(summary.PnLAsset, profitSummaryAsset) && strings.EqualFold(summary.MarketType, "futures") {
		profit.AvailableToWithdraw, profit.AvailableToWithdrawVerified, err = sumVerifiedWithdrawProfit(verifiedAvailable, summary.Exchange, strategyID)
		if err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "可提取金额汇总无效: " + err.Error()})
			return
		}
	}
	if err := roundStrategyProfitAmounts(&profit); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "策略详情盈利金额无法安全舍入: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"profit":  profit,
	})
}

// 獲取提取规则（從數據库读取，空库時返回空數组）
func getWithdrawRulesHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	accountID := GetCurrentAccountID()
	dbRules, err := st.ListProfitWithdrawRules(accountID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢提取规则失败: " + err.Error()})
		return
	}

	// 轉换為 API 响应格式
	rules := make([]ProfitWithdrawRule, 0, len(dbRules))
	for _, r := range dbRules {
		rules = append(rules, ProfitWithdrawRule{
			ID:                r.ID,
			ExchangeID:        r.ExchangeID,
			StrategyID:        r.StrategyID,
			IsEnabled:         r.Enabled,
			TriggerAmount:     r.TriggerAmount,
			WithdrawRatio:     r.WithdrawRatio,
			Frequency:         r.Frequency,
			Destination:       r.Destination,
			MinWithdrawAmount: r.MinWithdrawAmount,
			TargetAddress:     r.WalletAddress,
			CreatedAt:         r.CreatedAt.Format(time.RFC3339),
			UpdatedAt:         r.UpdatedAt.Format(time.RFC3339),
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"rules":   rules,
	})
}

// 更新提取规则（全量替换：先刪除账戶下所有规则，再插入新规则）
func updateWithdrawRulesHandler(c *gin.Context) {
	var req struct {
		Rules []ProfitWithdrawRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲服務未就绪（storageProv=nil）"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲接口未就绪（GetStorage()=nil），请检查 storage.enabled 配置和數據库初始化日志"})
		return
	}

	accountID := GetCurrentAccountID()

	// 構建新规则列表
	now := time.Now()
	newRules := make([]*storage.ProfitWithdrawRule, 0, len(req.Rules))
	for _, r := range req.Rules {
		rule := &storage.ProfitWithdrawRule{
			ID:                r.ID,
			AccountID:         accountID,
			AccountScope:      accountScopeForExchange(r.ExchangeID),
			ExchangeID:        r.ExchangeID,
			StrategyID:        r.StrategyID,
			Enabled:           r.IsEnabled,
			TriggerAmount:     r.TriggerAmount,
			WithdrawRatio:     r.WithdrawRatio,
			Frequency:         r.Frequency,
			Destination:       r.Destination,
			WalletAddress:     r.TargetAddress,
			MinWithdrawAmount: r.MinWithdrawAmount,
			CreatedAt:         now,
			UpdatedAt:         now,
		}
		// 如果 ID 為空或以 temp- 开头，生成新 ID
		if rule.ID == "" || strings.HasPrefix(rule.ID, "temp-") {
			rule.ID = fmt.Sprintf("rule_%s_%d", accountID, now.UnixNano())
			now = now.Add(time.Nanosecond) // 确保唯一
		}
		if rule.Enabled && rule.Destination == "" {
			rule.Destination = "account"
		}
		if err := profit.ValidateWithdrawRule(rule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "提取规则无效: " + err.Error()})
			return
		}
		newRules = append(newRules, rule)
	}

	// 全量替换规则
	if err := st.ReplaceProfitWithdrawRules(accountID, newRules); err != nil {
		if errors.Is(err, storage.ErrOverlappingProfitWithdrawRule) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已更新",
		"rules":   req.Rules,
	})
}

// 創建或更新單個提取规则
func upsertWithdrawRuleHandler(c *gin.Context) {
	var req ProfitWithdrawRule
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	accountID := GetCurrentAccountID()
	now := time.Now()

	rule := &storage.ProfitWithdrawRule{
		ID:                req.ID,
		AccountID:         accountID,
		AccountScope:      accountScopeForExchange(req.ExchangeID),
		ExchangeID:        req.ExchangeID,
		StrategyID:        req.StrategyID,
		Enabled:           req.IsEnabled,
		TriggerAmount:     req.TriggerAmount,
		WithdrawRatio:     req.WithdrawRatio,
		Frequency:         req.Frequency,
		Destination:       req.Destination,
		WalletAddress:     req.TargetAddress,
		MinWithdrawAmount: req.MinWithdrawAmount,
		CreatedAt:         now,
		UpdatedAt:         now,
	}

	// 如果没有 ID，生成一個新的
	if rule.ID == "" || strings.HasPrefix(rule.ID, "temp-") {
		rule.ID = fmt.Sprintf("rule_%s_%d", accountID, now.UnixNano())
	}
	if rule.Enabled && rule.Destination == "" {
		rule.Destination = "account"
	}
	if err := profit.ValidateWithdrawRule(rule); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "提取规则无效: " + err.Error()})
		return
	}

	if err := st.UpsertProfitWithdrawRule(accountID, rule); err != nil {
		if errors.Is(err, storage.ErrOverlappingProfitWithdrawRule) {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "保存规则失败: " + err.Error()})
		return
	}

	req.ID = rule.ID
	req.CreatedAt = now.Format(time.RFC3339)
	req.UpdatedAt = now.Format(time.RFC3339)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已保存",
		"rule":    req,
	})
}

// 刪除提取规则
func deleteWithdrawRuleHandler(c *gin.Context) {
	ruleID := c.Param("id")

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	accountID := GetCurrentAccountID()
	if err := st.DeleteProfitWithdrawRule(accountID, ruleID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "刪除规则失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "提取规则已刪除",
		"ruleId":  ruleID,
	})
}

// 手动提取
func manualWithdrawWindow(st storage.Storage, accountID, accountScope, exchangeID, symbol string, amount float64, now time.Time) (time.Time, time.Time, string, float64, error) {
	scopeRecordReader, hasScopeRecords := st.(interface {
		GetWithdrawRecordsForAccountScope(accountScope string, limit int) ([]*storage.ProfitWithdrawRecord, error)
	})
	legacyRecordReader, hasLegacyRecords := st.(interface {
		GetLegacyWithdrawRecordsForExchange(exchange string, limit int) ([]*storage.ProfitWithdrawRecord, error)
	})
	scopeRuleReader, hasScopeRules := st.(interface {
		ListProfitWithdrawRulesForAccountScope(accountScope string) ([]*storage.ProfitWithdrawRule, error)
	})
	if !hasScopeRecords || !hasLegacyRecords || !hasScopeRules {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("storage lacks account-scope withdrawal history and rule reads")
	}
	coverageReader, hasFundingCoverage := st.(interface {
		GetFundingIncomeCoverage(exchange, symbol, marketType, accountScope string) (time.Time, time.Time, error)
	})
	fillReader, hasFillCoverage := st.(interface {
		GetOrderFillCoverage(exchange, marketType, symbol, accountScope string) (*storage.OrderFillCoverage, error)
	})
	pnlReader, hasWithdrawalPnL := st.(interface {
		GetRealizedPnLForWithdrawal(exchange, symbol, accountScope string, startTime, endTime time.Time) (float64, error)
	})
	if !hasFundingCoverage || !hasFillCoverage || !hasWithdrawalPnL {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("storage lacks verified funding, fill, or realized-PnL coverage")
	}
	fundingFrom, fundingThrough, err := coverageReader.GetFundingIncomeCoverage(exchangeID, symbol, "futures", accountScope)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("read funding income coverage: %w", err)
	}
	fills, err := fillReader.GetOrderFillCoverage(exchangeID, "futures", symbol, accountScope)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("read execution coverage: %w", err)
	}
	if fills == nil || fundingFrom.IsZero() || fundingThrough.IsZero() || fills.CoveredFrom.IsZero() || fills.CoveredThrough.IsZero() {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("complete funding and execution history is unavailable")
	}
	windowStart := fundingFrom
	if fills.CoveredFrom.After(windowStart) {
		windowStart = fills.CoveredFrom
	}
	windowEnd := fundingThrough
	if fills.CoveredThrough.Before(windowEnd) {
		windowEnd = fills.CoveredThrough
	}
	if now.Before(windowEnd) {
		windowEnd = now
	}
	records, err := scopeRecordReader.GetWithdrawRecordsForAccountScope(accountScope, 1000)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("read scoped prior withdrawal records: %w", err)
	}
	if len(records) >= 1000 {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("withdrawal history reached its verification limit")
	}
	legacyRecords, err := st.GetWithdrawRecords(accountID, 1000)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("read legacy withdrawal checkpoints: %w", err)
	}
	if len(legacyRecords) >= 1000 {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("legacy withdrawal history reached its verification limit")
	}
	legacyUnscoped, err := legacyRecordReader.GetLegacyWithdrawRecordsForExchange(exchangeID, 1000)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("read unscoped legacy withdrawal records: %w", err)
	}
	if len(legacyUnscoped) >= 1000 {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("unscoped legacy withdrawal history reached its verification limit")
	}
	seenRecords := make(map[string]struct{}, len(records))
	for _, record := range records {
		if record != nil {
			seenRecords[record.ID] = struct{}{}
		}
	}
	for _, record := range append(legacyRecords, legacyUnscoped...) {
		if record == nil {
			continue
		}
		if _, exists := seenRecords[record.ID]; exists {
			continue
		}
		seenRecords[record.ID] = struct{}{}
		records = append(records, record)
	}
	legacyCheckpoint, err := profit.LegacyWithdrawalCheckpoint(records, exchangeID)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, err
	}
	var latest *storage.ProfitWithdrawRecord
	for _, record := range records {
		if record == nil || !strings.EqualFold(record.ExchangeID, exchangeID) {
			continue
		}
		if record.AccountScope == "" {
			continue
		}
		if record.AccountScope != accountScope || !strings.EqualFold(record.StrategyID, symbol) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(record.Status)) {
		case "completed":
			if latest == nil || record.CreatedAt.After(latest.CreatedAt) {
				latest = record
			}
		case "failed", "cancelled":
			continue
		default:
			return time.Time{}, time.Time{}, "", 0, fmt.Errorf("a prior withdrawal has an unresolved or unknown status; reconcile it before another transfer")
		}
	}
	if rules, err := scopeRuleReader.ListProfitWithdrawRulesForAccountScope(accountScope); err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("read automatic withdrawal rules: %w", err)
	} else {
		for _, rule := range rules {
			if rule != nil && rule.Enabled && rule.AccountScope == accountScope && strings.EqualFold(rule.ExchangeID, exchangeID) && strings.EqualFold(rule.StrategyID, symbol) {
				return time.Time{}, time.Time{}, "", 0, fmt.Errorf("an automatic withdrawal rule is enabled for this accounting stream")
			}
		}
	}
	if latest != nil {
		if latest.CreatedAt.IsZero() {
			return time.Time{}, time.Time{}, "", 0, fmt.Errorf("latest withdrawal has no trusted checkpoint time")
		}
		windowStart = latest.CreatedAt
	}
	if legacyCheckpoint.After(windowStart) {
		windowStart = legacyCheckpoint
	}
	if !windowStart.Before(windowEnd) {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("no fully covered realized-profit interval is available")
	}
	verifiedProfit, err := pnlReader.GetRealizedPnLForWithdrawal(exchangeID, symbol, accountScope, windowStart, windowEnd)
	if err != nil {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("verify realized profit: %w", err)
	}
	if math.IsNaN(verifiedProfit) || math.IsInf(verifiedProfit, 0) || amount < 0 || (amount > 0 && (verifiedProfit <= 0 || amount > verifiedProfit)) {
		return time.Time{}, time.Time{}, "", 0, fmt.Errorf("requested amount exceeds fully covered realized USDT profit")
	}
	checkpointID := ""
	if latest != nil {
		checkpointID = latest.ID
	}
	return windowStart, windowEnd, checkpointID, verifiedProfit, nil
}

func validateManualTransferReceipt(transferID string, transferErr error) error {
	if transferErr != nil {
		return transferErr
	}
	if strings.TrimSpace(transferID) == "" {
		return errors.New("交易所返回成功但未提供可核验的转账流水号")
	}
	return nil
}

func withdrawProfitHandler(c *gin.Context) {
	var req struct {
		ExchangeID    string  `json:"exchangeId"`
		StrategyID    string  `json:"strategyId"`
		Amount        float64 `json:"amount"`
		Destination   string  `json:"destination"`
		WalletAddress string  `json:"walletAddress"`
		Currency      string  `json:"currency"`
		Note          string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}
	if strings.TrimSpace(req.ExchangeID) == "" || strings.TrimSpace(req.StrategyID) == "" || req.Amount <= 0 || math.IsNaN(req.Amount) || math.IsInf(req.Amount, 0) {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "请指定单一交易所、交易对和有效提取金额",
		})
		return
	}
	currency := req.Currency
	if currency == "" {
		currency = "USDT"
	}
	if !strings.EqualFold(currency, "USDT") || (req.Destination != "" && req.Destination != "account") || strings.TrimSpace(req.WalletAddress) != "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "当前仅支持已验证的 USDT 合约到账户划转，不支持钱包提现"})
		return
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	if exchangeGetterFunc == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "交易所獲取器未配置"})
		return
	}
	ex := exchangeGetterFunc(req.ExchangeID)
	if ex == nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "未找到該交易所實例"})
		return
	}

	// 內部轉帳通常無手续费
	fee := 0.0
	netAmount := req.Amount
	recordID := "wd_" + utils.NewCompactOrderID()
	accountID := GetCurrentAccountID()
	if accountID == "" {
		accountID = "default"
	}
	accountScope := accountScopeForExchange(req.ExchangeID)
	if accountScope == "" {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "无法确认当前交易所账户作用域，拒绝发起资金划转"})
		return
	}
	windowStart, windowEnd, checkpointID, verifiedProfit, err := manualWithdrawWindow(st, accountID, accountScope, req.ExchangeID, req.StrategyID, req.Amount, time.Now())
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "无法验证可提现利润: " + err.Error()})
		return
	}
	ctx := c.Request.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	record := &storage.ProfitWithdrawRecord{
		ID:           recordID,
		RuleID:       "",
		AccountID:    accountID,
		AccountScope: accountScope,
		ExchangeID:   req.ExchangeID,
		StrategyID:   req.StrategyID,
		Amount:       req.Amount,
		Fee:          fee,
		NetAmount:    netAmount,
		Currency:     currency,
		Type:         "manual",
		Status:       "processing",
		Destination:  "account",
		CreatedAt:    time.Now(),
		Note:         req.Note,
	}
	reservations, ok := st.(interface {
		ReserveManualWithdrawRecord(record *storage.ProfitWithdrawRecord, windowStart time.Time, checkpointID string, verifiedProfit float64) error
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲不支持原子化手動提取預留，已拒絕轉賬"})
		return
	}
	if err := reservations.ReserveManualWithdrawRecord(record, windowStart, checkpointID, verifiedProfit); err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "提取預留失敗，請刷新收益後重試: " + err.Error()})
		return
	}

	if err := profit.ValidateTransferSafety(ctx, ex, req.StrategyID, accountScope, req.Amount, windowStart, windowEnd); err != nil {
		if updateErr := st.UpdateWithdrawRecordStatus(recordID, "failed", "", "转账前安全校验失败，未发起划转: "+err.Error()); updateErr != nil {
			logger.Error("手动利润提取预留未能释放 record=%s safety_err=%v: %v", recordID, err, updateErr)
		}
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "转账前安全校验未通过，未提交划转: " + err.Error()})
		return
	}
	transferID, err := ex.InternalTransfer(ctx, "UMFUTURE", "SPOT", currency, req.Amount)
	err = validateManualTransferReceipt(transferID, err)
	if err != nil {
		const pendingReason = "转账结果未核实；请先核对交易所资金流水，禁止重复提交"
		if updateErr := st.UpdateWithdrawRecordStatus(recordID, "pending", "", pendingReason+": "+err.Error()); updateErr != nil {
			logger.Error("手动利润提取结果未知且记录更新失败 record=%s: %v", recordID, updateErr)
		}
		c.JSON(http.StatusAccepted, gin.H{
			"success": true,
			"message": pendingReason,
			"record": WithdrawRecord{
				ID: recordID, ExchangeID: req.ExchangeID, StrategyID: req.StrategyID,
				Amount: req.Amount, Fee: fee, NetAmount: netAmount, Currency: currency,
				Type: "manual", Status: "pending", Destination: "account",
				CreatedAt: record.CreatedAt.Format(time.RFC3339), FailedReason: pendingReason, Note: req.Note,
			},
		})
		return
	}

	status := "completed"
	message := "提取已完成"
	if updateErr := st.UpdateWithdrawRecordStatus(recordID, "completed", transferID, ""); updateErr != nil {
		logger.Error("手动利润提取已转账但完成状态持久化失败 record=%s: %v", recordID, updateErr)
		status = "pending"
		message = "转账可能已完成，但记录尚未核实；请核对交易所资金流水，勿重复提交"
	}
	completedAt := time.Now()

	respRecord := WithdrawRecord{
		ID:            recordID,
		ExchangeID:    req.ExchangeID,
		StrategyID:    req.StrategyID,
		StrategyName:  getStrategyName(req.StrategyID),
		Amount:        req.Amount,
		Fee:           fee,
		NetAmount:     netAmount,
		Currency:      currency,
		Type:          "manual",
		Status:        status,
		Destination:   "account",
		TargetAddress: req.WalletAddress,
		CreatedAt:     record.CreatedAt.Format(time.RFC3339),
		CompletedAt:   completedAt.Format(time.RFC3339),
		Note:          req.Note,
	}
	responseCode := http.StatusOK
	if status == "pending" {
		responseCode = http.StatusAccepted
	}
	c.JSON(responseCode, gin.H{
		"success": true,
		"message": message,
		"record":  respRecord,
	})
}

// 獲取提取历史
func getWithdrawHistoryHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "提取记录存储不可用"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "提取记录存储不可用"})
		return
	}

	accountID := GetCurrentAccountID()
	limit := 100
	if l := c.Query("limit"); l != "" {
		if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	dbRecords, err := st.GetWithdrawRecords(accountID, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "查詢提取历史失败: " + err.Error()})
		return
	}

	records := make([]WithdrawRecord, 0, len(dbRecords))
	for _, r := range dbRecords {
		rec := WithdrawRecord{
			ID:            r.ID,
			ExchangeID:    r.ExchangeID,
			StrategyID:    r.StrategyID,
			StrategyName:  getStrategyName(r.StrategyID),
			Amount:        r.Amount,
			Fee:           r.Fee,
			NetAmount:     r.NetAmount,
			Currency:      r.Currency,
			Type:          r.Type,
			Status:        r.Status,
			Destination:   r.Destination,
			TargetAddress: "",
			TxHash:        r.TransferID,
			CreatedAt:     r.CreatedAt.Format(time.RFC3339),
			FailedReason:  r.FailedReason,
			Note:          r.Note,
		}
		if r.CompletedAt != nil {
			rec.CompletedAt = r.CompletedAt.Format(time.RFC3339)
		}
		records = append(records, rec)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"records": records,
		"total":   len(records),
	})
}

func reconcileWithdrawRecordHandler(c *gin.Context) {
	var req struct {
		Outcome   string `json:"outcome"`
		Reference string `json:"reference"`
		Evidence  string `json:"evidence"`
		Confirmed bool   `json:"confirmed"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || !req.Confirmed {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "必须确认已逐项核对交易所资金流水"})
		return
	}
	if (req.Outcome != "completed" && req.Outcome != "failed") || strings.TrimSpace(req.Reference) == "" || len(strings.TrimSpace(req.Evidence)) < 24 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "核账结果无效；必须提供流水参考和至少 24 字的核账依据"})
		return
	}
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "提取记录存储不可用，无法核账"})
		return
	}
	st := storageProv.GetStorage()
	reader, ok := st.(interface {
		GetWithdrawRecord(accountID, recordID string) (*storage.ProfitWithdrawRecord, error)
		ResolvePendingWithdrawRecord(accountID, recordID, outcome, reference, evidence string) error
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储后端不支持安全核账，自动划转保持锁定"})
		return
	}
	accountID := GetCurrentAccountID()
	if accountID == "" {
		accountID = "default"
	}
	record, err := reader.GetWithdrawRecord(accountID, c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"success": false, "message": "提取记录不存在或不属于当前账户"})
		return
	}
	if record.AccountScope == "" || record.AccountScope != accountScopeForExchange(record.ExchangeID) {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "提取记录缺少或不匹配当前账户作用域；拒绝自动释放，需人工按原始账户流水处理"})
		return
	}
	if err := reader.ResolvePendingWithdrawRecord(accountID, record.ID, req.Outcome, req.Reference, req.Evidence); err != nil {
		c.JSON(http.StatusConflict, gin.H{"success": false, "message": "核账未完成：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "message": "核账决定已留痕；对应自动规则 claim 已在同一事务中处理", "status": req.Outcome})
}

// 獲取盈利趋势
func getProfitTrendHandler(c *gin.Context) {
	period := c.DefaultQuery("period", "30d")
	exchangeID := c.Query("exchange_id")
	// strategyID := c.Query("strategy_id") // 暂時不支援按策略過滤

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存儲接口未就绪"})
		return
	}

	// 根據周期计算天數
	days := 30
	switch period {
	case "7d":
		days = 7
	case "30d":
		days = 30
	case "90d":
		days = 90
	case "1y":
		days = 365
	}

	now := utils.NowConfiguredTimezone()
	startDate := now.AddDate(0, 0, -days)
	profitScopes, err := resolveProfitAccountScopes(exchangeID)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "无法验证趋势数据账户作用域: " + err.Error()})
		return
	}
	dailyReader, ok := st.(interface {
		QueryDailyPnLByAccountScopeAndAsset(exchange, accountScope, asset string, startDate, endDate time.Time) ([]*storage.DailyStatisticsWithTradeCount, error)
	})
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "存储未提供按账户作用域核验的每日 PnL 查询"})
		return
	}
	dailyStatsByDate := make(map[string]float64)
	allStatsBeforeByDate := make(map[string]float64)
	for _, scope := range profitScopes {
		dailyStats, queryErr := dailyReader.QueryDailyPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, startDate, now)
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势 PnL 未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		for _, stat := range dailyStats {
			if stat == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势 PnL 包含空白账本行"})
				return
			}
			key := stat.Date.Format("2006-01-02")
			if err := addFiniteProfitToMap(dailyStatsByDate, key, stat.TotalPnL); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
				return
			}
		}
		allStatsBefore, queryErr := dailyReader.QueryDailyPnLByAccountScopeAndAsset(scope.exchange, scope.scope, profitSummaryAsset, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), startDate.AddDate(0, 0, -1))
		if queryErr != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势基线未能通过账户/币种完整性核验: " + queryErr.Error()})
			return
		}
		for _, stat := range allStatsBefore {
			if stat == nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势基线包含空白账本行"})
				return
			}
			key := stat.Date.Format("2006-01-02")
			if err := addFiniteProfitToMap(allStatsBeforeByDate, key, stat.TotalPnL); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": err.Error()})
				return
			}
		}
	}
	// 獲取起始之前的累计盈利作為 base
	baseProfit := 0.0
	for _, pnl := range allStatsBeforeByDate {
		var ok bool
		baseProfit, ok = addFiniteProfitValues(baseProfit, pnl)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势基线累计金额溢出"})
			return
		}
	}

	// 將結果按日期填充，缺失的日期补0
	trendMap := dailyStatsByDate

	trend := make([]ProfitTrendPoint, days+1)
	cumProfit := baseProfit
	for i := 0; i <= days; i++ {
		date := startDate.AddDate(0, 0, i)
		dateStr := date.Format("2006-01-02")
		dailyProfit := trendMap[dateStr]
		var ok bool
		cumProfit, ok = addFiniteProfitValues(cumProfit, dailyProfit)
		if !ok {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势累计金额溢出"})
			return
		}
		roundedDailyProfit, dailyOK := roundProfitToCents(dailyProfit)
		roundedCumProfit, cumulativeOK := roundProfitToCents(cumProfit)
		if !dailyOK || !cumulativeOK {
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "趋势金额无法安全舍入"})
			return
		}

		trend[i] = ProfitTrendPoint{
			Timestamp: dateStr,
			Profit:    roundedDailyProfit,
			CumProfit: roundedCumProfit,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"trend":   trend,
		"period":  period,
	})
}

func currentAccountWithdrawRecord(c *gin.Context) (*storage.ProfitWithdrawRecord, int, string) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil || storageProv.GetStorage() == nil {
		return nil, http.StatusServiceUnavailable, "提取记录存储不可用，无法核实状态"
	}
	reader, ok := storageProv.GetStorage().(interface {
		GetWithdrawRecord(accountID, recordID string) (*storage.ProfitWithdrawRecord, error)
	})
	if !ok {
		return nil, http.StatusServiceUnavailable, "存储后端不支持账户隔离的提取记录查询"
	}
	accountID := GetCurrentAccountID()
	if accountID == "" {
		accountID = "default"
	}
	record, err := reader.GetWithdrawRecord(accountID, c.Param("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, http.StatusNotFound, "提取记录不存在或不属于当前账户"
		}
		return nil, http.StatusInternalServerError, "读取提取记录失败"
	}
	return record, http.StatusOK, ""
}

// 取消提取
func cancelWithdrawHandler(c *gin.Context) {
	record, status, message := currentAccountWithdrawRecord(c)
	if status != http.StatusOK {
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	responseMessage := "该状态下的提取记录不可取消，状态不会被修改。"
	switch record.Status {
	case "pending", "processing":
		responseMessage = "资金划转不可直接取消；结果可能未知的记录必须先核对交易所流水，并通过核账流程处理。"
	case "completed":
		responseMessage = "资金划转已完成，不能取消已执行的资金操作。"
	}
	c.JSON(http.StatusConflict, gin.H{
		"success": false,
		"message": responseMessage,
	})
}

// 獲取提取详情
func getWithdrawDetailHandler(c *gin.Context) {
	dbRecord, status, message := currentAccountWithdrawRecord(c)
	if status != http.StatusOK {
		c.JSON(status, gin.H{"success": false, "message": message})
		return
	}
	record := WithdrawRecord{
		ID:           dbRecord.ID,
		ExchangeID:   dbRecord.ExchangeID,
		StrategyID:   dbRecord.StrategyID,
		StrategyName: getStrategyName(dbRecord.StrategyID),
		Amount:       dbRecord.Amount,
		Fee:          dbRecord.Fee,
		NetAmount:    dbRecord.NetAmount,
		Currency:     dbRecord.Currency,
		Type:         dbRecord.Type,
		Status:       dbRecord.Status,
		Destination:  dbRecord.Destination,
		TxHash:       dbRecord.TransferID,
		CreatedAt:    dbRecord.CreatedAt.Format(time.RFC3339),
		FailedReason: dbRecord.FailedReason,
		Note:         dbRecord.Note,
	}
	if dbRecord.CompletedAt != nil {
		record.CompletedAt = dbRecord.CompletedAt.Format(time.RFC3339)
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"record":  record,
	})
}

// 估算提取费用
func estimateWithdrawFeeHandler(c *gin.Context) {
	var req struct {
		ExchangeID    string  `json:"exchangeId"`
		StrategyID    string  `json:"strategyId"`
		Amount        float64 `json:"amount"`
		Destination   string  `json:"destination"`
		WalletAddress string  `json:"walletAddress"`
		Currency      string  `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"message": "無效的请求數據: " + err.Error(),
		})
		return
	}

	currency := req.Currency
	if currency == "" {
		currency = "USDT"
	}
	if strings.TrimSpace(req.ExchangeID) == "" || strings.TrimSpace(req.StrategyID) == "" || req.Amount <= 0 ||
		math.IsNaN(req.Amount) || math.IsInf(req.Amount, 0) || !strings.EqualFold(currency, "USDT") ||
		(req.Destination != "" && req.Destination != "account") || strings.TrimSpace(req.WalletAddress) != "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "内部到账估算仅支持指定交易所/交易对及有效 USDT 金额"})
		return
	}
	fee := 0.0 // UMFUTURE→SPOT 为内部账户划转，不是链上提现。
	netAmount := req.Amount
	estimatedArrival := time.Now().Format(time.RFC3339)
	roundedFee, feeOK := roundProfitToCents(fee)
	roundedNetAmount, netOK := roundProfitToCents(netAmount)
	if !feeOK || !netOK {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "金额无法安全舍入"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":          true,
		"fee":              roundedFee,
		"netAmount":        roundedNetAmount,
		"estimatedArrival": estimatedArrival,
	})
}
