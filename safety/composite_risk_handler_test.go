package safety

import (
	"context"
	"testing"

	"quantmesh/config"
)

type fixedScoreFactor struct {
	score float64
}

func (f fixedScoreFactor) Name() string    { return "fixed" }
func (f fixedScoreFactor) Weight() float64 { return 1 }
func (f fixedScoreFactor) Evaluate(context.Context) FactorResult {
	return FactorResult{Score: f.score, Confidence: 1, Reason: "fixed"}
}

func TestCompositeRiskResultHandlerReceivesLevel(t *testing.T) {
	cfg := &config.Config{}
	cfg.CompositeRisk.Enabled = true
	controller := NewCompositeRiskController(cfg)
	controller.RegisterFactor(fixedScoreFactor{score: 90})

	var got []RiskLevel
	controller.SetResultHandler(func(r CompositeRiskResult) {
		got = append(got, r.Level)
	})

	res := controller.Evaluate(context.Background())
	if res.Level != RiskStopTrading {
		t.Fatalf("score=90 应为 stop_trading, got %s", res.Level)
	}
	if len(got) != 1 || got[0] != RiskStopTrading {
		t.Fatalf("回调应收到一次 stop_trading, got %v", got)
	}
}
