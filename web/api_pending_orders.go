package web

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"quantmesh/position"
	"quantmesh/utils"
	"reflect"
	"strings"
	"time"
)

// ========== 待成交訂單相關API ==========

// getPendingOrders 獲取待成交订單列表
// GET /api/orders/pending
func getPendingOrders(c *gin.Context) {
	ex := strings.TrimSpace(c.Query("exchange"))
	sym := strings.TrimSpace(c.Query("symbol"))
	if (ex == "") != (sym == "") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "pending_order_scope_incomplete"})
		return
	}
	pmProvider := pendingPositionProvider(c, ex, sym)
	if pmProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "pending_order_evidence_unavailable", "source": "local_slots", "requires_reconciliation": true})
		return
	}

	slots := pmProvider.GetAllSlots()
	pendingOrders := make([]PendingOrderInfo, 0)

	for _, slot := range slots {
		// 筛选状態為 PLACED/CONFIRMED/PARTIALLY_FILLED 的订單
		if isPendingOrderStatus(slot.OrderStatus) {
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

	c.JSON(http.StatusOK, gin.H{"orders": pendingOrders, "count": len(pendingOrders), "leverage": leverage, "source": "local_slots"})
}

// Explicit requests must not fall back to a different symbol or market.
// An unscoped legacy view still uses its default provider; this is not account-wide evidence.
func pendingPositionProvider(c *gin.Context, exchange, symbol string) PositionManagerProvider {
	var provider PositionManagerProvider
	if exchange == "" && symbol == "" {
		provider = PickPositionProvider(c)
	} else {
		key := makeSymbolKey(exchange, symbol, strings.TrimSpace(c.Query("market_type")))
		providersMu.RLock()
		provider = positionProviders[key]
		providersMu.RUnlock()
		if provider == nil {
			runtime := pickSymbolRuntimeByQuery(c)
			if pendingRuntimeScopeMatches(runtime, exchange, symbol, c.Query("market_type")) {
				provider = positionProviderFromSymbolRuntime(runtime)
			}
		}
	}
	if provider == nil {
		return nil
	}
	rv := reflect.ValueOf(provider)
	if rv.Kind() == reflect.Ptr && rv.IsNil() {
		return nil
	}
	if adapter, ok := provider.(*positionManagerAdapter); ok && adapter.manager == nil {
		return nil
	}
	return provider
}

func pendingRuntimeScopeMatches(runtime interface{}, exchange, symbol, marketType string) bool {
	if runtime == nil {
		return false
	}
	rv := reflect.ValueOf(runtime)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return false
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return false
	}
	cfg := rv.FieldByName("Config")
	if cfg.Kind() == reflect.Ptr {
		if cfg.IsNil() {
			return false
		}
		cfg = cfg.Elem()
	}
	if cfg.Kind() != reflect.Struct {
		return false
	}
	ex, sym, mt := cfg.FieldByName("Exchange"), cfg.FieldByName("Symbol"), cfg.FieldByName("MarketType")
	if ex.Kind() != reflect.String || sym.Kind() != reflect.String || mt.Kind() != reflect.String {
		return false
	}
	actualMarket := strings.ToLower(strings.TrimSpace(mt.String()))
	if actualMarket == "" {
		actualMarket = "futures"
	}
	margin := cfg.FieldByName("UseSpotMargin")
	if actualMarket == "spot" && margin.Kind() == reflect.Bool && margin.Bool() {
		actualMarket = "spot_margin"
	}
	wantMarket := strings.ToLower(strings.TrimSpace(marketType))
	if wantMarket == "" {
		wantMarket = "futures"
	}
	return strings.EqualFold(ex.String(), exchange) && strings.EqualFold(sym.String(), symbol) && actualMarket == wantMarket
}

func isPendingOrderStatus(status string) bool {
	switch status {
	case position.OrderStatusPlaced, position.OrderStatusConfirmed, position.OrderStatusPartiallyFilled,
		position.OrderStatusUnknown, position.OrderStatusCancelRequested:
		return true
	default:
		return false
	}
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
