package replay

import (
	"errors"
	"fmt"
	"math"
	"strings"

	"quantmesh/config"
	"quantmesh/position"
)

// simExecutor 實現 position.OrderExecutorInterface。
// 下單語義與 order.ExchangeOrderExecutor 保持一致（不含分布式鎖、限流等待與網絡重試）：
//   - PostOnly 被拒（-5022）時往遠離盤口方向移一個 tick 重掛，最多 post_only_reprice_max_attempts 次，永不降級 GTC；
//     返回的 Order.Price 為最終掛單價，ClientOrderID 不變；
//   - 批量下單中保證金不足標記 HasMarginError，reduce-only 被拒記入 ReduceOnlyErrors。
//
// 實盤執行器的 25 單/秒限流與重定價間隔 100ms 是牆鐘等待，無法在加速回放中複用，故在此等價重寫。
type simExecutor struct {
	ex                 *simExchange
	postOnlyMaxAttempt int
}

func newSimExecutor(ex *simExchange, botCfg *config.Config) *simExecutor {
	return &simExecutor{
		ex:                 ex,
		postOnlyMaxAttempt: config.EffectivePostOnlyRepriceMaxAttempts(botCfg.Trading.PostOnlyRepriceMaxAttempts),
	}
}

// repricePostOnly 與 order.repricePostOnly 相同：BUY 下移、SELL 上移一個 tick；BUY 下移後不為正時返回 false
func repricePostOnly(price float64, side string, priceDecimals int) (float64, bool) {
	if priceDecimals < 0 {
		priceDecimals = 0
	}
	tick := math.Pow10(-priceDecimals)
	scale := math.Pow10(priceDecimals)
	var next float64
	if strings.EqualFold(side, sideSell) {
		next = price + tick
	} else {
		next = price - tick
	}
	next = math.Round(next*scale) / scale
	if next <= 0 {
		return price, false
	}
	return next, true
}

// PlaceOrder 下單（PostOnly 被拒時重定價）
func (e *simExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	if req == nil {
		return nil, fmt.Errorf("replay executor: nil order request: %w", errInvalidOrder)
	}
	decimals := req.PriceDecimals
	if decimals <= 0 {
		decimals = e.ex.priceDecimals
	}
	price := req.Price
	rejects := 0
	for {
		ord, err := e.ex.placeLimit(req, price)
		if err == nil {
			if rejects > 0 {
				e.ex.mu.Lock()
				e.ex.stats.postOnlyRepriced += rejects
				e.ex.mu.Unlock()
			}
			return ord, nil
		}
		if !req.PostOnly || !errors.Is(err, errPostOnlyWouldCross) {
			return nil, err
		}
		rejects++
		if rejects > e.postOnlyMaxAttempt {
			e.countFinalReject(rejects - 1)
			return nil, fmt.Errorf("PostOnly 重定價 %d 次後仍被拒 %s %s 原價=%.*f 最後價=%.*f: %w",
				e.postOnlyMaxAttempt, req.Symbol, req.Side, decimals, req.Price, decimals, price, err)
		}
		next, ok := repricePostOnly(price, req.Side, decimals)
		if !ok {
			e.countFinalReject(rejects - 1)
			return nil, fmt.Errorf("PostOnly 被拒且無法再重定價 %s %s 價格=%.*f: %w", req.Symbol, req.Side, decimals, price, err)
		}
		price = next
	}
}

func (e *simExecutor) countFinalReject(repriced int) {
	e.ex.mu.Lock()
	e.ex.stats.postOnlyFinal++
	e.ex.stats.postOnlyRepriced += repriced
	e.ex.mu.Unlock()
}

// BatchPlaceOrders 批量下單
func (e *simExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	res := e.BatchPlaceOrdersWithDetails(orders)
	return res.PlacedOrders, res.HasMarginError
}

// BatchPlaceOrdersWithDetails 批量下單（錯誤分類與實盤執行器一致）
func (e *simExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	res := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, 0, len(orders)),
		ReduceOnlyErrors: make(map[string]bool),
	}
	for _, req := range orders {
		ord, err := e.PlaceOrder(req)
		if err != nil {
			switch {
			case errors.Is(err, errMarginInsufficient):
				res.HasMarginError = true
			case errors.Is(err, errReduceOnlyRejected):
				res.ReduceOnlyErrors[req.ClientOrderID] = true
			}
			continue
		}
		res.PlacedOrders = append(res.PlacedOrders, ord)
	}
	return res
}

// BatchCancelOrders 批量撤單（CANCELED 回報異步投遞）
func (e *simExecutor) BatchCancelOrders(orderIDs []int64) error {
	e.ex.cancel(orderIDs)
	return nil
}
