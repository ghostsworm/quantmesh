package position

import (
	"math"
	"sync"
	"sync/atomic"

	"quantmesh/config"
	"quantmesh/logger"
)

// gridOrdersPostOnly 網格開/平倉單一律以 PostOnly 掛單（被拒時由執行器重定價，永不降級 GTC），
// 因此費率下界按 maker 費率計算。
const gridOrdersPostOnly = true

// feeFloorRoundTrips 一格往返的手續費次數（開倉 + 平倉）
const feeFloorRoundTrips = 2

// feeRateState 手續費率（並發安全，可在運行中刷新）
type feeRateState struct {
	mu    sync.RWMutex
	maker float64
	taker float64
	set   bool
	// belowFloorWarned 配置利差低於費率下界時每個 Bot 只告警一次
	belowFloorWarned atomic.Bool
}

// SetFeeRates 注入 maker/taker 費率（如 0.0002 表示 0.02%）。
// maker 可為負（返佣），計算下界時按 0 處理；非有限或超出 [-1, 1] 的
// 費率視為不可信，忽略本次設置以免污染委託價格。
func (spm *SuperPositionManager) SetFeeRates(maker, taker float64) {
	if !validGridFeeRates(maker, taker) {
		logger.Warn("⚠️ [%s] 忽略無效手續費率 maker=%.6f taker=%.6f", spm.logPrefix(), maker, taker)
		return
	}
	spm.fees.mu.Lock()
	changed := !spm.fees.set || spm.fees.maker != maker || spm.fees.taker != taker
	spm.fees.maker = maker
	spm.fees.taker = taker
	spm.fees.set = true
	spm.fees.mu.Unlock()
	if changed {
		logger.Info("💳 [%s] 費率感知利差使用手續費: maker %.4f%% / taker %.4f%%", spm.logPrefix(), maker*100, taker*100)
	}
}

func validGridFeeRates(maker, taker float64) bool {
	return !math.IsNaN(maker) && !math.IsInf(maker, 0) && maker >= -1 && maker <= 1 &&
		!math.IsNaN(taker) && !math.IsInf(taker, 0) && taker > 0 && taker <= 1
}

// GetFeeRates 返回當前 maker/taker 費率及是否已設置
func (spm *SuperPositionManager) GetFeeRates() (maker, taker float64, ok bool) {
	spm.fees.mu.RLock()
	defer spm.fees.mu.RUnlock()
	return spm.fees.maker, spm.fees.taker, spm.fees.set
}

// feeAwareSpreadFloor 平倉利差下界 = entryPrice × (2×費率 + 安全邊際)；
// postOnly 時用 maker 費率，否則用 taker。未啟用、未注入費率或價格無效時返回 0。
func (spm *SuperPositionManager) feeAwareSpreadFloor(entryPrice float64, postOnly bool) float64 {
	if entryPrice <= 0 || spm.config == nil {
		return 0
	}
	fa := spm.config.Trading.FeeAwareSpread
	if !fa.IsEnabled() {
		return 0
	}
	maker, taker, ok := spm.GetFeeRates()
	if !ok {
		return 0
	}
	rate := taker
	if postOnly {
		rate = math.Max(maker, 0)
	}
	floor := entryPrice * (feeFloorRoundTrips*rate + fa.GetSafetyMarginRatio())
	if math.IsNaN(floor) || math.IsInf(floor, 0) || floor < 0 {
		return 0
	}
	return floor
}

// applyFeeAwareSpread 返回 max(配置利差, 費率下界)；配置利差低於下界時每個 Bot 只告警一次
func (spm *SuperPositionManager) applyFeeAwareSpread(spread, entryPrice float64, postOnly bool) float64 {
	floor := spm.feeAwareSpreadFloor(entryPrice, postOnly)
	if floor <= spread {
		return spread
	}
	if spm.fees.belowFloorWarned.CompareAndSwap(false, true) {
		logger.Warn("⚠️ [%s] 配置的平倉利差 %.8f 低於手續費下界 %.8f（價格 %.8f × (2×費率 + 安全邊際 %.4f%%)），已自動抬高到下界；請調大 profit_spread/price_interval",
			spm.logPrefix(), spread, floor, entryPrice, spm.config.Trading.FeeAwareSpread.GetSafetyMarginRatio()*100)
	}
	return floor
}

// closeSpreadForSlot 平倉利差：槽位檔位利差（三級火箭）與按開倉基準價計算的費率下界取大
func (spm *SuperPositionManager) closeSpreadForSlot(slotPrice, gridPrice, entryPrice float64) float64 {
	return spm.applyFeeAwareSpread(spm.getProfitSpreadForSlot(slotPrice, gridPrice), entryPrice, gridOrdersPostOnly)
}

// priceTick 價格最小變動單位（按價格小數位）
func (spm *SuperPositionManager) priceTick() float64 {
	decimals := spm.priceDecimals
	if decimals < 0 {
		decimals = 0
	}
	return math.Pow10(-decimals)
}

// makerSafeClosePrice 確保 PostOnly 平倉價位於盤口外側，避免必然被拒：
// SELL 不低於 現價 + tick×(1+連續拒單數)，BUY 不高於 現價 - tick×(1+連續拒單數)。
// 連續拒單數（PostOnlyFailCount）封頂於重定價次數上限。調用方持有 slot.mu 讀取 failCount。
func (spm *SuperPositionManager) makerSafeClosePrice(closePrice, currentPrice float64, closeSide string, failCount int) float64 {
	if currentPrice <= 0 {
		return closePrice
	}
	maxAttempts := config.EffectivePostOnlyRepriceMaxAttempts(spm.config.Trading.PostOnlyRepriceMaxAttempts)
	if failCount > maxAttempts {
		failCount = maxAttempts
	}
	if failCount < 0 {
		failCount = 0
	}
	offset := spm.priceTick() * float64(1+failCount)
	if closeSide == "SELL" {
		if minPrice := roundPrice(currentPrice+offset, spm.priceDecimals); closePrice < minPrice {
			return minPrice
		}
		return closePrice
	}
	if maxPrice := roundPrice(currentPrice-offset, spm.priceDecimals); closePrice > maxPrice && maxPrice > 0 {
		return maxPrice
	}
	return closePrice
}
