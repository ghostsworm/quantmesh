package config

import (
	"fmt"
	"strings"
)

// FundingPerpLeg 雙永续跨所套利：單腿（合約）
type FundingPerpLeg struct {
	Exchange string `yaml:"exchange" json:"exchange"`
	Symbol   string `yaml:"symbol" json:"symbol"`
}

// FundingPerpSpreadConfig 雙腿與閾值（market_type=funding_perp_spread 時必填）
type FundingPerpSpreadConfig struct {
	LegA FundingPerpLeg `yaml:"leg_a" json:"leg_a"`
	LegB FundingPerpLeg `yaml:"leg_b" json:"leg_b"`
	// MinFundingSpread 兩腿資金費率絕對差 |f_high-f_low| 達此值才開倉（如 0.0001 = 0.01%）
	MinFundingSpread float64 `yaml:"min_funding_spread" json:"min_funding_spread"`
	// ExitFundingSpread 價差回落至該值以下時平倉（可小於 min）
	ExitFundingSpread float64 `yaml:"exit_funding_spread" json:"exit_funding_spread"`
	// MaxBasisPct 兩腿標記/最新價價差百分比上限，超過則不開倉
	MaxBasisPct float64 `yaml:"max_basis_pct" json:"max_basis_pct"`
}

// ValidateFundingPerpSpread 校驗雙腿配置
func ValidateFundingPerpSpread(fp *FundingPerpSpreadConfig) error {
	if fp == nil {
		return fmt.Errorf("funding_perp_spread 配置為空")
	}
	a, b := strings.TrimSpace(fp.LegA.Exchange), strings.TrimSpace(fp.LegA.Symbol)
	c, d := strings.TrimSpace(fp.LegB.Exchange), strings.TrimSpace(fp.LegB.Symbol)
	if a == "" || b == "" || c == "" || d == "" {
		return fmt.Errorf("leg_a / leg_b 的 exchange 與 symbol 均必填")
	}
	if strings.EqualFold(a, c) && strings.EqualFold(b, d) {
		return fmt.Errorf("兩腿不可為同一交易所同一合約")
	}
	return nil
}

// GenerateBotIDFundingPerpSpread 雙永续套利 Bot ID（與單所 Bot 區分）
func GenerateBotIDFundingPerpSpread(fp *FundingPerpSpreadConfig) string {
	if fp == nil {
		return ""
	}
	k1 := strings.ToLower(strings.TrimSpace(fp.LegA.Exchange) + ":" + strings.TrimSpace(fp.LegA.Symbol))
	k2 := strings.ToLower(strings.TrimSpace(fp.LegB.Exchange) + ":" + strings.TrimSpace(fp.LegB.Symbol))
	if k1 > k2 {
		k1, k2 = k2, k1
	}
	return fmt.Sprintf("%s:%s|%s", MarketTypeFundingPerpSpread, k1, k2)
}

// FundingPerpSpreadLegTouches 是否佔用該交易所+合約 symbol 的期貨倉位
func FundingPerpSpreadLegTouches(fp *FundingPerpSpreadConfig, exchange, symbol string) bool {
	if fp == nil {
		return false
	}
	match := func(leg FundingPerpLeg) bool {
		return strings.EqualFold(strings.TrimSpace(leg.Exchange), strings.TrimSpace(exchange)) &&
			strings.EqualFold(strings.TrimSpace(leg.Symbol), strings.TrimSpace(symbol))
	}
	return match(fp.LegA) || match(fp.LegB)
}

// BotUsesFuturesLeg 該 Bot 是否佔用該交易所+合約的期貨倉位（衝突檢測；現貨 Bot 返回 false）
func BotUsesFuturesLeg(bc *BotConfig, exchange, symbol string) bool {
	if bc == nil {
		return false
	}
	mt := bc.GetMarketType()
	if mt == MarketTypeFundingPerpSpread && bc.FundingPerpSpread != nil {
		return FundingPerpSpreadLegTouches(bc.FundingPerpSpread, exchange, symbol)
	}
	if mt == MarketTypeFundingCarry {
		return strings.EqualFold(bc.Exchange, exchange) && strings.EqualFold(bc.Symbol, symbol)
	}
	if mt == "futures" || mt == "" {
		return strings.EqualFold(bc.Exchange, exchange) && strings.EqualFold(bc.Symbol, symbol)
	}
	return false
}
