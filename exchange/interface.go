package exchange

import "context"

// IExchange 交易所接口（所有交易所必须实现）
type IExchange interface {
	// GetName 获取交易所名称
	GetName() string

	// === 订单相关 ===

	// PlaceOrder 下单
	PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error)

	// BatchPlaceOrders 批量下单
	// 返回：成功的订单列表，是否有保证金不足错误
	BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool)

	// CancelOrder 取消订单
	CancelOrder(ctx context.Context, symbol string, orderID int64) error

	// BatchCancelOrders 批量取消订单
	BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error

	// CancelAllOrders 取消所有订单（退出时使用）
	// 各交易所根据自己的能力实现：
	// - Bitget: 使用一键全撤API
	// - Gate.io/Binance: 查询所有未完成订单后批量撤销
	CancelAllOrders(ctx context.Context, symbol string) error

	// GetOrder 查询订单
	GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error)

	// GetOpenOrders 查询未完成订单
	GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error)

	// === 账户与持仓 ===

	// GetAccount 获取账户信息
	GetAccount(ctx context.Context) (*Account, error)

	// GetPositions 获取持仓信息
	GetPositions(ctx context.Context, symbol string) ([]*Position, error)

	// GetBalance 获取余额
	GetBalance(ctx context.Context, asset string) (float64, error)

	// === WebSocket ===

	// StartOrderStream 启动订单流（WebSocket）
	// 使用 func(interface{}) 避免子包的循环导入问题
	// 实际传递的是 OrderUpdate 类型
	StartOrderStream(ctx context.Context, callback func(interface{})) error

	// StopOrderStream 停止订单流
	StopOrderStream() error

	// === 市场数据（如果需要） ===

	// GetLatestPrice 获取最新价格
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)

	// StartPriceStream 启动价格流（WebSocket）
	StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error

	// StartKlineStream 启动K线流（WebSocket）
	// symbols: 交易对列表，interval: K线周期（如 "1m"），callback: K线更新回调
	StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error

	// StopKlineStream 停止K线流
	StopKlineStream() error

	// GetHistoricalKlines 获取历史K线数据
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error)

	// === 合约信息 ===

	// GetPriceDecimals 获取价格精度（小数位数）
	GetPriceDecimals() int

	// GetQuantityDecimals 获取数量精度（小数位数）
	GetQuantityDecimals() int

	// GetBaseAsset 获取基础资产（交易币种）
	// 例如: BTCUSDT -> BTC, ETHUSDT -> ETH, BTCUSD_PERP -> BTC
	GetBaseAsset() string

	// GetQuoteAsset 获取计价资产（结算币种）
	// 例如: BTCUSDT -> USDT, ETHUSDT -> USDT, BTCUSD_PERP -> USD
	GetQuoteAsset() string
}
