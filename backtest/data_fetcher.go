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
	"strings"
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

// GetHistoricalDataEx 支援多交易所的歷史數據獲取
func GetHistoricalDataEx(
	exchangeName string, // "binance", "bitget"
	symbol string,
	interval string,
	startTime time.Time,
	endTime time.Time,
	config map[string]string,
) ([]*exchange.Candle, error) {
	// 歷史數據當前主要從 Binance 獲取（流动性最好）
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

	// 2. 檢查缓存
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

	if len(candles) == 0 {
		logger.Warn("⚠️ 該時間範圍無 K 線數據，不寫入緩存: %s (%s 至 %s)", cacheKey,
			startTime.Format("2006-01-02"), endTime.Format("2006-01-02"))
		return candles, nil
	}

	// 4. 保存缓存
	if err := SaveToCache(cacheKey, candles); err != nil {
		logger.Warn("⚠️ 缓存保存失敗: %v", err)
	} else {
		sizeMB := float64(len(candles)*80) / 1024 / 1024
		logger.Info("💾 已缓存: %s (%d 根 K 線, %.2f MB)", cacheKey, len(candles), sizeMB)
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

	// 創建 Binance adapter（API 為空時使用公開數據適配器，K 線為公開接口無需認證）
	var adapter *binance.BinanceAdapter
	if binanceConfig["api_key"] != "" && binanceConfig["secret_key"] != "" {
		var err error
		adapter, err = binance.NewBinanceAdapter(binanceConfig, symbol)
		if err != nil {
			return nil, fmt.Errorf("創建 Binance adapter 失敗: %w", err)
		}
	} else {
		var err error
		adapter, err = binance.NewBinanceAdapterForPublicData(binanceConfig, symbol)
		if err != nil {
			return nil, fmt.Errorf("創建 Binance adapter 失敗: %w", err)
		}
	}

	allCandles := make([]*exchange.Candle, 0)
	currentStart := startTime

	// 計算每批的時间跨度（根據 interval）
	batchDuration := calculateBatchDuration(interval, 1000)

	totalBatches := int(endTime.Sub(startTime) / batchDuration)
	if totalBatches == 0 {
		totalBatches = 1
	}
	batchNum := 0

	// Binance 單次最多 1000 根，按起始時間分批請求（傳入 startTime 才能拉取指定區間，否則只會拿到「最近 1000 根」）
	for currentStart.Before(endTime) {
		batchNum++
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		startMs := currentStart.UnixMilli()
		candles, err := adapter.GetHistoricalKlinesFrom(ctx, symbol, interval, startMs, 1000)
		cancel()

		if err != nil {
			return nil, fmt.Errorf("獲取第 %d 批數據失敗: %w", batchNum, err)
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

		// 計算下一批的起始時间
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

// calculateBatchDuration 計算每批的時间跨度
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

// LoadFromCache 從统一目錄加載 CSV
func LoadFromCache(cacheKey string) ([]*exchange.Candle, error) {
	// 首先嘗試从统一目錄加載
	filename := filepath.Join("./data/kline", cacheKey+".csv")

	candles, err := loadCandlesFromFile(filename)
	if err == nil {
		return candles, nil
	}

	// 向后兼容：如果统一目錄没有，嘗試从旧的 backtest/cache 目錄
	legacyFilename := filepath.Join("backtest", "cache", cacheKey+".csv")
	return loadCandlesFromFile(legacyFilename)
}

// loadCandlesFromFile 从指定檔案加載 K 线數據
func loadCandlesFromFile(filename string) ([]*exchange.Candle, error) {

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	reader := csv.NewReader(file)

	// 跳過表头
	_, err = reader.Read()
	if err != nil {
		return nil, fmt.Errorf("读取表头失敗: %w", err)
	}

	// 使用流式读取，避免一次性加載整個檔案到記憶體
	// 限制最大读取數量，防止記憶體占用過大
	maxCandles := 1000000                         // 最多100万根K線
	candles := make([]*exchange.Candle, 0, 10000) // 預分配1万容量

	lineNum := 1
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 行失敗: %w", lineNum+1, err)
		}

		// 限制最大數量
		if len(candles) >= maxCandles {
			logger.Warn("⚠️ CSV 檔案過大，只读取前 %d 根K線", maxCandles)
			break
		}

		candle, err := parseCSVRecord(record)
		if err != nil {
			return nil, fmt.Errorf("解析第 %d 行失敗: %w", lineNum+1, err)
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
		return nil, fmt.Errorf("解析 timestamp 失敗: %w", err)
	}

	open, err := strconv.ParseFloat(record[1], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 open 失敗: %w", err)
	}

	high, err := strconv.ParseFloat(record[2], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 high 失敗: %w", err)
	}

	low, err := strconv.ParseFloat(record[3], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 low 失敗: %w", err)
	}

	close, err := strconv.ParseFloat(record[4], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 close 失敗: %w", err)
	}

	volume, err := strconv.ParseFloat(record[5], 64)
	if err != nil {
		return nil, fmt.Errorf("解析 volume 失敗: %w", err)
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

// SaveToCache 保存到统一目錄并录入數據库（無數據時不寫入，避免產生 K 線數為 0 的緩存條目）
func SaveToCache(cacheKey string, candles []*exchange.Candle) error {
	return SaveToCacheWithStorage(cacheKey, candles, nil)
}

// SaveToCacheWithStorage 保存到统一目錄（暂时不录入數據库以避免循环导入）
func SaveToCacheWithStorage(cacheKey string, candles []*exchange.Candle, storageService interface{}) error {
	if len(candles) == 0 {
		return nil
	}
	// 统一使用 ./data/kline 目錄
	cacheDir := "./data/kline"
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("創建缓存目錄失敗: %w", err)
	}

	filename := filepath.Join(cacheDir, cacheKey+".csv")
	file, err := os.Create(filename)
	if err != nil {
		return fmt.Errorf("創建缓存檔案失敗: %w", err)
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 寫入表头
	if err := writer.Write([]string{"timestamp", "open", "high", "low", "close", "volume", "symbol"}); err != nil {
		return fmt.Errorf("寫入表头失敗: %w", err)
	}

	// 寫入數據
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
			return fmt.Errorf("寫入數據失敗: %w", err)
		}
	}

	// 更新缓存索引（保持向后兼容）
	if err := updateCacheIndex(cacheKey, candles); err != nil {
		logger.Warn("⚠️ 更新缓存索引失敗: %v", err)
	}

	// 录入统一的 kline_files 數據库表
	// TODO: 录入统一的 kline_files 數據库表
	// 暂时跳过數據库录入以避免循环导入，后续通过迁移脚本處理

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
	// 格式1: BTCUSDT_1m_2023-01-01_2023-06-30 (4段)
	// 格式2: binance_BTCUSDT_1m_2023-01-01_2023-06-30 (5段，带交易所前缀)
	parts := strings.Split(cacheKey, "_")
	var symbol, interval, startStr, endStr string
	if len(parts) >= 5 {
		// 5段格式: exchange_symbol_interval_start_end
		symbol = parts[1]
		interval = parts[2]
		startStr = parts[3]
		endStr = parts[4]
	} else if len(parts) >= 4 {
		// 4段格式: symbol_interval_start_end
		symbol = parts[0]
		interval = parts[1]
		startStr = parts[2]
		endStr = parts[3]
	}

	start, _ := time.Parse("2006-01-02", startStr)
	end, _ := time.Parse("2006-01-02", endStr)

	// 計算檔案大小
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	fileInfo, err := os.Stat(filename)
	var sizeMB float64
	if err == nil && fileInfo != nil {
		sizeMB = float64(fileInfo.Size()) / 1024 / 1024
	}

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
