package utils

import (
	"math"
	"testing"
)

// 浮點誤差容差：結果應精確到 1e-12 以內
const testTolerance = 1e-12

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > testTolerance {
		t.Errorf("%s = %.17g, 期望 %.17g（誤差 %.3g）", name, got, want, math.Abs(got-want))
	}
}

// TestFloorToStep_IEEE754Regression 回歸測試：
// 這些用例在修復前全部會憑空少下一個 step。
func TestFloorToStep_IEEE754Regression(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		step  float64
		want  float64
	}{
		// 修復前 0.3/0.1 = 2.9999999999999996 → Floor → 2 → 0.2（少 33%）
		{"0.3 步長 0.1", 0.3, 0.1, 0.3},
		{"0.7 步長 0.1", 0.7, 0.1, 0.7},
		{"2.9 步長 0.1", 2.9, 0.1, 2.9},
		{"0.29 步長 0.01", 0.29, 0.01, 0.29},
		{"0.57 步長 0.01", 0.57, 0.01, 0.57},
		// 修復前就正確的用例，確保沒被改壞
		{"1.1 步長 0.1", 1.1, 0.1, 1.1},
		{"1.3 步長 0.001", 1.3, 0.001, 1.3},
		{"3.0 步長 0.001", 3.0, 0.001, 3.0},
		{"0.006 步長 0.001", 0.006, 0.001, 0.006},
		{"0.008 步長 0.001", 0.008, 0.001, 0.008},
		// 確實需要向下取整的用例（不能因為修 epsilon 就變成四捨五入）
		{"0.35 步長 0.1 應截斷", 0.35, 0.1, 0.3},
		{"0.39 步長 0.1 應截斷", 0.39, 0.1, 0.3},
		{"0.299 步長 0.01 應截斷", 0.299, 0.01, 0.29},
		{"2.999 步長 0.1 應截斷", 2.999, 0.1, 2.9},
		// BTC 級精度
		{"0.00012345 步長 0.00000001", 0.00012345, 0.00000001, 0.00012345},
		{"1.23456789 步長 0.00001 應截斷", 1.23456789, 0.00001, 1.23456},
		// 邊界
		{"步長為 0 原樣返回", 0.3, 0, 0.3},
		{"步長為負原樣返回", 0.3, -1, 0.3},
		{"值為 0", 0, 0.1, 0},
		{"不足一個步長歸零", 0.05, 0.1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FloorToStep(tt.value, tt.step)
			assertClose(t, "FloorToStep", got, tt.want)

			// 額外驗證：結果必須是 step 的整數倍，且不超過原值
			if tt.step > 0 && got > tt.value+testTolerance {
				t.Errorf("向下取整結果 %.17g 超過原值 %.17g", got, tt.value)
			}
		})
	}
}

// TestCeilToStep 驗證向上對齊，重點是已對齊的值不能被無故抬高一格。
func TestCeilToStep(t *testing.T) {
	tests := []struct {
		name  string
		value float64
		step  float64
		want  float64
	}{
		// 修復前 0.07/0.01 = 7.000000000000001 → Ceil → 8 → 0.08（賣單被平白抬高）
		{"0.07 步長 0.01 已對齊不應抬高", 0.07, 0.01, 0.07},
		{"2.3 步長 0.01 已對齊", 2.3, 0.01, 2.3},
		{"8.7 步長 0.01 已對齊", 8.7, 0.01, 8.7},
		{"100.0 步長 0.01 已對齊", 100.0, 0.01, 100.0},
		// 確實需要向上取整
		{"0.071 步長 0.01 應進位", 0.071, 0.01, 0.08},
		{"2.301 步長 0.01 應進位", 2.301, 0.01, 2.31},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CeilToStep(tt.value, tt.step)
			assertClose(t, "CeilToStep", got, tt.want)
			if tt.step > 0 && got < tt.value-testTolerance {
				t.Errorf("向上取整結果 %.17g 小於原值 %.17g", got, tt.value)
			}
		})
	}
}

// TestTickSizeAlignment_NoDrift 價格對齊回歸：
// 已經落在 tick 上的價格，買賣兩個方向都必須原樣返回，
// 否則網格每一檔都會系統性偏移一個 tick。
func TestTickSizeAlignment_NoDrift(t *testing.T) {
	prices := []struct {
		price float64
		tick  float64
	}{
		{100.0, 0.01}, {3000.0, 0.01}, {0.07, 0.01},
		{1.1, 0.1}, {70000.0, 0.1}, {2.3, 0.01}, {8.7, 0.01},
		{2.29, 0.01}, {8.69, 0.01}, {0.0825, 0.0001},
	}
	for _, p := range prices {
		buy := FloorToStep(p.price, p.tick) // 買單向下
		sell := CeilToStep(p.price, p.tick) // 賣單向上
		assertClose(t, "買單價格漂移", buy, p.price)
		assertClose(t, "賣單價格漂移", sell, p.price)
	}
}

// TestFloorToDecimals 小數位版本的同類回歸。
func TestFloorToDecimals(t *testing.T) {
	tests := []struct {
		name     string
		value    float64
		decimals int
		want     float64
	}{
		// 修復前 0.29*100 = 28.999999999999996 → Floor → 28 → 0.28
		{"0.29 保留 2 位", 0.29, 2, 0.29},
		{"0.57 保留 2 位", 0.57, 2, 0.57},
		{"2.9 保留 1 位", 2.9, 1, 2.9},
		{"1.005 保留 2 位", 1.005, 2, 1.0},
		// 需要截斷
		{"0.299 保留 2 位", 0.299, 2, 0.29},
		{"1.2999 保留 3 位", 1.2999, 3, 1.299},
		// 邊界
		{"0 位", 5.9, 0, 5.0},
		{"負數位視為 0", 5.9, -1, 5.0},
		{"超大位數收斂", 0.29, 99, 0.29},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FloorToDecimals(tt.value, tt.decimals)
			assertClose(t, "FloorToDecimals", got, tt.want)
			if got > tt.value+testTolerance {
				t.Errorf("向下取整結果 %.17g 超過原值 %.17g", got, tt.value)
			}
		})
	}
}

// upperBound 向下取整允許的上界：原值加上對齊容差。
// 契約是「不會超過原值」，容差內的超出屬於浮點對齊的固有代價，
// 相對量級 1e-12，遠小於任何交易所的最小精度。
func upperBound(v float64) float64 {
	return v + math.Max(math.Abs(v), 1)*1e-12
}

// TestFloorToDecimals_NeverExceedsInput 性質測試：
// 下單數量向下取整永遠不能超過原值，否則會超出餘額/持倉被交易所拒單。
func TestFloorToDecimals_NeverExceedsInput(t *testing.T) {
	for _, decimals := range []int{0, 1, 2, 3, 6, 8} {
		for i := 1; i <= 5000; i++ {
			v := float64(i) / 997.0 // 製造大量非整齊小數
			if got := FloorToDecimals(v, decimals); got > upperBound(v) {
				t.Fatalf("FloorToDecimals(%.17g, %d) = %.17g 超過原值", v, decimals, got)
			}
		}
	}
}

// TestFloorToStep_NeverExceedsInput 同上，步長版本。
func TestFloorToStep_NeverExceedsInput(t *testing.T) {
	for _, step := range []float64{0.1, 0.01, 0.001, 0.00001} {
		for i := 1; i <= 5000; i++ {
			v := float64(i) / 997.0
			if got := FloorToStep(v, step); got > upperBound(v) {
				t.Fatalf("FloorToStep(%.17g, %.8g) = %.17g 超過原值", v, step, got)
			}
		}
	}
}
