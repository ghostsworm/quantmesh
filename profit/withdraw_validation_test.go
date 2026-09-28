package profit

import (
	"math"
	"testing"

	"quantmesh/storage"
)

func TestValidateWithdrawRuleRejectsUnsafeOrUnsupportedSettings(t *testing.T) {
	valid := storage.ProfitWithdrawRule{
		ID: "rule-1", ExchangeID: "binance", Enabled: true,
		TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate,
		Destination: "account", MinWithdrawAmount: 1,
	}
	tests := []struct {
		name   string
		mutate func(*storage.ProfitWithdrawRule)
	}{
		{name: "ratio greater than one", mutate: func(r *storage.ProfitWithdrawRule) { r.WithdrawRatio = 1.01 }},
		{name: "zero ratio while enabled", mutate: func(r *storage.ProfitWithdrawRule) { r.WithdrawRatio = 0 }},
		{name: "NaN trigger", mutate: func(r *storage.ProfitWithdrawRule) { r.TriggerAmount = math.NaN() }},
		{name: "unsupported frequency", mutate: func(r *storage.ProfitWithdrawRule) { r.Frequency = "monthly" }},
		{name: "unsupported wallet destination", mutate: func(r *storage.ProfitWithdrawRule) { r.Destination = "wallet" }},
		{name: "missing exchange", mutate: func(r *storage.ProfitWithdrawRule) { r.ExchangeID = "" }},
		{name: "negative max", mutate: func(r *storage.ProfitWithdrawRule) { value := -1.0; r.MaxWithdrawAmount = &value }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := valid
			tt.mutate(&rule)
			if err := ValidateWithdrawRule(&rule); err == nil {
				t.Fatal("unsafe rule was accepted")
			}
		})
	}
}

func TestValidateWithdrawRuleAllowsDisabledDraftWithoutTransferDestination(t *testing.T) {
	rule := &storage.ProfitWithdrawRule{Enabled: false, WithdrawRatio: 0, Destination: "wallet"}
	if err := ValidateWithdrawRule(rule); err != nil {
		t.Fatalf("disabled draft should remain editable: %v", err)
	}
}
