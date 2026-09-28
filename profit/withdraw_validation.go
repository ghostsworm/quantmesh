package profit

import (
	"fmt"
	"math"
	"strings"

	"quantmesh/storage"
)

// ValidateWithdrawRule rejects automatic-withdrawal settings that could move
// more than the configured share or claim an unsupported destination.
func ValidateWithdrawRule(rule *storage.ProfitWithdrawRule) error {
	if rule == nil {
		return fmt.Errorf("提取规则为空")
	}
	if rule.Enabled && strings.TrimSpace(rule.ExchangeID) == "" {
		return fmt.Errorf("启用提取规则必须指定交易所")
	}
	if rule.Enabled && strings.TrimSpace(rule.StrategyID) == "" {
		return fmt.Errorf("启用提取规则必须指定可核算的交易对")
	}
	if rule.Enabled && strings.TrimSpace(rule.AccountScope) == "" {
		return fmt.Errorf("启用提取规则必须绑定不可变账户作用域")
	}
	if !finiteNonNegative(rule.TriggerAmount) {
		return fmt.Errorf("触发金额必须是有限的非负数")
	}
	if !finiteNonNegative(rule.WithdrawRatio) || rule.WithdrawRatio > 1 || (rule.Enabled && rule.WithdrawRatio == 0) {
		return fmt.Errorf("提取比例必须大于 0 且不超过 1（禁用规则可为 0）")
	}
	if !finiteNonNegative(rule.MinWithdrawAmount) {
		return fmt.Errorf("最小提取金额必须是有限的非负数")
	}
	if rule.MaxWithdrawAmount != nil && !finiteNonNegative(*rule.MaxWithdrawAmount) {
		return fmt.Errorf("最大提取金额必须是有限的非负数")
	}
	if !rule.Enabled {
		return nil
	}
	switch rule.Frequency {
	case frequencyImmediate, frequencyDaily, frequencyWeekly:
	default:
		return fmt.Errorf("不支持的提取频率 %q", rule.Frequency)
	}
	if rule.Destination != "" && rule.Destination != "account" {
		return fmt.Errorf("自动提取目前只支持账户内部划转，不支持目的地 %q", rule.Destination)
	}
	return nil
}

func finiteNonNegative(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0) && value >= 0
}
