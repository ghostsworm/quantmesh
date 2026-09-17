package utils

import (
	"strings"
	"testing"
)

const (
	uniqueTestOrderCount  = 5000
	uniqueTestPrice       = 65000.5
	uniqueTestDecimals    = 1
	binanceMaxClientIDLen = 36
	okxMaxClientIDLen     = 32
)

func TestGenerateOrderID_UniqueBeyondSequenceCapacity(t *testing.T) {
	seen := make(map[string]struct{}, 2*uniqueTestOrderCount)
	for i := 0; i < uniqueTestOrderCount; i++ {
		id := AddBrokerPrefix("binance", GenerateOrderID(uniqueTestPrice, "BUY", uniqueTestDecimals))
		if len(id) > binanceMaxClientIDLen {
			t.Fatalf("binance id %q exceeds %d chars", id, binanceMaxClientIDLen)
		}
		if _, dup := seen[id]; dup {
			t.Fatalf("duplicate id %q at #%d", id, i)
		}
		seen[id] = struct{}{}

		okx := GenerateOrderIDOKX(uniqueTestPrice, "SELL", uniqueTestDecimals)
		if len(okx) > okxMaxClientIDLen || strings.Contains(okx, "_") {
			t.Fatalf("okx id %q violates clOrdId rules", okx)
		}
		if _, dup := seen[okx]; dup {
			t.Fatalf("duplicate okx id %q at #%d", okx, i)
		}
		seen[okx] = struct{}{}
	}
}

func TestOrderIDGenerator_LogicalSecondMonotonic(t *testing.T) {
	g := &OrderIDGenerator{}
	const wall = int64(1_700_000_000)
	for i := 1; i <= maxOrderIDSeqPerSecond; i++ {
		if sec, seq := g.nextLocked(wall); sec != wall || seq != i {
			t.Fatalf("#%d: got (%d,%d)", i, sec, seq)
		}
	}
	if sec, seq := g.nextLocked(wall); sec != wall+1 || seq != 1 {
		t.Fatalf("exhausted second must borrow next second, got (%d,%d)", sec, seq)
	}
	// 牆鐘追上借用的秒：繼續在該秒內遞增，不重置
	if sec, seq := g.nextLocked(wall + 1); sec != wall+1 || seq != 2 {
		t.Fatalf("wall clock catching up must not reset sequence, got (%d,%d)", sec, seq)
	}
	// 牆鐘回撥：沿用邏輯秒
	if sec, seq := g.nextLocked(wall - 5); sec != wall+1 || seq != 3 {
		t.Fatalf("wall clock going back must keep logical second, got (%d,%d)", sec, seq)
	}
}
