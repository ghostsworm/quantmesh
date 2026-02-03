package storage

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"quantmesh/logger"
)

// MigrateExistingKlineFiles 扫描现有文件并导入 kline_files 表
// 这是一个一次性迁移函数，用于将现有的 K 线文件导入新的统一管理系统
func (s *SQLiteStorage) MigrateExistingKlineFiles() error {
	logger.Info("🔄 开始迁移现有 K 线文件到统一管理系统...")

	// 1. 扫描 ./data/kline 目录
	if err := s.scanKlineDirectory("./data/kline", "collector"); err != nil {
		return fmt.Errorf("扫描 ./data/kline 目录失败: %w", err)
	}

	// 2. 扫描 backtest/cache 目录
	if err := s.scanBacktestCacheDirectory("backtest/cache"); err != nil {
		return fmt.Errorf("扫描 backtest/cache 目录失败: %w", err)
	}

	logger.Info("✅ K 线文件迁移完成")
	return nil
}

// scanKlineDirectory 扫描 KlineCollector 目录
func (s *SQLiteStorage) scanKlineDirectory(dir, source string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Info("📁 目录 %s 不存在，跳过", dir)
		return nil
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}

	count := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".csv") {
			continue
		}

		// 解析 KlineCollector 文件名：{interval}_{exchange}_{symbol}_{date}.csv
		if err := s.importKlineCollectorFile(dir, file.Name(), source); err != nil {
			logger.Warn("⚠️ 导入文件失败: %s, 错误: %v", file.Name(), err)
			continue
		}
		count++
	}

	logger.Info("📊 从 %s 导入了 %d 个 K 线文件", dir, count)
	return nil
}

// scanBacktestCacheDirectory 扫描回测缓存目录
func (s *SQLiteStorage) scanBacktestCacheDirectory(dir string) error {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		logger.Info("📁 目录 %s 不存在，跳过", dir)
		return nil
	}

	files, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("读取目录失败: %w", err)
	}

	count := 0
	moved := 0
	for _, file := range files {
		if file.IsDir() || !strings.HasSuffix(file.Name(), ".csv") {
			continue
		}

		// 解析回测缓存文件名：{exchange}_{symbol}_{interval}_{start}_{end}.csv
		if err := s.importBacktestCacheFile(dir, file.Name()); err != nil {
			logger.Warn("⚠️ 导入文件失败: %s, 错误: %v", file.Name(), err)
			continue
		}

		// 移动文件到统一目录
		oldPath := filepath.Join(dir, file.Name())
		newPath := filepath.Join("./data/kline", file.Name())
		if err := s.moveFileToUnifiedDir(oldPath, newPath); err != nil {
			logger.Warn("⚠️ 移动文件失败: %s, 错误: %v", file.Name(), err)
		} else {
			moved++
		}
		count++
	}

	logger.Info("📊 从 %s 导入了 %d 个回测缓存文件，移动了 %d 个", dir, count, moved)
	return nil
}

// importKlineCollectorFile 导入 KlineCollector 文件
func (s *SQLiteStorage) importKlineCollectorFile(dir, filename, source string) error {
	// 解析文件名：{interval}_{exchange}_{symbol}_{date}.csv
	parts := strings.Split(strings.TrimSuffix(filename, ".csv"), "_")
	if len(parts) < 4 {
		return fmt.Errorf("文件名格式不正确: %s", filename)
	}

	interval := parts[0]
	exchange := parts[1]
	symbol := parts[2]
	dateStr := parts[3]

	// 解析日期
	startTime, err := time.Parse("20060102", dateStr)
	if err != nil {
		return fmt.Errorf("解析日期失败: %s", dateStr)
	}

	// 判断文件是否已完成（当天文件为 collecting，其他为 completed）
	today := time.Now().Format("20060102")
	status := "completed"
	var endTime *time.Time
	if dateStr == today {
		status = "collecting"
	} else {
		// 设置结束时间为当天 23:59:59
		endOfDay := startTime.Add(24*time.Hour - time.Second)
		endTime = &endOfDay
	}

	// 分析文件内容获取列数、行数、文件大小
	filePath := filepath.Join(dir, filename)
	hasDepth, candleCount, fileSize, err := s.analyzeCSVFile(filePath)
	if err != nil {
		return fmt.Errorf("分析文件失败: %w", err)
	}

	// 检查是否已存在
	existing, err := s.GetKlineFileByFilename(filename)
	if err != nil {
		return fmt.Errorf("查询现有记录失败: %w", err)
	}
	if existing != nil {
		logger.Debug("📝 文件 %s 已存在，跳过", filename)
		return nil
	}

	// 创建记录
	kf := &KlineFile{
		Filename:    filename,
		Exchange:    exchange,
		Symbol:      symbol,
		Interval:    interval,
		StartTime:   startTime,
		EndTime:     endTime,
		Status:      status,
		HasDepth:    hasDepth,
		CandleCount: candleCount,
		FileSize:    fileSize,
		Source:      source,
	}

	if err := s.CreateKlineFile(kf); err != nil {
		return fmt.Errorf("创建记录失败: %w", err)
	}

	return nil
}

// importBacktestCacheFile 导入回测缓存文件
func (s *SQLiteStorage) importBacktestCacheFile(dir, filename string) error {
	// 解析文件名：{exchange}_{symbol}_{interval}_{start}_{end}.csv
	parts := strings.Split(strings.TrimSuffix(filename, ".csv"), "_")
	if len(parts) < 5 {
		return fmt.Errorf("文件名格式不正确: %s", filename)
	}

	exchange := parts[0]
	symbol := parts[1]
	interval := parts[2]
	startDateStr := parts[3]
	endDateStr := parts[4]

	// 解析日期
	startTime, err := time.Parse("2006-01-02", startDateStr)
	if err != nil {
		return fmt.Errorf("解析开始日期失败: %s", startDateStr)
	}
	endTime, err := time.Parse("2006-01-02", endDateStr)
	if err != nil {
		return fmt.Errorf("解析结束日期失败: %s", endDateStr)
	}
	// 设置为当天结束时间
	endTime = endTime.Add(24*time.Hour - time.Second)

	// 分析文件内容
	filePath := filepath.Join(dir, filename)
	hasDepth, candleCount, fileSize, err := s.analyzeCSVFile(filePath)
	if err != nil {
		return fmt.Errorf("分析文件失败: %w", err)
	}

	// 检查是否已存在
	existing, err := s.GetKlineFileByFilename(filename)
	if err != nil {
		return fmt.Errorf("查询现有记录失败: %w", err)
	}
	if existing != nil {
		logger.Debug("📝 文件 %s 已存在，跳过", filename)
		return nil
	}

	// 创建记录
	kf := &KlineFile{
		Filename:    filename,
		Exchange:    exchange,
		Symbol:      symbol,
		Interval:    interval,
		StartTime:   startTime,
		EndTime:     &endTime,
		Status:      "completed",
		HasDepth:    hasDepth,
		CandleCount: candleCount,
		FileSize:    fileSize,
		Source:      "backtest_cache",
	}

	if err := s.CreateKlineFile(kf); err != nil {
		return fmt.Errorf("创建记录失败: %w", err)
	}

	return nil
}

// analyzeCSVFile 分析 CSV 文件，返回 (是否有深度, K线数量, 文件大小, 错误)
func (s *SQLiteStorage) analyzeCSVFile(filePath string) (bool, int, int64, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return false, 0, 0, fmt.Errorf("打开文件失败: %w", err)
	}
	defer file.Close()

	// 获取文件大小
	stat, err := file.Stat()
	if err != nil {
		return false, 0, 0, fmt.Errorf("获取文件信息失败: %w", err)
	}
	fileSize := stat.Size()

	reader := csv.NewReader(file)

	// 读取表头判断列数
	header, err := reader.Read()
	if err != nil {
		return false, 0, fileSize, fmt.Errorf("读取表头失败: %w", err)
	}

	columnCount := len(header)
	hasDepth := false

	// 判断是否有深度数据
	if columnCount == 26 {
		hasDepth = true
	} else if columnCount == 6 || columnCount == 7 {
		hasDepth = false
	} else {
		logger.Warn("⚠️ 文件 %s 列数异常: %d", filepath.Base(filePath), columnCount)
	}

	// 计算行数（不包含表头）
	candleCount := 0
	for {
		_, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			// 忽略解析错误，继续计数
			continue
		}
		candleCount++
	}

	return hasDepth, candleCount, fileSize, nil
}

// moveFileToUnifiedDir 移动文件到统一目录
func (s *SQLiteStorage) moveFileToUnifiedDir(oldPath, newPath string) error {
	// 确保目标目录存在
	targetDir := filepath.Dir(newPath)
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		return fmt.Errorf("创建目标目录失败: %w", err)
	}

	// 检查目标文件是否已存在
	if _, err := os.Stat(newPath); err == nil {
		logger.Debug("📝 目标文件 %s 已存在，跳过移动", newPath)
		return nil
	}

	// 移动文件
	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("移动文件失败: %w", err)
	}

	return nil
}

// RunKlineFilesMigration 运行 K 线文件迁移（供外部调用）
func RunKlineFilesMigration(storage *SQLiteStorage) error {
	return storage.MigrateExistingKlineFiles()
}
