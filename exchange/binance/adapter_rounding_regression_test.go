package binance

import (
	"math"
	"testing"
)

// TestRoundToStepSize_NoPhantomLoss 回歸測試：
// 修復前 roundToStepSize 直接寫 math.Floor(qty/step)*step，
// 0.3/0.1 在雙精度下等於 2.9999999999999996，Floor 後變成 2，
// 下單量憑空少掉整整一個 step（0.3 → 0.2，少 33%）。
func TestRoundToStepSize_NoPhantomLoss(t *testing.T) {
	tests := []struct {
		name     string
		stepSize float64
		quantity float64
		want     float64
	}{
		{"0.3 步長 0.1 不應丟量", 0.1, 0.3, 0.3},
		{"0.7 步長 0.1 不應丟量", 0.1, 0.7, 0.7},
		{"2.9 步長 0.1 不應丟量", 0.1, 2.9, 2.9},
		{"0.29 步長 0.01 不應丟量", 0.01, 0.29, 0.29},
		{"0.57 步長 0.01 不應丟量", 0.01, 0.57, 0.57},
		// 確實需要向下取整的情形必須保持截斷語義
		{"0.35 步長 0.1 應截斷", 0.1, 0.35, 0.3},
		{"0.0019 步長 0.001 應截斷", 0.001, 0.0019, 0.001},
		{"步長為 0 原樣返回", 0, 0.3, 0.3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			adapter := &BinanceAdapter{stepSize: tt.stepSize}
			if got := adapter.roundToStepSize(tt.quantity); math.Abs(got-tt.want) > 1e-9 {
				t.Fatalf("roundToStepSize(%v) = %.17g, 期望 %.17g", tt.quantity, got, tt.want)
			}
		})
	}
}

// TestRoundToTickSize_NoDrift 回歸測試：
// 已經落在 tick 上的價格不能被挪動，否則網格每一檔都會系統性偏移一個 tick。
// 修復前 8.70/0.01 = 869.9999999999999，買單會被壓成 8.69；
// 0.07/0.01 = 7.000000000000001，賣單會被抬到 0.08。
func TestRoundToTickSize_NoDrift(t *testing.T) {
	aligned := []struct {
		tickSize float64
		price    float64
	}{
		{0.01, 8.7}, {0.01, 2.3}, {0.01, 0.07}, {0.01, 100.0},
		{0.1, 1.1}, {0.1, 70000.0}, {0.0001, 0.0825},
	}

	for _, c := range aligned {
		adapter := &BinanceAdapter{tickSize: c.tickSize}
		if got := adapter.roundToTickSize(c.price, SideBuy); math.Abs(got-c.price) > 1e-9 {
			t.Errorf("買單 tick=%v price=%v 被壓低到 %.17g", c.tickSize, c.price, got)
		}
		if got := adapter.roundToTickSize(c.price, SideSell); math.Abs(got-c.price) > 1e-9 {
			t.Errorf("賣單 tick=%v price=%v 被抬高到 %.17g", c.tickSize, c.price, got)
		}
	}
}

// TestRoundToTickSize_DirectionPreserved 未對齊的價格仍須按方向取整：
// 買單向下（對買家有利），賣單向上（對賣家有利）。
func TestRoundToTickSize_DirectionPreserved(t *testing.T) {
	adapter := &BinanceAdapter{tickSize: 0.1}

	if got := adapter.roundToTickSize(100.06, SideBuy); math.Abs(got-100.0) > 1e-9 {
		t.Fatalf("買單應向下取整到 100.0，實得 %.17g", got)
	}
	if got := adapter.roundToTickSize(100.01, SideSell); math.Abs(got-100.1) > 1e-9 {
		t.Fatalf("賣單應向上取整到 100.1，實得 %.17g", got)
	}
}
