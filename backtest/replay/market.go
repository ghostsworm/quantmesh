package replay

import (
	"fmt"
	"sort"
	"strings"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

// IntrabarOptions K 線拆分為 K 線內路徑的選項
type IntrabarOptions struct {
	// Path 路徑類型；空為 auto
	Path IntrabarPath
	// StepsPerLeg 每段插值步數；<=0 用默認值 1（只有 O、X、Y、C 四個點）
	StepsPerLeg int
	// IntervalMs K 線周期；<=0 時按相鄰 K 線時間差推斷
	IntervalMs int64
}

// ParseIntrabarPath 解析路徑名（大小寫不敏感）
func ParseIntrabarPath(s string) (IntrabarPath, error) {
	switch strings.ToUpper(strings.TrimSpace(s)) {
	case "", "AUTO":
		return IntrabarPathAuto, nil
	case string(IntrabarPathOHLC):
		return IntrabarPathOHLC, nil
	case string(IntrabarPathOLHC):
		return IntrabarPathOLHC, nil
	default:
		return "", fmt.Errorf("unsupported intrabar path %q (want auto, OHLC or OLHC)", s)
	}
}

// AggTradesToTicks 將 aggTrades 轉為 Tick（按時間穩定排序）
func AggTradesToTicks(rows []backtest.AggTradeRow) []Tick {
	ticks := make([]Tick, 0, len(rows))
	for _, r := range rows {
		if r.Price <= 0 {
			continue
		}
		ticks = append(ticks, Tick{Timestamp: r.Timestamp, Price: r.Price, Quantity: r.Quantity})
	}
	sort.SliceStable(ticks, func(i, j int) bool { return ticks[i].Timestamp < ticks[j].Timestamp })
	return ticks
}

// CandlesToTicks 將 K 線拆成 K 線內路徑（無 tick 數據時的回退）。
// 每根 K 線生成 O → X → Y → C，auto 模式陽線（C≥O）X=L、Y=H，陰線 X=H、Y=L；
// 每段按 StepsPerLeg 線性插值，K 線成交量平均分配到各路徑點，時間戳在 K 線周期內均勻分佈。
// 注意：路徑是假設，真實 K 線內順序未知；aggTrade 數據可用時應優先使用。
func CandlesToTicks(candles []*exchange.Candle, opt IntrabarOptions) ([]Tick, error) {
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles to ticks: no candles")
	}
	path := opt.Path
	if path == "" {
		path = IntrabarPathAuto
	}
	if _, err := ParseIntrabarPath(string(path)); err != nil {
		return nil, err
	}
	steps := opt.StepsPerLeg
	if steps <= 0 {
		steps = DefaultIntrabarStepsPerLeg
	}
	sorted := make([]*exchange.Candle, 0, len(candles))
	for _, c := range candles {
		if c != nil {
			sorted = append(sorted, c)
		}
	}
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Timestamp < sorted[j].Timestamp })
	interval := opt.IntervalMs
	if interval <= 0 {
		interval = inferCandleIntervalMs(sorted)
	}

	pointsPerCandle := 1 + 3*steps
	ticks := make([]Tick, 0, len(sorted)*pointsPerCandle)
	for _, c := range sorted {
		if c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0 {
			return nil, fmt.Errorf("candles to ticks: candle ts=%d has non-positive OHLC (%v/%v/%v/%v)", c.Timestamp, c.Open, c.High, c.Low, c.Close)
		}
		x, y := c.High, c.Low
		switch path {
		case IntrabarPathOLHC:
			x, y = c.Low, c.High
		case IntrabarPathAuto:
			if c.Close >= c.Open {
				x, y = c.Low, c.High
			}
		}
		prices := make([]float64, 0, pointsPerCandle)
		prices = append(prices, c.Open)
		for _, leg := range [][2]float64{{c.Open, x}, {x, y}, {y, c.Close}} {
			for s := 1; s <= steps; s++ {
				prices = append(prices, leg[0]+(leg[1]-leg[0])*float64(s)/float64(steps))
			}
		}
		qty := c.Volume / float64(len(prices))
		dt := interval / int64(len(prices))
		for i, p := range prices {
			ticks = append(ticks, Tick{Timestamp: c.Timestamp + int64(i)*dt, Price: p, Quantity: qty})
		}
	}
	return ticks, nil
}

// inferCandleIntervalMs 取相鄰 K 線最小正時間差
func inferCandleIntervalMs(candles []*exchange.Candle) int64 {
	best := int64(0)
	for i := 1; i < len(candles); i++ {
		d := candles[i].Timestamp - candles[i-1].Timestamp
		if d > 0 && (best == 0 || d < best) {
			best = d
		}
	}
	if best <= 0 {
		return DefaultCandleIntervalMs
	}
	return best
}
