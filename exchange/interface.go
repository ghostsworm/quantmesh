package exchange

import (
	"context"
	"errors"

	"quantmesh/exchange/income"
)

// 錯誤定義
var (
	ErrNotImplemented = errors.New("功能未實現")
)

// IExchange 交易所介面（所有交易所必須實現）
type IExchange interface {
	// GetName 獲取交易所名称
	GetName() string

	// GetMarketType 獲取市場類型：spot 現貨 / futures 合約
	GetMarketType() string

	// === 訂單相關 ===

	// PlaceOrder 下單
	PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error)

	// BatchPlaceOrders 批量下單
	// 回傳：成功的订單列表，是否有保证金不足錯误
	BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool)

	// CancelOrder 取消訂單
	CancelOrder(ctx context.Context, symbol string, orderID int64) error

	// BatchCancelOrders 批量取消訂單
	BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error

	// CancelAllOrders 取消所有订單（退出時使用）
	// 各交易所根據自己的能力實現：
	// - Bitget: 使用一鍵全撤API
	// - Gate.io/Binance: 查詢所有未完成订單后批量撤銷
	CancelAllOrders(ctx context.Context, symbol string) error

	// GetOrder 查詢訂單
	GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error)

	// GetOpenOrders 查詢未完成订單
	GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error)

	// GetOrderFills 查詢訂單成交記錄（用於獲取手續費）
	// orderID: 訂單ID，如果為0則查詢 symbol 的所有最近成交記錄
	// 返回: 成交記錄列表，每條記錄包含手續費信息
	// 不支援的交易所可返回 nil, nil
	GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error)

	// === 帳戶與持倉 ===

	// GetAccount 獲取帳戶信息
	GetAccount(ctx context.Context) (*Account, error)

	// GetPositions 獲取持倉信息
	GetPositions(ctx context.Context, symbol string) ([]*Position, error)

	// GetBalance 獲取餘額
	GetBalance(ctx context.Context, asset string) (float64, error)

	// === WebSocket ===

	// StartOrderStream 啟動訂單流（WebSocket）
	// 使用 func(interface{}) 避免子包的循環匯入问题
	// 實際傳遞的是 OrderUpdate 類型
	StartOrderStream(ctx context.Context, callback func(interface{})) error

	// StopOrderStream 停止訂單流
	StopOrderStream() error

	// === 市场數據（如果需要） ===

	// GetLatestPrice 獲取最新價格
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)

	// StartPriceStream 啟動價格流（WebSocket）
	StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error

	// StartKlineStream 啟動K線流（WebSocket）
	// symbols: 交易對列表，interval: K線週期（如 "1m"），callback: K線更新回呼
	StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error

	// StopKlineStream 停止K線流
	StopKlineStream() error

	// GetHistoricalKlines 獲取歷史K線數據
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error)

	// === 合約信息 ===

	// GetPriceDecimals 獲取價格精度（小數位數）
	GetPriceDecimals() int

	// GetQuantityDecimals 獲取數量精度（小數位數）
	GetQuantityDecimals() int

	// GetBaseAsset 獲取基础资產（交易币种）
	// 例如: BTCUSDT -> BTC, ETHUSDT -> ETH, BTCUSD_PERP -> BTC
	GetBaseAsset() string

	// GetQuoteAsset 獲取计價资產（結算币种）
	// 例如: BTCUSDT -> USDT, ETHUSDT -> USDT, BTCUSD_PERP -> USD
	GetQuoteAsset() string

	// === 订單金額預估 ===

	// EstimateFinalOrderAmount 預估最终下單金額（USDT）
	// 用於资金分配器在下單前准确預留资金
	// 交易所可能因最小名义金額、精度對齐等原因調整數量，導致實際金額與原始金額不同
	// 参數: symbol 交易對, price 價格, quantity 數量, reduceOnly 是否只减倉
	// 返回: 預估的最终名义金額（USDT）
	EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64

	// === 资金费率 ===

	// GetFundingRate 獲取资金费率
	// symbol: 交易對（如 BTCUSDT）
	// 返回: 资金费率（如 0.0001 表示 0.01%）
	GetFundingRate(ctx context.Context, symbol string) (float64, error)

	// GetIncomeHistory 獲取收入歷史（資金費用等）
	// symbol: 交易對（可選，空則查全部）
	// incomeType: FUNDING_FEE 等（可選，空則查全部類型）
	// startTime, endTime: 毫秒時間戳
	// 返回: 收入記錄列表；不支援的交易所可返回 nil, nil
	GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error)

	// === 現貨價格 ===

	// GetSpotPrice 獲取現貨市场價格
	// symbol: 交易對（如 BTCUSDT）
	// 返回: 現貨價格
	// 注意: 此方法用於獲取現貨市场價格，與 GetLatestPrice（合約價格）区分
	GetSpotPrice(ctx context.Context, symbol string) (float64, error)

	// === 订單簿深度 ===

	// GetOrderBook 獲取訂單簿深度
	// symbol: 交易對, limit: 檔位數量 (通常支援 5/10/20/50/100)
	// 返回: 订單簿數據，包含買賣盘深度
	GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error)

	// === 資金費率詳情 ===

	// GetFundingInfo 獲取資金費率詳細信息（含下次結算時間）
	// 不支援的交易所可返回 nil, ErrNotImplemented
	GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error)

	// === 內部轉帳（盈利提取） ===

	// InternalTransfer 交易所內部轉帳
	// fromAccount: 源账戶類型 (如 "UMFUTURE" 期貨帳戶)
	// toAccount: 目標账戶類型 (如 "SPOT" 現貨账戶)
	// asset: 资產類型 (如 "USDT")
	// amount: 轉账金額
	// 返回: 轉账ID, error
	InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error)
}

// ISpotMarginExchange 現貨槓桿交易所介面（借幣做空）
// 僅 Binance Spot Margin 等支援借還的交易所實現
type ISpotMarginExchange interface {
	IExchange
	Borrow(ctx context.Context, asset string, amount float64) (int64, error)
	Repay(ctx context.Context, asset string, amount float64) (int64, error)
}
