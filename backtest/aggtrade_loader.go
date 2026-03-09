package backtest

import (
	"compress/gzip"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

// AggTradeLoader 聚合交易數據加載器
// 從 Binance aggTrades CSV 檔案加載真實 tick 數據
type AggTradeLoader struct {
	dataDir string
	symbol  string
}

// NewAggTradeLoader 創建聚合交易加載器
func NewAggTradeLoader(dataDir, symbol string) *AggTradeLoader {
	return &AggTradeLoader{
		dataDir: dataDir,
		symbol:  strings.ToUpper(symbol),
	}
}

// AggTradeRow 聚合交易數據行（CSV 格式）
// Binance aggTrades 格式：
// aggTradeId,price,quantity,firstTradeId,lastTradeId,timestamp,isBuyerMaker
type AggTradeRow struct {
	AggTradeID   int64
	Price        float64
	Quantity     float64
	FirstTradeID int64
	LastTradeID  int64
	Timestamp    int64
	IsBuyerMaker bool // true = 買方是做市商（主動賣），false = 買方是吃單者（主動買）
}

// LoadAggTradesFromCSV 從 CSV 檔案加載聚合交易數據
func (atl *AggTradeLoader) LoadAggTradesFromCSV(filePath string) ([]AggTradeRow, error) {
	logger.Info("Loading aggTrades from CSV: %s", filePath)

	// 檢查檔案擴展名
	var reader io.Reader
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	if strings.HasSuffix(filePath, ".gz") {
		gzReader, err := gzip.NewReader(file)
		if err != nil {
			return nil, fmt.Errorf("failed to create gzip reader: %w", err)
		}
		defer gzReader.Close()
		reader = gzReader
	} else {
		reader = file
	}

	csvReader := csv.NewReader(reader)

	var trades []AggTradeRow
	lineNum := 0
	invalidCount := 0

	for {
		record, err := csvReader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read CSV: %w", err)
		}

		lineNum++
		if lineNum == 1 {
			// 跳过标题行（如果有）
			if len(record) > 0 && strings.ToLower(record[0]) == "agg_trade_id" {
				continue
			}
		}

		if len(record) < 7 {
			logger.Warn("Skipping invalid line %d: insufficient columns", lineNum)
			invalidCount++
			continue
		}

		trade, err := parseAggTradeRow(record)
		if err != nil {
			logger.Warn("Skipping invalid line %d: %v", lineNum, err)
			invalidCount++
			continue
		}

		trades = append(trades, trade)
	}

	logger.Info("Loaded %d aggTrades (skipped %d invalid lines)", len(trades), invalidCount)
	return trades, nil
}

// parseAggTradeRow 解析聚合交易行
func parseAggTradeRow(record []string) (AggTradeRow, error) {
	var trade AggTradeRow
	var err error

	// 解析 aggTradeId
	trade.AggTradeID, err = strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return trade, fmt.Errorf("invalid aggTradeId: %w", err)
	}

	// 解析 price
	trade.Price, err = strconv.ParseFloat(record[1], 64)
	if err != nil {
		return trade, fmt.Errorf("invalid price: %w", err)
	}

	// 解析 quantity
	trade.Quantity, err = strconv.ParseFloat(record[2], 64)
	if err != nil {
		return trade, fmt.Errorf("invalid quantity: %w", err)
	}

	// 解析 firstTradeId
	trade.FirstTradeID, err = strconv.ParseInt(record[3], 10, 64)
	if err != nil {
		return trade, fmt.Errorf("invalid firstTradeId: %w", err)
	}

	// 解析 lastTradeId
	trade.LastTradeID, err = strconv.ParseInt(record[4], 10, 64)
	if err != nil {
		return trade, fmt.Errorf("invalid lastTradeId: %w", err)
	}

	// 解析 timestamp (毫秒)
	trade.Timestamp, err = strconv.ParseInt(record[5], 10, 64)
	if err != nil {
		return trade, fmt.Errorf("invalid timestamp: %w", err)
	}

	// 解析 isBuyerMaker
	isBuyerMaker, err := strconv.ParseBool(record[6])
	if err != nil {
		// 嘗試從字符串轉換
		if record[6] == "true" || record[6] == "TRUE" || record[6] == "1" {
			isBuyerMaker = true
		} else if record[6] == "false" || record[6] == "FALSE" || record[6] == "0" {
			isBuyerMaker = false
		} else {
			return trade, fmt.Errorf("invalid isBuyerMaker: %w", err)
		}
	}
	trade.IsBuyerMaker = isBuyerMaker

	return trade, nil
}

// LoadAggTradesFromDir 從目錄加載指定日期範圍的聚合交易數據
func (atl *AggTradeLoader) LoadAggTradesFromDir(start, end time.Time) ([]AggTradeRow, error) {
	var allTrades []AggTradeRow

	aggTradesDir := filepath.Join(atl.dataDir, "aggtrades", atl.symbol)

	// 遍歷日期範圍
	for date := start; !date.After(end); date = date.AddDate(0, 0, 1) {
		dateStr := date.Format("2006-01-02")
		csvFile := filepath.Join(aggTradesDir, fmt.Sprintf("%s-aggTrades-%s.csv", atl.symbol, dateStr))

		// 檢查檔案是否存在
		if _, err := os.Stat(csvFile); os.IsNotExist(err) {
			// 嘗試 gzip 格式
			gzFile := csvFile + ".gz"
			if _, err := os.Stat(gzFile); os.IsNotExist(err) {
				logger.Warn("aggTrades file not found for %s: %s", dateStr, csvFile)
				continue
			}
			csvFile = gzFile
		}

		// 加載單日數據
		trades, err := atl.LoadAggTradesFromCSV(csvFile)
		if err != nil {
			logger.Warn("Failed to load aggTrades for %s: %v", dateStr, err)
			continue
		}

		allTrades = append(allTrades, trades...)
	}

	// 按時間戳排序（確保時序正確）
	for i := 0; i < len(allTrades)-1; i++ {
		for j := i + 1; j < len(allTrades); j++ {
			if allTrades[i].Timestamp > allTrades[j].Timestamp {
				allTrades[i], allTrades[j] = allTrades[j], allTrades[i]
			}
		}
	}

	logger.Info("Loaded total %d aggTrades from %s to %s", len(allTrades),
		start.Format("2006-01-02"), end.Format("2006-01-02"))

	return allTrades, nil
}

// ConvertToTickTrades 將 AggTradeRow 轉換為 TickTrade 格式
// 用於與現有 TickMatcher 兼容
func (atl *AggTradeLoader) ConvertToTickTrades(aggTrades []AggTradeRow) []TickTrade {
	tickTrades := make([]TickTrade, 0, len(aggTrades))

	for _, at := range aggTrades {
		// 判斷交易方向：IsBuyerMaker=true 表示主動賣（賣單），false 表示主動買（買單）
		side := "buy"
		if at.IsBuyerMaker {
			side = "sell"
		}

		tickTrades = append(tickTrades, TickTrade{
			TradeID:   fmt.Sprintf("A%d", at.AggTradeID),
			Price:     at.Price,
			Size:      at.Quantity,
			Side:      side,
			Timestamp: at.Timestamp,
			// 其他字段使用默認值
			OrderID:    "",
			Strategy:   "market",
			StrategyID: "",
			AccountID:  "",
			GridLevel:  0,
			Slippage:   0,
		})
	}

	return tickTrades
}

// GetAggTradesStats 獲取聚合交易統計信息
type AggTradesStats struct {
	TotalTrades     int64
	TotalVolume     float64
	WeightedAvgPrice float64
	PriceRange       PriceRange
	BuyVolume       float64
	SellVolume      float64
	TimeRange       TimeRange
}

type PriceRange struct {
	Min float64
	Max float64
}

type TimeRange struct {
	Start int64
	End   int64
}

// GetStats 計算聚合交易統計信息
func (atl *AggTradeLoader) GetStats(trades []AggTradeRow) *AggTradesStats {
	if len(trades) == 0 {
		return &AggTradesStats{}
	}

	stats := &AggTradesStats{
		TotalTrades: int64(len(trades)),
		PriceRange: PriceRange{
			Min: trades[0].Price,
			Max: trades[0].Price,
		},
		TimeRange: TimeRange{
			Start: trades[0].Timestamp,
			End:   trades[len(trades)-1].Timestamp,
		},
	}

	totalValue := 0.0
	totalVolume := 0.0
	buyVolume := 0.0
	sellVolume := 0.0

	for _, trade := range trades {
		totalValue += trade.Price * trade.Quantity
		totalVolume += trade.Quantity

		if trade.Price < stats.PriceRange.Min {
			stats.PriceRange.Min = trade.Price
		}
		if trade.Price > stats.PriceRange.Max {
			stats.PriceRange.Max = trade.Price
		}

		// IsBuyerMaker=true 表示主動賣
		if trade.IsBuyerMaker {
			sellVolume += trade.Quantity
		} else {
			buyVolume += trade.Quantity
		}
	}

	stats.TotalVolume = totalVolume
	stats.WeightedAvgPrice = totalValue / totalVolume
	stats.BuyVolume = buyVolume
	stats.SellVolume = sellVolume

	return stats
}

// ResampleToKline 將 aggTrades 重採樣為 K 線數據
func (atl *AggTradeLoader) ResampleToKline(trades []AggTradeRow, interval time.Duration) []TickKline {
	if len(trades) == 0 {
		return nil
	}

	// 按時間間隔分組
	klineMap := make(map[int64]*TickKline)

	for _, trade := range trades {
		// 計算 K 線時間槽
		timestamp := time.UnixMilli(trade.Timestamp).UTC()
		klineTime := timestamp.Truncate(interval).UnixMilli() * 1000000 // 轉為納秒（兼容現有格式）

		if klineMap[klineTime] == nil {
			klineMap[klineTime] = &TickKline{
				Timestamp: klineTime,
				Open:      trade.Price,
				High:      trade.Price,
				Low:       trade.Price,
				Close:     trade.Price,
				Volume:    trade.Quantity,
			}
		} else {
			k := klineMap[klineTime]
			if trade.Price > k.High {
				k.High = trade.Price
			}
			if trade.Price < k.Low {
				k.Low = trade.Price
			}
			k.Close = trade.Price
			k.Volume += trade.Quantity
		}
	}

	// 轉換為數組並排序
	klines := make([]TickKline, 0, len(klineMap))
	for _, k := range klineMap {
		klines = append(klines, *k)
	}

	// 按時間排序
	for i := 0; i < len(klines)-1; i++ {
		for j := i + 1; j < len(klines); j++ {
			if klines[i].Timestamp > klines[j].Timestamp {
				klines[i], klines[j] = klines[j], klines[i]
			}
		}
	}

	return klines
}
