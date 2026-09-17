package safety

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

// 本文件是主動安全風控（RiskMonitor）的純計算部分：K 線緩存維護、均線/量比/波動率計算、
// 觸發與恢復判定。與 I/O、日誌、存儲解耦，便於用合成 K 線做表驅動測試。

// millisTimestampThreshold 大於該值的時間戳按毫秒解析（Binance/Bitget），否則按秒（Gate.io）
const millisTimestampThreshold = 10000000000

// candleOpenTime 解析 K 線開盤時間；時間戳為 0 時返回零值
func candleOpenTime(ts int64) time.Time {
	if ts <= 0 {
		return time.Time{}
	}
	if ts > millisTimestampThreshold {
		return time.UnixMilli(ts)
	}
	return time.Unix(ts, 0)
}

// parseCandleInterval 解析 "1m"/"15m"/"1h"/"4h"/"1d"/"1w" 形式的 K 線周期
func parseCandleInterval(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return 0, false
	}
	n, err := strconv.Atoi(s[:len(s)-1])
	if err != nil || n <= 0 {
		return 0, false
	}
	switch s[len(s)-1] {
	case 'm':
		return time.Duration(n) * time.Minute, true
	case 'h', 'H':
		return time.Duration(n) * time.Hour, true
	case 'd', 'D':
		return time.Duration(n) * 24 * time.Hour, true
	case 'w', 'W':
		return time.Duration(n) * 7 * 24 * time.Hour, true
	}
	return 0, false
}

// markFormingHistorical 交易所 REST 歷史 K 線的最後一根通常是尚未收盤的 K 線，
// 但部分適配器一律標記 IsClosed=true。開盤時間 + 周期 > now 的改標記為未完結，避免把半根 K 線算進均線。
func markFormingHistorical(candles []*exchange.Candle, interval string, now time.Time) {
	d, ok := parseCandleInterval(interval)
	if !ok {
		return
	}
	for _, c := range candles {
		if c == nil || c.Timestamp <= 0 {
			continue
		}
		if candleOpenTime(c.Timestamp).Add(d).After(now) {
			c.IsClosed = false
		}
	}
}

// upsertCandle 把一根 K 線合併進緩存並裁剪，返回新切片：
//   - 與已有 K 線同開盤時間：替換（同一根 K 線的更新 / 未完結→完結），不再重複追加；
//   - 比最後一根更舊且找不到同時間戳：亂序過期數據，忽略；
//   - 更新的時間戳：追加，並丟棄中間殘留的未完結 K 線（其完結版本丟失時不讓半根數據參與計算）；
//   - 時間戳為 0（無時間信息的數據源）：沿用舊邏輯（完結追加；未完結替換末尾未完結或追加）。
//
// 最終保留最近 window+1 根完結 K 線以及末尾至多 1 根未完結 K 線。
func upsertCandle(candles []*exchange.Candle, c *exchange.Candle, window int) []*exchange.Candle {
	n := len(candles)
	switch {
	case c.Timestamp <= 0 || n == 0 || candles[n-1].Timestamp <= 0:
		if !c.IsClosed && n > 0 && !candles[n-1].IsClosed {
			candles[n-1] = c
		} else {
			candles = append(candles, c)
		}
	case c.Timestamp == candles[n-1].Timestamp:
		candles[n-1] = c
	case c.Timestamp < candles[n-1].Timestamp:
		replaced := false
		for i := n - 2; i >= 0; i-- {
			if candles[i].Timestamp == c.Timestamp {
				candles[i] = c
				replaced = true
				break
			}
			if candles[i].Timestamp < c.Timestamp {
				break
			}
		}
		if !replaced {
			return candles
		}
	default:
		candles = append(candles, c)
	}
	return pruneCandles(candles, window)
}

// pruneCandles 去掉非末尾的未完結 K 線，只保留最近 window+1 根完結 K 線（+末尾未完結）
func pruneCandles(candles []*exchange.Candle, window int) []*exchange.Candle {
	keepClosed := window + 1
	out := make([]*exchange.Candle, 0, keepClosed+1)
	last := len(candles) - 1
	closed := 0
	start := 0
	for i := last; i >= 0; i-- {
		if candles[i].IsClosed {
			closed++
			if closed == keepClosed {
				start = i
				break
			}
		}
	}
	for i := start; i <= last; i++ {
		if candles[i].IsClosed || i == last {
			out = append(out, candles[i])
		}
	}
	return out
}

// klineStats 一次均線/量比/波動率計算結果
type klineStats struct {
	current        *exchange.Candle
	avgPrice       float64
	avgVol         float64
	priceDevPct    float64 // (現價-均價)/均價×100
	volRatio       float64
	returnStdevPct float64 // 窗口內相鄰完結 K 線收盤收益率的標準差（百分比）
}

// computeKlineStats 計算統計量。useLatest=true 用最後一根（可為未完結）作為當前 K 線；
// 否則用最新一根完結 K 線。均值只使用當前 K 線之前的 window 根完結 K 線。
func computeKlineStats(candles []*exchange.Candle, window int, useLatest bool) (klineStats, string) {
	var st klineStats
	if window <= 0 {
		return st, "窗口無效"
	}
	curIdx := -1
	if useLatest {
		curIdx = len(candles) - 1
	} else {
		for i := len(candles) - 1; i >= 0; i-- {
			if candles[i].IsClosed {
				curIdx = i
				break
			}
		}
	}
	if curIdx < 0 {
		return st, "無完結K線"
	}
	st.current = candles[curIdx]

	closes := make([]float64, 0, window)
	var totalVol float64
	for i := curIdx - 1; i >= 0 && len(closes) < window; i-- {
		if candles[i].IsClosed {
			closes = append(closes, candles[i].Close)
			totalVol += candles[i].Volume
		}
	}
	if len(closes) < window {
		return st, fmt.Sprintf("完結K線不足(%d<%d)", len(closes), window)
	}
	var totalPrice float64
	for _, p := range closes {
		totalPrice += p
	}
	st.avgPrice = totalPrice / float64(len(closes))
	st.avgVol = totalVol / float64(len(closes))
	if st.avgPrice > 0 {
		st.priceDevPct = (st.current.Close - st.avgPrice) / st.avgPrice * 100
	}
	if st.avgVol > 0 {
		st.volRatio = st.current.Volume / st.avgVol
	}
	// closes 為時間倒序，收益率方向不影響標準差
	if len(closes) >= 3 {
		rets := make([]float64, 0, len(closes)-1)
		for i := 0; i+1 < len(closes); i++ {
			if closes[i+1] > 0 {
				rets = append(rets, (closes[i]-closes[i+1])/closes[i+1]*100)
			}
		}
		st.returnStdevPct = stdev(rets)
	}
	return st, ""
}

func stdev(xs []float64) float64 {
	if len(xs) < 2 {
		return 0
	}
	var mean float64
	for _, x := range xs {
		mean += x
	}
	mean /= float64(len(xs))
	var ss float64
	for _, x := range xs {
		ss += (x - mean) * (x - mean)
	}
	return math.Sqrt(ss / float64(len(xs)-1))
}

// detectorParams 觸發/恢復判定所需參數
type detectorParams struct {
	window           int
	volumeMultiplier float64
	interval         string
	resolved         config.ResolvedRiskAnomalyDetector
}

func detectorParamsFromConfig(cfg *config.Config) detectorParams {
	rc := cfg.RiskControl
	mult := rc.VolumeMultiplier
	if mult <= 0 {
		mult = 3.0
	}
	return detectorParams{
		window:           rc.AverageWindow,
		volumeMultiplier: mult,
		interval:         rc.Interval,
		resolved:         rc.RiskAnomalyDetectorConfig.Resolve(len(rc.MonitorSymbols), rc.RecoveryThreshold),
	}
}

// requiredDropPct 觸發所需跌幅 = max(最小跌幅, k × 收益率標準差)
func (p detectorParams) requiredDropPct(st klineStats) float64 {
	req := p.resolved.MinPriceDropPct
	if v := p.resolved.VolatilityMultiplier * st.returnStdevPct; v > req {
		req = v
	}
	return req
}

// isStale 最新 K 線開盤時間距今超過 StaleBars 個周期
func (p detectorParams) isStale(c *exchange.Candle, now time.Time) bool {
	if p.resolved.StaleBars <= 0 || c == nil || c.Timestamp <= 0 {
		return false
	}
	d, ok := parseCandleInterval(p.interval)
	if !ok {
		return false
	}
	return now.Sub(candleOpenTime(c.Timestamp)) > time.Duration(p.resolved.StaleBars)*d
}

// evaluatePanic 單交易對觸發判定：價格低於均線且跌幅 ≥ requiredDropPct，且量比 > volume_multiplier，且數據未過期
func (p detectorParams) evaluatePanic(candles []*exchange.Candle, now time.Time) (bool, string) {
	st, why := computeKlineStats(candles, p.window, true)
	if why != "" {
		return false, why
	}
	if p.isStale(st.current, now) {
		return false, "K線過期"
	}
	req := p.requiredDropPct(st)
	drop := -st.priceDevPct
	if st.priceDevPct < 0 && drop >= req && st.volRatio > p.volumeMultiplier {
		return true, fmt.Sprintf("價格%.2f%%低於均線(門檻%.2f%%)/量×%.1f", st.priceDevPct, req, st.volRatio)
	}
	return false, ""
}

// priceRecovered 價格已回到均線附近：偏離 ≥ −RecoveryMaxDropPct（為 0 時要求回到均線上方）
func (p detectorParams) priceRecovered(st klineStats) bool {
	if p.resolved.RecoveryMaxDropPct <= 0 {
		return st.priceDevPct > 0
	}
	return st.priceDevPct >= -p.resolved.RecoveryMaxDropPct
}

// evaluateRecovery 單交易對恢復判定（只用完結 K 線）：
// 價格已恢復，且（量已回落到倍數以下 或 價格已在均線上方——上方放量不屬於恐慌）
func (p detectorParams) evaluateRecovery(candles []*exchange.Candle) (bool, string) {
	st, why := computeKlineStats(candles, p.window, false)
	if why != "" {
		return false, why
	}
	priceOK := p.priceRecovered(st)
	volOK := st.volRatio < p.volumeMultiplier
	if priceOK && (volOK || st.priceDevPct >= 0) {
		return true, "價格回归均線/量正常"
	}
	if !priceOK {
		return false, fmt.Sprintf("價格%.2f<均價%.2f(偏離%.2f%%)", st.current.Close, st.avgPrice, st.priceDevPct)
	}
	return false, fmt.Sprintf("量%.0f>均量×%.1f", st.current.Volume, p.volumeMultiplier)
}
