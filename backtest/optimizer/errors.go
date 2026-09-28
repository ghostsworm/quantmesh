package optimizer

import "errors"

var (
	errSearchSpaceTooLarge        = errors.New("optimizer: exhaustive search exceeds a resource limit (100000 candidates, or universal dimension/parameter budget) or cannot advance safely")
	errInvalidRange               = errors.New("optimizer: invalid search space range")
	errStopped                    = errors.New("optimizer: stopped by context")
	errInvalidValidationRatio     = errors.New("optimizer: validation_ratio must be in (0, 0.5)")
	errInvalidOptimizerConfig     = errors.New("optimizer: numeric configuration values must be finite")
	errNoValidOptimizationResults = errors.New("optimizer: no candidates produced finite training and validation scores")
	errValidationNotEnoughBars    = errors.New("optimizer: not enough candles for train/validation split")
)
