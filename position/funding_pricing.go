package position

import (
	"math"
	"time"

	"quantmesh/strategy/regime"
)

// ========== 資金費進定價（R5 第五節第 8 條）==========
//
// funding_rate.pricing_enabled：付費一側（LONG 遇正費率 / SHORT 遇負費率）
//   offset = |rate| × price × 距下次結算小時 / 8，封頂 fundingPricingCapIntervalRatio × 間隔
//   開倉價遠離現價 offset，平倉價外移 offset（持倉成本由更好的進出價補償）。
// 收費一側不移動（不把開倉單推近盤口，避免 PostOnly 被拒）。
// funding_rate.pre_settlement_pause_minutes > 0：結算前 N 分鐘暫停付費一側新開倉。

const (
	// fundingPricingCapIntervalRatio 資金費偏移上限（間隔的比例）
	fundingPricingCapIntervalRatio = 0.5
	// fundingSettlementPeriodHours 資金費結算周期（小時）；下次結算時間未知時按整周期計
	fundingSettlementPeriodHours = 8.0
	// maxOpenPriceShiftIntervalRatio 開倉價總外移（庫存偏斜 + 資金費）上限，保持在一個間隔以內，避免越過相鄰槽位
	maxOpenPriceShiftIntervalRatio = 0.9
)

// fundingSettlementSource 可選：提供下次結算時間（safety.FundingRateMonitor 滿足）
type fundingSettlementSource interface {
	GetNextFundingTime() time.Time
}

// fundingPayingRate 開倉方向需支付的費率（>0 表示付費）：LONG=rate，SHORT=−rate；無監控器返回 false
func (spm *SuperPositionManager) fundingPayingRate(dir regime.Direction) (float64, bool) {
	if spm.fundingMonitor == nil {
		return 0, false
	}
	rate := spm.fundingMonitor.GetCurrentRate()
	if math.IsNaN(rate) || math.IsInf(rate, 0) {
		return 0, false
	}
	if dir == regime.DirectionShort {
		rate = -rate
	}
	return rate, true
}

// timeToFundingSettlement 距下次結算的時間；未知或已過期返回 false
func (spm *SuperPositionManager) timeToFundingSettlement(now time.Time) (time.Duration, bool) {
	src, ok := spm.fundingMonitor.(fundingSettlementSource)
	if !ok {
		return 0, false
	}
	next := src.GetNextFundingTime()
	if next.IsZero() || !next.After(now) {
		return 0, false
	}
	return next.Sub(now), true
}

// fundingPriceOffset 資金費價格偏移 = payingRate × price × hours/8，封頂 cap×interval；非付費或參數無效返回 0
func fundingPriceOffset(payingRate, price, hoursToSettlement, interval float64) float64 {
	if payingRate <= 0 || price <= 0 || interval <= 0 {
		return 0
	}
	hours := math.Min(math.Max(hoursToSettlement, 0), fundingSettlementPeriodHours)
	offset := payingRate * price * hours / fundingSettlementPeriodHours
	return math.Min(offset, fundingPricingCapIntervalRatio*interval)
}

// fundingPlan 返回 (價格偏移, 是否結算前暫停開倉)。調用方持有 spm.mu。
func (spm *SuperPositionManager) fundingPlan(dir regime.Direction, price, interval float64, now time.Time) (float64, bool) {
	fr := spm.config.FundingRate
	if !fr.PricingEnabled && fr.PreSettlementPauseMinutes <= 0 {
		return 0, false
	}
	paying, ok := spm.fundingPayingRate(dir)
	if !ok || paying <= 0 {
		return 0, false
	}
	until, known := spm.timeToFundingSettlement(now)
	shift := 0.0
	if fr.PricingEnabled {
		hours := fundingSettlementPeriodHours
		if known {
			hours = until.Hours()
		}
		shift = fundingPriceOffset(paying, price, hours, interval)
	}
	pause := fr.PreSettlementPauseMinutes > 0 && known &&
		until <= time.Duration(fr.PreSettlementPauseMinutes)*time.Minute
	return shift, pause
}
