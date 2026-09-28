package position

import (
	"math"

	"quantmesh/strategy/regime"
)

// ========== 庫存偏斜（R5 第五節第 7 條）==========
//
// inv = 已成交層數 / 最大層數（0..1），s = trading.inventory_skew.strength：
//   - 開倉窗口 × (1 − inv×s)，係數 ≤ 0 時停止開倉；否則至少保留 1 檔
//   - 開倉價再遠離現價 inv×s×間隔（LONG 下移買價，SHORT 上移賣價）
//   - 平倉利差 × (1 − 0.5×inv×s)，但不低於費率感知下界（fee_aware_spread）

// inventoryCloseSpreadSkewRatio 平倉利差隨庫存縮小的比例係數（0.5×inv×s）
const inventoryCloseSpreadSkewRatio = 0.5

// inventorySkew 由庫存比例計算出的偏斜參數
type inventorySkew struct {
	windowFactor        float64 // 開倉窗口係數
	priceShiftIntervals float64 // 開倉價外移（以間隔為單位）
	closeSpreadFactor   float64 // 平倉利差係數
}

// computeInventorySkew 按庫存比例與強度計算偏斜參數（輸入會被截斷到 [0,1]）
func computeInventorySkew(inv, strength float64) inventorySkew {
	inv = clamp01(inv)
	strength = clamp01(strength)
	k := inv * strength
	return inventorySkew{
		windowFactor:        1 - k,
		priceShiftIntervals: k,
		closeSpreadFactor:   1 - inventoryCloseSpreadSkewRatio*k,
	}
}

func clamp01(v float64) float64 {
	if math.IsNaN(v) || v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// scaleWindow 窗口按係數縮放：factor ≤ 0 → 0（停止開倉）；否則 max(1, floor(window×factor))；原窗口 ≤ 0 保持不變
func scaleWindow(window int, factor float64) int {
	if window <= 0 {
		return window
	}
	if factor <= 0 || math.IsNaN(factor) {
		return 0
	}
	return max(1, int(math.Floor(float64(window)*factor)))
}

// inventoryRatio 庫存比例 layers/maxLayers，截斷到 [0,1]；maxLayers ≤ 0 返回 0
func inventoryRatio(layers, maxLayers int) float64 {
	if maxLayers <= 0 {
		return 0
	}
	return clamp01(float64(layers) / float64(maxLayers))
}

// inventoryMaxLayers 庫存偏斜的最大層數來源：Bot 獨立風控 max_position_layers → 開倉管理 max_position_layers → 網格風控 max_grid_layers；均未配置返回 0
func (spm *SuperPositionManager) inventoryMaxLayers() int {
	oc := spm.openingControl()
	if _, _, layers := oc.PositionLimits(); layers > 0 {
		return layers
	}
	if g := spm.gridRiskControl(); g.Enabled && g.MaxGridLayers > 0 {
		return g.MaxGridLayers
	}
	return 0
}

// countFilledLayers 已成交層數：BOTH 模式按腿統計（未標腿的槽位視為多腿），單向模式統計全部
func (spm *SuperPositionManager) countFilledLayers(dir regime.Direction) int {
	both := spm.isBoth()
	wantLeg := PositionLegLong
	if dir == regime.DirectionShort {
		wantLeg = PositionLegShort
	}
	layers := 0
	spm.slots.Range(func(_, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			leg := slot.PositionLeg
			if leg == PositionLegNone {
				leg = PositionLegLong
			}
			if !both || leg == wantLeg {
				layers++
			}
		}
		slot.mu.RUnlock()
		return true
	})
	return layers
}

// skewedCloseSpread 平倉利差：factor ≥ 1 時與 closeSpreadForSlot 完全一致；
// factor < 1 時取 max(槽位利差×factor, 費率下界)，且不超過未偏斜的利差。
func (spm *SuperPositionManager) skewedCloseSpread(slotPrice, gridPrice, basePrice, factor float64) float64 {
	spread := spm.closeSpreadForSlot(slotPrice, gridPrice, basePrice)
	if factor >= 1 || factor <= 0 || math.IsNaN(factor) {
		return spread
	}
	skewed := spm.getProfitSpreadForSlot(slotPrice, gridPrice) * factor
	if floor := spm.feeAwareSpreadFloor(basePrice, gridOrdersPostOnly); skewed < floor {
		skewed = floor
	}
	return math.Min(skewed, spread)
}

// absoluteGridInterval 絕對網格間隔（價格單位）：等比網格按現價換算
func (spm *SuperPositionManager) absoluteGridInterval(price float64) float64 {
	interval := spm.config.Trading.PriceInterval
	if spm.config.Trading.GridMode == gridModeGeometric {
		interval *= price
	}
	if interval <= 0 || math.IsNaN(interval) || math.IsInf(interval, 0) {
		return 0
	}
	return interval
}

// shiftedOpenPrice 開倉委託價遠離現價 shift（BUY 下移、SELL 上移）；槽位鍵不變（ClientOrderID 仍編碼槽位價）
func (spm *SuperPositionManager) shiftedOpenPrice(slotPrice, shift float64, openSide string) float64 {
	if shift <= 0 {
		return slotPrice
	}
	if openSide == "BUY" {
		if p := roundPrice(slotPrice-shift, spm.priceDecimals); p > 0 {
			return p
		}
		return slotPrice
	}
	return roundPrice(slotPrice+shift, spm.priceDecimals)
}
