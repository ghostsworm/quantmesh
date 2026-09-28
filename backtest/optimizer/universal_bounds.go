package optimizer

// Bound dimensions too: a million single-value axes still produce only one
// combination, but would otherwise exhaust stack and map memory.
const maxUniversalDimensions = 32
const maxUniversalParameterCells = 1000000

// CountUniversalParamCombos validates and counts without allocating candidates.
// Empty ranges intentionally mean one run with strategy defaults.
func CountUniversalParamCombos(space UniversalSearchSpace) (int, error) {
	if len(space.Ranges) > maxUniversalDimensions {
		return 0, errSearchSpaceTooLarge
	}
	total := 1
	for key, r := range space.Ranges {
		if key == "" || !finiteRange(r.Min, r.Max, r.Step) {
			return 0, errInvalidRange
		}
		count := floatStepCount(r.Min, r.Max, r.Step)
		if count == 0 || total > MaxGridSearchCandidates/count {
			return 0, errSearchSpaceTooLarge
		}
		total *= count
	}
	if len(space.Ranges) > 0 && total > maxUniversalParameterCells/len(space.Ranges) {
		return 0, errSearchSpaceTooLarge
	}
	return total, nil
}
