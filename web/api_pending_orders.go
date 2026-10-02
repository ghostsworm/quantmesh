package web

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"quantmesh/utils"
	"time"
)

// ========== 待成交訂單相關API ==========

// getPendingOrders 獲取待成交订單列表
// GET /api/orders/pending
func getPendingOrders(c *gin.Context) {
	pmProvider := PickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}, "leverage": 1})
		return
	}

	slots := pmProvider.GetAllSlots()
	var pendingOrders []PendingOrderInfo

	for _, slot := range slots {
		// 筛选状態為 PLACED/CONFIRMED/PARTIALLY_FILLED 的订單
		if slot.OrderStatus == "PLACED" || slot.OrderStatus == "CONFIRMED" || slot.OrderStatus == "PARTIALLY_FILLED" {
			// 计算订單原始數量：使用配置的订單金額 / 订單價格
			var quantity float64
			if slot.OrderPrice > 0 && orderQuantityConfig > 0 {
				quantity = orderQuantityConfig / slot.OrderPrice
			} else if slot.OrderFilledQty > 0 {
				// 如果無法计算，使用已成交數量作為估算
				quantity = slot.OrderFilledQty
			}

			pendingOrders = append(pendingOrders, PendingOrderInfo{
				OrderID:        slot.OrderID,
				ClientOrderID:  slot.ClientOID,
				Exchange:       slot.Exchange,
				Symbol:         slot.Symbol,
				Price:          slot.OrderPrice,
				Quantity:       quantity,
				Side:           slot.OrderSide,
				Status:         slot.OrderStatus,
				FilledQuantity: slot.OrderFilledQty,
				CreatedAt:      utils.ToUTC8(slot.OrderCreatedAt),
				SlotPrice:      slot.Price,
				StrategyName:   slot.StrategyName,
				StrategyType:   slot.StrategyType,
			})
		}
	}

	// 獲取槓桿倍數（用於計算資金占用）
	leverage := 1
	if pmProvider != nil {
		if l := pmProvider.GetLeverage(); l > 0 {
			leverage = l
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": pendingOrders, "count": len(pendingOrders), "leverage": leverage})
}

// PendingOrderInfo 待成交订單信息
type PendingOrderInfo struct {
	OrderID        int64     `json:"order_id"`
	ClientOrderID  string    `json:"client_order_id"`
	Exchange       string    `json:"exchange"` // 交易所
	Symbol         string    `json:"symbol"`   // 交易对
	Price          float64   `json:"price"`
	Quantity       float64   `json:"quantity"`
	Side           string    `json:"side"` // BUY/SELL
	Status         string    `json:"status"`
	FilledQuantity float64   `json:"filled_quantity"`
	CreatedAt      time.Time `json:"created_at"`
	SlotPrice      float64   `json:"slot_price"`    // 槽位價格
	StrategyName   string    `json:"strategy_name"` // 策略名称
	StrategyType   string    `json:"strategy_type"` // 策略類型
}
