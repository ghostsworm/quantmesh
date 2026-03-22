package monitor

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
)

// KlineCollector K线数据收集器
type KlineCollector struct {
	config    *config.Config
	storage   storage.Storage               // 使用Storage接口
	exchanges map[string]exchange.IExchange // 使用IExchange接口
	mu        sync.RWMutex
	running   bool
	stopCh    chan struct{}
	wg        sync.WaitGroup

	// 主要交易所列表（前5大）
	majorExchanges []string
	// 主要币种列表
	majorSymbols []string
	// 数据存储目录
	dataDir string
}

// GetDataDir 获取数据目录
func (kc *KlineCollector) GetDataDir() string {
	return kc.dataDir
}

// KlineFileInfo CSV文件信息
type KlineFileInfo struct {
	Filename    string    `json:"filename"`
	Exchange    string    `json:"exchange"`
	Symbol      string    `json:"symbol"`
	Interval    string    `json:"interval"` // tick, 1m, 1h
	HasDepth    bool      `json:"has_depth"`
	FileSize    int64     `json:"file_size"`
	CreatedAt   time.Time `json:"created_at"`
	ModifiedAt  time.Time `json:"modified_at"`
	IsProtected bool      `json:"is_protected"`
}

// NewKlineCollector 创建K线数据收集器
func NewKlineCollector(cfg *config.Config, storageProv interface{ GetStorage() storage.Storage }, exchanges map[string]exchange.IExchange) *KlineCollector {
	var st storage.Storage
	if storageProv != nil {
		st = storageProv.GetStorage()
	}
	// 主要交易所（前5大）
	majorExchanges := []string{"binance", "okx", "bybit", "bitget", "gate"}

	// 主要币种
	majorSymbols := []string{
		"BTCUSDT", "ETHUSDT", "SOLUSDT", "BNBUSDT", "XRPUSDT",
		"DOGEUSDT", "ADAUSDT", "LTCUSDT", "LINKUSDT", "DOTUSDT",
		"MATICUSDT", "AVAXUSDT", "UNIUSDT", "ATOMUSDT", "APTUSDT",
	}

	return &KlineCollector{
		config:         cfg,
		storage:        st,
		exchanges:      exchanges,
		majorExchanges: majorExchanges,
		majorSymbols:   majorSymbols,
		dataDir:        "./data/kline",
		stopCh:         make(chan struct{}),
	}
}

// Start 启动收集器
func (kc *KlineCollector) Start() error {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if kc.running {
		return fmt.Errorf("收集器已在运行")
	}

	// 创建数据目录
	if err := os.MkdirAll(kc.dataDir, 0755); err != nil {
		return fmt.Errorf("创建数据目录失败: %w", err)
	}

	kc.running = true

	// 启动tick级数据收集（每个交易所每个币种）
	kc.wg.Add(1)
	go kc.collectTickData()

	// 启动分钟级数据收集（带订单深度）
	kc.wg.Add(1)
	go kc.collectMinuteData()

	// 启动小时级数据收集（带订单深度）
	kc.wg.Add(1)
	go kc.collectHourlyData()

	// 启动清理任务（每天清理7天前的未保护文件）
	kc.wg.Add(1)
	go kc.cleanupOldFiles()

	// 启动文件状态更新任务（每小时检查一次，更新已完成的文件状态）
	kc.wg.Add(1)
	go kc.updateFileStatusPeriodically()

	// 启动时同步所有现有文件到数据库
	go func() {
		if err := kc.syncAllExistingFiles(); err != nil {
			logger.Warn("⚠️ 启动时同步现有文件失败: %v", err)
		}
		// 然后更新已完成文件的状态
		if err := kc.updateCompletedKlineFiles(); err != nil {
			logger.Warn("⚠️ 启动时更新文件状态失败: %v", err)
		}
	}()

	logger.Info("✅ K线数据收集器已启动")
	return nil
}

// Stop 停止收集器
func (kc *KlineCollector) Stop() {
	kc.mu.Lock()
	defer kc.mu.Unlock()

	if !kc.running {
		return
	}

	close(kc.stopCh)
	kc.wg.Wait()
	kc.running = false
	logger.Info("✅ K线数据收集器已停止")
}

// collectTickData 收集tick级K线数据（最新一天）
func (kc *KlineCollector) collectTickData() {
	defer kc.wg.Done()

	ticker := time.NewTicker(1 * time.Minute) // 每分钟检查一次
	defer ticker.Stop()

	for {
		select {
		case <-kc.stopCh:
			return
		case <-ticker.C:
			kc.collectTickDataOnce()
		}
	}
}

// collectTickDataOnce 执行一次tick数据收集
func (kc *KlineCollector) collectTickDataOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now()
	oneDayAgo := now.Add(-24 * time.Hour)

	for _, exchangeName := range kc.majorExchanges {
		ex, ok := kc.exchanges[exchangeName]
		if !ok {
			continue
		}

		for _, symbol := range kc.majorSymbols {
			// 获取最新K线数据（1分钟K线作为tick数据）
			klines, err := ex.GetHistoricalKlines(ctx, symbol, "1m", 1440) // 24小时 * 60分钟
			if err != nil {
				logger.Warn("获取tick数据失败 %s %s: %v", exchangeName, symbol, err)
				continue
			}

			// 过滤最近24小时的数据
			var recentKlines []*exchange.Candle
			for _, kline := range klines {
				timestamp := time.Unix(kline.Timestamp/1000, 0)
				if timestamp.After(oneDayAgo) {
					recentKlines = append(recentKlines, kline)
				}
			}

			if len(recentKlines) == 0 {
				continue
			}

			// 保存到CSV
			filename := fmt.Sprintf("tick_%s_%s_%s.csv", exchangeName, symbol, now.Format("20060102"))
			filepath := filepath.Join(kc.dataDir, filename)
			if err := kc.saveKlinesToCSV(filepath, recentKlines, false); err != nil {
				logger.Warn("保存tick数据失败 %s: %v", filepath, err)
			} else {
				// 同步到数据库（新文件）
				if err := kc.syncKlineFileToDatabase(filename, false, true); err != nil {
					logger.Warn("⚠️ 同步文件到数据库失败: %s, 错误: %v", filename, err)
				}
			}
		}
	}
}

// collectMinuteData 收集分钟级K线数据（带订单深度）
func (kc *KlineCollector) collectMinuteData() {
	defer kc.wg.Done()

	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-kc.stopCh:
			return
		case <-ticker.C:
			kc.collectMinuteDataOnce()
		}
	}
}

// collectMinuteDataOnce 执行一次分钟级数据收集
func (kc *KlineCollector) collectMinuteDataOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now()

	for _, exchangeName := range kc.majorExchanges {
		ex, ok := kc.exchanges[exchangeName]
		if !ok {
			continue
		}

		for _, symbol := range kc.majorSymbols {
			// 获取最新1分钟K线
			klines, err := ex.GetHistoricalKlines(ctx, symbol, "1m", 1)
			if err != nil || len(klines) == 0 {
				continue
			}

			kline := klines[0]

			// 获取订单深度
			orderbook, err := ex.GetOrderBook(ctx, symbol, 20)
			if err != nil {
				logger.Warn("获取订单深度失败 %s %s: %v", exchangeName, symbol, err)
				// 即使没有订单深度也保存K线数据
			}

			// 保存到CSV（带订单深度）
			filename := fmt.Sprintf("1m_%s_%s_%s.csv", exchangeName, symbol, now.Format("20060102"))
			filepath := filepath.Join(kc.dataDir, filename)
			if err := kc.saveKlineWithDepthToCSV(filepath, kline, orderbook); err != nil {
				logger.Warn("保存分钟级数据失败 %s: %v", filepath, err)
			} else {
				// 同步到数据库（新文件）
				if err := kc.syncKlineFileToDatabase(filename, true, true); err != nil {
					logger.Warn("⚠️ 同步文件到数据库失败: %s, 错误: %v", filename, err)
				}
			}
		}
	}
}

// collectHourlyData 收集小时级K线数据（带订单深度）
func (kc *KlineCollector) collectHourlyData() {
	defer kc.wg.Done()

	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-kc.stopCh:
			return
		case <-ticker.C:
			kc.collectHourlyDataOnce()
		}
	}
}

// collectHourlyDataOnce 执行一次小时级数据收集
func (kc *KlineCollector) collectHourlyDataOnce() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	now := time.Now()

	for _, exchangeName := range kc.majorExchanges {
		ex, ok := kc.exchanges[exchangeName]
		if !ok {
			continue
		}

		for _, symbol := range kc.majorSymbols {
			// 获取最新1小时K线
			klines, err := ex.GetHistoricalKlines(ctx, symbol, "1h", 1)
			if err != nil || len(klines) == 0 {
				continue
			}

			kline := klines[0]

			// 获取订单深度
			orderbook, err := ex.GetOrderBook(ctx, symbol, 20)
			if err != nil {
				logger.Warn("获取订单深度失败 %s %s: %v", exchangeName, symbol, err)
			}

			// 保存到CSV（带订单深度）
			filename := fmt.Sprintf("1h_%s_%s_%s.csv", exchangeName, symbol, now.Format("20060102"))
			filepath := filepath.Join(kc.dataDir, filename)
			if err := kc.saveKlineWithDepthToCSV(filepath, kline, orderbook); err != nil {
				logger.Warn("保存小时级数据失败 %s: %v", filepath, err)
			} else {
				// 同步到数据库（新文件）
				if err := kc.syncKlineFileToDatabase(filename, true, true); err != nil {
					logger.Warn("⚠️ 同步文件到数据库失败: %s, 错误: %v", filename, err)
				}
			}
		}
	}
}

// saveKlinesToCSV 保存K线数据到CSV（不带订单深度）
func (kc *KlineCollector) saveKlinesToCSV(filepath string, klines []*exchange.Candle, shouldAppend bool) error {
	var file *os.File
	var err error

	if shouldAppend {
		file, err = os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	} else {
		file, err = os.Create(filepath)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 如果不是追加模式，写入表头
	if !shouldAppend {
		header := []string{"timestamp", "open", "high", "low", "close", "volume"}
		if err := writer.Write(header); err != nil {
			return err
		}
	}

	// 写入数据
	for _, kline := range klines {
		record := []string{
			fmt.Sprintf("%d", kline.Timestamp),
			fmt.Sprintf("%.8f", kline.Open),
			fmt.Sprintf("%.8f", kline.High),
			fmt.Sprintf("%.8f", kline.Low),
			fmt.Sprintf("%.8f", kline.Close),
			fmt.Sprintf("%.8f", kline.Volume),
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}

	return nil
}

// saveKlineWithDepthToCSV 保存K线数据到CSV（带订单深度）
func (kc *KlineCollector) saveKlineWithDepthToCSV(filepath string, kline *exchange.Candle, orderbook *exchange.OrderBook) error {
	var file *os.File
	var err error

	// 检查文件是否存在
	_, err = os.Stat(filepath)
	shouldAppend := err == nil

	if shouldAppend {
		file, err = os.OpenFile(filepath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	} else {
		file, err = os.Create(filepath)
	}
	if err != nil {
		return err
	}
	defer file.Close()

	writer := csv.NewWriter(file)
	defer writer.Flush()

	// 如果不是追加模式，写入表头
	if !shouldAppend {
		header := []string{
			"timestamp", "open", "high", "low", "close", "volume",
			"bid_price_1", "bid_qty_1", "ask_price_1", "ask_qty_1",
			"bid_price_2", "bid_qty_2", "ask_price_2", "ask_qty_2",
			"bid_price_3", "bid_qty_3", "ask_price_3", "ask_qty_3",
			"bid_price_4", "bid_qty_4", "ask_price_4", "ask_qty_4",
			"bid_price_5", "bid_qty_5", "ask_price_5", "ask_qty_5",
		}
		if err := writer.Write(header); err != nil {
			return err
		}
	}

	// 构建记录
	record := []string{
		fmt.Sprintf("%d", kline.Timestamp),
		fmt.Sprintf("%.8f", kline.Open),
		fmt.Sprintf("%.8f", kline.High),
		fmt.Sprintf("%.8f", kline.Low),
		fmt.Sprintf("%.8f", kline.Close),
		fmt.Sprintf("%.8f", kline.Volume),
	}

	// 添加订单深度数据（前5档）
	if orderbook != nil {
		for i := 0; i < 5; i++ {
			if i < len(orderbook.Bids) {
				record = append(record,
					fmt.Sprintf("%.8f", orderbook.Bids[i].Price),
					fmt.Sprintf("%.8f", orderbook.Bids[i].Quantity),
				)
			} else {
				record = append(record, "", "")
			}
			if i < len(orderbook.Asks) {
				record = append(record,
					fmt.Sprintf("%.8f", orderbook.Asks[i].Price),
					fmt.Sprintf("%.8f", orderbook.Asks[i].Quantity),
				)
			} else {
				record = append(record, "", "")
			}
		}
	} else {
		// 没有订单深度，填充空值
		for i := 0; i < 10; i++ {
			record = append(record, "")
		}
	}

	if err := writer.Write(record); err != nil {
		return err
	}

	return nil
}

// cleanupOldFiles 清理7天前的未保护文件
func (kc *KlineCollector) cleanupOldFiles() {
	defer kc.wg.Done()

	ticker := time.NewTicker(24 * time.Hour) // 每天执行一次
	defer ticker.Stop()

	// 启动时立即执行一次
	kc.cleanupOldFilesOnce()

	for {
		select {
		case <-kc.stopCh:
			return
		case <-ticker.C:
			kc.cleanupOldFilesOnce()
		}
	}
}

// cleanupOldFilesOnce 执行一次清理
func (kc *KlineCollector) cleanupOldFilesOnce() {
	sevenDaysAgo := time.Now().AddDate(0, 0, -7)

	// 获取所有保护的文件列表
	protectedFiles, err := kc.getProtectedFiles()
	if err != nil {
		logger.Warn("获取保护文件列表失败: %v", err)
		return
	}
	protectedSet := make(map[string]bool)
	for _, f := range protectedFiles {
		protectedSet[f] = true
	}

	// 遍历数据目录
	files, err := os.ReadDir(kc.dataDir)
	if err != nil {
		logger.Warn("读取数据目录失败: %v", err)
		return
	}

	deletedCount := 0
	for _, file := range files {
		if file.IsDir() {
			continue
		}

		filename := file.Name()
		if !kc.isKlineFile(filename) {
			continue
		}

		// 检查是否被保护
		if protectedSet[filename] {
			continue
		}

		// 检查文件修改时间
		info, err := file.Info()
		if err != nil {
			continue
		}

		if info.ModTime().Before(sevenDaysAgo) {
			filepath := filepath.Join(kc.dataDir, filename)
			if err := os.Remove(filepath); err != nil {
				logger.Warn("删除旧文件失败 %s: %v", filepath, err)
			} else {
				deletedCount++
				logger.Info("🗑️ 删除旧文件: %s", filename)
			}
		}
	}

	if deletedCount > 0 {
		logger.Info("✅ 清理完成，删除了 %d 个旧文件", deletedCount)
	}
}

// isKlineFile 检查是否是K线数据文件
func (kc *KlineCollector) isKlineFile(filename string) bool {
	return isKlineFilename(filename)
}

// getProtectedFiles 获取保护的文件列表
func (kc *KlineCollector) getProtectedFiles() ([]string, error) {
	if kc.storage == nil {
		return []string{}, nil
	}

	return kc.storage.GetProtectedKlineFiles()
}

// ListFiles 列出所有K线数据文件
func (kc *KlineCollector) ListFiles() ([]KlineFileInfo, error) {
	files, err := os.ReadDir(kc.dataDir)
	if err != nil {
		return nil, err
	}

	var fileInfos []KlineFileInfo
	protectedFiles, _ := kc.getProtectedFiles()
	protectedSet := make(map[string]bool)
	for _, f := range protectedFiles {
		protectedSet[f] = true
	}

	for _, file := range files {
		if file.IsDir() || !kc.isKlineFile(file.Name()) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		fileInfo := kc.parseFilename(file.Name())
		fileInfo.FileSize = info.Size()
		fileInfo.ModifiedAt = info.ModTime()
		fileInfo.CreatedAt = info.ModTime() // 如果没有创建时间，使用修改时间
		fileInfo.IsProtected = protectedSet[file.Name()]

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos, nil
}

// parseFilename 解析文件名
func (kc *KlineCollector) parseFilename(filename string) KlineFileInfo {
	return parseKlineFilename(filename)
}

// DefaultKlineDataDir 默认 K 线数据目录
const DefaultKlineDataDir = "./data/kline"

// ListKlineFilesFromDir 从指定目录扫描并列出 K 线文件（不依赖 KlineCollector 实例）
// 当 KlineCollector 未初始化时，可由 web 层调用此函数获取文件列表
func ListKlineFilesFromDir(dataDir string, st storage.Storage) ([]KlineFileInfo, error) {
	files, err := os.ReadDir(dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []KlineFileInfo{}, nil
		}
		return nil, err
	}

	var fileInfos []KlineFileInfo
	protectedSet := make(map[string]bool)
	if st != nil {
		protectedFiles, _ := st.GetProtectedKlineFiles()
		for _, f := range protectedFiles {
			protectedSet[f] = true
		}
	}

	for _, file := range files {
		if file.IsDir() || !isKlineFilename(file.Name()) {
			continue
		}

		info, err := file.Info()
		if err != nil {
			continue
		}

		fileInfo := parseKlineFilename(file.Name())
		fileInfo.FileSize = info.Size()
		fileInfo.ModifiedAt = info.ModTime()
		fileInfo.CreatedAt = info.ModTime()
		fileInfo.IsProtected = protectedSet[file.Name()]

		fileInfos = append(fileInfos, fileInfo)
	}

	return fileInfos, nil
}

// isKlineFilename 检查是否是 K 线数据文件名
func isKlineFilename(filename string) bool {
	return len(filename) > 4 && filename[len(filename)-4:] == ".csv" &&
		(filename[:4] == "tick" || (len(filename) >= 2 && (filename[:2] == "1m" || filename[:2] == "1h")))
}

// parseKlineFilename 解析 K 线文件名
func parseKlineFilename(filename string) KlineFileInfo {
	info := KlineFileInfo{Filename: filename}
	parts := splitFilename(filename)
	if len(parts) >= 4 {
		info.Interval = parts[0]
		info.Exchange = parts[1]
		info.Symbol = parts[2]
		info.HasDepth = info.Interval == "1m" || info.Interval == "1h"
	}
	return info
}

// splitFilename 分割文件名
func splitFilename(filename string) []string {
	// 移除.csv扩展名
	name := filename
	if len(name) > 4 && name[len(name)-4:] == ".csv" {
		name = name[:len(name)-4]
	}

	var parts []string
	current := ""
	for _, char := range name {
		if char == '_' {
			if current != "" {
				parts = append(parts, current)
				current = ""
			}
		} else {
			current += string(char)
		}
	}
	if current != "" {
		parts = append(parts, current)
	}

	return parts
}

// syncKlineFileToDatabase 同步 K 线文件信息到数据库
func (kc *KlineCollector) syncKlineFileToDatabase(filename string, hasDepth bool, isNewFile bool) error {
	if kc.storage == nil {
		return nil // 无存储服务，跳过同步
	}

	// 解析文件名获取元信息
	parts := splitFilename(filename)
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

	// 判断状态：当天文件为 collecting，其他为 completed
	today := time.Now().Format("20060102")
	status := "completed"
	var endTime *time.Time
	if dateStr == today {
		status = "collecting"
	} else {
		// 昨天或更早的文件，设置结束时间
		endOfDay := startTime.Add(24*time.Hour - time.Second)
		endTime = &endOfDay
	}

	// 获取文件信息
	filePath := filepath.Join(kc.dataDir, filename)
	stat, err := os.Stat(filePath)
	if err != nil {
		return fmt.Errorf("获取文件信息失败: %w", err)
	}

	// 如果是新文件，创建数据库记录
	if isNewFile {
		// 检查是否已存在记录
		sqliteStorage, ok := kc.storage.(*storage.SQLiteStorage)
		if !ok {
			return nil // 非 SQLite 存储，跳过
		}

		existing, err := sqliteStorage.GetKlineFileByFilename(filename)
		if err != nil {
			return fmt.Errorf("查询现有记录失败: %w", err)
		}
		if existing != nil {
			return nil // 已存在，跳过
		}

		kf := &storage.KlineFile{
			Filename:    filename,
			Exchange:    exchange,
			Symbol:      symbol,
			Interval:    interval,
			StartTime:   startTime,
			EndTime:     endTime,
			Status:      status,
			HasDepth:    hasDepth,
			CandleCount: 0, // 初始为0，后续更新
			FileSize:    stat.Size(),
			Source:      "collector",
		}

		if err := sqliteStorage.CreateKlineFile(kf); err != nil {
			return fmt.Errorf("创建K线文件记录失败: %w", err)
		}

		logger.Debug("📝 K线文件已记录到数据库: %s", filename)
	}

	return nil
}

// updateCompletedKlineFiles 更新已完成的 K 线文件状态
// 每日结束时或服务启动时调用，将昨天的文件状态更新为 completed
func (kc *KlineCollector) updateCompletedKlineFiles() error {
	if kc.storage == nil {
		return nil
	}

	sqliteStorage, ok := kc.storage.(*storage.SQLiteStorage)
	if !ok {
		return nil
	}

	// 获取 collecting 状态的文件
	files, err := sqliteStorage.ListKlineFiles(&storage.KlineFileFilter{
		Status: "collecting",
		Source: "collector",
	})
	if err != nil {
		return fmt.Errorf("查询采集中文件失败: %w", err)
	}

	today := time.Now().Format("20060102")
	updated := 0

	for _, kf := range files {
		// 解析文件名中的日期
		parts := splitFilename(kf.Filename)
		if len(parts) < 4 {
			continue
		}
		dateStr := parts[3]

		// 如果不是今天的文件，更新为已完成
		if dateStr != today {
			// 设置结束时间为当天结束
			endTime := kf.StartTime.Add(24*time.Hour - time.Second)

			// 获取文件的实际统计信息
			filePath := filepath.Join(kc.dataDir, kf.Filename)
			stat, err := os.Stat(filePath)
			if err != nil {
				logger.Warn("⚠️ 文件 %s 不存在，跳过状态更新", kf.Filename)
				continue
			}

			// 计算 K 线数量（简单估算：文件大小/平均行长度）
			candleCount := estimateCandleCount(stat.Size(), kf.HasDepth)

			if err := sqliteStorage.UpdateKlineFileStatus(kf.Filename, "completed", &endTime, candleCount, stat.Size()); err != nil {
				logger.Warn("⚠️ 更新文件状态失败: %s, 错误: %v", kf.Filename, err)
				continue
			}

			logger.Info("✅ 更新文件状态为已完成: %s", kf.Filename)
			updated++
		}
	}

	if updated > 0 {
		logger.Info("📊 更新了 %d 个文件状态为已完成", updated)
	}

	return nil
}

// estimateCandleCount 估算 K 线数量
func estimateCandleCount(fileSize int64, hasDepth bool) int {
	if fileSize <= 0 {
		return 0
	}

	// 估算每行平均字节数
	avgBytesPerLine := 80 // 基础估算
	if hasDepth {
		avgBytesPerLine = 200 // 带深度数据行更长
	}

	// 减去表头行
	estimatedLines := int(fileSize) / avgBytesPerLine
	if estimatedLines > 0 {
		estimatedLines-- // 减去表头
	}

	return estimatedLines
}

// updateFileStatusPeriodically 定期更新文件状态
func (kc *KlineCollector) updateFileStatusPeriodically() {
	defer kc.wg.Done()

	ticker := time.NewTicker(1 * time.Hour) // 每小时检查一次
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := kc.updateCompletedKlineFiles(); err != nil {
				logger.Warn("⚠️ 定期更新文件状态失败: %v", err)
			}
		case <-kc.stopCh:
			logger.Info("📊 文件状态更新任务已停止")
			return
		}
	}
}

// syncAllExistingFiles 启动时同步所有现有文件到数据库
// 扫描 dataDir 中的所有 .csv 文件，将不在数据库中的文件添加进去
func (kc *KlineCollector) syncAllExistingFiles() error {
	if kc.storage == nil {
		return nil
	}

	sqliteStorage, ok := kc.storage.(*storage.SQLiteStorage)
	if !ok {
		return nil
	}

	// 扫描数据目录
	entries, err := os.ReadDir(kc.dataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 目录不存在，跳过
		}
		return fmt.Errorf("读取数据目录失败: %w", err)
	}

	synced := 0
	skipped := 0
	errors := 0

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		filename := entry.Name()
		if !strings.HasSuffix(filename, ".csv") {
			continue
		}

		// 检查是否已存在记录
		existing, err := sqliteStorage.GetKlineFileByFilename(filename)
		if err != nil {
			logger.Warn("⚠️ 查询文件 %s 失败: %v", filename, err)
			errors++
			continue
		}
		if existing != nil {
			skipped++
			continue // 已存在，跳过
		}

		// 解析文件名
		parts := splitFilename(filename)
		if len(parts) < 4 {
			logger.Debug("⚠️ 文件名格式不正确，跳过: %s", filename)
			continue
		}

		interval := parts[0]
		exchange := parts[1]
		symbol := parts[2]
		dateStr := parts[3]

		// 解析日期
		startTime, err := time.Parse("20060102", dateStr)
		if err != nil {
			logger.Debug("⚠️ 解析日期失败，跳过: %s", filename)
			continue
		}

		// 获取文件信息
		filePath := filepath.Join(kc.dataDir, filename)
		stat, err := os.Stat(filePath)
		if err != nil {
			logger.Warn("⚠️ 获取文件信息失败: %s, 错误: %v", filename, err)
			errors++
			continue
		}

		// 判断状态
		today := time.Now().Format("20060102")
		status := "completed"
		var endTime *time.Time
		if dateStr == today {
			status = "collecting"
		} else {
			endOfDay := startTime.Add(24*time.Hour - time.Second)
			endTime = &endOfDay
		}

		// 判断是否有深度数据
		hasDepth := interval == "1m" || interval == "1h"

		// 估算 K 线数量
		candleCount := estimateCandleCount(stat.Size(), hasDepth)

		// 创建记录
		kf := &storage.KlineFile{
			Filename:    filename,
			Exchange:    exchange,
			Symbol:      symbol,
			Interval:    interval,
			StartTime:   startTime,
			EndTime:     endTime,
			Status:      status,
			HasDepth:    hasDepth,
			CandleCount: candleCount,
			FileSize:    stat.Size(),
			Source:      "collector", // 假设是 collector 来源
		}

		if err := sqliteStorage.CreateKlineFile(kf); err != nil {
			logger.Warn("⚠️ 创建文件记录失败: %s, 错误: %v", filename, err)
			errors++
			continue
		}

		synced++
	}

	if synced > 0 || errors > 0 {
		logger.Info("📊 同步现有文件完成: 新增 %d 个, 跳过 %d 个, 失败 %d 个", synced, skipped, errors)
	}

	return nil
}
