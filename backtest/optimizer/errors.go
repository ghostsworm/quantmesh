package optimizer

import "errors"

var (
	errInvalidRange = errors.New("optimizer: invalid search space range")
	errStopped      = errors.New("optimizer: stopped by context")
)
