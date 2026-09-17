// Package replay 實盤同構網格回放引擎。
//
// 與 backtest.RunGridBacktest（K 線觸價即成交）不同，本引擎直接驅動實盤使用的
// position.SuperPositionManager：價格通過 AdjustOrders(price) 餵入（與實盤價格循環同一入口），
// 掛單經模擬執行器/交易所撮合，成交以與 WebSocket 相同形狀的 position.OrderUpdate
// 回調 OnOrderUpdate。撮合模型見 docs/decisions/2026-09-17-replay-backtest.md。
//
// 本包位於 backtest 子包：position → storage → backtest 已存在依賴，backtest 本身不能 import position。
package replay

import (
	"fmt"
	"math"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

// 撮合與回放默認值
const (
	// DefaultMakerFeeRate 默認 maker 費率（0.02%）
	DefaultMakerFeeRate = 0.0002
	// DefaultTakerFeeRate 默認 taker 費率（0.05%）
	DefaultTakerFeeRate = 0.0005
	// DefaultParticipationRate 默認參與率：穿價成交量中可分配給本策略掛單的比例
	DefaultParticipationRate = 1.0
	// DefaultAdjustIntervalMs 默認 AdjustOrders 調用間隔（毫秒），與實盤價格循環 50ms 一致
	DefaultAdjustIntervalMs = 50
	// DefaultInitialCapital 默認初始資金（USDT）
	DefaultInitialCapital = 10000.0
	// DefaultLeverage 默認槓桿
	DefaultLeverage = 1
	// DefaultEquitySampleMs 權益/敞口採樣間隔（毫秒），用於權益曲線與敞口時間序列
	DefaultEquitySampleMs = int64(60 * 60 * 1000)
	// FundingIntervalMs 資金費結算間隔（8 小時，對齊 UTC 00/08/16 點）
	FundingIntervalMs = int64(8 * 60 * 60 * 1000)
	// DefaultCandleIntervalMs 無法從 K 線推斷周期時的默認值（1 分鐘）
	DefaultCandleIntervalMs = int64(60 * 1000)
	// DefaultIntrabarStepsPerLeg K 線內路徑每段（O→X、X→Y、Y→C）的插值步數
	DefaultIntrabarStepsPerLeg = 1
	// maxExposureSamples 敞口時間序列最多保留的點數（超過時抽稀）
	maxExposureSamples = 2000
	// quantityEpsilon 數量比較容差
	quantityEpsilon = 1e-12
)

// IntrabarPath K 線內價格路徑
type IntrabarPath string

const (
	// IntrabarPathAuto 按 K 線方向：陽線 O→L→H→C，陰線 O→H→L→C
	IntrabarPathAuto IntrabarPath = "auto"
	// IntrabarPathOHLC 固定 O→H→L→C
	IntrabarPathOHLC IntrabarPath = "OHLC"
	// IntrabarPathOLHC 固定 O→L→H→C
	IntrabarPathOLHC IntrabarPath = "OLHC"
)

// Tick 一筆成交（aggTrade 或 K 線內路徑點）
type Tick struct {
	Timestamp int64   `json:"timestamp"` // 毫秒
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"` // 成交量（基幣）；<=0 視為成交量未知（按無限流動性處理）
}

// FundingPoint 資金費率序列點
type FundingPoint struct {
	Timestamp int64   `json:"timestamp"` // 生效時間（毫秒）
	Rate      float64 `json:"rate"`      // 每 8 小時費率（如 0.0001）
}

// MatchingConfig 撮合模型
type MatchingConfig struct {
	MakerFeeRate float64 `json:"maker_fee_rate"`
	TakerFeeRate float64 `json:"taker_fee_rate"`
	// ParticipationRate 參與率 (0,1]：每筆穿價/觸價成交量中可成交給本策略的比例；<=0 用默認值
	ParticipationRate float64 `json:"participation_rate"`
	// QueueFactor 排隊模型：>0 時，成交價恰好等於掛單價（觸價未穿價）也可成交，
	// 但須先在該價位累計成交量 >= 掛單數量 × QueueFactor（模擬排在前面的隊列）；0 表示觸價不成交
	QueueFactor float64 `json:"queue_factor"`
	// FillOnTouch 樂觀模式：觸價即成交（舊引擎口徑，僅用於對比；開啟時忽略 QueueFactor）
	FillOnTouch bool `json:"fill_on_touch"`
}

// Config 回放配置
type Config struct {
	// Bot 實盤 Bot 配置（Trading 段），引擎會複製一份，不修改調用方對象
	Bot *config.Config
	// InitialCapital 初始資金（USDT）
	InitialCapital float64
	// Leverage 槓桿（通過 GetAccount.AccountLeverage 提供給倉位管理器）
	Leverage int
	// PriceDecimals / QuantityDecimals 交易所精度；<0 表示按首個價格推斷
	PriceDecimals    int
	QuantityDecimals int
	Matching         MatchingConfig
	// AdjustIntervalMs AdjustOrders 最小調用間隔（模擬時間）；<=0 用默認 50ms
	AdjustIntervalMs int64
	// FundingEnabled 是否結算資金費
	FundingEnabled bool
	// FundingRate 無資金費序列時使用的固定每 8h 費率
	FundingRate float64
	// FundingSeries 資金費率序列（按時間升序）；非空時優先使用
	FundingSeries []FundingPoint
	// EnforceMargin 是否模擬保證金不足拒單。倉位管理器注入了模擬時鐘，保證金鎖（默認 10 秒）按模擬時間解除。
	// 回測任務參數 enforce_margin 缺省為 true；直接構造 Config 時零值為關閉，需顯式開啟
	EnforceMargin bool
	// EquitySampleMs 權益曲線/敞口採樣間隔；<=0 用默認 1 小時
	EquitySampleMs int64

	// OrderCleaner 是否在模擬時間上運行 safety.OrderCleaner（與實盤 symbol_manager 一致）：
	// 每經過 Bot.Timing.OrderCleanupInterval 秒（無效時 safety.DefaultOrderCleanupInterval）同步執行一輪，
	// 掛單數達到 trading.order_cleanup_threshold 時撤銷數量多一側最遠的 cleanup_batch_size 張。
	// 關閉時單邊行情中遠端掛單會累積到閾值，網格停止開倉（實盤不會出現）。
	// 回測任務參數 order_cleaner 缺省為 true；直接構造 Config 時零值為關閉。
	OrderCleaner bool
	// Setup 可選：倉位管理器創建、模擬時鐘與費率注入之後、Initialize 之前調用，
	// 用於注入 RegimeProvider（ConfigureRegimeControl）、FundingMonitor（SetFundingMonitor）等實盤由 symbol_manager 完成的依賴。
	// clock 為引擎的模擬時鐘（*SimClock）。返回錯誤時 Run 失敗。
	Setup func(spm *position.SuperPositionManager, clock position.Clock) error
	// OnTick 可選：每個 tick 撮合並投遞回報之後、訂單清理與 AdjustOrders 之前調用（不持有倉位管理器鎖），
	// now 為該 tick 的模擬時間。用於在模擬時間上同步驅動 regime.Detector.Refresh 與 RefreshRegimeInterval 等實盤後台循環。
	OnTick func(now time.Time, price float64)
}

// InferPriceDecimals 按價格量級推斷價格精度（僅在未提供交易所精度時使用）
func InferPriceDecimals(price float64) int {
	switch {
	case price >= 10000:
		return 1
	case price >= 100:
		return 2
	case price >= 1:
		return 4
	default:
		return 6
	}
}

// InferQuantityDecimals 按價格量級推斷數量精度
func InferQuantityDecimals(price float64) int {
	switch {
	case price >= 100:
		return 3
	case price >= 1:
		return 1
	default:
		return 0
	}
}

// normalized 返回填充默認值後的配置副本；Bot 配置深拷貝一層（Trading 為值類型）
func (c Config) normalized(firstPrice float64) (Config, error) {
	if c.Bot == nil {
		return c, fmt.Errorf("replay config: bot config is nil")
	}
	botCopy := *c.Bot
	c.Bot = &botCopy
	if c.Bot.Trading.Symbol == "" {
		return c, fmt.Errorf("replay config: trading.symbol is empty")
	}
	if c.Bot.Trading.PriceInterval <= 0 {
		return c, fmt.Errorf("replay config: trading.price_interval must be positive, got %.8f", c.Bot.Trading.PriceInterval)
	}
	if c.Bot.Trading.OrderQuantity <= 0 {
		return c, fmt.Errorf("replay config: trading.order_quantity must be positive, got %.8f", c.Bot.Trading.OrderQuantity)
	}
	if c.InitialCapital <= 0 {
		c.InitialCapital = DefaultInitialCapital
	}
	if c.Leverage <= 0 {
		c.Leverage = DefaultLeverage
	}
	if c.PriceDecimals < 0 {
		c.PriceDecimals = InferPriceDecimals(firstPrice)
	}
	if c.QuantityDecimals < 0 {
		c.QuantityDecimals = InferQuantityDecimals(firstPrice)
	}
	m := &c.Matching
	if m.MakerFeeRate == 0 && m.TakerFeeRate == 0 {
		m.MakerFeeRate = DefaultMakerFeeRate
		m.TakerFeeRate = DefaultTakerFeeRate
	}
	if m.TakerFeeRate <= 0 {
		m.TakerFeeRate = DefaultTakerFeeRate
	}
	if m.ParticipationRate <= 0 || m.ParticipationRate > 1 {
		m.ParticipationRate = DefaultParticipationRate
	}
	if m.QueueFactor < 0 || math.IsNaN(m.QueueFactor) {
		return c, fmt.Errorf("replay config: queue_factor must be >= 0, got %v", m.QueueFactor)
	}
	if c.AdjustIntervalMs <= 0 {
		c.AdjustIntervalMs = DefaultAdjustIntervalMs
	}
	if c.EquitySampleMs <= 0 {
		c.EquitySampleMs = DefaultEquitySampleMs
	}
	return c, nil
}

// ExposurePoint 敞口採樣點
type ExposurePoint struct {
	Timestamp int64   `json:"timestamp"`
	NetQty    float64 `json:"net_qty"`  // 淨持倉（多正空負）
	Notional  float64 `json:"notional"` // 淨持倉名義價值（多正空負）
	Equity    float64 `json:"equity"`
}

// ExposureSummary 敞口摘要
type ExposureSummary struct {
	MaxAbsQty                  float64         `json:"max_abs_qty"`
	MaxAbsNotional             float64         `json:"max_abs_notional"`
	TimeWeightedAvgAbsNotional float64         `json:"time_weighted_avg_abs_notional"`
	TimeInMarketPct            float64         `json:"time_in_market_pct"` // 有持倉的時間占比（%）
	FinalNetQty                float64         `json:"final_net_qty"`
	Samples                    []ExposurePoint `json:"samples"`
}

// Metrics 回放專有指標
type Metrics struct {
	InitialCapital float64 `json:"initial_capital"`
	FinalEquity    float64 `json:"final_equity"`
	// NetPnL = 已實現 + 未實現 − 手續費 − 資金費
	NetPnL        float64 `json:"net_pnl"`
	RealizedPnL   float64 `json:"realized_pnl"` // 毛已實現盈虧（未扣費）
	UnrealizedPnL float64 `json:"unrealized_pnl"`

	FeesMaker   float64 `json:"fees_maker"`
	FeesTaker   float64 `json:"fees_taker"`
	FeesTotal   float64 `json:"fees_total"`
	FundingPaid float64 `json:"funding_paid"` // 正數=支付，負數=收取

	MaxDrawdownPct float64 `json:"max_drawdown_pct"`
	MaxDrawdownAbs float64 `json:"max_drawdown_abs"`

	Fills        int     `json:"fills"` // 成交回報次數（含部分成交）
	MakerFills   int     `json:"maker_fills"`
	TakerFills   int     `json:"taker_fills"`
	PartialFills int     `json:"partial_fills"`
	MakerRatio   float64 `json:"maker_ratio"` // maker 成交量 / 總成交量

	OrdersPlaced         int `json:"orders_placed"`
	OrdersCanceled       int `json:"orders_canceled"`
	PostOnlyRejects      int `json:"post_only_rejects"`       // 交易所層 PostOnly 拒單次數（含重定價前的每次拒絕）
	PostOnlyRepriced     int `json:"post_only_repriced"`      // 重定價次數
	PostOnlyFinalRejects int `json:"post_only_final_rejects"` // 重定價耗盡後最終失敗的下單
	ReduceOnlyRejects    int `json:"reduce_only_rejects"`
	MarginRejects        int `json:"margin_rejects"`

	// ClosedGrids 平倉（減倉）成交次數，作為「完成的格子」計數
	ClosedGrids int `json:"closed_grids"`
	// NetProfitPerGrid (已實現 − 總手續費) / ClosedGrids
	NetProfitPerGrid float64 `json:"net_profit_per_grid"`
	// FeePerGrid 總手續費 / ClosedGrids
	FeePerGrid float64 `json:"fee_per_grid"`
	// GridNetProfitToFeeRatio (已實現 − 總手續費) / 總手續費
	GridNetProfitToFeeRatio float64 `json:"grid_net_profit_to_fee_ratio"`

	Exposure ExposureSummary `json:"exposure"`

	TicksProcessed int `json:"ticks_processed"`
	AdjustCalls    int `json:"adjust_calls"`
	// OrderCleanerRuns 模擬時間上執行 OrderCleaner 的輪數（Config.OrderCleaner 關閉時為 0）
	OrderCleanerRuns int   `json:"order_cleaner_runs"`
	StartTime        int64 `json:"start_time"`
	EndTime          int64 `json:"end_time"`
}
