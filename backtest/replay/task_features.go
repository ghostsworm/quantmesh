package replay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
	"quantmesh/strategy/regime"
)

// 回測任務（engine=replay）的新功能參數。鍵名與配置文件一致：
//
//	regime_filter / adaptive_interval / upper_bound_freeze / inventory_skew / fee_aware_spread：
//	    true/false（只設 enabled，其餘用默認值）或與 trading.<鍵> 相同字段的對象（未知字段報錯）
//	funding_rate：數字 = 固定每 8h 費率（結算與資金費定價共用，舊行為）；對象 = funding_rate 配置段（enabled、pricing_enabled、bias_enabled…）
//	funding_pricing：true 等同 funding_rate.enabled + pricing_enabled（簡寫）
//	funding_constant_rate：funding_rate 為對象時的固定每 8h 費率（缺省 0）
//	funding_series：[{timestamp(ms), rate}] 資金費率序列，提供時優先於固定費率
//	max_position_layers：trading.open_position_control.max_position_layers（inventory_skew 按層數計算庫存比例）
const (
	paramRegimeFilter        = "regime_filter"
	paramAdaptiveInterval    = "adaptive_interval"
	paramUpperBoundFreeze    = "upper_bound_freeze"
	paramInventorySkew       = "inventory_skew"
	paramFeeAwareSpread      = "fee_aware_spread"
	paramFundingPricing      = "funding_pricing"
	paramFundingConstantRate = "funding_constant_rate"
	paramFundingSeries       = "funding_series"
	paramMaxPositionLayers   = "max_position_layers"

	// defaultTaskFundingConstantRate 沒有資金費率序列時的默認固定費率：0，即資金費定價不偏移、不結算資金費
	defaultTaskFundingConstantRate = 0.0

	// FundingSourceSeries 資金費來自任務提供的序列
	FundingSourceSeries = "series"
	// FundingSourceConstant 資金費使用固定費率（Web 回測從交易所/文件加載 K 線時沒有歷史資金費率序列）
	FundingSourceConstant = "constant"
)

// FeatureReport 回測結果中披露的新功能口徑（寫入 replay_metrics.features）
type FeatureReport struct {
	RegimeFilter     bool `json:"regime_filter"`
	AdaptiveInterval bool `json:"adaptive_interval"`
	UpperBoundFreeze bool `json:"upper_bound_freeze"`
	InventorySkew    bool `json:"inventory_skew"`
	FeeAwareSpread   bool `json:"fee_aware_spread"`
	FundingPricing   bool `json:"funding_pricing"`

	// RegimeKlineInterval regime 檢測器 K 線周期；RegimeKlineBars 由任務 K 線聚合得到的根數；RegimeRequiredBars 檢測器預熱所需根數
	RegimeKlineInterval string `json:"regime_kline_interval,omitempty"`
	RegimeKlineBars     int    `json:"regime_kline_bars,omitempty"`
	RegimeRequiredBars  int    `json:"regime_required_bars,omitempty"`
	// RegimeSharePct 各行情狀態佔採樣點百分比（unknown = 檢測器尚未就緒）
	RegimeSharePct       map[string]float64 `json:"regime_share_pct,omitempty"`
	MeanIntervalMultiple float64            `json:"mean_interval_multiple,omitempty"`

	// FundingSource series / constant；FundingConstantRate 固定費率（每 8h）
	FundingSource       string  `json:"funding_source,omitempty"`
	FundingConstantRate float64 `json:"funding_constant_rate"`
	FundingSeriesPoints int     `json:"funding_series_points,omitempty"`

	Stats *FeatureStats `json:"stats,omitempty"`
	// Notes 口徑說明（例如預熱不足、資金費率為固定值）
	Notes []string `json:"notes,omitempty"`
}

// applyTaskFeatureParams 把任務參數中的新功能開關寫入 Bot 配置，並解析資金費率來源
func applyTaskFeatureParams(p map[string]interface{}, bot *config.Config) (constantRate float64, series []FundingPoint, err error) {
	t := &bot.Trading
	if _, err = decodeFeatureParam(p, paramRegimeFilter, &t.RegimeFilter, func(b bool) { t.RegimeFilter.Enabled = b }); err != nil {
		return 0, nil, err
	}
	if _, err = decodeFeatureParam(p, paramAdaptiveInterval, &t.AdaptiveInterval, func(b bool) { t.AdaptiveInterval.Enabled = b }); err != nil {
		return 0, nil, err
	}
	if _, err = decodeFeatureParam(p, paramUpperBoundFreeze, &t.UpperBoundFreeze, func(b bool) { t.UpperBoundFreeze.Enabled = b }); err != nil {
		return 0, nil, err
	}
	if _, err = decodeFeatureParam(p, paramInventorySkew, &t.InventorySkew, func(b bool) { t.InventorySkew.Enabled = b }); err != nil {
		return 0, nil, err
	}
	if _, err = decodeFeatureParam(p, paramFeeAwareSpread, &t.FeeAwareSpread, func(b bool) { t.FeeAwareSpread.Enabled = &b }); err != nil {
		return 0, nil, err
	}
	if _, ok := p[paramMaxPositionLayers]; ok {
		t.OpenPositionControl.MaxPositionLayers = paramInt(p, paramMaxPositionLayers, 0)
	}

	// funding_rate：數字為固定費率（舊行為），對象為配置段
	constantRate = paramFloat(p, paramFundingRate, defaultTaskFundingConstantRate)
	if raw, ok := p[paramFundingRate].(map[string]interface{}); ok {
		if err = decodeJSONStrict(raw, &bot.FundingRate); err != nil {
			return 0, nil, fmt.Errorf("params.%s: %w", paramFundingRate, err)
		}
		constantRate = paramFloat(p, paramFundingConstantRate, defaultTaskFundingConstantRate)
	}
	if _, err = decodeFeatureParam(p, paramFundingPricing, nil, func(b bool) {
		bot.FundingRate.Enabled = b || bot.FundingRate.Enabled
		bot.FundingRate.PricingEnabled = b
	}); err != nil {
		return 0, nil, err
	}
	if series, err = parseFundingSeries(p[paramFundingSeries]); err != nil {
		return 0, nil, err
	}
	if err = validateTaskFeatures(bot); err != nil {
		return 0, nil, err
	}
	return constantRate, series, nil
}

// decodeFeatureParam 讀取「布爾或配置對象」參數；dst 為 nil 時只接受布爾。返回參數是否存在
func decodeFeatureParam(p map[string]interface{}, key string, dst interface{}, setEnabled func(bool)) (bool, error) {
	raw, ok := p[key]
	if !ok || raw == nil {
		return false, nil
	}
	switch v := raw.(type) {
	case bool:
		setEnabled(v)
		return true, nil
	case string:
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			return true, fmt.Errorf("params.%s: want bool or object, got %q", key, v)
		}
		setEnabled(b)
		return true, nil
	case map[string]interface{}:
		if dst == nil {
			return true, fmt.Errorf("params.%s: want bool, got object", key)
		}
		if err := decodeJSONStrict(v, dst); err != nil {
			return true, fmt.Errorf("params.%s: %w", key, err)
		}
		return true, nil
	default:
		return true, fmt.Errorf("params.%s: want bool or object, got %T", key, raw)
	}
}

// decodeJSONStrict 通過 JSON 把 map 解碼到配置結構，未知字段報錯（防止拼寫錯誤被靜默忽略）
func decodeJSONStrict(src map[string]interface{}, dst interface{}) error {
	b, err := json.Marshal(src)
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

// parseFundingSeries 解析 [{timestamp, rate}]，按時間排序
func parseFundingSeries(raw interface{}) ([]FundingPoint, error) {
	if raw == nil {
		return nil, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("params.%s: %w", paramFundingSeries, err)
	}
	var pts []FundingPoint
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&pts); err != nil {
		return nil, fmt.Errorf("params.%s: want [{timestamp, rate}]: %w", paramFundingSeries, err)
	}
	for i, pt := range pts {
		if pt.Timestamp <= 0 {
			return nil, fmt.Errorf("params.%s[%d]: timestamp must be positive (ms)", paramFundingSeries, i)
		}
	}
	sort.SliceStable(pts, func(i, j int) bool { return pts[i].Timestamp < pts[j].Timestamp })
	return pts, nil
}

// validateTaskFeatures 與實盤啟動時的校驗一致（config.validateGridR5Features 與 regime.NewDetector 的配置校驗）
func validateTaskFeatures(bot *config.Config) error {
	t := bot.Trading
	if t.InventorySkew.Strength < 0 || t.InventorySkew.Strength > 1 {
		return fmt.Errorf("params.%s.strength must be within [0, 1], got %v", paramInventorySkew, t.InventorySkew.Strength)
	}
	if t.UpperBoundFreeze.ATRMultiplier < 0 {
		return fmt.Errorf("params.%s.atr_multiplier must be >= 0, got %v", paramUpperBoundFreeze, t.UpperBoundFreeze.ATRMultiplier)
	}
	a := t.AdaptiveInterval
	if a.ATRMultiplier < 0 || a.MinInterval < 0 || a.MaxInterval < 0 || a.ChangeThresholdRatio < 0 {
		return fmt.Errorf("params.%s values must be >= 0", paramAdaptiveInterval)
	}
	if !NeedsRegimeDetector(bot) {
		return nil
	}
	if _, err := regimeControlOptions(bot); err != nil {
		return err
	}
	if err := position.RegimeConfigFromConfig(t.RegimeFilter).WithDefaults().Validate(); err != nil {
		return fmt.Errorf("params.%s: %w", paramRegimeFilter, err)
	}
	return nil
}

// installTaskFeatures 為回測任務注入新功能：regime K 線由任務 K 線聚合，資金費率來自序列或固定費率。
// 返回的報告在 Engine.Run 之後由 finalizeFeatureReport 補全統計。
func installTaskFeatures(cfg *Config, candles []*exchange.Candle) (*FeatureReport, *FeatureStats, error) {
	bot := cfg.Bot
	t := bot.Trading
	rep := &FeatureReport{
		RegimeFilter:        t.RegimeFilter.Enabled,
		AdaptiveInterval:    t.AdaptiveInterval.Enabled,
		UpperBoundFreeze:    t.UpperBoundFreeze.Enabled,
		InventorySkew:       t.InventorySkew.Enabled,
		FeeAwareSpread:      t.FeeAwareSpread.IsEnabled(),
		FundingPricing:      bot.FundingRate.Enabled && bot.FundingRate.PricingEnabled,
		FundingConstantRate: cfg.FundingRate,
	}
	if len(cfg.FundingSeries) > 0 {
		rep.FundingSource = FundingSourceSeries
		rep.FundingSeriesPoints = len(cfg.FundingSeries)
	} else if cfg.FundingEnabled || bot.FundingRate.Enabled {
		rep.FundingSource = FundingSourceConstant
		rep.Notes = append(rep.Notes, fmt.Sprintf(
			"資金費率使用固定值 %.6f/8h（未提供 %s；Web 回測從交易所或文件加載 K 線時沒有歷史資金費率），資金費結算與資金費定價均按該值計算",
			cfg.FundingRate, paramFundingSeries))
	}
	if bot.FundingRate.Enabled && !bot.FundingRate.PricingEnabled && !bot.FundingRate.BiasEnabled && bot.FundingRate.PreSettlementPauseMinutes <= 0 {
		rep.Notes = append(rep.Notes, "funding_rate.enabled 已開啟但未開啟 pricing_enabled/bias_enabled/pre_settlement_pause_minutes，資金費監控器不影響下單")
	}

	opts := FeatureOptions{Symbol: t.Symbol, FundingSeries: cfg.FundingSeries, FundingFallbackRate: cfg.FundingRate}
	if NeedsRegimeDetector(bot) {
		interval := RegimeKlineInterval(bot)
		rep.RegimeKlineInterval = interval
		rep.RegimeRequiredBars = position.RegimeConfigFromConfig(t.RegimeFilter).WithDefaults().RequiredBars()
		bars, err := regimeKlinesFromTaskCandles(candles, interval, t.Symbol)
		if err != nil {
			return nil, nil, err
		}
		rep.RegimeKlineBars = len(bars)
		opts.RegimeKlines = bars
		if len(bars) < rep.RegimeRequiredBars {
			rep.Notes = append(rep.Notes, fmt.Sprintf(
				"regime 檢測器需要 %d 根 %s K 線預熱，任務只有 %d 根（回測區間內不含更早的歷史），不足期間行情狀態為 unknown，regime_filter/adaptive_interval/upper_bound_freeze 在此期間不生效",
				rep.RegimeRequiredBars, interval, len(bars)))
		}
	}
	stats, err := InstallFeatureHooks(cfg, opts)
	if err != nil {
		return nil, nil, err
	}
	return rep, stats, nil
}

// finalizeFeatureReport 回放結束後寫入統計
func finalizeFeatureReport(rep *FeatureReport, stats *FeatureStats) {
	if rep == nil || stats == nil {
		return
	}
	rep.Stats = stats
	if stats.RegimeEnabled {
		rep.RegimeSharePct = stats.RegimeSharePct()
		rep.MeanIntervalMultiple = stats.MeanIntervalMultiple()
	}
}

// regimeKlinesFromTaskCandles 把任務 K 線聚合為檢測器周期。任務 K 線周期必須能整除檢測器周期（例如 1m/15m → 1h）。
func regimeKlinesFromTaskCandles(candles []*exchange.Candle, interval, symbol string) ([]*exchange.Candle, error) {
	d, err := regime.ParseKlineInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("params.%s.kline_interval: %w", paramRegimeFilter, err)
	}
	targetMs := d.Milliseconds()
	sorted := make([]*exchange.Candle, 0, len(candles))
	for _, c := range candles {
		if c != nil {
			sorted = append(sorted, c)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	uniq := sorted[:0]
	for _, c := range sorted {
		if len(uniq) > 0 && uniq[len(uniq)-1].Timestamp == c.Timestamp {
			continue
		}
		uniq = append(uniq, c)
	}
	if len(uniq) == 0 {
		return nil, nil
	}
	srcMs := inferCandleIntervalMs(uniq)
	if srcMs > targetMs || targetMs%srcMs != 0 {
		return nil, fmt.Errorf("regime 檢測器周期 %s 不能由任務 K 線周期 %dms 聚合（需要更小且能整除的 K 線周期，或調整 %s.kline_interval）",
			interval, srcMs, paramRegimeFilter)
	}
	bars, err := AggregateCandles(uniq, targetMs)
	if err != nil {
		return nil, err
	}
	for _, b := range bars {
		if b.Symbol == "" {
			b.Symbol = symbol
		}
	}
	return bars, nil
}
