package binance

import (
	"math"
	"testing"
)

func TestApplyTopLevelFuturesBalances(t *testing.T) {
	t.Parallel()
	a, w, m := applyTopLevelFuturesBalances(0, 0, 0, "9775.95", "10212.48", "9775.95")
	if math.Abs(a-9775.95) > 1e-6 {
		t.Fatalf("available: got %v want 9775.95", a)
	}
	if math.Abs(w-10212.48) > 1e-6 {
		t.Fatalf("wallet: got %v want 10212.48", w)
	}
	if math.Abs(m-9775.95) > 1e-6 {
		t.Fatalf("margin: got %v want 9775.95", m)
	}
}

func TestApplyTopLevelFuturesBalancesPreservesAssetSum(t *testing.T) {
	t.Parallel()
	// 單資產模式下資產行已有值，不應被覆蓋
	a, w, m := applyTopLevelFuturesBalances(100, 200, 300, "50", "60", "70")
	if a != 100 || w != 200 || m != 300 {
		t.Fatalf("expected sums preserved, got avail=%v wallet=%v margin=%v", a, w, m)
	}
}

func TestApplyTopLevelFuturesBalancesUsesMarginWhenTopAvailZero(t *testing.T) {
	t.Parallel()
	// 幣安多資產下帳戶級 availableBalance 可能為 0，但 totalMarginBalance 仍正確
	a, w, m := applyTopLevelFuturesBalances(0, 0, 0, "0", "10212.48", "9775.95")
	if math.Abs(a-9775.95) > 1e-6 {
		t.Fatalf("available: got %v want 9775.95", a)
	}
	if math.Abs(w-10212.48) > 1e-6 {
		t.Fatalf("wallet: got %v want 10212.48", w)
	}
	if math.Abs(m-9775.95) > 1e-6 {
		t.Fatalf("margin: got %v want 9775.95", m)
	}
}
