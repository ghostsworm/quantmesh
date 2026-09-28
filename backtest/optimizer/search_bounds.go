package optimizer

import "math"

// MaxGridSearchCandidates bounds the raw Cartesian product before allocation or
// filtering. It is a resource limit, not a claim about statistical adequacy.
const MaxGridSearchCandidates = 100000

func finiteRange(min, max, step float64) bool {
	return !math.IsNaN(min) && !math.IsNaN(max) && !math.IsNaN(step) &&
		!math.IsInf(min, 0) && !math.IsInf(max, 0) && !math.IsInf(step, 0) &&
		min <= max && step > 0 && (min == max || min+step > min && max+step > max)
}

func floatStepCount(min, max, step float64) int {
	if !finiteRange(min, max, step) {
		return 0
	}
	span := (max - min) / step
	if math.IsInf(span, 0) || span >= MaxGridSearchCandidates {
		return 0
	}
	return int(math.Floor(span)) + 1
}

func intStepCount(min, max, step int) int {
	if min < 0 || max < min || step <= 0 {
		return 0
	}
	// Subtraction is safe after establishing nonnegative, ordered endpoints.
	span := (max - min) / step
	if span >= MaxGridSearchCandidates {
		return 0
	}
	return span + 1
}

// ValidateGridSearchSize applies only to exhaustive search. Bayesian/genetic
// search does not materialize the Cartesian product and keeps its own budget.
func ValidateGridSearchSize(space OptimSearchSpace) error {
	counts := []int{
		floatStepCount(space.PriceLowRange.Min, space.PriceLowRange.Max, space.PriceLowRange.Step),
		floatStepCount(space.PriceHighRange.Min, space.PriceHighRange.Max, space.PriceHighRange.Step),
		intStepCount(space.GridCountRange.Min, space.GridCountRange.Max, space.GridCountRange.Step),
		floatStepCount(space.OrderQtyRange.Min, space.OrderQtyRange.Max, space.OrderQtyRange.Step),
	}
	total := 1
	for _, count := range counts {
		if count == 0 || total > MaxGridSearchCandidates/count {
			return errSearchSpaceTooLarge
		}
		total *= count
	}
	return nil
}
