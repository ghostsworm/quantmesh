package optimizer

import (
	"context"
	"fmt"
	"math"
	"testing"
)

func TestUniversalBoundsRejectBeforeEnumeration(t *testing.T) {
	cases := []UniversalSearchSpace{
		{Ranges: map[string]ParamRange{"x": {Min: 0, Max: 1, Step: 0}}},
		{Ranges: map[string]ParamRange{"x": {Min: 2, Max: 1, Step: 1}}},
		{Ranges: map[string]ParamRange{"x": {Min: 0, Max: math.Inf(1), Step: 1}}},
		{Ranges: map[string]ParamRange{"x": {Min: 1e20, Max: 2e20, Step: 1}}},
		{Ranges: map[string]ParamRange{"x": {Min: 0, Max: 1, Step: 1e-12}}},
		{Ranges: map[string]ParamRange{"x": {Min: 0, Max: 400, Step: 1}, "y": {Min: 0, Max: 400, Step: 1}}},
	}
	u := &UniversalOptimizer{}
	for i, space := range cases {
		if _, err := CountUniversalParamCombos(space); err == nil {
			t.Fatalf("case %d accepted", i)
		}
		if u.EnumerateParamCombos(space) != nil {
			t.Fatalf("case %d enumerated invalid space", i)
		}
		if _, err := u.Run(context.Background(), "id", "BTCUSDT", "1h", nil, space, 1000, nil); err == nil {
			t.Fatalf("case %d run accepted", i)
		}
	}
}

func TestUniversalBoundsDimensionsAndParameterCells(t *testing.T) {
	space := UniversalSearchSpace{Ranges: map[string]ParamRange{}}
	for i := 0; i <= maxUniversalDimensions; i++ {
		space.Ranges[fmt.Sprint(i)] = ParamRange{Min: 1, Max: 1, Step: 1}
	}
	if _, err := CountUniversalParamCombos(space); err == nil {
		t.Fatal("unbounded recursion dimensions accepted")
	}
	delete(space.Ranges, fmt.Sprint(maxUniversalDimensions))
	space.Ranges["0"] = ParamRange{Min: 1, Max: 40000, Step: 1}
	if _, err := CountUniversalParamCombos(space); err == nil {
		t.Fatal("too many map cells accepted")
	}
}

func TestUniversalCountsMatchEnumerationAndPreserveTinyValues(t *testing.T) {
	u := &UniversalOptimizer{}
	spaces := []UniversalSearchSpace{
		{},
		{Ranges: map[string]ParamRange{"tiny": {Min: 0, Max: 2e-12, Step: 1e-12}}},
		{Ranges: map[string]ParamRange{"negative": {Min: -2, Max: 0, Step: 0.5}}},
	}
	for _, strategy := range []string{"grid", "momentum", "mean_reversion", "trend_following", "dca", "martingale"} {
		spaces = append(spaces, GetDefaultSearchSpace(strategy))
	}
	for _, space := range spaces {
		count, err := CountUniversalParamCombos(space)
		if err != nil {
			t.Fatal(err)
		}
		if got := len(u.EnumerateParamCombos(space)); got != count {
			t.Fatalf("count=%d actual=%d", count, got)
		}
	}
	values := stepsRange(0, 2e-12, 1e-12)
	if len(values) != 3 || values[1] != 1e-12 || values[2] != 2e-12 {
		t.Fatalf("precision silently rounded away: %v", values)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := u.Run(ctx, "id", "BTCUSDT", "1h", nil, UniversalSearchSpace{}, 1000, nil); err != context.Canceled {
		t.Fatalf("canceled run: %v", err)
	}
}
