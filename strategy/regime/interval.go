package regime

import "math"

// quantizeEps 浮点取整容差（相对 base 的倍数），避免 3.0000000001 被 ceil 成 4
const quantizeEps = 1e-9

func validPositive(v float64) bool {
	return v > 0 && !math.IsInf(v, 0) && !math.IsNaN(v)
}

// AdaptiveInterval 计算 ATR 自适应网格间隔：
//
//	raw = multiplier × atr，夹到 [minInterval, maxInterval]，再量化为 baseInterval 的整数倍（至少 1 倍）。
//
// 量化保证所有槽位仍落在 anchor + n×baseInterval 上，改间隔不需要移动锚点。
//   - baseInterval 非法（≤0/NaN/Inf）时返回 0，调用方应视为"不调整"。
//   - minInterval ≤ 0 视为 baseInterval；maxInterval ≤ 0 视为无上限；max < min 时以 min 为准。
//   - atr 或 multiplier 非法时以 baseInterval 作为 raw（仍受 min/max 约束）。
//   - 若 [min,max] 内不存在 base 的整数倍，取不小于 min 的最小倍数（宁宽勿窄）。
func AdaptiveInterval(atr, multiplier, baseInterval, minInterval, maxInterval float64) float64 {
	if !validPositive(baseInterval) {
		return 0
	}
	lo := minInterval
	if !validPositive(lo) {
		lo = baseInterval
	}
	hi := maxInterval
	if !validPositive(hi) {
		hi = math.Inf(1)
	}
	if hi < lo {
		hi = lo
	}

	raw := baseInterval
	if validPositive(atr) && validPositive(multiplier) {
		raw = atr * multiplier
	}
	raw = math.Min(math.Max(raw, lo), hi)

	n := math.Max(1, math.Round(raw/baseInterval))
	minN := math.Max(1, math.Ceil(lo/baseInterval-quantizeEps))
	if n < minN {
		n = minN
	}
	if !math.IsInf(hi, 1) {
		maxN := math.Floor(hi/baseInterval + quantizeEps)
		if n > maxN {
			n = math.Max(maxN, minN)
		}
	}
	return n * baseInterval
}

// QuantizeInterval 把任意间隔（例如 PolicyFor 的 IntervalScale 放大后）量化为 base 的整数倍（至少 1 倍，四舍五入）。
// base 非法时返回 0。
func QuantizeInterval(interval, baseInterval float64) float64 {
	return AdaptiveInterval(interval, 1, baseInterval, baseInterval, 0)
}

// ShouldChangeInterval 仅当 proposed 相对 current 的变化比例 ≥ thresholdRatio 时返回 true（防抖）。
//   - proposed 非法 → false
//   - current 非法（尚未设置）→ true
//   - thresholdRatio ≤ 0 → 只要不相等即 true
func ShouldChangeInterval(current, proposed, thresholdRatio float64) bool {
	if !validPositive(proposed) {
		return false
	}
	if !validPositive(current) {
		return true
	}
	if proposed == current {
		return false
	}
	if thresholdRatio <= 0 {
		return true
	}
	return math.Abs(proposed-current)/current >= thresholdRatio-quantizeEps
}

// ResolveBounds 解析配置中的 min/max：0 分别回落为 base 与 base × DefaultMaxIntervalMultiple。
func (c AdaptiveIntervalConfig) ResolveBounds(baseInterval float64) (minInterval, maxInterval float64) {
	minInterval, maxInterval = c.MinInterval, c.MaxInterval
	if !validPositive(minInterval) {
		minInterval = baseInterval
	}
	if !validPositive(maxInterval) {
		maxInterval = baseInterval * DefaultMaxIntervalMultiple
	}
	return minInterval, maxInterval
}

// Next 综合计算下一次应使用的间隔：
//   - 未启用或 base 非法 → 返回 baseInterval（保持静态网格）
//   - 否则计算量化后的目标间隔，变化不足 ChangeThresholdRatio 时保持 current
//
// current ≤ 0 表示尚未设置，直接返回目标间隔。
func (c AdaptiveIntervalConfig) Next(current, atr, baseInterval float64) float64 {
	if !c.Enabled || !validPositive(baseInterval) {
		return baseInterval
	}
	lo, hi := c.ResolveBounds(baseInterval)
	proposed := AdaptiveInterval(atr, c.ATRMultiplier, baseInterval, lo, hi)
	if ShouldChangeInterval(current, proposed, c.ChangeThresholdRatio) {
		return proposed
	}
	return current
}

// AlignToAnchor 返回不高于 price 的最近槽位价 anchor + n×interval（n 可为负）。
// interval 非法时原样返回 price。
func AlignToAnchor(price, anchor, interval float64) float64 {
	if !validPositive(interval) {
		return price
	}
	n := math.Floor((price-anchor)/interval + quantizeEps)
	return anchor + n*interval
}
