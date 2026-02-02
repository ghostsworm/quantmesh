package backtest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

// CacheStats 缓存统计
type CacheStats struct {
	FileCount int     `json:"file_count"`
	TotalSize int64   `json:"total_size"`
	SizeMB    float64 `json:"size_mb"`
}

// ListCache 列出所有缓存
func ListCache() ([]CacheInfo, error) {
	indexFile := filepath.Join("backtest", "cache", "cache_index.json")

	// 读取索引文件
	data, err := os.ReadFile(indexFile)
	if err != nil {
		if os.IsNotExist(err) {
			return []CacheInfo{}, nil
		}
		return nil, fmt.Errorf("读取缓存索引失败: %w", err)
	}

	index := make(map[string]CacheIndexEntry)
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("解析缓存索引失败: %w", err)
	}

	caches := make([]CacheInfo, 0, len(index))
	cacheDir := filepath.Join("backtest", "cache")
	for name, entry := range index {
		candles := entry.Candles
		sizeMB := entry.SizeMB
		// 索引中 K 線數為 0 時，嘗試從 CSV 重新統計（自愈舊數據或錯誤寫入）
		if entry.Candles == 0 {
			if n, sz := countCsvLinesAndSize(filepath.Join(cacheDir, name+".csv")); n >= 0 {
				candles = n
				sizeMB = float64(sz) / 1024 / 1024
			}
		}
		caches = append(caches, CacheInfo{
			Name:     name,
			Symbol:   entry.Symbol,
			Interval: entry.Interval,
			Start:    entry.Start,
			End:      entry.End,
			Candles:  candles,
			SizeMB:   sizeMB,
			Created:  entry.Created,
		})
	}

	return caches, nil
}

// countCsvLinesAndSize 統計 CSV 數據行數（不含表頭）與文件字節數。無文件或錯誤時返回 -1, 0。
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
		return fmt.Errorf("清理缓存失败: %w", err)
	}
	return nil
}

// DeleteCache 刪除指定缓存
func DeleteCache(cacheKey string) error {
	// 刪除 CSV 文件
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("刪除缓存文件失败: %w", err)
	}

	// 更新索引
	indexFile := filepath.Join("backtest", "cache", "cache_index.json")
	data, err := os.ReadFile(indexFile)
	if err != nil {
		return nil // 索引文件不存在，忽略
	}

	index := make(map[string]CacheIndexEntry)
	if err := json.Unmarshal(data, &index); err != nil {
		return fmt.Errorf("解析缓存索引失败: %w", err)
	}

	delete(index, cacheKey)

	// 保存索引
	data, err = json.MarshalIndent(index, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(indexFile, data, 0644)
}

// GetCacheStats 獲取缓存统计
func GetCacheStats() (CacheStats, error) {
	cacheDir := filepath.Join("backtest", "cache")
	files, err := filepath.Glob(filepath.Join(cacheDir, "*.csv"))
	if err != nil {
		return CacheStats{}, fmt.Errorf("读取缓存目錄失败: %w", err)
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
				return fmt.Errorf("刪除過期缓存 %s 失败: %w", cache.Name, err)
			}
			deletedCount++
		}
	}

	if deletedCount > 0 {
		fmt.Printf("✅ 已清理 %d 個過期缓存\n", deletedCount)
	}

	return nil
}
