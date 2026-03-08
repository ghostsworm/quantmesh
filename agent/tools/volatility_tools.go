package tools

import (
	"context"
	"fmt"

	"quantmesh/agent/types"
	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/logger"
)

// VolatilityConfigTool 波动率配置工具
type VolatilityConfigTool struct {
	BaseTool
	configStore *config.BotConfigManager
}

// NewVolatilityConfigTool 创建波动率配置工具
func NewVolatilityConfigTool(configStore *config.BotConfigManager) *VolatilityConfigTool {
	return &VolatilityConfigTool{
		BaseTool: BaseTool{
			name:        "configure_volatility_detection",
			description: "配置波动率检测和自动暂停开仓功能",
			category:    CategorySystem,
		},
		configStore: configStore,
	}
}

// ParameterSchema 返回参数模式
func (t *VolatilityConfigTool) ParameterSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"symbol": map[string]interface{}{
				"type":        "string",
				"description": "交易对符号，如 BTCUSDT",
			},
			"enable_detection": map[string]interface{}{
				"type":        "boolean",
				"description": "是否启用波动率检测",
			},
			"use_preset": map[string]interface{}{
				"type":        "boolean",
				"description": "是否使用内置预设（推荐）",
			},
			"custom_thresholds": map[string]interface{}{
				"type":        "object",
				"description": "自定义阈值（不使用预设时）",
				"properties": map[string]interface{}{
					"low":      map[string]interface{}{"type": "number"},
					"normal":   map[string]interface{}{"type": "number"},
					"high":     map[string]interface{}{"type": "number"},
					"extreme":  map[string]interface{}{"type": "number"},
				},
			},
			"pause_on_high": map[string]interface{}{
				"type":        "boolean",
				"description": "高波动时是否自动暂停开仓",
			},
			"pause_on_extreme": map[string]interface{}{
				"type":        "boolean",
				"description": "极端波动时是否自动暂停开仓",
			},
			"pause_on_downtrend": map[string]interface{}{
				"type":        "boolean",
				"description": "做多策略在高波动下跌时是否暂停开仓",
			},
			"pause_on_uptrend": map[string]interface{}{
				"type":        "boolean",
				"description": "做空策略在高波动上涨时是否暂停开仓",
			},
			"auto_resume": map[string]interface{}{
				"type":        "boolean",
				"description": "波动率回归正常时是否自动恢复开仓",
			},
		},
		"required": []string{"symbol"},
	}
}

// AssessRisk 评估操作风险
func (t *VolatilityConfigTool) AssessRisk(params map[string]interface{}) types.SecurityLevel {
	// 配置操作属于中等风险
	return types.SecurityLevelMedium
}

// Execute 执行工具
func (t *VolatilityConfigTool) Execute(ctx context.Context, params map[string]interface{}) (types.ToolResult, error) {
	symbol, _ := params["symbol"].(string)
	enableDetection, _ := params["enable_detection"].(bool)
	usePreset, _ := params["use_preset"].(bool)
	pauseOnHigh, _ := params["pause_on_high"].(bool)
	pauseOnExtreme, _ := params["pause_on_extreme"].(bool)
	pauseOnDowntrend, _ := params["pause_on_downtrend"].(bool)
	pauseOnUptrend, _ := params["pause_on_uptrend"].(bool)
	autoResume, _ := params["auto_resume"].(bool)

	// 获取预设信息
	preset := indicators.GetVolatilityPreset(symbol)
	presetInfo := indicators.GetPresetForSymbol(symbol)

	result := map[string]interface{}{
		"symbol":          symbol,
		"enabled":         enableDetection,
		"use_preset":      usePreset,
		"preset_info":     presetInfo,
		"pause_on_high":   pauseOnHigh,
		"pause_on_extreme": pauseOnExtreme,
		"pause_on_downtrend": pauseOnDowntrend,
		"pause_on_uptrend":   pauseOnUptrend,
		"auto_resume":     autoResume,
	}

	// 如果不使用预设，处理自定义阈值
	if !usePreset {
		if customThresholds, ok := params["custom_thresholds"].(map[string]interface{}); ok {
			result["custom_thresholds"] = customThresholds
		}
	}

	// 生成配置建议
	suggestions := t.generateConfigurationSuggestions(symbol, preset, pauseOnHigh, pauseOnExtreme)

	result["suggestions"] = suggestions
	result["config_summary"] = t.generateConfigSummary(symbol, enableDetection, usePreset, preset)

	logger.Info("🔧 [波动率配置] %s: 检测=%v, 预设=%v", symbol, enableDetection, usePreset)

	return types.ToolResult{
		Result: result,
	}, nil
}

// generateConfigurationSuggestions 生成配置建议
func (t *VolatilityConfigTool) generateConfigurationSuggestions(symbol string, preset indicators.VolatilityPreset, pauseOnHigh, pauseOnExtreme bool) []string {
	suggestions := []string{}

	// 根据品种类型给出建议
	switch preset.Name {
	case "Gold":
		suggestions = append(suggestions, "💡 黄金品种波动相对温和，建议启用高波动暂停")
		suggestions = append(suggestions, "💡 可以设置较低的阈值以获得更及时的保护")
	case "BTC", "ETH":
		suggestions = append(suggestions, "💡 BTC/ETH 波动较大，建议启用极端波动暂停")
		suggestions = append(suggestions, "💡 建议同时启用趋势过滤以在单边行情中保护仓位")
	case "Stablecoin":
		suggestions = append(suggestions, "💡 稳定币品种波动极小，波动率检测意义不大")
		suggestions = append(suggestions, "💡 可以禁用波动率检测或设置极低阈值")
	case "Meme", "DeFi":
		suggestions = append(suggestions, "⚠️ 该品种波动极大，强烈建议启用所有保护机制")
		suggestions = append(suggestions, "⚠️ 建议在极端波动时自动暂停开仓")
	default:
		suggestions = append(suggestions, "💡 建议根据实际波动情况调整阈值")
	}

	// 根据暂停设置给出建议
	if pauseOnHigh {
		suggestions = append(suggestions, "✅ 高波动暂停已启用，可以在波动增加时保护资金")
	}
	if pauseOnExtreme {
		suggestions = append(suggestions, "✅ 极端波动暂停已启用，可以在市场剧烈波动时避免损失")
	}

	return suggestions
}

// generateConfigSummary 生成配置摘要
func (t *VolatilityConfigTool) generateConfigSummary(symbol string, enableDetection, usePreset bool, preset indicators.VolatilityPreset) string {
	var summary string

	summary += fmt.Sprintf("交易对: %s\n", symbol)
	summary += fmt.Sprintf("波动率检测: %v\n", enableDetection)

	if enableDetection {
		if usePreset {
			summary += fmt.Sprintf("使用预设: %s\n", preset.Name)
			summary += fmt.Sprintf("阈值配置: 低<%.1f%%, 正常<%.1f%%, 高<%.1f%%, 极端≥%.1f%%\n",
				preset.LowThreshold, preset.NormalThreshold, preset.HighThreshold, preset.ExtremeThreshold)
		} else {
			summary += "使用自定义阈值\n"
		}
	}

	return summary
}

// GetVolatilityStatus 获取波动率状态（用于查询）
func (t *VolatilityConfigTool) GetVolatilityStatus(symbol string) map[string]interface{} {
	preset := indicators.GetVolatilityPreset(symbol)

	return map[string]interface{}{
		"symbol":       symbol,
		"preset_name":  preset.Name,
		"thresholds": map[string]interface{}{
			"low":     preset.LowThreshold,
			"normal":  preset.NormalThreshold,
			"high":    preset.HighThreshold,
			"extreme": preset.ExtremeThreshold,
		},
		"periods": map[string]interface{}{
			"short":  preset.ShortPeriod,
			"medium": preset.MediumPeriod,
			"long":   preset.LongPeriod,
		},
	}
}

// ListAvailablePresets 列出所有可用的预设
func (t *VolatilityConfigTool) ListAvailablePresets() []string {
	return indicators.ListAllPresets()
}
