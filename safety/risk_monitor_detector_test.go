package safety

import (
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

// 合成 K 線：以固定「現在」為基準，1m 周期，毫秒時間戳
var detectorNow = time.Date(2026, 9, 17, 13, 11, 38, 0, time.UTC)

const testWindow = 20

func minuteOpen(minutesAgo int) int64 {
	base := detectorNow.Truncate(time.Minute)
	return base.Add(-time.Duration(minutesAgo) * time.Minute).UnixMilli()
}

// baseHistory 生成 testWindow+1 根完結 K 線（價格在 base 附近 ±0.01% 擺動，量 1000），
// 最後一根開盤於 2 分鐘前，為後續 K 線留出時間位。
func baseHistory(symbol string, base float64) []*exchange.Candle {
	n := testWindow + 1
	out := make([]*exchange.Candle, 0, n)
	for i := 0; i < n; i++ {
		px := base * (1 + 0.0001*float64(i%2*2-1))
		out = append(out, &exchange.Candle{Symbol: symbol, Close: px, Volume: 1000, Timestamp: minuteOpen(n - i + 1), IsClosed: true})
	}
	return out
}

func newDetectorMonitor(t *testing.T, symbols []string, tweak func(*config.Config)) *RiskMonitor {
	t.Helper()
	cfg := &config.Config{}
	cfg.RiskControl.Enabled = true
	cfg.RiskControl.MonitorSymbols = symbols
	cfg.RiskControl.Interval = "1m"
	cfg.RiskControl.AverageWindow = testWindow
	cfg.RiskControl.VolumeMultiplier = 3.0
	cfg.RiskControl.RecoveryThreshold = 3 // 全局默認值，單交易對 runtime 下舊代碼永不恢復
	if tweak != nil {
		tweak(cfg)
	}
	rm := NewRiskMonitor(cfg, &MockRiskExchange{})
	rm.now = func() time.Time { return detectorNow }
	for _, s := range symbols {
		rm.symbolDataMap[s].candles = baseHistory(s, 76480)
	}
	return rm
}

// feed 推送 K 線；pct 為相對 76480 的價格變化百分比
func feed(rm *RiskMonitor, symbol string, minutesAgo int, pct, volume float64, closed bool) {
	rm.onCandleUpdate(&exchange.Candle{
		Symbol:    symbol,
		Close:     76480 * (1 + pct/100),
		Volume:    volume,
		Timestamp: minuteOpen(minutesAgo),
		IsClosed:  closed,
	})
}

func TestRiskDetector_Trigger(t *testing.T) {
	tests := []struct {
		name    string
		symbols []string
		tweak   func(*config.Config)
		act     func(rm *RiskMonitor)
		want    bool
	}{
		{
			// 測試網試跑現場：−0.04% + 量×4.1 → 舊代碼觸發並停盤
			name:    "單交易對微跌+放量不觸發",
			symbols: []string{"BTCUSDT"},
			act:     func(rm *RiskMonitor) { feed(rm, "BTCUSDT", 0, -0.04, 4100, false) },
			want:    false,
		},
		{
			name:    "單交易對跌0.7%未達單交易對加嚴門檻(1.0%)不觸發",
			symbols: []string{"BTCUSDT"},
			act:     func(rm *RiskMonitor) { feed(rm, "BTCUSDT", 0, -0.7, 5000, false) },
			want:    false,
		},
		{
			name:    "單交易對3根K線跌2%且放量觸發",
			symbols: []string{"BTCUSDT"},
			act: func(rm *RiskMonitor) {
				feed(rm, "BTCUSDT", 1, -0.6, 1000, true)
				feed(rm, "BTCUSDT", 0, -1.2, 1000, true)
				feed(rm, "BTCUSDT", -1, -2.0, 5000, false)
			},
			want: true,
		},
		{
			name:    "多交易對同時3根跌2%觸發",
			symbols: []string{"BTCUSDT", "ETHUSDT"},
			act: func(rm *RiskMonitor) {
				for _, s := range []string{"BTCUSDT", "ETHUSDT"} {
					feed(rm, s, 1, -0.6, 1000, true)
					feed(rm, s, 0, -1.2, 1000, true)
					feed(rm, s, -1, -2.0, 5000, false)
				}
			},
			want: true,
		},
		{
			name:    "三交易對僅一個暴跌不觸發（默認需全部）",
			symbols: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
			act:     func(rm *RiskMonitor) { feed(rm, "SOLUSDT", 0, -3, 6000, false) },
			want:    false,
		},
		{
			name:    "min_panic_symbols=1 在多交易對下仍至少需要 2 個",
			symbols: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
			tweak:   func(c *config.Config) { c.RiskControl.MinPanicSymbols = 1 },
			act:     func(rm *RiskMonitor) { feed(rm, "SOLUSDT", 0, -3, 6000, false) },
			want:    false,
		},
		{
			name:    "min_panic_symbols=2 時三個中兩個暴跌觸發",
			symbols: []string{"BTCUSDT", "ETHUSDT", "SOLUSDT"},
			tweak:   func(c *config.Config) { c.RiskControl.MinPanicSymbols = 2 },
			act: func(rm *RiskMonitor) {
				feed(rm, "ETHUSDT", 0, -3, 6000, false)
				feed(rm, "SOLUSDT", 0, -3, 6000, false)
			},
			want: true,
		},
		{
			name:    "過期K線不觸發",
			symbols: []string{"BTCUSDT"},
			act: func(rm *RiskMonitor) {
				rm.now = func() time.Time { return detectorNow.Add(10 * time.Minute) }
				feed(rm, "BTCUSDT", 0, -3, 6000, false)
			},
			want: false,
		},
		{
			name:    "min_price_drop_pct<0 且 volatility_multiplier<0 回到舊行為：微跌+放量觸發",
			symbols: []string{"BTCUSDT"},
			tweak: func(c *config.Config) {
				c.RiskControl.MinPriceDropPct = -1
				c.RiskControl.VolatilityMultiplier = -1
			},
			act:  func(rm *RiskMonitor) { feed(rm, "BTCUSDT", 0, -0.04, 4100, false) },
			want: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := newDetectorMonitor(t, tt.symbols, tt.tweak)
			tt.act(rm)
			if got := rm.IsTriggered(); got != tt.want {
				t.Fatalf("IsTriggered=%v want %v (lastMsg=%q)", got, tt.want, rm.GetLastMsg())
			}
		})
	}
}

func TestRiskDetector_RecoveryAfterNormalization(t *testing.T) {
	tests := []struct {
		name        string
		recoveryPct float64 // 收盤相對 76480 的百分比
		volume      float64
		want        bool
	}{
		// 暴跌後均線 ≈ −0.19%。試跑現場：觸發後價格在均線下方 0.05~0.08% 橫盤 20+ 分鐘仍停盤
		{"均線下方約0.06%量正常：恢復", -0.25, 500, true},
		{"回到均線上方但放量：恢復（上方放量不是恐慌）", 0.3, 9000, true},
		{"仍低於均線約0.3%：不恢復", -0.5, 500, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rm := newDetectorMonitor(t, []string{"BTCUSDT"}, nil) // recovery_threshold=3 > 1 個監控交易對
			feed(rm, "BTCUSDT", 1, -0.6, 1000, true)
			feed(rm, "BTCUSDT", 0, -1.2, 1000, true)
			feed(rm, "BTCUSDT", -1, -2.0, 5000, false)
			if !rm.IsTriggered() {
				t.Fatal("前置：應先觸發")
			}
			// 暴跌 K 線收盤，下一根收在 recoveryPct
			rm.now = func() time.Time { return detectorNow.Add(2 * time.Minute) }
			feed(rm, "BTCUSDT", -1, -2.0, 1000, true)
			if !rm.IsTriggered() {
				t.Fatal("暴跌 K 線收盤時不應恢復")
			}
			rm.now = func() time.Time { return detectorNow.Add(3 * time.Minute) }
			feed(rm, "BTCUSDT", -2, tt.recoveryPct, tt.volume, true)
			if recovered := !rm.IsTriggered(); recovered != tt.want {
				t.Fatalf("recovered=%v want %v (lastMsg=%q)", recovered, tt.want, rm.GetLastMsg())
			}
		})
	}
}

func TestUpsertCandle_FormingClosedMixup(t *testing.T) {
	c := func(ts int64, close float64, closed bool) *exchange.Candle {
		return &exchange.Candle{Timestamp: ts, Close: close, IsClosed: closed}
	}
	var candles []*exchange.Candle
	candles = upsertCandle(candles, c(1000, 1, true), 3)
	candles = upsertCandle(candles, c(2000, 2, false), 3)
	candles = upsertCandle(candles, c(2000, 2.1, false), 3) // 同一根更新
	candles = upsertCandle(candles, c(2000, 2.2, true), 3)  // 同一根收盤：替換而非追加
	if len(candles) != 2 || !candles[1].IsClosed || candles[1].Close != 2.2 {
		t.Fatalf("同時間戳應替換: %+v", candles)
	}
	candles = upsertCandle(candles, c(3000, 3, false), 3)
	candles = upsertCandle(candles, c(4000, 4, false), 3) // 3000 的收盤丟失：丟棄殘留未完結
	if len(candles) != 3 || candles[1].Timestamp != 2000 || candles[2].Timestamp != 4000 {
		t.Fatalf("中間殘留未完結 K 線應被移除: %+v", candles)
	}
	candles = upsertCandle(candles, c(500, 0.5, true), 3) // 亂序舊數據忽略
	if len(candles) != 3 {
		t.Fatalf("亂序舊 K 線應忽略: %+v", candles)
	}
	for ts := int64(5000); ts <= 9000; ts += 1000 {
		candles = upsertCandle(candles, c(ts, float64(ts), true), 3)
	}
	if len(candles) != 4 { // window+1 根完結
		t.Fatalf("裁剪後應保留 4 根，實際 %d", len(candles))
	}
}

func TestMarkFormingHistorical(t *testing.T) {
	candles := []*exchange.Candle{
		{Timestamp: minuteOpen(2), IsClosed: true},
		{Timestamp: minuteOpen(0), IsClosed: true}, // 當前分鐘開盤，尚未收盤
	}
	markFormingHistorical(candles, "1m", detectorNow)
	if !candles[0].IsClosed || candles[1].IsClosed {
		t.Fatalf("僅當前未收盤 K 線應被標記為未完結: %v %v", candles[0].IsClosed, candles[1].IsClosed)
	}
}
