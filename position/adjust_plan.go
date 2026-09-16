package position

import (
	"math"
	"time"

	"quantmesh/logger"
	"quantmesh/strategy/regime"
)

// openingLegPlan 單條開倉腿本輪 AdjustOrders 的調整結果（默認值 = 原有行為）
type openingLegPlan struct {
	Window            int     // 開倉窗口（regime 縮放 + 庫存偏斜後）
	StopOpening       bool    // 本輪停止新開倉（庫存已滿偏斜到 0 / 結算前暫停）
	FreezeBound       bool    // 冻结順勢邊界（regime 順勢）
	PriceShift        float64 // 開倉價額外遠離現價（價格單位，≥0）
	CloseSpreadFactor float64 // 平倉利差係數（≤1）
	CloseShift        float64 // 平倉價額外外移（價格單位，≥0；資金費定價）
}

// planOpeningLeg 合併 regime 策略、庫存偏斜與資金費定價。調用方持有 spm.mu。
func (spm *SuperPositionManager) planOpeningLeg(dir regime.Direction, baseWindow int, currentPrice float64, t regimeTick, now time.Time) openingLegPlan {
	plan := openingLegPlan{Window: baseWindow, CloseSpreadFactor: 1}

	if t.policyActive() {
		p := regime.PolicyFor(t.effective, dir)
		if baseWindow > 0 {
			plan.Window = max(1, int(math.Floor(float64(baseWindow)*p.EntryWindowScale)))
		}
		plan.FreezeBound = p.FreezeFavorableBound
	}

	interval := spm.absoluteGridInterval(currentPrice)

	if skewCfg := spm.config.Trading.InventorySkew; skewCfg.Enabled {
		if maxLayers := spm.inventoryMaxLayers(); maxLayers > 0 {
			inv := inventoryRatio(spm.countFilledLayers(dir), maxLayers)
			s := computeInventorySkew(inv, skewCfg.WithDefaults().Strength)
			plan.Window = scaleWindow(plan.Window, s.windowFactor)
			if plan.Window == 0 && baseWindow > 0 {
				plan.StopOpening = true
			}
			plan.PriceShift += s.priceShiftIntervals * interval
			plan.CloseSpreadFactor = s.closeSpreadFactor
		} else if spm.regimeCtl.skewNoMaxLayersWarned.CompareAndSwap(false, true) {
			logger.Warn("⚠️ [%s] 已啟用 inventory_skew 但未配置最大層數（max_position_layers / max_grid_layers），庫存偏斜不生效", spm.logPrefix())
		}
	}

	shift, pause := spm.fundingPlan(dir, currentPrice, interval, now)
	if shift > 0 {
		plan.PriceShift += shift
		plan.CloseShift = shift
	}
	if pause {
		plan.StopOpening = true
		logger.Debug("⏸️ [%s] [資金費] %s 付費方臨近結算，暫停新開倉", spm.logPrefix(), dir)
	}

	if limit := maxOpenPriceShiftIntervalRatio * interval; plan.PriceShift > limit {
		plan.PriceShift = limit
	}
	return plan
}
