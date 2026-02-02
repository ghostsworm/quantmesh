package exchange

import (
	"math"

	"quantmesh/logger"
)

// ClipKlineSpikes 裁剪 K 線插針：將單根 K 線的 High/Low 限制在「鄰近收盤價與本根開收」的合理區間內，
// 避免交易所壞 tick 或異常數據導致圖表出現不合理的長影線。適用於所有交易所的歷史 K 線數據。
//
// 源頭數據可能異常的原因（交易所 API 仍可能返回異常 OHLC）：
//   - 壞 tick：某一筆成交錯誤（如胖手指、系統錯誤）被計入該週期的 High/Low
//   - 流動性瞬間枯竭：極端行情下單筆成交價偏離，被記入 K 線
//   - 數據源聚合/同步問題：多數據源合併或延遲導致偶發錯誤
//   - 合約/現貨或不同交易對的數據串線（極少見）
//
// bandPct: 允許的影線幅度，如 0.03 表示相對參考價上下 3%。正常波動保留，異常長影線會被裁剪並打日志。
func ClipKlineSpikes(candles []*Candle, bandPct float64) []*Candle {
	if len(candles) == 0 || bandPct <= 0 {
		return candles
	}
	for i := range candles {
		curr := candles[i]
		refHigh := math.Max(curr.Open, curr.Close)
		refLow := math.Min(curr.Open, curr.Close)
		if i > 0 && candles[i-1].Close > 0 {
			refHigh = math.Max(refHigh, candles[i-1].Close)
			refLow = math.Min(refLow, candles[i-1].Close)
		}
		if i+1 < len(candles) && candles[i+1].Close > 0 {
			refHigh = math.Max(refHigh, candles[i+1].Close)
			refLow = math.Min(refLow, candles[i+1].Close)
		}
		allowedHigh := refHigh * (1 + bandPct)
		allowedLow := refLow * (1 - bandPct)
		if allowedLow <= 0 {
			allowedLow = refLow * 0.99
		}
		clipped := false
		if curr.High > allowedHigh {
			logger.Warn("⚠️ [K線插針裁剪] %s, 時间: %d, High %.2f -> %.2f (上限 %.2f)",
				curr.Symbol, curr.Timestamp, curr.High, allowedHigh, allowedHigh)
			curr.High = allowedHigh
			clipped = true
		}
		if curr.Low < allowedLow {
			logger.Warn("⚠️ [K線插針裁剪] %s, 時间: %d, Low %.2f -> %.2f (下限 %.2f)",
				curr.Symbol, curr.Timestamp, curr.Low, allowedLow, allowedLow)
			curr.Low = allowedLow
			clipped = true
		}
		if clipped {
			if curr.High < curr.Low {
				curr.High = math.Max(curr.Open, curr.Close)
				curr.Low = math.Min(curr.Open, curr.Close)
			}
		}
	}
	return candles
}
