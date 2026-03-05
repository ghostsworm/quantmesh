package backtest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CacheInfo 缓存信息
type CacheInfo struct {
	Name     string    `json:"name"`
	Symbol   string    `json:"symbol"`
	Interval string    `json:"interval"`
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	Candles  int       `json:"candles"`
	SizeMB   float64   `json:"size_mb"`
	Created  time.Time `json:"created"`
}

// CacheStats 缓存統計
type CacheStats struct {
	FileCount int     `json:"file_count"`
	TotalSize int64   `json:"total_size"`
	SizeMB    float64 `json:"size_mb"`
}

// ListCache 列出所有缓存
func ListCache() ([]CacheInfo, error) {
	indexFile := filepath.Join("backtest", "cache", "cache_index.json")

	// 读取索引檔案
	data, err := os.ReadFile(indexFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []CacheInfo{}, nil
		}
		return nil, fmt.Errorf("读取缓存索引失敗: %w", err)
	}

	index := make(map[string]CacheIndexEntry)
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("解析缓存索引失敗: %w", err)
	}

	caches := make([]CacheInfo, 0, len(index))
	cacheDir := filepath.Join("backtest", "cache")
	for name, entry := range index {
		candles := entry.Candles
		sizeMB := entry.SizeMB
		symbol := entry.Symbol
		interval := entry.Interval
		start := entry.Start
		end := entry.End

		// 索引中 K 線數為 0 時，嘗試從 CSV 重新統計（自愈舊數據或錯誤寫入）
		if entry.Candles == 0 {
			if n, sz := countCsvLinesAndSize(filepath.Join(cacheDir, name+".csv")); n >= 0 {
				candles = n
				sizeMB = float64(sz) / 1024 / 1024
			}
		}

		// 索引中 symbol/interval/start/end 為空時，從缓存名解析（自愈舊數據）
		// 格式1: BTCUSDT_1m_2023-01-01_2023-06-30 (4段)
		// 格式2: binance_BTCUSDT_1m_2023-01-01_2023-06-30 (5段，带交易所前缀)
		if symbol == "" || interval == "" || start.IsZero() || end.IsZero() {
			parts := strings.Split(name, "_")
			if len(parts) >= 5 {
				// 5段格式: exchange_symbol_interval_start_end
				symbol = parts[1]
				interval = parts[2]
				start, _ = time.Parse("2006-01-02", parts[3])
				end, _ = time.Parse("2006-01-02", parts[4])
			} else if len(parts) >= 4 {
				// 4段格式: symbol_interval_start_end
				symbol = parts[0]
				interval = parts[1]
				start, _ = time.Parse("2006-01-02", parts[2])
				end, _ = time.Parse("2006-01-02", parts[3])
			}
		}

		caches = append(caches, CacheInfo{
			Name:     name,
			Symbol:   symbol,
			Interval: interval,
			Start:    start,
			End:      end,
			Candles:  candles,
			SizeMB:   sizeMB,
			Created:  entry.Created,
		})
	}

	return caches, nil
}

// countCsvLinesAndSize 統計 CSV 數據行數（不含表頭）與檔案字節數。無檔案或錯誤時返回 -1, 0。
func countCsvLinesAndSize(csvPath string) (dataLines int, fileSize int64) {
	fi, err := os.Stat(csvPath)
	if err != nil {
		return -1, 0
	}
	fileSize = fi.Size()
	f, err := os.Open(csvPath)
	if err != nil {
		return -1, fileSize
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	lines := 0
	for sc.Scan() {
		lines++
	}
	if err := sc.Err(); err != nil {
		return -1, fileSize
	}
	if lines <= 1 {
		return 0, fileSize
	}
	return lines - 1, fileSize
}

// ClearCache 清理所有缓存
func ClearCache() error {
	cacheDir := filepath.Join("backtest", "cache")
	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("清理缓存失敗: %w", err)
	}
	return nil
}

// DeleteCache 刪除指定缓存
func DeleteCache(cacheKey string) error {
	// 刪除 CSV 檔案
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("刪除缓存檔案失敗: %w", err)
	}

	// 更新索引
	indexFile := filepath.Join("backtest", "cache", "cache_index.json")
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil // 索引檔案不存在，忽略
	}

	index := make(map[string]CacheIndexEntry)
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("解析缓存索引失敗: %w", err)
	}

	delete(index, cacheKey)

	// 保存索引
	data, err = json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(indexFile, data, 0644)
}

// GetCacheStats 獲取缓存統計
func GetCacheStats() (CacheStats, error) {
	cacheDir := filepath.Join("backtest", "cache")
	files, err := filepath.Glob(filepath.Join(cacheDir, "*.csv"))
	if err != nil {
		return CacheStats{}, fmt.Errorf("读取缓存目錄失敗: %w", err)
	}

	var totalSize int64
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			continue
		}
		totalSize += info.Size()
	}

	return CacheStats{
		FileCount: len(files),
		TotalSize: totalSize,
		SizeMB:    float64(totalSize) / 1024 / 1024,
	}, nil
}

// CleanOldCache 清理過期缓存（超過指定天數未访问）
func CleanOldCache(days int) error {
	caches, err := ListCache()
	if err != nil {
		return err
	}

	cutoffTime := time.Now().AddDate(0, 0, -days)
	deletedCount := 0

	for _, cache := range caches {
		if cache.Created.Before(cutoffTime) {
			if err := DeleteCache(cache.Name); err != nil {
				return fmt.Errorf("刪除過期缓存 %s 失敗: %w", cache.Name, err)
			}
			deletedCount++
		}
	}

	if deletedCount > 0 {
		fmt.Printf("✅ 已清理 %d 個過期缓存\n", deletedCount)
	}

	return nil
}
