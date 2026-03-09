package web

import (
	"testing"

	"quantmesh/config"
)

func TestBuildGridRiskControlFromRequest(t *testing.T) {
	tests := []struct {
		name string
		req  CreateBotRequest
		want config.GridRiskControl
	}{
		{
			name: "empty_request",
			req:  CreateBotRequest{},
			want: config.GridRiskControl{},
		},
		{
			name: "with_risk_control",
			req: CreateBotRequest{
				GridRiskControlEnabled:         true,
				GridRiskControlStopLossRatio:   0.1,
				GridRiskControlTakeProfitRatio: 0.08,
				GridRiskControlTrailingRatio:   0.02,
				GridRiskControlTrendFilter:     true,
				GridRiskControlMaxGridLayers:   20,
				GridRiskControlMaxOpenOrdersCap: 5,
			},
			want: config.GridRiskControl{
				Enabled:                 true,
				StopLossRatio:           0.1,
				TakeProfitTriggerRatio:  0.08,
				TrailingTakeProfitRatio: 0.02,
				TrendFilterEnabled:      true,
				MaxGridLayers:           20,
				MaxOpenOrdersAtCap:      5,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildGridRiskControlFromRequest(tt.req)
			if got.Enabled != tt.want.Enabled ||
				got.StopLossRatio != tt.want.StopLossRatio ||
				got.TakeProfitTriggerRatio != tt.want.TakeProfitTriggerRatio ||
				got.TrailingTakeProfitRatio != tt.want.TrailingTakeProfitRatio ||
				got.TrendFilterEnabled != tt.want.TrendFilterEnabled ||
				got.MaxGridLayers != tt.want.MaxGridLayers ||
				got.MaxOpenOrdersAtCap != tt.want.MaxOpenOrdersAtCap {
				t.Errorf("buildGridRiskControlFromRequest() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
