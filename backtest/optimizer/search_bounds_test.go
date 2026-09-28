package optimizer

import (
	"context"
	"math"
	"testing"
)

func TestSearchRangeRejectsNonfiniteAndNonadvancingValues(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		for _, field := range []string{"min", "max", "step"} {
			s := tinySearchSpace()
			switch field {
			case "min":
				s.PriceLowRange.Min = value
			case "max":
				s.PriceLowRange.Max = value
			case "step":
				s.PriceLowRange.Step = value
			}
			if ValidateSearchSpace(s) == nil {
				t.Fatalf("accepted %s=%v", field, value)
			}
		}
	}
	s := tinySearchSpace()
	s.PriceLowRange = Range{Min: 1e20, Max: 2e20, Step: 1}
	if ValidateSearchSpace(s) == nil {
		t.Fatal("accepted nonadvancing float step")
	}
	if steps(1e20, 2e20, 1) != nil {
		t.Fatal("unsafe helper must reject")
	}
	s = tinySearchSpace()
	s.OrderQtyRange.Step = 0
	if ValidateSearchSpace(s) == nil {
		t.Fatal("accepted zero quantity step")
	}
}

func TestExhaustiveSearchBoundBeforeAllocation(t *testing.T) {
	s := tinySearchSpace()
	s.PriceLowRange = Range{Min: 1, Max: 1e9, Step: 0.01}
	if ValidateSearchSpace(s) != nil {
		t.Fatal("sampling methods may use a wide range")
	}
	if ValidateGridSearchSize(s) != errSearchSpaceTooLarge {
		t.Fatal("unbounded axis accepted")
	}
	g := &GridSearchOptimizer{}
	if g.enumerateParams(s, 1000, 0, 0) != nil {
		t.Fatal("oversized space allocated")
	}
	if _, err := g.Run(context.Background(), "BTCUSDT", nil, s, DefaultOptimConfig(), 1000); err != errSearchSpaceTooLarge {
		t.Fatalf("Run must reject before candle evaluation: %v", err)
	}
	s = tinySearchSpace()
	s.GridCountRange = IntRange{Min: 1, Max: 20000, Step: 1}
	if ValidateGridSearchSize(s) != errSearchSpaceTooLarge {
		t.Fatal("Cartesian product must be checked, not just each axis")
	}
	if ValidateGridSearchSize(tinySearchSpace()) != nil {
		t.Fatal("small valid space rejected")
	}
}

func TestBoundedIntegerStepsDoNotOverflow(t *testing.T) {
	max := int(^uint(0) >> 1)
	got := intSteps(max-1, max, 1)
	if len(got) != 2 || got[0] != max-1 || got[1] != max {
		t.Fatalf("endpoint overflow: %v", got)
	}
	if intSteps(0, max, 1) != nil {
		t.Fatal("unbounded integer axis accepted")
	}
	if got := steps(1, 2, 0.25); len(got) != 5 || got[4] != 2 {
		t.Fatalf("valid endpoint lost: %v", got)
	}
}
