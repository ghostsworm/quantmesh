package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/strategy/regime"
)

// 離線 regime 診斷：不經過網格，只在模擬時間上逐小時驅動 regime.Detector（ClosedKlineSource，無未來函數），
// 統計各狀態占比、切換次數、各狀態下未來 24h 收益、ATR 自適應間隔會選擇的倍數、自動上沿越界占比。
// 用於在回放引擎無法注入 RegimeProvider 時，為 regime/adaptive/freeze 提供間接證據。

const (
	diagKlineInterval   = "1h"
	diagForwardHours    = 24
	diagAutoBoundK      = 3.0
	hourMs              = int64(60 * 60 * 1000)
	percentScale        = 100.0
	fwdReturnMinSamples = 1
)

// manualClock 可設置的時鐘
type manualClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *manualClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *manualClock) Set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

// RegimeStat 單個狀態的統計
type RegimeStat struct {
	Hours              int     `json:"hours"`
	SharePct           float64 `json:"share_pct"`
	MeanFwd24hRetPct   float64 `json:"mean_fwd_24h_return_pct"`
	MeanFwd24hRangePct float64 `json:"mean_fwd_24h_range_pct"` // (max high − min low)/close
}

// RegimeDiag 一個交易對 × 時間段的診斷
type RegimeDiag struct {
	Symbol   string `json:"symbol"`
	Segment  string `json:"segment"`
	Hours    int    `json:"hours"`
	Switches int    `json:"switches"`
	// RefreshErrors 檢測器拉取失敗次數（段首無足夠歷史時預期出現）
	RefreshErrors    int                   `json:"refresh_errors"`
	LastRefreshError string                `json:"last_refresh_error,omitempty"`
	ByRegime         map[string]RegimeStat `json:"by_regime"`
	AutoBoundHits    float64               `json:"auto_upper_bound_breach_pct"` // 收盤價 > EMA+3ATR 的小時占比（快照可用時）
	// AdaptiveMultiple base 名 → 自適應間隔（k=0.5）相對 base 的時間加權平均倍數（僅快照可用時）
	AdaptiveMultiple map[string]float64 `json:"adaptive_interval_mean_multiple"`
	// PolicyMultiple base 名 → 自適應 × regime IntervalScale（LONG）量化後的平均倍數
	PolicyMultiple map[string]float64 `json:"adaptive_with_regime_mean_multiple"`
}

// RunRegimeDiag 在 [seg.StartMs, seg.EndMs) 內逐小時評估。hourly 必須包含段前的歷史（用於預熱），
// 檢測器只能看到評估時刻前已收盤的 K 線。
func RunRegimeDiag(symbol string, hourly []*exchange.Candle, seg Segment, bases []BaseSpec, startPriceOf func(Segment) float64, priceDecimals int) (RegimeDiag, error) {
	diag := RegimeDiag{Symbol: symbol, Segment: seg.Name, ByRegime: map[string]RegimeStat{},
		AdaptiveMultiple: map[string]float64{}, PolicyMultiple: map[string]float64{}}
	clk := &manualClock{now: time.UnixMilli(seg.StartMs)}
	src, err := NewClosedKlineSource(symbol, diagKlineInterval, hourly, clk.Now)
	if err != nil {
		return diag, err
	}
	det, err := regime.NewDetector(symbol, regime.RegimeConfig{Enabled: true, KlineInterval: diagKlineInterval}.WithDefaults(), src, regime.WithClock(clk.Now))
	if err != nil {
		return diag, fmt.Errorf("regime diag %s: %w", symbol, err)
	}
	byOpen := make(map[int64]int, len(hourly))
	for i, b := range hourly {
		byOpen[b.Timestamp] = i
	}
	adaptive := regime.AdaptiveIntervalConfig{Enabled: true}.WithDefaults()
	type baseState struct {
		base, adaptCur      float64
		adaptSum, policySum float64
		n                   int
	}
	states := make([]baseState, len(bases))
	startPrice := startPriceOf(seg)
	for i, b := range bases {
		states[i].base = roundTo(startPrice*b.IntervalRatio, priceDecimals)
	}

	type acc struct {
		hours      int
		fwdSum     float64
		rangeSum   float64
		fwdSamples int
	}
	accs := map[regime.Regime]*acc{}
	prev := regime.Unknown
	first := true
	usable, breaches := 0, 0
	ctx := context.Background()
	for t := seg.StartMs - seg.StartMs%hourMs; t < seg.EndMs; t += hourMs {
		if t < seg.StartMs {
			continue
		}
		clk.Set(time.UnixMilli(t))
		if err := det.Refresh(ctx); err != nil {
			// 數據不足（段首無歷史）時 bootstrap 失敗屬預期：該小時計為 Unknown，並計數留痕
			diag.RefreshErrors++
			diag.LastRefreshError = err.Error()
		}
		snap := det.Snapshot()
		eff := snap.Effective()
		if !first && eff != prev {
			diag.Switches++
		}
		first = false
		prev = eff
		a := accs[eff]
		if a == nil {
			a = &acc{}
			accs[eff] = a
		}
		a.hours++
		diag.Hours++
		// 未來 24h 收益與振幅：只用於事後評估，不回饋給檢測器
		if idx, ok := byOpen[t]; ok && idx+diagForwardHours-1 < len(hourly) && hourly[idx+diagForwardHours-1].Timestamp < seg.EndMs {
			open := hourly[idx].Open
			hi, lo := 0.0, math.MaxFloat64
			for k := idx; k < idx+diagForwardHours; k++ {
				hi = math.Max(hi, hourly[k].High)
				lo = math.Min(lo, hourly[k].Low)
			}
			closeFwd := hourly[idx+diagForwardHours-1].Close
			a.fwdSum += (closeFwd/open - 1) * percentScale
			a.rangeSum += (hi - lo) / open * percentScale
			a.fwdSamples++
		}
		if snap.Ready && !snap.Stale && snap.ATR > 0 {
			usable++
			if snap.Close > snap.EMA+diagAutoBoundK*snap.ATR {
				breaches++
			}
			for i := range states {
				st := &states[i]
				if st.base <= 0 {
					continue
				}
				st.adaptCur = adaptive.Next(st.adaptCur, snap.ATR, st.base)
				scale := regime.PolicyFor(eff, regime.DirectionLong).IntervalScale
				if eff == regime.Unknown {
					scale = 1
				}
				policy := regime.QuantizeInterval(st.adaptCur*scale, st.base)
				st.adaptSum += st.adaptCur / st.base
				st.policySum += policy / st.base
				st.n++
			}
		}
	}
	for r, a := range accs {
		st := RegimeStat{Hours: a.hours}
		if diag.Hours > 0 {
			st.SharePct = float64(a.hours) / float64(diag.Hours) * percentScale
		}
		if a.fwdSamples >= fwdReturnMinSamples {
			st.MeanFwd24hRetPct = a.fwdSum / float64(a.fwdSamples)
			st.MeanFwd24hRangePct = a.rangeSum / float64(a.fwdSamples)
		}
		diag.ByRegime[r.String()] = st
	}
	if usable > 0 {
		diag.AutoBoundHits = float64(breaches) / float64(usable) * percentScale
	}
	for i, b := range bases {
		if states[i].n > 0 {
			diag.AdaptiveMultiple[b.Name] = states[i].adaptSum / float64(states[i].n)
			diag.PolicyMultiple[b.Name] = states[i].policySum / float64(states[i].n)
		}
	}
	return diag, nil
}

// sortedRegimeNames 報告中狀態的固定順序
func sortedRegimeNames(m map[string]RegimeStat) []string {
	order := map[string]int{regime.Range.String(): 0, regime.TrendUp.String(): 1, regime.TrendDown.String(): 2, regime.Unknown.String(): 3}
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Slice(names, func(i, j int) bool { return order[names[i]] < order[names[j]] })
	return names
}
