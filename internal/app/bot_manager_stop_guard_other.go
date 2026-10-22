//go:build !darwin && !linux && !windows

package app

import (
	"fmt"
	"os"
)

func tryStopGuardLock(*os.File) (bool, error) {
	return false, fmt.Errorf("Bot transition file locking unsupported on this platform")
}
