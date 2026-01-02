package mexc

// MEXCOrderSide MEXC 订单方向
type MEXCOrderSide int

const (
	MEXCOrderSideOpenLong   MEXCOrderSide = 1 // 开多
	MEXCOrderSideCloseLong  MEXCOrderSide = 2 // 平多
	MEXCOrderSideOpenShort  MEXCOrderSide = 3 // 开空
	MEXCOrderSideCloseShort MEXCOrderSide = 4 // 平空
)

// MEXCOrderType MEXC 订单类型
type MEXCOrderType int

const (
	MEXCOrderTypeLimit  MEXCOrderType = 1 // 限价单
	MEXCOrderTypeMarket MEXCOrderType = 2 // 市价单
)

// MEXCOpenType MEXC 仓位类型
type MEXCOpenType int

const (
	MEXCOpenTypeIsolated MEXCOpenType = 1 // 逐仓
	MEXCOpenTypeCross    MEXCOpenType = 2 // 全仓
)

// MEXCOrderState MEXC 订单状态
type MEXCOrderState int

const (
	MEXCOrderStateNew             MEXCOrderState = 1 // 未成交
	MEXCOrderStatePartiallyFilled MEXCOrderState = 2 // 部分成交
	MEXCOrderStateFilled          MEXCOrderState = 3 // 已成交
	MEXCOrderStateCanceled        MEXCOrderState = 4 // 已撤销
	MEXCOrderStatePartialCanceled MEXCOrderState = 5 // 部分成交已撤销
)

// MEXCPositionType MEXC 持仓方向
type MEXCPositionType int

const (
	MEXCPositionTypeLong  MEXCPositionType = 1 // 多仓
	MEXCPositionTypeShort MEXCPositionType = 2 // 空仓
)

// MEXCPositionState MEXC 持仓状态
type MEXCPositionState int

const (
	MEXCPositionStateHolding MEXCPositionState = 1 // 持仓中
	MEXCPositionStateManaged MEXCPositionState = 2 // 系统托管中
	MEXCPositionStateClosed  MEXCPositionState = 3 // 已平仓
)

// MEXCInterval K线周期
type MEXCInterval string

const (
	MEXCInterval1m  MEXCInterval = "Min1"
	MEXCInterval5m  MEXCInterval = "Min5"
	MEXCInterval15m MEXCInterval = "Min15"
	MEXCInterval30m MEXCInterval = "Min30"
	MEXCInterval1h  MEXCInterval = "Min60"
	MEXCInterval4h  MEXCInterval = "Hour4"
	MEXCInterval1d  MEXCInterval = "Day1"
	MEXCInterval1w  MEXCInterval = "Week1"
	MEXCInterval1M  MEXCInterval = "Month1"
)

// ConvertInterval 转换 K线周期
func ConvertInterval(interval string) MEXCInterval {
	switch interval {
	case "1m":
		return MEXCInterval1m
	case "5m":
		return MEXCInterval5m
	case "15m":
		return MEXCInterval15m
	case "30m":
		return MEXCInterval30m
	case "1h":
		return MEXCInterval1h
	case "4h":
		return MEXCInterval4h
	case "1d":
		return MEXCInterval1d
	case "1w":
		return MEXCInterval1w
	case "1M":
		return MEXCInterval1M
	default:
		return MEXCInterval1m
	}
}
