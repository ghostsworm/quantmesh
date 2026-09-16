package bybit

import (
	"fmt"
	"strings"
)

// 內部（exchange 包）通用常量的字面值。
// bybit 包不能匯入 exchange（循環匯入），因此在此鏡像 exchange/types.go 中的取值；
// exchange 包的 wrapper 測試會校驗兩邊一致。
const (
	InternalSideBuy  = "BUY"
	InternalSideSell = "SELL"

	InternalOrderTypeLimit  = "LIMIT"
	InternalOrderTypeMarket = "MARKET"

	InternalStatusNew             = "NEW"
	InternalStatusPartiallyFilled = "PARTIALLY_FILLED"
	InternalStatusFilled          = "FILLED"
	InternalStatusCanceled        = "CANCELED"
	InternalStatusRejected        = "REJECTED"
	InternalStatusExpired         = "EXPIRED"

	// InternalTimeInForceGTX 內部 Post Only 的 TimeInForce 取值
	InternalTimeInForceGTX = "GTX"
)

// Bybit V5 其他原生訂單狀態
const (
	OrderStatusUntriggered             OrderStatus = "Untriggered"
	OrderStatusTriggered               OrderStatus = "Triggered"
	OrderStatusDeactivated             OrderStatus = "Deactivated"
	OrderStatusPartiallyFilledCanceled OrderStatus = "PartiallyFilledCanceled"
)

var sideToBybit = map[string]Side{
	InternalSideBuy:  SideBuy,
	InternalSideSell: SideSell,
}

var sideFromBybit = map[Side]string{
	SideBuy:  InternalSideBuy,
	SideSell: InternalSideSell,
}

var orderTypeToBybit = map[string]OrderType{
	InternalOrderTypeLimit:  OrderTypeLimit,
	InternalOrderTypeMarket: OrderTypeMarket,
}

var orderTypeFromBybit = map[OrderType]string{
	OrderTypeLimit:  InternalOrderTypeLimit,
	OrderTypeMarket: InternalOrderTypeMarket,
}

var statusFromBybit = map[OrderStatus]string{
	OrderStatusNew:                     InternalStatusNew,
	OrderStatusUntriggered:             InternalStatusNew,
	OrderStatusTriggered:               InternalStatusNew,
	OrderStatusPartiallyFilled:         InternalStatusPartiallyFilled,
	OrderStatusFilled:                  InternalStatusFilled,
	OrderStatusCanceled:                InternalStatusCanceled,
	OrderStatusPartiallyFilledCanceled: InternalStatusCanceled,
	OrderStatusDeactivated:             InternalStatusCanceled,
	OrderStatusRejected:                InternalStatusRejected,
	OrderStatusExpired:                 InternalStatusExpired,
}

// ToNativeSide 內部方向（BUY/SELL）→ Bybit side（Buy/Sell）
func ToNativeSide(internal string) (Side, error) {
	if s, ok := sideToBybit[strings.ToUpper(strings.TrimSpace(internal))]; ok {
		return s, nil
	}
	return "", fmt.Errorf("Bybit 不支援的訂單方向: %q", internal)
}

// ToNativeOrderType 內部訂單類型 → Bybit orderType（Limit/Market）
func ToNativeOrderType(internal string) (OrderType, error) {
	if t, ok := orderTypeToBybit[strings.ToUpper(strings.TrimSpace(internal))]; ok {
		return t, nil
	}
	return "", fmt.Errorf("Bybit 不支援的訂單類型: %q", internal)
}

// ToNativeTimeInForce 限價單的 timeInForce：postOnly 或內部 GTX → PostOnly，否則 GTC
func ToNativeTimeInForce(postOnly bool, internalTimeInForce string) TimeInForce {
	if postOnly || strings.EqualFold(internalTimeInForce, InternalTimeInForceGTX) {
		return TimeInForcePO
	}
	return TimeInForceGTC
}

// ToInternalSide Bybit side → 內部方向
func ToInternalSide(side Side) (string, error) {
	if s, ok := sideFromBybit[side]; ok {
		return s, nil
	}
	return "", fmt.Errorf("未知的 Bybit 訂單方向: %q", side)
}

// ToInternalOrderType Bybit orderType → 內部訂單類型
func ToInternalOrderType(t OrderType) (string, error) {
	if s, ok := orderTypeFromBybit[t]; ok {
		return s, nil
	}
	return "", fmt.Errorf("未知的 Bybit 訂單類型: %q", t)
}

// ToInternalStatus Bybit 訂單狀態 → 內部訂單狀態
func ToInternalStatus(status OrderStatus) (string, error) {
	if s, ok := statusFromBybit[status]; ok {
		return s, nil
	}
	return "", fmt.Errorf("未知的 Bybit 訂單狀態: %q", status)
}
