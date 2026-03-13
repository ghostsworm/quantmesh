package option

import "context"

// Adapter 期权仓位适配器接口（统一 Binance/Deribit 等）
type Adapter interface {
	// FetchPositions 拉取期权持仓，返回标准化后的 Put 仓位
	FetchPositions(ctx context.Context, symbol string) ([]OptionHedgePosition, error)
	// Exchange 返回交易所标识
	Exchange() string
}
