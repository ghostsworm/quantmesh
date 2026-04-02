package optimizer

import "errors"

var (
	errInvalidRange           = errors.New("optimizer: invalid search space range")
	errStopped                = errors.New("optimizer: stopped by context")
	errInvalidValidationRatio = errors.New("optimizer: validation_ratio must be in (0, 0.5)")
	errValidationNotEnoughBars = errors.New("optimizer: not enough candles for train/validation split")
)
