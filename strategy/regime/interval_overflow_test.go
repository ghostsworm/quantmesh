package regime

import (
	"errors"
	"testing"
	"time"
)

func TestParseKlineIntervalRejectsOverflow(t *testing.T) {
	for _, interval := range []string{"9007199254740992m", "9223372036854775807h", "106752d"} {
		if _, err := ParseKlineInterval(interval); !errors.Is(err, ErrInvalidConfig) {
			t.Fatalf("interval %q: expected invalid config, got %v", interval, err)
		}
	}
	for _, interval := range []string{"60m", "1h"} {
		got, err := ParseKlineInterval(interval)
		if err != nil || got != time.Hour {
			t.Fatalf("interval %q: %v %v", interval, got, err)
		}
	}
}
