package position

import (
	"context"
	"math"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

const (
	// accountCacheTTL 下單路徑使用的帳戶信息（可用餘額、帳戶槓桿）緩存有效期
	accountCacheTTL = 5 * time.Second
	// stopLossEquityRefreshInterval 權益止損使用的帳戶權益刷新間隔（後台異步刷新，不阻塞 tick）
	stopLossEquityRefreshInterval = 10 * time.Second
	// accountFetchTimeout 單次 GetAccount 超時
	accountFetchTimeout = 5 * time.Second

	// adjustOrdersForceInterval 即使價格未跨檔、無訂單事件，也至少每隔此間隔全量重算一次
	// （兜底覆蓋冷卻期到期、暫停/恢復、熱更新等不產生訂單事件的狀態變化）
	adjustOrdersForceInterval = 1 * time.Second
	// adjustOrdersPriceBucketRatio 價格分桶粒度（相對網格間距的比例）；與開倉安全緩衝 0.1×間距一致，
	// 價格在同一桶內移動不會改變任何槽位的可掛單判定
	adjustOrdersPriceBucketRatio = 0.1
)

// accountCache 帳戶信息緩存
type accountCache struct {
	mu         sync.RWMutex
	result     interface{}
	fetchedAt  time.Time
	refreshing atomic.Bool
	// equityFallbackWarned 權益止損無權益數據時回退持倉分母，每個 Bot 只告警一次
	equityFallbackWarned atomic.Bool
}

// adjustDebounce AdjustOrders 去抖狀態（bucket/lastRunAt 僅在持有 spm.mu 時讀寫）
type adjustDebounce struct {
	dirty     atomic.Bool
	hasRun    bool
	bucket    int64
	lastRunAt time.Time
}

// markAdjustDirty 標記需要在下一個 tick 全量重算（訂單事件、錨點/參數變化時調用；無鎖，可在持有槽位鎖時調用）
func (spm *SuperPositionManager) markAdjustDirty() {
	spm.adjust.dirty.Store(true)
}

// adjustPriceBucket 返回價格所在的去抖分桶；無法計算時返回 false（不去抖）
func (spm *SuperPositionManager) adjustPriceBucket(price float64) (int64, bool) {
	interval := spm.config.Trading.PriceInterval
	if spm.config.Trading.GridMode == "geometric" {
		// 等比網格 PriceInterval 為比例，換算成錨點附近的絕對間距
		interval *= spm.anchorPrice()
	}
	size := interval * adjustOrdersPriceBucketRatio
	if size <= 0 || price <= 0 || math.IsNaN(size) || math.IsInf(size, 0) {
		return 0, false
	}
	return int64(math.Floor(price / size)), true
}

// shouldSkipAdjust 判斷本 tick 是否可跳過全量重算：價格未跨出上次的分桶、無訂單事件、距上次全量重算未超過兜底間隔。
// 不跳過時記錄本次狀態並清除 dirty。調用方持有 spm.mu。
func (spm *SuperPositionManager) shouldSkipAdjust(price float64, now time.Time) bool {
	bucket, ok := spm.adjustPriceBucket(price)
	dirty := spm.adjust.dirty.Swap(false)
	if ok && !dirty && spm.adjust.hasRun && bucket == spm.adjust.bucket &&
		now.Sub(spm.adjust.lastRunAt) < adjustOrdersForceInterval {
		return true
	}
	spm.adjust.hasRun = ok
	spm.adjust.bucket = bucket
	spm.adjust.lastRunAt = now
	return false
}

// storeAccountSnapshot 寫入帳戶信息緩存
func (spm *SuperPositionManager) storeAccountSnapshot(result interface{}, at time.Time) {
	spm.account.mu.Lock()
	spm.account.result = result
	spm.account.fetchedAt = at
	spm.account.mu.Unlock()
}

// invalidateAccountCache 使帳戶緩存失效（如保證金不足後需要立即拿到真實餘額）
func (spm *SuperPositionManager) invalidateAccountCache() {
	spm.account.mu.Lock()
	spm.account.fetchedAt = time.Time{}
	spm.account.mu.Unlock()
}

func (spm *SuperPositionManager) cachedAccount(maxAge time.Duration) (interface{}, bool) {
	spm.account.mu.RLock()
	defer spm.account.mu.RUnlock()
	if spm.account.result == nil || spm.account.fetchedAt.IsZero() {
		return nil, false
	}
	return spm.account.result, spm.since(spm.account.fetchedAt) < maxAge
}

// getAccountCached 下單路徑取帳戶信息：緩存未過期直接返回，否則同步拉取並寫緩存。
// 調用方不得持有槽位鎖。
func (spm *SuperPositionManager) getAccountCached(ctx context.Context) (interface{}, error) {
	if res, fresh := spm.cachedAccount(accountCacheTTL); fresh {
		return res, nil
	}
	if spm.exchange == nil {
		return nil, nil
	}
	fetchCtx, cancel := context.WithTimeout(ctx, accountFetchTimeout)
	defer cancel()
	res, err := spm.exchange.GetAccount(fetchCtx)
	if err != nil {
		return nil, err
	}
	if !isNilInterface(res) {
		spm.storeAccountSnapshot(res, spm.now())
	}
	return res, nil
}

// refreshAccountAsync 後台刷新帳戶緩存（同一時刻最多一個刷新協程）
func (spm *SuperPositionManager) refreshAccountAsync() {
	if spm.exchange == nil || !spm.account.refreshing.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer spm.account.refreshing.Store(false)
		ctx, cancel := context.WithTimeout(context.Background(), accountFetchTimeout)
		defer cancel()
		res, err := spm.exchange.GetAccount(ctx)
		if err != nil {
			logger.Debug("🔍 [%s] 後台刷新帳戶權益失敗，沿用緩存: %v", spm.logPrefix(), err)
			return
		}
		if !isNilInterface(res) {
			spm.storeAccountSnapshot(res, spm.now())
		}
	}()
}

// accountEquityForStopLoss 返回緩存的帳戶權益（TotalMarginBalance，無則 TotalWalletBalance）；
// 過期時觸發後台刷新但不等待。無數據時返回 0。不發起同步網絡請求，可在 tick 中調用。
func (spm *SuperPositionManager) accountEquityForStopLoss() float64 {
	res, fresh := spm.cachedAccount(stopLossEquityRefreshInterval)
	if !fresh {
		spm.refreshAccountAsync()
	}
	return accountEquityFromResult(res)
}

// stopLossDenominator 硬止損比例分母：position 用持倉名義價值；equity 用帳戶權益（暫無權益數據時回退持倉價值）
func (spm *SuperPositionManager) stopLossDenominator(positionValue float64) (float64, string) {
	if spm.gridRiskControl().GetStopLossBasis() != config.StopLossBasisEquity {
		return positionValue, config.StopLossBasisPosition
	}
	if equity := spm.accountEquityForStopLoss(); equity > 0 {
		return equity, config.StopLossBasisEquity
	}
	if spm.account.equityFallbackWarned.CompareAndSwap(false, true) {
		logger.Warn("⚠️ [%s] [网格风控] stop_loss_basis=equity 但暫無帳戶權益數據，暫按持倉價值計算止損", spm.logPrefix())
	}
	return positionValue, config.StopLossBasisPosition
}

// accountEquityFromResult 反射解析帳戶權益（兼容多交易所帳戶類型）
func accountEquityFromResult(result interface{}) float64 {
	v := reflect.ValueOf(result)
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 0
	}
	for _, name := range []string{"TotalMarginBalance", "TotalWalletBalance"} {
		if f := v.FieldByName(name); f.IsValid() && f.CanInterface() {
			if val, ok := f.Interface().(float64); ok && val > 0 {
				return val
			}
		}
	}
	return 0
}

// accountAvailableBalance 反射解析可用餘額
func accountAvailableBalance(result interface{}) float64 {
	v := reflect.ValueOf(result)
	for v.IsValid() && (v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface) {
		if v.IsNil() {
			return 0
		}
		v = v.Elem()
	}
	if !v.IsValid() || v.Kind() != reflect.Struct {
		return 0
	}
	if f := v.FieldByName("AvailableBalance"); f.IsValid() && f.CanInterface() {
		if val, ok := f.Interface().(float64); ok {
			return val
		}
	}
	return 0
}

func isNilInterface(v interface{}) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice:
		return rv.IsNil()
	}
	return false
}
