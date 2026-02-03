package backtest

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// DepthSnapshotForBacktest 回测用深度快照
type DepthSnapshotForBacktest struct {
	Timestamp  int64   // 时间戳（毫秒）
	TotalDepth float64 // 总深度(USDT)
	BidDepth   float64 // 买盘深度(USDT)
	AskDepth   float64 // 卖盘深度(USDT)
}

// KlineFileMeta K线文件元信息
type KlineFileMeta struct {
	Symbol   string
	Interval string
	Date     string
	HasDepth bool
}

// ValidateKlineFileForBacktest 校验 K 线文件是否可用于回测
// 注：为避免循环导入，此函数移到 web 层实现

// LoadCandlesFromKlineFile 从 KlineCollector CSV 文件加载 K 线数据
// 支持 7 列（无深度）和 26 列（带深度）格式
func LoadCandlesFromKlineFile(dataDir, filename string) ([]*exchange.Candle, []*DepthSnapshotForBacktest, error) {
	filepath := filepath.Join(dataDir, filename)

	file, err := os.Open(filepath)
	if err != nil {
		return nil, nil, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// 读取表头，判断列数
	header, err := reader.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("读取表头失败: %w", err)
	}

	columnCount := len(header)
	hasDepth := columnCount == 26 // 26列表示带深度数据

	logger.Info("📊 加载 K线文件: %s，列数=%d，带深度=%v", filename, columnCount, hasDepth)

	var candles []*exchange.Candle
	var depthSnapshots []*DepthSnapshotForBacktest

	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("读取第 %d 行失败: %w", lineNum+1, err)
		}

		// 解析前 6 列 K线数据
		if len(record) < 6 {
			return nil, nil, fmt.Errorf("第 %d 行数据不足6列", lineNum+1)
		}

		candle, err := parseKlineRecord(record[:6])
		if err != nil {
			return nil, nil, fmt.Errorf("解析第 %d 行K线失败: %w", lineNum+1, err)
		}
		candles = append(candles, candle)

		// 如果有深度数据，解析深度信息
		if hasDepth && len(record) >= 26 {
			depth := parseDepthRecord(candle.Timestamp, record[6:])
			depthSnapshots = append(depthSnapshots, depth)
		}

		lineNum++
	}

	logger.Info("✅ 加载完成: %d 根K线", len(candles))
	if hasDepth {
		logger.Info("✅ 深度数据: %d 个快照", len(depthSnapshots))
	}

	return candles, depthSnapshots, nil
}

// LoadCandlesFromCache 从回测缓存加载 K 线数据
func LoadCandlesFromCache(cacheName string) ([]*exchange.Candle, error) {
	return LoadFromCache(cacheName)
}

// ParseKlineFileMeta 从文件名解析元信息
func ParseKlineFileMeta(filename string) KlineFileMeta {
	// 格式: {interval}_{exchange}_{symbol}_{date}.csv
	// 示例: 1m_binance_BTCUSDT_20260102.csv

	meta := KlineFileMeta{}

	// 移除.csv扩展名
	name := filename
	if strings.HasSuffix(name, ".csv") {
		name = name[:len(name)-4]
	}

	parts := strings.Split(name, "_")
	if len(parts) >= 4 {
		meta.Interval = parts[0]
		// parts[1] 是交易所，暂时不需要
		meta.Symbol = parts[2]
		meta.Date = parts[3]
		meta.HasDepth = meta.Interval == "1m" || meta.Interval == "1h"
	}

	return meta
}

// parseKlineRecord 解析单根 K线记录（前6列）
func parseKlineRecord(record []string) (*exchange.Candle, error) {
	if len(record) < 6 {
		return nil, fmt.Errorf("K线数据不足6列")
	}

	timestamp, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("解析时间戳失败: %w", err)
	}

	open, err := strconv.ParseFloat(record[1], 64)
	if err != nil {
		return nil, fmt.Errorf("解析开盘价失败: %w", err)
	}

	high, err := strconv.ParseFloat(record[2], 64)
	if err != nil {
		return nil, fmt.Errorf("解析最高价失败: %w", err)
	}

	low, err := strconv.ParseFloat(record[3], 64)
	if err != nil {
		return nil, fmt.Errorf("解析最低价失败: %w", err)
	}

	close, err := strconv.ParseFloat(record[4], 64)
	if err != nil {
		return nil, fmt.Errorf("解析收盘价失败: %w", err)
	}

	volume, err := strconv.ParseFloat(record[5], 64)
	if err != nil {
		return nil, fmt.Errorf("解析成交量失败: %w", err)
	}

	// 获取 symbol（如果有第7列）
	symbol := ""
	if len(record) > 6 {
		symbol = record[6]
	}

	return &exchange.Candle{
		Timestamp: timestamp,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		Symbol:    symbol,
	}, nil
}

// parseDepthRecord 解析深度记录（后20列）
// 格式：bid_price_1, bid_qty_1, ask_price_1, ask_qty_1, ..., ask_price_5, ask_qty_5
func parseDepthRecord(timestamp int64, depthRecord []string) *DepthSnapshotForBacktest {
	if len(depthRecord) < 20 {
		return &DepthSnapshotForBacktest{Timestamp: timestamp}
	}

	var bidDepth, askDepth float64

	// 解析 5 档买卖盘
	for i := 0; i < 5; i++ {
		bidPriceIdx := i * 4
		bidQtyIdx := i*4 + 1
		askPriceIdx := i*4 + 2
		askQtyIdx := i*4 + 3

		// 买盘
		if bidPriceIdx < len(depthRecord) && bidQtyIdx < len(depthRecord) {
			if bidPrice, err := strconv.ParseFloat(depthRecord[bidPriceIdx], 64); err == nil {
				if bidQty, err := strconv.ParseFloat(depthRecord[bidQtyIdx], 64); err == nil {
					bidDepth += bidPrice * bidQty
				}
			}
		}

		// 卖盘
		if askPriceIdx < len(depthRecord) && askQtyIdx < len(depthRecord) {
			if askPrice, err := strconv.ParseFloat(depthRecord[askPriceIdx], 64); err == nil {
				if askQty, err := strconv.ParseFloat(depthRecord[askQtyIdx], 64); err == nil {
					askDepth += askPrice * askQty
				}
			}
		}
	}

	return &DepthSnapshotForBacktest{
		Timestamp:  timestamp,
		TotalDepth: bidDepth + askDepth,
		BidDepth:   bidDepth,
		AskDepth:   askDepth,
	}
}
