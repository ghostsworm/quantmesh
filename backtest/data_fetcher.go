package backtest

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"quantmesh/exchange"
	"quantmesh/exchange/binance"
	"quantmesh/logger"
)

// GetHistoricalData 智能獲取歷史數據（优先缓存）
// 兼容舊調用：僅支援 Binance
func GetHistoricalData(
	symbol string, // "BTCUSDT"
	interval string, // "1m", "5m", "1h"
	startTime time.Time,
	endTime time.Time,
	binanceConfig map[string]string,
) ([]*exchange.Candle, error) {
	return GetHistoricalDataEx("binance", symbol, interval, startTime, endTime, binanceConfig)
}

// GetHistoricalDataEx 支援多交易所的历史數據獲取
func GetHistoricalDataEx(
	exchangeName string, // "binance", "bitget"
	symbol string,
	interval string,
	startTime time.Time,
	endTime time.Time,
	config map[string]string,
) ([]*exchange.Candle, error) {
	// 历史數據當前主要從 Binance 獲取（流动性最好）
	// Bitget 等交易所的 USDT 現貨對與 Binance 數據通常一致
	if exchangeName != "binance" && exchangeName != "bitget" {
		exchangeName = "binance"
	}
	// Bitget 暂用 Binance 數據源，缓存键用 binance 以共享缓存
	cacheExchange := exchangeName
	if exchangeName == "bitget" {
		cacheExchange = "binance"
	}
	binanceConfig := config
	if exchangeName == "bitget" {
		binanceConfig = map[string]string{"api_key": "", "secret_key": "", "testnet": "false"}
	}

	// 1. 生成缓存键
	cacheKey := fmt.Sprintf("%s_%s_%s_%s_%s",
		cacheExchange, symbol, interval,
		startTime.Format("2006-01-02"),
		endTime.Format("2006-01-02"),
	)

	// 2. 检查缓存
	if candles, err := LoadFromCache(cacheKey); err == nil {
		logger.Info("✅ 從缓存加載: %s (%d 根K線)", cacheKey, len(candles))
		return candles, nil
	}

	// 3. 從交易所獲取（Binance/Bitget 均使用 Binance 數據源，USDT 現貨對通用）
	logger.Info("⬇️ 從 Binance 下載: %s %s (%s 至 %s)",
		symbol, interval,
		startTime.Format("2006-01-02"),
		endTime.Format("2006-01-02"))

	candles, err := fetchFromBinance(symbol, interval, startTime, endTime, binanceConfig)
	if err != nil {
		return nil, err
	}

	// 4. 保存缓存
	if err := SaveToCache(cacheKey, candles); err != nil {
		logger.Warn("⚠️ 缓存保存失败: %v", err)
	} else {
		sizeMB := float64(len(candles)*80) / 1024 / 1024
		logger.Info("💾 已缓存: %s (%.2f MB)", cacheKey, sizeMB)
	}

	return candles, nil
}

// fetchFromBinance 從 Binance 分批獲取數據
func fetchFromBinance(
	symbol string,
	interval string,
	startTime time.Time,
	endTime time.Time,
	binanceConfig map[string]string,
) ([]*exchange.Candle, error) {

	// 創建 Binance adapter
	adapter, err := binance.NewBinanceAdapter(binanceConfig, symbol)
	if err != nil {
		return nil, fmt.Errorf("創建 Binance adapter 失败: %w", err)
	}

	allCandles := make([]*exchange.Candle, 0)
	currentStart := startTime

	// 计算每批的時间跨度（根據 interval）
	batchDuration := calculateBatchDuration(interval, 1000)

	totalBatches := int(endTime.Sub(startTime) / batchDuration)
	if totalBatches == 0 {
		totalBatches = 1
	}
	batchNum := 0

	// Binance 單次最多 1000 根，需要分批
	for currentStart.Before(endTime) {
		batchNum++
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

		candles, err := adapter.GetHistoricalKlines(ctx, symbol, interval, 1000)
		cancel()

		if err != nil {
			return nil, fmt.Errorf("獲取第 %d 批數據失败: %w", batchNum, err)
		}

		if len(candles) == 0 {
			break
		}

		// 過滤時间範圍内的數據
		for _, candle := range candles {
			candleTime := time.Unix(candle.Timestamp/1000, 0)
			if candleTime.After(endTime) {
				break
			}
			if candleTime.Before(startTime) {
				continue
			}
			// 轉换為 exchange.Candle
			allCandles = append(allCandles, &exchange.Candle{
				Symbol:    candle.Symbol,
				Open:      candle.Open,
				High:      candle.High,
				Low:       candle.Low,
				Close:     candle.Close,
				Volume:    candle.Volume,
				Timestamp: candle.Timestamp,
				IsClosed:  candle.IsClosed,
			})
		}

		// 计算下一批的起始時间
		if len(candles) > 0 {
			lastTimestamp := candles[len(candles)-1].Timestamp
			currentStart = time.Unix(lastTimestamp/1000, 0).Add(time.Second)

			// 如果已經超過結束時间，退出
			if currentStart.After(endTime) {
				break
			}
		} else {
			break
		}

		// 显示進度
		progress := float64(batchNum) / float64(totalBatches) * 100
		if progress > 100 {
			progress = 100
		}
		logger.Info("📊 下載進度: %.1f%% (已獲取 %d 根K線)", progress, len(allCandles))

		// 避免触发限流
		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("✅ 下載完成: 共 %d 根K線", len(allCandles))
	return allCandles, nil
}

// calculateBatchDuration 计算每批的時间跨度
func calculateBatchDuration(interval string, limit int) time.Duration {
	var duration time.Duration

	switch interval {
	case "1m":
		duration = time.Minute
	case "3m":
		duration = 3 * time.Minute
	case "5m":
		duration = 5 * time.Minute
	case "15m":
		duration = 15 * time.Minute
	case "30m":
		duration = 30 * time.Minute
	case "1h":
		duration = time.Hour
	case "2h":
		duration = 2 * time.Hour
	case "4h":
		duration = 4 * time.Hour
	case "6h":
		duration = 6 * time.Hour
	case "8h":
		duration = 8 * time.Hour
	case "12h":
		duration = 12 * time.Hour
	case "1d":
		duration = 24 * time.Hour
	case "3d":
		duration = 3 * 24 * time.Hour
	case "1w":
		duration = 7 * 24 * time.Hour
	case "1M":
		duration = 30 * 24 * time.Hour
	default:
		duration = time.Hour
	}

	return duration * time.Duration(limit)
}

// LoadFromCache 從 CSV 加載
func LoadFromCache(cacheKey string) ([]*exchange.Candle, error) {
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)
	
	// 跳過表头
	_, err = reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取表头失败: %w", err)
	}

	// 使用流式读取，避免一次性加載整個文件到記憶體
	// 限制最大读取數量，防止記憶體占用過大
	maxCandles := 1000000 // 最多100万根K線
	candles := make([]*exchange.Candle, 0, 10000) // 預分配1万容量
	
	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 行失败: %w", lineNum+1, err)
		}
		
		// 限制最大數量
		if len(candles) >= maxCandles {
			logger.Warn("⚠️ CSV 文件過大，只读取前 %d 根K線", maxCandles)
			break
		}
		
		candle, err := parseCSVRecord(record)
		if err != nil {
			return nil, fmt.Errorf("解析第 %d 行失败: %w", lineNum+1, err)
		}
		candles = append(candles, candle)
		lineNum++
	}

	return candles, nil
}

// parseCSVRecord 解析 CSV 記錄
func parseCSVRecord(record []string) (*exchange.Candle, error) {
	if len(record) != 7 {
		return nil, fmt.Errorf("記錄字段數量錯误: 期望7個，實際%d個", len(record))
	}

	timestamp, err := strconv.ParseInt(record[0], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("解析 timestamp 失败: %w", err)
	}

	open, err := strconv.ParseFloat(record[1], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 open 失败: %w", err)
	}

	high, err := strconv.ParseFloat(record[2], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 high 失败: %w", err)
	}

	low, err := strconv.ParseFloat(record[3], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 low 失败: %w", err)
	}

	close, err := strconv.ParseFloat(record[4], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 close 失败: %w", err)
	}

	volume, err := strconv.ParseFloat(record[5], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 volume 失败: %w", err)
	}

	symbol := record[6]

	return &exchange.Candle{
		Timestamp: timestamp,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		Symbol:    symbol,
		IsClosed:  true,
	}, nil
}

// SaveToCache 保存到 CSV
func SaveToCache(cacheKey string, candles []*exchange.Candle) error {
	// 确保目錄存在
	cacheDir := filepath.Join("backtest", "cache")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("創建缓存目錄失败: %w", err)
	}

	filename := filepath.Join(cacheDir, cacheKey+".csv")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("創建缓存文件失败: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 写入表头
	if err := writer.Write([]string{"timestamp", "open", "high", "low", "close", "volume", "symbol"}); err != nil {
		return fmt.Errorf("写入表头失败: %w", err)
	}

	// 写入數據
	for _, c := range candles {
		record := []string{
			fmt.Sprintf("%d", c.Timestamp),
			fmt.Sprintf("%.8f", c.Open),
			fmt.Sprintf("%.8f", c.High),
			fmt.Sprintf("%.8f", c.Low),
			fmt.Sprintf("%.8f", c.Close),
			fmt.Sprintf("%.8f", c.Volume),
			c.Symbol,
		}
		if err := writer.Write(record); err != nil {
			return fmt.Errorf("写入數據失败: %w", err)
		}
	}

	// 更新缓存索引
	if err := updateCacheIndex(cacheKey, candles); err != nil {
		logger.Warn("⚠️ 更新缓存索引失败: %v", err)
	}

	return nil
}

// CacheIndexEntry 缓存索引条目
type CacheIndexEntry struct {
	Symbol   string    `json:"symbol"`
	Interval string    `json:"interval"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Candles  int       `json:"candles"`
	SizeMB   float64   `json:"size_mb"`
	Created  time.Time `json:"created"`
}

// updateCacheIndex 更新缓存索引
func updateCacheIndex(cacheKey string, candles []*exchange.Candle) error {
	indexFile := filepath.Join("backtest", "cache", "cache_index.json")

	// 读取現有索引
	index := make(map[string]CacheIndexEntry)
	if data, err := os.ReadFile(indexFile); err == nil {
		json.Unmarshal(data, &index)
	}

	// 解析缓存键
	// 格式: BTCUSDT_1h_2023-01-01_2023-06-30
	var symbol, interval, startStr, endStr string
	fmt.Sscanf(cacheKey, "%[^_]_%[^_]_%[^_]_%s", &symbol, &interval, &startStr, &endStr)

	start, _ := time.Parse("2006-01-02", startStr)
	end, _ := time.Parse("2006-01-02", endStr)

	// 计算文件大小
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	fileInfo, _ := os.Stat(filename)
	sizeMB := float64(fileInfo.Size()) / 1024 / 1024

	// 更新索引
	index[cacheKey] = CacheIndexEntry{
		Symbol:   symbol,
		Interval: interval,
		Start:    start,
		End:      end,
		Candles:  len(candles),
		SizeMB:   sizeMB,
		Created:  time.Now(),
	}

	// 保存索引
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(indexFile, data, 0644)
}
