package config

import "strings"

// MarketTypeFundingCarry 資金費率期現套利：現貨多 + 永續空，與 spot/futures 並列為獨立 Bot 維度
const MarketTypeFundingCarry = "funding_carry"

// ValidMarketType 是否為合法 market_type（空視為由調用方默認為 futures）
func ValidMarketType(mt string) bool {
	switch mt {
	case "", "spot", "futures", MarketTypeFundingCarry:
		return true
	default:
		return false
	}
}

// IsFundingCarryMarketType 是否為資金費套利 Bot
func IsFundingCarryMarketType(mt string) bool {
	return mt == MarketTypeFundingCarry
}

// BotsSameSymbolMarketConflict 配置層衝突：同交易所同幣下 funding_carry 與其它 market_type 互斥；
// 非 funding 時僅當 market_type 相同時衝突（保留原可同時存在 spot+futures 兩個 Bot 的語義）
func BotsSameSymbolMarketConflict(exchangeA, symbolA, mtA, exchangeB, symbolB, mtB string) bool {
	if !strings.EqualFold(exchangeA, exchangeB) || !strings.EqualFold(symbolA, symbolB) {
		return false
	}
	if mtA == MarketTypeFundingCarry || mtB == MarketTypeFundingCarry {
		return true
	}
	return strings.EqualFold(mtA, mtB)
}
