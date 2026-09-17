package replay

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"quantmesh/backtest"
	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
)

// 任務參數鍵（task.Params，與舊網格回測共用 grid_spacing/profit_spread/order_quantity/direction/price_low/price_high）
const (
	paramGridSpacing       = "grid_spacing"
	paramGridCount         = "grid_count"
	paramProfitSpread      = "profit_spread"
	paramBuyWindowSize     = "buy_window_size"
	paramSellWindowSize    = "sell_window_size"
	paramOrderQuantity     = "order_quantity"
	paramDirection         = "direction"
	paramMarketType        = "market_type"
	paramPriceLow          = "price_low"
	paramPriceHigh         = "price_high"
	paramFeeRate           = "fee_rate"
	paramMakerFeeRate      = "maker_fee_rate"
	paramTakerFeeRate      = "taker_fee_rate"
	paramParticipationRate = "participation_rate"
	paramQueueFactor       = "queue_factor"
	paramFillOnTouch       = "fill_on_touch"
	paramIntrabarPath      = "intrabar_path"
	paramIntrabarSteps     = "intrabar_steps_per_leg"
	paramAdjustIntervalMs  = "adjust_interval_ms"
	paramFundingEnabled    = "funding_enabled"
	paramFundingRate       = "funding_rate"
	paramPriceDecimals     = "price_decimals"
	paramQuantityDecimals  = "quantity_decimals"
	paramAggTradeDir       = "aggtrade_dir"
	paramEnforceMargin     = "enforce_margin"
	paramPostOnlyReprice   = "post_only_reprice_max_attempts"
	// paramOrderCleaner 是否在模擬時間上運行 safety.OrderCleaner（缺省 true，與實盤一致）
	paramOrderCleaner          = "order_cleaner"
	paramOrderCleanupThreshold = "order_cleanup_threshold"
	paramCleanupBatchSize      = "cleanup_batch_size"
	paramOrderCleanupInterval  = "order_cleanup_interval"

	// defaultTaskOrderCleaner 回測任務默認運行訂單清理器：否則單邊行情中遠端掛單累積到閾值後網格停止開倉
	defaultTaskOrderCleaner = true
	// defaultTaskOrderCleanupIntervalSec 與實盤配置默認 timing.order_cleanup_interval=60 秒一致
	defaultTaskOrderCleanupIntervalSec = 60

	// defaultTaskWindowSize 未配置買/賣窗口且無 grid_count 時的默認窗口
	defaultTaskWindowSize = 10
	// defaultTaskOrderQuantity 默認每格金額（USDT）
	defaultTaskOrderQuantity = 100.0
	// defaultTaskEnforceMargin 回測任務默認模擬保證金不足拒單：
	// 倉位管理器已注入模擬時鐘，保證金鎖按模擬時間解除，不再「永久停單」
	defaultTaskEnforceMargin = true
	// unsetDecimals 表示精度按價格推斷
	unsetDecimals = -1
)

// RunGridTask 實現 backtest.ReplayTaskRunner：把回測任務參數映射為實盤 Bot 配置並回放。
// 數據源優先級：params.aggtrade_dir 下的 aggTrades（任務時間範圍）> 傳入的 K 線（K 線內路徑回退）。
// 未配置 price_low/price_high 時不限制區間（實盤語義），不會從回測期數據推導區間。
func RunGridTask(task *backtest.BacktestTask, candles []*exchange.Candle) (*backtest.BacktestResult, interface{}, error) {
	if task == nil {
		return nil, nil, fmt.Errorf("replay task: nil task")
	}
	cfg, err := ConfigFromTask(task)
	if err != nil {
		return nil, nil, fmt.Errorf("replay task %s: %w", task.ID, err)
	}

	var ticks []Tick
	if dir := paramString(task.Params, paramAggTradeDir, ""); dir != "" {
		loader := backtest.NewAggTradeLoader(dir, task.Symbol)
		rows, loadErr := loader.LoadAggTradesFromDir(task.StartTime, task.EndTime)
		if loadErr != nil {
			return nil, nil, fmt.Errorf("replay task %s: load aggTrades from %s: %w", task.ID, dir, loadErr)
		}
		ticks = AggTradesToTicks(rows)
		if len(ticks) == 0 {
			logger.Warn("⚠️ [回放回測] 任務 %s 在 %s 未找到 aggTrades，回退為 K 線內路徑", task.ID, dir)
		}
	}
	if len(ticks) == 0 {
		path, pathErr := ParseIntrabarPath(paramString(task.Params, paramIntrabarPath, ""))
		if pathErr != nil {
			return nil, nil, fmt.Errorf("replay task %s: %w", task.ID, pathErr)
		}
		ticks, err = CandlesToTicks(candles, IntrabarOptions{
			Path:        path,
			StepsPerLeg: paramInt(task.Params, paramIntrabarSteps, DefaultIntrabarStepsPerLeg),
		})
		if err != nil {
			return nil, nil, fmt.Errorf("replay task %s: %w", task.ID, err)
		}
	}

	res, err := NewEngine(cfg).Run(ticks)
	if err != nil {
		return nil, nil, fmt.Errorf("replay task %s: %w", task.ID, err)
	}
	logger.Info("✅ [回放回測] 任務 %s: 淨盈虧=%.4f 手續費=%.4f(maker %.4f/taker %.4f) 資金費=%.4f 成交=%d maker占比=%.2f%% 每格淨利/手續費=%.3f",
		task.ID, res.Metrics.NetPnL, res.Metrics.FeesTotal, res.Metrics.FeesMaker, res.Metrics.FeesTaker,
		res.Metrics.FundingPaid, res.Metrics.Fills, res.Metrics.MakerRatio*100, res.Metrics.GridNetProfitToFeeRatio)
	return res.Backtest, res.Metrics, nil
}

// ConfigFromTask 由任務參數構造回放配置
func ConfigFromTask(task *backtest.BacktestTask) (Config, error) {
	p := task.Params
	spacing := paramFloat(p, paramGridSpacing, 0)
	gridCount := paramInt(p, paramGridCount, 0)
	priceLow := paramFloat(p, paramPriceLow, 0)
	priceHigh := paramFloat(p, paramPriceHigh, 0)
	if spacing <= 0 {
		if priceLow > 0 && priceHigh > priceLow && gridCount > 0 {
			spacing = (priceHigh - priceLow) / float64(gridCount)
		} else {
			return Config{}, fmt.Errorf("params.%s must be positive (or provide explicit %s/%s/%s)",
				paramGridSpacing, paramPriceLow, paramPriceHigh, paramGridCount)
		}
	}
	window := defaultTaskWindowSize
	if gridCount > 0 {
		window = gridCount
	}

	bot := &config.Config{}
	bot.App.CurrentExchange = simExchangeName
	bot.Trading.Symbol = strings.ToUpper(task.Symbol)
	bot.Trading.MarketType = paramString(p, paramMarketType, "futures")
	bot.Trading.Direction = strings.ToUpper(paramString(p, paramDirection, "LONG"))
	bot.Trading.PriceInterval = spacing
	bot.Trading.ProfitSpread = paramFloat(p, paramProfitSpread, 0)
	bot.Trading.OrderQuantity = paramFloat(p, paramOrderQuantity, defaultTaskOrderQuantity)
	bot.Trading.BuyWindowSize = paramInt(p, paramBuyWindowSize, window)
	bot.Trading.SellWindowSize = paramInt(p, paramSellWindowSize, window)
	bot.Trading.PriceLow = priceLow
	bot.Trading.PriceHigh = priceHigh
	bot.Trading.PostOnlyRepriceMaxAttempts = paramInt(p, paramPostOnlyReprice, 0)
	bot.Trading.OrderCleanupThreshold = paramInt(p, paramOrderCleanupThreshold, 0)
	bot.Trading.CleanupBatchSize = paramInt(p, paramCleanupBatchSize, 0)
	bot.Timing.OrderCleanupInterval = paramInt(p, paramOrderCleanupInterval, defaultTaskOrderCleanupIntervalSec)

	taker := paramFloat(p, paramTakerFeeRate, paramFloat(p, paramFeeRate, DefaultTakerFeeRate))
	maker := paramFloat(p, paramMakerFeeRate, DefaultMakerFeeRate)
	leverage := int(math.Round(task.Leverage))
	fundingRate := paramFloat(p, paramFundingRate, 0)

	return Config{
		Bot:              bot,
		InitialCapital:   task.TotalCapital,
		Leverage:         leverage,
		PriceDecimals:    paramInt(p, paramPriceDecimals, unsetDecimals),
		QuantityDecimals: paramInt(p, paramQuantityDecimals, unsetDecimals),
		Matching: MatchingConfig{
			MakerFeeRate:      maker,
			TakerFeeRate:      taker,
			ParticipationRate: paramFloat(p, paramParticipationRate, DefaultParticipationRate),
			QueueFactor:       paramFloat(p, paramQueueFactor, 0),
			FillOnTouch:       paramBool(p, paramFillOnTouch, false),
		},
		AdjustIntervalMs: int64(paramInt(p, paramAdjustIntervalMs, DefaultAdjustIntervalMs)),
		FundingEnabled:   paramBool(p, paramFundingEnabled, fundingRate != 0),
		FundingRate:      fundingRate,
		EnforceMargin:    paramBool(p, paramEnforceMargin, defaultTaskEnforceMargin),
		OrderCleaner:     paramBool(p, paramOrderCleaner, defaultTaskOrderCleaner),
	}, nil
}

func paramFloat(m map[string]interface{}, key string, def float64) float64 {
	switch v := m[key].(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func paramInt(m map[string]interface{}, key string, def int) int {
	if _, ok := m[key]; !ok {
		return def
	}
	f := paramFloat(m, key, math.NaN())
	if math.IsNaN(f) {
		return def
	}
	return int(math.Round(f))
}

func paramString(m map[string]interface{}, key, def string) string {
	if s, ok := m[key].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	return def
}

func paramBool(m map[string]interface{}, key string, def bool) bool {
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		if b, err := strconv.ParseBool(strings.TrimSpace(v)); err == nil {
			return b
		}
	}
	return def
}
