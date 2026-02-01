package backtest

import (
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
	for name, entry := range index {
		caches = append(caches, CacheInfo{
			Name:     name,
			Symbol:   entry.Symbol,
			Interval: entry.Interval,
			Start:    entry.Start,
			End:      entry.End,
			Candles:  entry.Candles,
			SizeMB:   entry.SizeMB,
			Created:  entry.Created,
		})
	}

	return caches, nil
}

// ClearCache 清理所有缓存
func ClearCache() error {
	cacheDir := filepath.Join("backtest", "cache")
	if err := os.RemoveAll(cacheDir); err != nil {
		return fmt.Errorf("清理缓存失败: %w", err)
	}
	return nil
}

// DeleteCache 删除指定缓存
func DeleteCache(cacheKey string) error {
	// 删除 CSV 文件
	filename := filepath.Join("backtest", "cache", cacheKey+".csv")
	if err := os.Remove(filename); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("删除缓存文件失败: %w", err)
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

// GetCacheStats 获取缓存统计
func GetCacheStats() (CacheStats, error) {
	cacheDir := filepath.Join("backtest", "cache")
	files, err := filepath.Glob(filepath.Join(cacheDir, "*.csv"))
	if err != nil {
		return CacheStats{}, fmt.Errorf("读取缓存目录失败: %w", err)
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

// CleanOldCache 清理过期缓存（超过指定天数未访问）
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
				return fmt.Errorf("删除过期缓存 %s 失败: %w", cache.Name, err)
			}
			deletedCount++
		}
	}

	if deletedCount > 0 {
		fmt.Printf("✅ 已清理 %d 个过期缓存\n", deletedCount)
	}

	return nil
}
