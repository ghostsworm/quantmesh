package position

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"time"

	"quantmesh/execution"
)

type liquidationSlotResidual struct {
	price, qty float64
	side, leg  string
}

// Residuals are per Bot/slot, not per exchange account. Their new CIDs must be
// installed before submission so market fills use the same economic ledger.
func (spm *SuperPositionManager) liquidationSlotResiduals() ([]liquidationSlotResidual, error) {
	var residuals []liquidationSlotResidual
	var problem error
	spm.slots.Range(func(key, value interface{}) bool {
		s := value.(*InventorySlot)
		s.mu.RLock()
		defer s.mu.RUnlock()
		if !finiteNonnegative(s.PositionQty) {
			problem = fmt.Errorf("槽位 %.8f 持倉數量無效", key.(float64))
			return false
		}
		if s.OrderID != 0 || s.ClientOID != "" || s.OrderStatus == OrderStatusUnknown || s.SlotStatus == SlotStatusPending {
			problem = fmt.Errorf("槽位 %.8f 尚有未核實訂單，禁止市價覆蓋", key.(float64))
			return false
		}
		if s.PositionQty <= liquidationQtyEpsilon {
			return true
		}
		r := liquidationSlotResidual{price: key.(float64), qty: s.PositionQty, side: "SELL", leg: PositionLegLong}
		if spm.liquidationIsShortLeg(s.PositionLeg) {
			r.side, r.leg = "BUY", PositionLegShort
		}
		residuals = append(residuals, r)
		return true
	})
	sort.Slice(residuals, func(i, j int) bool { return residuals[i].price < residuals[j].price })
	return residuals, problem
}

func finiteNonnegative(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 }

func (spm *SuperPositionManager) closeOwnedLiquidationResidual(ctx context.Context, venue LiquidationVenue, symbol string,
	residualSell, residualBuy float64, deadline time.Time, ownedIDs *[]int64) []string {
	residuals, err := spm.liquidationSlotResiduals()
	if err != nil {
		return []string{err.Error()}
	}
	var sell, buy float64
	for _, r := range residuals {
		if r.side == "SELL" {
			sell += r.qty
		} else {
			buy += r.qty
		}
	}
	if math.Abs(sell-residualSell) > liquidationQtyEpsilon || math.Abs(buy-residualBuy) > liquidationQtyEpsilon {
		return []string{fmt.Sprintf("成交核實與槽位殘餘不一致：帳本 SELL=%.8f BUY=%.8f，核實 SELL=%.8f BUY=%.8f", sell, buy, residualSell, residualBuy)}
	}
	if !spm.isSpot() {
		qctx, cancel := venueCallContext(ctx)
		sizes, queryErr := venue.GetPositionSizes(qctx, symbol)
		cancel()
		if queryErr != nil {
			return []string{fmt.Sprintf("持倉交叉核實失敗: %v", queryErr)}
		}
		if err := validateLiquidationPositions(sizes); err != nil {
			return []string{err.Error()}
		}
		var availableSell, availableBuy float64
		for _, size := range sizes {
			if size > 0 {
				availableSell += size
			} else {
				availableBuy -= size
			}
		}
		// Net account zero is not proof that this Bot's gross economic legs are
		// settled. Do not silently discard offsetting inventory or manufacture PnL.
		if sell > availableSell+liquidationQtyEpsilon || buy > availableBuy+liquidationQtyEpsilon {
			return []string{fmt.Sprintf("本 Bot 殘餘與交易所可減倉份額不一致：本地 SELL=%.8f BUY=%.8f，可用 SELL=%.8f BUY=%.8f，需對賬", sell, buy, availableSell, availableBuy)}
		}
	}
	if len(residuals) == 0 {
		return nil
	}
	executor, ok := spm.executor.(ContextOrderExecutor)
	if !ok {
		return []string{"市價補平缺少帶 ctx 的意圖執行器"}
	}
	for _, r := range residuals {
		p, err := spm.submitSlotLiquidationMarket(ctx, executor, r)
		if p.orderID > 0 {
			*ownedIDs = append(*ownedIDs, p.orderID)
		}
		if err != nil {
			return []string{err.Error()}
		}
		if _, _, err := spm.settleLiquidationLimitOrders(ctx, venue, symbol, []liquidationPlacedOrder{p}, deadline); err != nil {
			return []string{fmt.Sprintf("市價平倉 #%d 終態未核實: %v", p.orderID, err)}
		}
	}
	left, err := spm.liquidationSlotResiduals()
	if err != nil {
		return []string{err.Error()}
	}
	var problems []string
	for _, r := range left {
		problems = append(problems, fmt.Sprintf("本 Bot 槽位 %.8f 殘留 %s %.8f", r.price, r.side, r.qty))
	}
	return problems
}

func (spm *SuperPositionManager) submitSlotLiquidationMarket(ctx context.Context, executor ContextOrderExecutor, r liquidationSlotResidual) (liquidationPlacedOrder, error) {
	p := liquidationPlacedOrder{slotPrice: r.price, side: r.side, qty: r.qty}
	if err := ctx.Err(); err != nil {
		return p, err
	}
	s := spm.getOrCreateSlot(r.price)
	s.mu.Lock()
	if s.OrderID != 0 || s.ClientOID != "" || s.SlotStatus == SlotStatusPending || s.OrderStatus == OrderStatusUnknown || math.Abs(s.PositionQty-r.qty) > liquidationQtyEpsilon {
		s.mu.Unlock()
		return p, fmt.Errorf("槽位 %.8f 在市價補平前發生變更，需重新核實", r.price)
	}
	p.clientOID = spm.generateClientOrderID(r.price, r.side, "stop_loss")
	req := &OrderRequest{Symbol: spm.config.Trading.Symbol, Side: r.side, Type: "MARKET", Quantity: r.qty,
		ReduceOnly: !spm.isSpot(), PositionSide: r.leg, ClientOrderID: p.clientOID, OrderSource: "liquidation",
		ExposureKey:  gridExposureKey(r.price, r.leg),
		StrategyName: s.StrategyName, StrategyType: s.StrategyType, PriceDecimals: spm.priceDecimals}
	s.ClientOID, s.OrderSide, s.OrderPrice = p.clientOID, r.side, 0
	s.OrderFilledQty, s.OrderFilledNotional = 0, 0
	s.OrderStatus, s.SlotStatus = OrderStatusNotPlaced, SlotStatusPending
	s.OrderCreatedAt = spm.now()
	s.mu.Unlock()
	ord, err := executor.PlaceOrderContext(ctx, req)
	if ord != nil {
		p.orderID = ord.OrderID
	}
	if errors.Is(err, execution.ErrOrderUnknown) {
		spm.retainUnknownOrders([]*OrderRequest{req}, map[string]bool{req.ClientOrderID: true})
		return p, err
	}
	if err != nil {
		s.mu.Lock()
		if s.SlotStatus == SlotStatusPending && s.ClientOID == p.clientOID {
			s.ClientOID, s.OrderSide, s.OrderStatus, s.SlotStatus = "", "", OrderStatusNotPlaced, SlotStatusFree
		}
		s.mu.Unlock()
		return p, err
	}
	if ord == nil || ord.OrderID <= 0 || !spm.sameClientOrderID(ord.ClientOrderID, p.clientOID) {
		spm.retainUnknownOrders([]*OrderRequest{req}, map[string]bool{req.ClientOrderID: true})
		return p, fmt.Errorf("市價補平缺少匹配回執: %w", execution.ErrOrderUnknown)
	}
	return p, spm.acknowledgeLiquidationOrder(p, ord)
}
