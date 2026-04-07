package exchange

import (
	"context"
	"time"
)

// EstimateNextFundingUTC8h 根據當前 UTC 時間估算下一次 8 小時整點資金費結算時間（00/08/16 UTC）。
// 當交易所未返回 next funding time 時作為後備，與策略層邏輯一致。
func EstimateNextFundingUTC8h(now time.Time) time.Time {
	now = now.UTC()
	hour := now.Hour()
	base := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	switch {
	case hour < 8:
		return base.Add(8 * time.Hour)
	case hour < 16:
		return base.Add(16 * time.Hour)
	default:
		return base.Add(24 * time.Hour)
	}
}

// FundingInfoFromRateEstimate 使用當前費率與標記/指數價，並以 UTC 8h 估算下次結算時間。
func FundingInfoFromRateEstimate(symbol string, rate, markPrice, indexPrice float64) *FundingInfo {
	return &FundingInfo{
		Symbol:          symbol,
		Rate:            rate,
		NextFundingTime: EstimateNextFundingUTC8h(time.Now().UTC()),
		MarkPrice:       markPrice,
		IndexPrice:      indexPrice,
	}
}

// FundingInfoFallbackFromRate 合約適配器僅實現 GetFundingRate 時，用 UTC 8h 估算下次結算時間。
type fundingRateLatestPricer interface {
	GetFundingRate(context.Context, string) (float64, error)
	GetLatestPrice(context.Context, string) (float64, error)
}

func FundingInfoFallbackFromRate(ctx context.Context, symbol string, ex fundingRateLatestPricer) (*FundingInfo, error) {
	rate, err := ex.GetFundingRate(ctx, symbol)
	if err != nil {
		return nil, err
	}
	mark, _ := ex.GetLatestPrice(ctx, symbol)
	return FundingInfoFromRateEstimate(symbol, rate, mark, mark), nil
}

// fundingRateNoSymbolPricer 適配器內部已綁定合約，GetFundingRate 無 symbol 參數
type fundingRateNoSymbolPricer interface {
	GetFundingRate(context.Context) (float64, error)
	GetLatestPrice(context.Context, string) (float64, error)
}

// FundingInfoFallbackFromRateFixedSymbol 用於上述適配器（如 MEXC、Deribit）
func FundingInfoFallbackFromRateFixedSymbol(ctx context.Context, symbol string, ex fundingRateNoSymbolPricer) (*FundingInfo, error) {
	rate, err := ex.GetFundingRate(ctx)
	if err != nil {
		return nil, err
	}
	mark, _ := ex.GetLatestPrice(ctx, symbol)
	return FundingInfoFromRateEstimate(symbol, rate, mark, mark), nil
}
