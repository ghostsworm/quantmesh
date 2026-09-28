package accounting

import (
	"strings"
	"testing"
)

func TestAccountDecimalsRemainExact(t *testing.T) {
	for _, value := range []string{"NaN", "Inf", "1e3", "", " 1", "1/3", "0.1234567890123456789", strings.Repeat("9", 81), strings.Repeat("9", 100)} {
		if _, err := Decimal(value); err == nil {
			t.Errorf("invalid decimal accepted %q", value)
		}
	}
	for _, input := range []string{strings.Repeat("9", 80), "-" + strings.Repeat("9", 80) + ".123456789012345678"} {
		canonical, err := CanonicalDecimal(input)
		if err != nil {
			t.Fatal(err)
		}
		original, err := Decimal(input)
		if err != nil {
			t.Fatal(err)
		}
		roundTrip, err := Decimal(canonical)
		if err != nil || original.Cmp(roundTrip) != 0 {
			t.Fatalf("canonical value cannot round trip: %v", err)
		}
	}
	for input, want := range map[string]string{"-0": "0.000000000000000000", "0.000000000000000001": "0.000000000000000001", "+001.2": "1.200000000000000000"} {
		got, err := CanonicalDecimal(input)
		if err != nil || got != want {
			t.Errorf("canonical(%q)=%q,%v", input, got, err)
		}
	}
}
