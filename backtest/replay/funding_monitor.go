package replay

import (
	"sort"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

// 與 safety.FundingRateMonitor 相同的偏向閾值默認值
const (
	defaultHighRateThreshold = 0.001
	defaultPauseBuyThreshold = 0.0015
	// 偏向係數（與 safety.FundingRateMonitor.computeOpenBias 一致）
	biasFavorable = 1.2
	biasNormal    = 1.0
	biasReduced   = 0.7
	biasHeavy     = 0.3
	biasPaused    = 0.0
)

// SimFundingMonitor 回放用資金費率監控器，實現 position.FundingMonitor 與下次結算時間接口
// （GetNextFundingTime，資金費進定價使用）。所有時間按注入的模擬時鐘計算。
//
// 無未來函數口徑：GetCurrentRate 返回「最後一個結算時間 ≤ 模擬時間」的已結算費率。
// 實盤 FundingRateMonitor 讀的是交易所的當期預測費率（lastFundingRate，結算前持續變化），
// 回放沒有該序列，因此用上一期已結算費率近似；序列之前使用 fallbackRate。
// 下次結算時間按 UTC 00/08/16 對齊（Binance USD-M 默認 8 小時周期）。
//
// 偏向係數（GetBuyBias/GetSellBias）與 safety.FundingRateMonitor 的分段規則一致，僅在 funding_rate.bias_enabled 時偏離 1.0。
type SimFundingMonitor struct {
	cfg      config.FundingRateConfig
	series   []FundingPoint
	fallback float64
	clock    position.Clock
}

var _ position.FundingMonitor = (*SimFundingMonitor)(nil)

// NewSimFundingMonitor 創建模擬資金費率監控器；series 會被複製並按時間排序，clock 為 nil 時使用牆鐘
func NewSimFundingMonitor(cfg config.FundingRateConfig, series []FundingPoint, fallbackRate float64, clock position.Clock) *SimFundingMonitor {
	s := append([]FundingPoint(nil), series...)
	sort.SliceStable(s, func(i, j int) bool { return s[i].Timestamp < s[j].Timestamp })
	if clock == nil {
		clock = position.RealClock()
	}
	return &SimFundingMonitor{cfg: cfg, series: s, fallback: fallbackRate, clock: clock}
}

// RateAt 時刻 t 可見的費率：最後一個結算時間 ≤ t 的點；之前沒有點時返回 fallbackRate
func (m *SimFundingMonitor) RateAt(t time.Time) float64 {
	ts := t.UnixMilli()
	i := sort.Search(len(m.series), func(k int) bool { return m.series[k].Timestamp > ts })
	if i == 0 {
		return m.fallback
	}
	return m.series[i-1].Rate
}

// GetCurrentRate 當前模擬時間可見的費率
func (m *SimFundingMonitor) GetCurrentRate() float64 { return m.RateAt(m.clock.Now()) }

// GetNextFundingTime 嚴格晚於當前模擬時間的下一個 8 小時結算點（UTC 00/08/16）
func (m *SimFundingMonitor) GetNextFundingTime() time.Time {
	ts := m.clock.Now().UnixMilli()
	next := ts - ts%FundingIntervalMs + FundingIntervalMs
	return time.UnixMilli(next).UTC()
}

// IsHighRate 費率高於 high_rate_threshold
func (m *SimFundingMonitor) IsHighRate() bool {
	return m.GetCurrentRate() > m.highThreshold()
}

// GetBuyBias 做多開倉偏向係數（bias_enabled=false 時恆為 1.0）
func (m *SimFundingMonitor) GetBuyBias() float64 {
	if !m.cfg.BiasEnabled {
		return biasNormal
	}
	return m.openBias(m.GetCurrentRate(), true)
}

// GetSellBias 做空開倉偏向係數，與 GetBuyBias 鏡像
func (m *SimFundingMonitor) GetSellBias() float64 {
	if !m.cfg.BiasEnabled {
		return biasNormal
	}
	return m.openBias(-m.GetCurrentRate(), false)
}

// ShouldPauseBuying 買入偏向為 0
func (m *SimFundingMonitor) ShouldPauseBuying() bool { return m.GetBuyBias() == biasPaused }

func (m *SimFundingMonitor) highThreshold() float64 {
	if m.cfg.HighRateThreshold > 0 {
		return m.cfg.HighRateThreshold
	}
	return defaultHighRateThreshold
}

// openBias 按對開倉方不利的費率分段（規則同 safety.FundingRateMonitor.computeOpenBias）
func (m *SimFundingMonitor) openBias(adverseRate float64, zeroFavorable bool) float64 {
	high := m.highThreshold()
	pause := m.cfg.PauseBuyThreshold
	if pause <= 0 {
		pause = defaultPauseBuyThreshold
	}
	mid := high / 2
	switch {
	case adverseRate < 0 || (zeroFavorable && adverseRate == 0):
		return biasFavorable
	case adverseRate <= mid:
		return biasNormal
	case adverseRate <= high:
		return biasReduced
	case adverseRate <= pause:
		return biasHeavy
	default:
		return biasPaused
	}
}
