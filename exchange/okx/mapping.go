package okx

import (
	"fmt"
	"strings"
)

// 內部（exchange 包）通用常量的字面值。
// okx 包不能匯入 exchange（循環匯入），因此在此鏡像 exchange/types.go 中的取值；
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

// OKX 原生 ordType 取值（除 limit/market 外）
const (
	OrderTypePostOnly        OrderType = "post_only"
	OrderTypeFOK             OrderType = "fok"
	OrderTypeIOC             OrderType = "ioc"
	OrderTypeOptimalLimitIOC OrderType = "optimal_limit_ioc"
	OrderTypeMMP             OrderType = "mmp"
	OrderTypeMMPAndPostOnly  OrderType = "mmp_and_post_only"
)

// OrderStatusMMPCanceled OKX 做市商保護觸發的撤單狀態
const OrderStatusMMPCanceled OrderStatus = "mmp_canceled"

var sideToOKX = map[string]Side{
	InternalSideBuy:  SideBuy,
	InternalSideSell: SideSell,
}

var sideFromOKX = map[Side]string{
	SideBuy:  InternalSideBuy,
	SideSell: InternalSideSell,
}

var orderTypeFromOKX = map[OrderType]string{
	OrderTypeLimit:           InternalOrderTypeLimit,
	OrderTypeMarket:          InternalOrderTypeMarket,
	OrderTypePostOnly:        InternalOrderTypeLimit,
	OrderTypeFOK:             InternalOrderTypeLimit,
	OrderTypeIOC:             InternalOrderTypeLimit,
	OrderTypeMMP:             InternalOrderTypeLimit,
	OrderTypeMMPAndPostOnly:  InternalOrderTypeLimit,
	OrderTypeOptimalLimitIOC: InternalOrderTypeMarket,
}

// OKX 訂單狀態：live / partially_filled / filled / canceled / mmp_canceled
var statusFromOKX = map[OrderStatus]string{
	OrderStatusNew:             InternalStatusNew,
	OrderStatusPartiallyFilled: InternalStatusPartiallyFilled,
	OrderStatusFilled:          InternalStatusFilled,
	OrderStatusCanceled:        InternalStatusCanceled,
	OrderStatusMMPCanceled:     InternalStatusCanceled,
	OrderStatusRejected:        InternalStatusRejected,
	OrderStatusExpired:         InternalStatusExpired,
}

// ToNativeSide 內部方向（BUY/SELL）→ OKX side（buy/sell）
func ToNativeSide(internal string) (Side, error) {
	if s, ok := sideToOKX[strings.ToUpper(strings.TrimSpace(internal))]; ok {
		return s, nil
	}
	return "", fmt.Errorf("OKX 不支援的訂單方向: %q", internal)
}

// ToNativeOrderType 內部訂單類型 → OKX ordType。
// LIMIT 且 postOnly（或 TimeInForce=GTX）→ post_only；市價單不允許 post only。
func ToNativeOrderType(internalType string, postOnly bool, timeInForce string) (OrderType, error) {
	wantPostOnly := postOnly || strings.EqualFold(timeInForce, InternalTimeInForceGTX)
	switch strings.ToUpper(strings.TrimSpace(internalType)) {
	case InternalOrderTypeLimit:
		if wantPostOnly {
			return OrderTypePostOnly, nil
		}
		return OrderTypeLimit, nil
	case InternalOrderTypeMarket:
		if wantPostOnly {
			return "", fmt.Errorf("OKX 市價單不支援 post only")
		}
		return OrderTypeMarket, nil
	default:
		return "", fmt.Errorf("OKX 不支援的訂單類型: %q", internalType)
	}
}

// ToInternalSide OKX side → 內部方向
func ToInternalSide(side Side) (string, error) {
	if s, ok := sideFromOKX[side]; ok {
		return s, nil
	}
	return "", fmt.Errorf("未知的 OKX 訂單方向: %q", side)
}

// ToInternalOrderType OKX ordType → 內部訂單類型
func ToInternalOrderType(t OrderType) (string, error) {
	if s, ok := orderTypeFromOKX[t]; ok {
		return s, nil
	}
	return "", fmt.Errorf("未知的 OKX 訂單類型: %q", t)
}

// ToInternalStatus OKX 訂單狀態 → 內部訂單狀態
func ToInternalStatus(status OrderStatus) (string, error) {
	if s, ok := statusFromOKX[status]; ok {
		return s, nil
	}
	return "", fmt.Errorf("未知的 OKX 訂單狀態: %q", status)
}
