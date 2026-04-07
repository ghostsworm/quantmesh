package config

import (
	"encoding/json"
	"strings"
)

// ApplyBotStrategiesToLocalConfig 將 Bot（SymbolConfig）上的 strategies 列表合并到本交易对运行时的 localCfg.Strategies。
// 用于实盘 startSymbolRuntime：使 StrategyManager 注册的键（如 trend）与 API 使用的类型名（如 trend_following）一致。
// 若 strategies 为空，不修改 local.Strategies（沿用全局配置）。
func ApplyBotStrategiesToLocalConfig(local *Config, symCfg *SymbolConfig) {
	if local == nil || symCfg == nil || len(symCfg.Strategies) == 0 {
		return
	}

	// 单策略且仅为 grid：保持 legacy 行为（仅 SuperPositionManager 网格路径，不启多策略模块）
	if len(symCfg.Strategies) == 1 {
		t := strings.TrimSpace(symCfg.Strategies[0].Type)
		if strings.EqualFold(t, "grid") {
			local.Strategies.Enabled = false
			if local.Strategies.Configs != nil {
				local.Strategies.Configs = nil
			}
			if symCfg.TotalAllocatedCapital > 0 {
				local.Strategies.CapitalAllocation.TotalCapital = symCfg.TotalAllocatedCapital
			}
			return
		}
	}

	configs := make(map[string]StrategyConfig)
	for _, si := range symCfg.Strategies {
		expandStrategyInstances(si, configs)
	}

	local.Strategies.Enabled = len(configs) > 0
	local.Strategies.Configs = configs

	if symCfg.TotalAllocatedCapital > 0 {
		local.Strategies.CapitalAllocation.TotalCapital = symCfg.TotalAllocatedCapital
	}
}

// ShouldSkipInitialGridAdjustOrders 启动时是否跳过 SuperPositionManager 的首轮 AdjustOrders，
// 避免纯趋势/动量等非网格策略误挂网格单。
func ShouldSkipInitialGridAdjustOrders(local *Config) bool {
	if local == nil || !local.Strategies.Enabled {
		return false
	}
	gc, ok := local.Strategies.Configs["grid"]
	return !ok || !gc.Enabled
}

func expandStrategyInstances(si StrategyInstance, out map[string]StrategyConfig) {
	rawType := strings.TrimSpace(si.Type)
	if rawType == "" {
		return
	}

	switch strings.ToLower(rawType) {
	case "grid+trend":
		w := si.Weight
		if w <= 0 {
			w = 1
		}
		cfg := cloneConfigMap(si.Config)
		gridW, trendW := splitComboWeights(cfg, w)
		out["grid"] = StrategyConfig{
			Enabled: true,
			Type:    "grid",
			Weight:  gridW,
			Config:  stripComboWeightKeys(cfg),
		}
		out["trend"] = StrategyConfig{
			Enabled: true,
			Type:    "trend",
			Weight:  trendW,
			Config:  normalizeTrendFollowingConfig(stripComboWeightKeys(cfg)),
		}
	default:
		key := apiStrategyTypeToRuntimeKey(rawType)
		if key == "" {
			return
		}
		w := si.Weight
		if w <= 0 {
			w = 1
		}
		cfg := normalizeStrategyConfigByKey(key, cloneConfigMap(si.Config))
		out[key] = StrategyConfig{
			Enabled: true,
			Type:    key,
			Weight:  w,
			Config:  cfg,
		}
	}
}

func apiStrategyTypeToRuntimeKey(apiType string) string {
	switch strings.ToLower(strings.TrimSpace(apiType)) {
	case "grid":
		return "grid"
	case "trend_following", "trend":
		return "trend"
	case "momentum":
		return "momentum"
	case "mean_reversion":
		return "mean_reversion"
	case "dca":
		return "dca"
	case "martingale":
		return "martingale"
	case "dca_enhanced":
		return "dca_enhanced"
	case "combo":
		return "combo"
	default:
		return ""
	}
}

func splitComboWeights(cfg map[string]interface{}, totalWeight float64) (gridW, trendW float64) {
	gridW = totalWeight * 0.5
	trendW = totalWeight * 0.5
	if cfg == nil {
		return gridW, trendW
	}
	if g, ok := float64FromInterface(cfg["grid_weight"]); ok && g > 0 {
		if t, ok2 := float64FromInterface(cfg["trend_weight"]); ok2 && t > 0 {
			s := g + t
			if s > 0 {
				return totalWeight * (g / s), totalWeight * (t / s)
			}
		}
	}
	return gridW, trendW
}

func stripComboWeightKeys(cfg map[string]interface{}) map[string]interface{} {
	if cfg == nil {
		return nil
	}
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		if k == "grid_weight" || k == "trend_weight" {
			continue
		}
		out[k] = v
	}
	return out
}

func cloneConfigMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func normalizeStrategyConfigByKey(runtimeKey string, cfg map[string]interface{}) map[string]interface{} {
	switch runtimeKey {
	case "trend":
		return normalizeTrendFollowingConfig(cfg)
	default:
		return cfg
	}
}

// normalizeTrendFollowingConfig 对齐回测/API 命名与 strategy.TrendFollowingStrategy（short_period/long_period），并处理 JSON 数字类型。
func normalizeTrendFollowingConfig(cfg map[string]interface{}) map[string]interface{} {
	if cfg == nil {
		return nil
	}
	out := cloneConfigMap(cfg)

	if _, hasShort := out["short_period"]; !hasShort {
		if fp, ok := intFromConfig(out, "fast_period"); ok {
			out["short_period"] = fp
		}
	}
	if _, hasLong := out["long_period"]; !hasLong {
		if sp, ok := intFromConfig(out, "slow_period"); ok {
			out["long_period"] = sp
		}
	}
	return out
}

func intFromConfig(m map[string]interface{}, key string) (int, bool) {
	v, ok := m[key]
	if !ok || v == nil {
		return 0, false
	}
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float64:
		if n == float64(int(n)) {
			return int(n), true
		}
		return int(n), true
	case float32:
		return int(n), true
	case json.Number:
		i64, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i64), true
	default:
		return 0, false
	}
}

func float64FromInterface(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	default:
		return 0, false
	}
}
