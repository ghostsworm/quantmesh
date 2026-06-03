package profit

import (
	"context"
	"testing"
	"time"

	"quantmesh/storage"
)

func TestWithdrawExecutorShouldExecute(t *testing.T) {
	executor := NewWithdrawExecutor(context.Background(), nil, nil)
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	lastWeek := now.AddDate(0, 0, -8)

	tests := []struct {
		name      string
		frequency string
		last      *time.Time
		want      bool
	}{
		{name: "immediate always executes", frequency: "immediate", last: &now, want: true},
		{name: "daily first run", frequency: "daily", last: nil, want: true},
		{name: "daily same day skipped", frequency: "daily", last: &now, want: false},
		{name: "daily previous day executes", frequency: "daily", last: &yesterday, want: true},
		{name: "weekly first run", frequency: "weekly", last: nil, want: true},
		{name: "weekly same week skipped", frequency: "weekly", last: &now, want: false},
		{name: "weekly previous week executes", frequency: "weekly", last: &lastWeek, want: true},
		{name: "unknown frequency skipped", frequency: "monthly", last: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &storage.ProfitWithdrawRule{LastTriggeredAt: tt.last}
			if got := executor.shouldExecute(rule, tt.frequency); got != tt.want {
				t.Fatalf("shouldExecute(%q) = %v, want %v", tt.frequency, got, tt.want)
			}
		})
	}
}
