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

// DataLoader 历史数据加载器
// 支持 CSV 和 gzip 压缩的 CSV 文件
type DataLoader struct {
	dataDir string
	symbol  string
}

// NewDataLoader 创建数据加载器
func NewDataLoader(dataDir, symbol string) *DataLoader {
	return &DataLoader{
		dataDir: dataDir,
		symbol:  symbol,
	}
}

// KlineRow K线数据行（CSV格式）
type KlineRow struct {
	OpenTime  int64
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	CloseTime int64
	NumTrades int64
}

// LoadKlinesFromCSV 从CSV文件加载K线数据
// 支持的CSV格式（参考币安格式）：
// open_time,open,high,low,close,volume,close_time,quote_volume,trades,....
func (dl *DataLoader) LoadKlinesFromCSV(filePath string) ([]KlineRow, error) {
	logger.Info("Loading klines from CSV: %s", filePath)

	// 检查文件扩展名
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

	var klines []KlineRow
	lineNum := 0

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
			if len(record) > 0 && strings.ToLower(record[0]) == "open_time" {
				continue
			}
		}

		if len(record) < 7 {
			logger.Warn("Skipping invalid line %d: insufficient columns", lineNum)
			continue
		}

		kline, err := parseKlineRow(record)
		if err != nil {
			logger.Warn("Skipping invalid line %d: %v", lineNum, err)
			continue
		}

		klines = append(klines, kline)

		// 进度显示
		if lineNum%10000 == 0 {
			logger.Info("Loaded %d klines...", lineNum)
		}
	}

	logger.Info("Successfully loaded %d klines from %s", len(klines), filePath)
	return klines, nil
}

// parseKlineRow 解析K线数据行
func parseKlineRow(record []string) (KlineRow, error) {
	var kline KlineRow
	var err error

	// 解析时间戳
	kline.OpenTime, err = strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return kline, fmt.Errorf("invalid open_time: %w", err)
	}

	// 解析OHLCV
	kline.Open, err = strconv.ParseFloat(record[1], 64)
	if err != nil {
		return kline, fmt.Errorf("invalid open: %w", err)
	}

	kline.High, err = strconv.ParseFloat(record[2], 64)
	if err != nil {
		return kline, fmt.Errorf("invalid high: %w", err)
	}

	kline.Low, err = strconv.ParseFloat(record[3], 64)
	if err != nil {
		return kline, fmt.Errorf("invalid low: %w", err)
	}

	kline.Close, err = strconv.ParseFloat(record[4], 64)
	if err != nil {
		return kline, fmt.Errorf("invalid close: %w", err)
	}

	kline.Volume, err = strconv.ParseFloat(record[5], 64)
	if err != nil {
		return kline, fmt.Errorf("invalid volume: %w", err)
	}

	// 解析收盘时间
	kline.CloseTime, err = strconv.ParseInt(record[6], 10, 64)
	if err != nil {
		return kline, fmt.Errorf("invalid close_time: %w", err)
	}

	// 解析成交笔数（如果有）
	if len(record) > 8 {
		kline.NumTrades, err = strconv.ParseInt(record[8], 10, 64)
		if err != nil {
			kline.NumTrades = 0
		}
	}

	return kline, nil
}

// LoadKlinesFromDir 从目录加载所有K线数据
// 目录结构：data_dir/{symbol}/1m/*.csv.gz
func (dl *DataLoader) LoadKlinesFromDir() ([]KlineRow, error) {
	symbolDir := filepath.Join(dl.dataDir, dl.symbol, "1m")
	if _, err := os.Stat(symbolDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("symbol directory does not exist: %s", symbolDir)
	}

	files, err := filepath.Glob(filepath.Join(symbolDir, "*.csv*"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob CSV files: %w", err)
	}

	if len(files) == 0 {
		return nil, fmt.Errorf("no CSV files found in %s", symbolDir)
	}

	logger.Info("Found %d CSV files in %s", len(files), symbolDir)

	var allKlines []KlineRow
	for _, file := range files {
		klines, err := dl.LoadKlinesFromCSV(file)
		if err != nil {
			logger.Warn("Failed to load %s: %v", file, err)
			continue
		}
		allKlines = append(allKlines, klines...)
	}

	if len(allKlines) == 0 {
		return nil, fmt.Errorf("no valid klines loaded")
	}

	// 按时间排序
	allKlines = sortKlines(allKlines)

	logger.Info("Loaded total %d klines for %s", len(allKlines), dl.symbol)
	return allKlines, nil
}

// sortKlines 按时间排序K线数据
func sortKlines(klines []KlineRow) []KlineRow {
	// 简单的冒泡排序（对于小数据集足够）
	// 对于大数据集，可以考虑使用更高效的排序算法
	n := len(klines)
	for i := 0; i < n-1; i++ {
		for j := 0; j < n-i-1; j++ {
			if klines[j].OpenTime > klines[j+1].OpenTime {
				klines[j], klines[j+1] = klines[j+1], klines[j]
			}
		}
	}
	return klines
}

// FilterByTimeRange 按时间范围过滤
func (dl *DataLoader) FilterByTimeRange(klines []KlineRow, start, end time.Time) []KlineRow {
	startTs := start.UnixMilli()
	endTs := end.UnixMilli()

	var filtered []KlineRow
	for _, k := range klines {
		if k.OpenTime >= startTs && k.OpenTime < endTs {
			filtered = append(filtered, k)
		}
	}

	return filtered
}

// ConvertToTickKlines 转换为TickKline格式
func ConvertToTickKlines(klines []KlineRow) []TickKline {
	tickKlines := make([]TickKline, len(klines))
	for i, k := range klines {
		tickKlines[i] = TickKline{
			Timestamp: k.OpenTime,
			Open:      k.Open,
			High:      k.High,
			Low:       k.Low,
			Close:     k.Close,
			Volume:    k.Volume,
		}
	}
	return tickKlines
}

// KlineToTickKline 单个K线转换
func KlineToTickKline(k KlineRow) TickKline {
	return TickKline{
		Timestamp: k.OpenTime,
		Open:      k.Open,
		High:      k.High,
		Low:       k.Low,
		Close:     k.Close,
		Volume:    k.Volume,
	}
}

// FundingRateRow 资金费率数据行
type FundingRateRow struct {
	FundingTime int64
	FundingRate float64
}

// LoadFundingRatesFromCSV 从CSV文件加载资金费率数据
func (dl *DataLoader) LoadFundingRatesFromCSV(filePath string) ([]FundingRateRow, error) {
	logger.Info("Loading funding rates from CSV: %s", filePath)

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

	var rates []FundingRateRow
	lineNum := 0

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
			if len(record) > 0 && strings.ToLower(record[0]) == "funding_time" {
				continue
			}
		}

		if len(record) < 2 {
			continue
		}

		ts, err := strconv.ParseInt(record[0], 10, 64)
		if err != nil {
			continue
		}

		rate, err := strconv.ParseFloat(record[1], 64)
		if err != nil {
			continue
		}

		rates = append(rates, FundingRateRow{
			FundingTime: ts,
			FundingRate: rate,
		})
	}

	logger.Info("Loaded %d funding rate records", len(rates))
	return rates, nil
}

// LoadFundingRatesFromDir 从目录加载所有资金费率数据
func (dl *DataLoader) LoadFundingRatesFromDir() ([]FundingRateRow, error) {
	fundingDir := filepath.Join(dl.dataDir, "funding_rate", dl.symbol)
	if _, err := os.Stat(fundingDir); os.IsNotExist(err) {
		// 资金费率数据可选，不存在时不报错
		logger.Info("Funding rate directory does not exist: %s (skipping)", fundingDir)
		return []FundingRateRow{}, nil
	}

	files, err := filepath.Glob(filepath.Join(fundingDir, "*.csv*"))
	if err != nil {
		return nil, fmt.Errorf("failed to glob CSV files: %w", err)
	}

	if len(files) == 0 {
		return []FundingRateRow{}, nil
	}

	var allRates []FundingRateRow
	for _, file := range files {
		rates, err := dl.LoadFundingRatesFromCSV(file)
		if err != nil {
			logger.Warn("Failed to load %s: %v", file, err)
			continue
		}
		allRates = append(allRates, rates...)
	}

	return allRates, nil
}

// DataStats 数据统计信息
type DataStats struct {
	TotalKlines    int
	StartTime      time.Time
	EndTime        time.Time
	TimeSpan       time.Duration
	AvgVolume      float64
	TotalVolume    float64
	PriceChange    float64
	PriceChangePct float64
}

// GetDataStats 获取数据统计信息
func GetDataStats(klines []KlineRow) DataStats {
	if len(klines) == 0 {
		return DataStats{}
	}

	stats := DataStats{
		TotalKlines: len(klines),
		StartTime:   time.Unix(klines[0].OpenTime/1000, 0),
		EndTime:     time.Unix(klines[len(klines)-1].OpenTime/1000, 0),
	}

	stats.TimeSpan = stats.EndTime.Sub(stats.StartTime)

	totalVol := 0.0
	for _, k := range klines {
		totalVol += k.Volume
	}
	stats.TotalVolume = totalVol
	stats.AvgVolume = totalVol / float64(len(klines))

	if len(klines) > 0 {
		stats.PriceChange = klines[len(klines)-1].Close - klines[0].Open
		if klines[0].Open > 0 {
			stats.PriceChangePct = (stats.PriceChange / klines[0].Open) * 100
		}
	}

	return stats
}

// ValidateKlines 验证K线数据
func ValidateKlines(klines []KlineRow) error {
	if len(klines) == 0 {
		return fmt.Errorf("no klines to validate")
	}

	prevTime := int64(0)
	prevClose := klines[0].Close

	for i, k := range klines {
		// 检查时间戳递增
		if k.OpenTime < prevTime {
			return fmt.Errorf("timestamp not increasing at index %d", i)
		}

		// 检查OHLC逻辑
		if k.High < k.Low {
			return fmt.Errorf("invalid high/low at index %d: high (%.2f) < low (%.2f)", i, k.High, k.Low)
		}
		if k.Open < 0 || k.Close < 0 || k.High < 0 || k.Low < 0 {
			return fmt.Errorf("negative price at index %d", i)
		}
		if k.Volume < 0 {
			return fmt.Errorf("negative volume at index %d", i)
		}

		// 检查价格连续性（允许跳涨但不应该超过50%）
		priceChange := abs((k.Close - prevClose) / prevClose)
		if priceChange > 0.5 {
			logger.Warn("Large price gap detected at index %d: %.2f%% change", i, priceChange*100)
		}

		prevTime = k.OpenTime
		prevClose = k.Close
	}

	return nil
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// ResampleToInterval 重采样到不同时间周期
func ResampleToInterval(klines []KlineRow, intervalMinutes int) []KlineRow {
	if len(klines) == 0 || intervalMinutes < 1 {
		return klines
	}

	intervalMs := int64(intervalMinutes * 60 * 1000)
	var resampled []KlineRow

	i := 0
	for i < len(klines) {
		startTime := klines[i].OpenTime
		endTime := startTime + intervalMs

		var bucket []KlineRow
		for i < len(klines) && klines[i].OpenTime < endTime {
			bucket = append(bucket, klines[i])
			i++
		}

		if len(bucket) == 0 {
			continue
		}

		// 合并K线
		merged := KlineRow{
			OpenTime:  bucket[0].OpenTime,
			Open:      bucket[0].Open,
			CloseTime: bucket[len(bucket)-1].CloseTime,
		}

		high, low := bucket[0].High, bucket[0].Low
		totalVol := 0.0
		totalTrades := int64(0)

		for _, k := range bucket {
			if k.High > high {
				high = k.High
			}
			if k.Low < low {
				low = k.Low
			}
			totalVol += k.Volume
			totalTrades += k.NumTrades
		}

		merged.High = high
		merged.Low = low
		merged.Close = bucket[len(bucket)-1].Close
		merged.Volume = totalVol
		merged.NumTrades = totalTrades

		resampled = append(resampled, merged)
	}

	return resampled
}
