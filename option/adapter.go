package option

import "context"

// FetchParams 拉取期权仓位参数
type FetchParams struct {
	Symbol    string // 标的，如 BTCUSDT
	Direction string // LONG=拉 Put 仓位，SHORT=拉 Call 仓位；空时默认 LONG
}

// Adapter 期权仓位适配器接口（统一 Binance/Deribit 等）
type Adapter interface {
	// FetchPositions 拉取期权持仓，按 direction 决定拉 Put 还是 Call
	// direction=LONG 或空：拉 Put（对冲做多网格的价格下跌风险）
	// direction=SHORT：拉 Call（对冲做空网格的价格上涨风险）
	FetchPositions(ctx context.Context, params FetchParams) ([]OptionHedgePosition, error)
	// Exchange 返回交易所标识
	Exchange() string
}
