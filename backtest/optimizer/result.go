package optimizer

import (
	"encoding/json"
	"math"
	"sort"
	"time"

	"quantmesh/backtest"
)

// ParamResult 单组参数的回测结果与得分
type ParamResult struct {
	Params  backtest.GridBacktestParams `json:"params"`
	Score   float64                    `json:"score"`
	Metrics backtest.Metrics           `json:"metrics"`
}

// OptimResult 优化结果
type OptimResult struct {
	BestParams  backtest.GridBacktestParams `json:"best_params"`
	BestScore   float64                    `json:"best_score"`
	BestMetrics backtest.Metrics          `json:"best_metrics"`
	AllResults  []ParamResult              `json:"all_results"`
	HeatmapData *HeatmapData               `json:"heatmap_data,omitempty"`
	Elapsed     time.Duration              `json:"elapsed"`
	Iterations  int                       `json:"iterations"`
	Method      string                    `json:"method"`
}

// HeatmapData 热力图数据，供前端 ECharts 使用
type HeatmapData struct {
	XAxis []interface{} `json:"x_axis"` // 如 GridCount 列表
	YAxis []interface{} `json:"y_axis"`  // 如 "P_low–P_high" 或区间描述
	Data  [][]float64   `json:"data"`   // [y_idx][x_idx] = score
}

// BuildHeatmapFromResults 从 AllResults 构建热力图数据
// xKey: "grid_count", yKey: "price_range" 等
func BuildHeatmapFromResults(results []ParamResult, xKey, yKey string) *HeatmapData {
	if len(results) == 0 {
		return nil
	}
	// 收集唯一的 x、y 并排序
	xSet := make(map[interface{}]bool)
	ySet := make(map[interface{}]bool)
	for _, r := range results {
		var xv, yv interface{}
		switch xKey {
		case "grid_count":
			xv = r.Params.GridCount
		case "order_quantity":
			xv = r.Params.OrderQuantity
		default:
			xv = r.Params.GridCount
		}
		switch yKey {
		case "price_range":
			yv = r.Params.PriceHigh - r.Params.PriceLow
		case "price_low":
			yv = r.Params.PriceLow
		case "price_high":
			yv = r.Params.PriceHigh
		default:
			yv = r.Params.PriceHigh - r.Params.PriceLow
		}
		xSet[xv] = true
		ySet[yv] = true
	}
	xList := sortedKeysNum(xSet)
	yList := sortedKeysNum(ySet)
	xIdx := make(map[interface{}]int)
	yIdx := make(map[interface{}]int)
	for i, v := range xList {
		xIdx[v] = i
	}
	for i, v := range yList {
		yIdx[v] = i
	}
	// data[y][x] = score，未覆盖的用 NaN 占位
	nan := math.NaN()
	data := make([][]float64, len(yList))
	for i := range data {
		data[i] = make([]float64, len(xList))
		for j := range data[i] {
			data[i][j] = nan
		}
	}
	for _, r := range results {
		var xv, yv interface{}
		switch xKey {
		case "grid_count":
			xv = r.Params.GridCount
		case "order_quantity":
			xv = r.Params.OrderQuantity
		default:
			xv = r.Params.GridCount
		}
		switch yKey {
		case "price_range":
			yv = r.Params.PriceHigh - r.Params.PriceLow
		case "price_low":
			yv = r.Params.PriceLow
		case "price_high":
			yv = r.Params.PriceHigh
		default:
			yv = r.Params.PriceHigh - r.Params.PriceLow
		}
		xi, okX := xIdx[xv]
		yi, okY := yIdx[yv]
		if okX && okY {
			data[yi][xi] = r.Score
		}
	}
	xAxis := make([]interface{}, len(xList))
	yAxis := make([]interface{}, len(yList))
	for i, v := range xList {
		xAxis[i] = v
	}
	for i, v := range yList {
		yAxis[i] = v
	}
	return &HeatmapData{XAxis: xAxis, YAxis: yAxis, Data: data}
}

func sortedKeysNum(m map[interface{}]bool) []interface{} {
	var keys []interface{}
	for k := range m {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		af, okA := toFloat(keys[i])
		bf, okB := toFloat(keys[j])
		if okA && okB {
			return af < bf
		}
		sa, _ := json.Marshal(keys[i])
		sb, _ := json.Marshal(keys[j])
		return string(sa) < string(sb)
	})
	return keys
}

func toFloat(v interface{}) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	default:
		return 0, false
	}
}
