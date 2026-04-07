package config

import "strings"

// BotFuturesLegs 用於衝突檢測的期貨合約腿（exchange, symbol）；現貨等非期貨 Bot 返回空
func BotFuturesLegs(bc *BotConfig) [][2]string {
	if bc == nil {
		return nil
	}
	mt := bc.GetMarketType()
	if mt == MarketTypeFundingPerpSpread && bc.FundingPerpSpread != nil {
		fp := bc.FundingPerpSpread
		return [][2]string{
			{strings.TrimSpace(fp.LegA.Exchange), strings.TrimSpace(fp.LegA.Symbol)},
			{strings.TrimSpace(fp.LegB.Exchange), strings.TrimSpace(fp.LegB.Symbol)},
		}
	}
	if mt == MarketTypeFundingCarry {
		return [][2]string{{bc.Exchange, bc.Symbol}}
	}
	if mt == "futures" || mt == "" {
		return [][2]string{{bc.Exchange, bc.Symbol}}
	}
	return nil
}

// BotsShareFuturesLeg 兩個 Bot 是否佔用同一期貨合約腿
func BotsShareFuturesLeg(a, b *BotConfig) bool {
	la, lb := BotFuturesLegs(a), BotFuturesLegs(b)
	for _, x := range la {
		for _, y := range lb {
			if strings.EqualFold(x[0], y[0]) && strings.EqualFold(x[1], y[1]) {
				return true
			}
		}
	}
	return false
}

// CarriesFundingCarryConflict 若 carry 為資金費期現套利，是否與 other 互斥（同幣種或另一 Bot 腿觸及該合約）
func CarriesFundingCarryConflict(carry, other *BotConfig) bool {
	if carry == nil || other == nil || carry.GetMarketType() != MarketTypeFundingCarry {
		return false
	}
	ex, sym := carry.Exchange, carry.Symbol
	if strings.EqualFold(other.Exchange, ex) && strings.EqualFold(other.Symbol, sym) {
		return true
	}
	return BotUsesFuturesLeg(other, ex, sym)
}

// BotsConflict Bot 級運行/配置衝突：期現套利規則 + 期貨腿重疊
func BotsConflict(a, b *BotConfig) bool {
	if a == nil || b == nil {
		return false
	}
	if CarriesFundingCarryConflict(a, b) || CarriesFundingCarryConflict(b, a) {
		return true
	}
	return BotsShareFuturesLeg(a, b)
}
