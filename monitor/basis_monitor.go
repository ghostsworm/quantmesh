package monitor

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
)

// BasisMonitor 价差监控器
type BasisMonitor struct {
	db           storage.Storage
	exchange     exchange.IExchange
	exchangeName string
	symbols      []string
	interval     time.Duration
	ctx          context.Context
	cancel       context.CancelFunc

	// 缓存最新价差数据
	latestData map[string]*storage.BasisData
	dataMutex  sync.RWMutex

	// 事件回调
	onBasisUpdate func(*storage.BasisData)
}

// NewBasisMonitor 创建价差监控器
func NewBasisMonitor(db storage.Storage, ex exchange.IExchange, symbols []string, intervalMinutes int) *BasisMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	// 默认监控主流交易对
	if len(symbols) == 0 {
		symbols = []string{
			"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT",
		}
	}

	interval := time.Duration(intervalMinutes) * time.Minute
	if interval <= 0 {
		interval = 1 * time.Minute // 默认1分钟
	}

	return &BasisMonitor{
		db:           db,
		exchange:     ex,
		exchangeName: ex.GetName(),
		symbols:      symbols,
		interval:     interval,
		ctx:          ctx,
		cancel:       cancel,
		latestData:   make(map[string]*storage.BasisData),
	}
}

// SetBasisUpdateCallback 设置价差更新回调
func (bm *BasisMonitor) SetBasisUpdateCallback(callback func(*storage.BasisData)) {
	bm.onBasisUpdate = callback
}

// Start 启动价差监控
func (bm *BasisMonitor) Start() {
	logger.Info("📊 启动价差监控服务 (交易所: %s, 交易对: %v, 间隔: %v)",
		bm.exchangeName, bm.symbols, bm.interval)

	go bm.monitorLoop()
}

// Stop 停止价差监控
func (bm *BasisMonitor) Stop() {
	if bm.cancel != nil {
		bm.cancel()
	}
	logger.Info("⏹️ 价差监控服务已停止")
}

// monitorLoop 监控循环
func (bm *BasisMonitor) monitorLoop() {
	// 立即执行一次
	bm.checkAllBasis()

	// 创建定时器
	ticker := time.NewTicker(bm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-bm.ctx.Done():
			return
		case <-ticker.C:
			bm.checkAllBasis()
		}
	}
}

// checkAllBasis 检查所有交易对的价差
func (bm *BasisMonitor) checkAllBasis() {
	logger.Info("🔍 开始检查价差...")

	// 使用 WaitGroup 并发获取所有交易对的价差
	var wg sync.WaitGroup
	for _, symbol := range bm.symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			if err := bm.checkBasis(sym); err != nil {
				logger.Warn("⚠️ [价差] %s 检查失败: %v", sym, err)
			}
		}(symbol)
	}
	wg.Wait()

	logger.Info("✅ 价差检查完成")
}

// checkBasis 检查单个交易对的价差
func (bm *BasisMonitor) checkBasis(symbol string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 并发获取现货价格、合约价格和资金费率
	var spotPrice, futuresPrice, fundingRate float64
	var spotErr, futuresErr, fundingErr error
	var wg sync.WaitGroup

	wg.Add(3)

	// 获取现货价格
	go func() {
		defer wg.Done()
		spotPrice, spotErr = bm.exchange.GetSpotPrice(ctx, symbol)
	}()

	// 获取合约价格
	go func() {
		defer wg.Done()
		futuresPrice, futuresErr = bm.exchange.GetLatestPrice(ctx, symbol)
	}()

	// 获取资金费率
	go func() {
		defer wg.Done()
		fundingRate, fundingErr = bm.exchange.GetFundingRate(ctx, symbol)
		if fundingErr != nil {
			// 资金费率获取失败不影响价差计算
			fundingRate = 0
		}
	}()

	wg.Wait()

	// 检查必要的数据是否获取成功
	if spotErr != nil {
		return fmt.Errorf("获取现货价格失败: %w", spotErr)
	}
	if futuresErr != nil {
		return fmt.Errorf("获取合约价格失败: %w", futuresErr)
	}

	// 计算价差
	basis := futuresPrice - spotPrice
	basisPercent := (basis / spotPrice) * 100

	// 创建价差数据
	data := &storage.BasisData{
		Symbol:       symbol,
		Exchange:     bm.exchangeName,
		SpotPrice:    spotPrice,
		FuturesPrice: futuresPrice,
		Basis:        basis,
		BasisPercent: basisPercent,
		FundingRate:  fundingRate,
		Timestamp:    time.Now().UTC(),
	}

	// 保存到数据库
	if err := bm.db.SaveBasisData(data); err != nil {
		logger.Warn("⚠️ 保存价差数据失败: %v", err)
	}

	// 更新缓存
	bm.dataMutex.Lock()
	bm.latestData[symbol] = data
	bm.dataMutex.Unlock()

	// 触发回调
	if bm.onBasisUpdate != nil {
		bm.onBasisUpdate(data)
	}

	// 记录日志
	logger.Info("💰 [价差] %s: 现货=%.2f, 合约=%.2f, 价差=%.2f (%.4f%%), 资金费率=%.6f%%",
		symbol, spotPrice, futuresPrice, basis, basisPercent, fundingRate*100)

	return nil
}

// GetCurrentBasis 获取当前价差（从缓存）
func (bm *BasisMonitor) GetCurrentBasis(symbol string) (*storage.BasisData, error) {
	bm.dataMutex.RLock()
	data, exists := bm.latestData[symbol]
	bm.dataMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("未找到交易对 %s 的价差数据", symbol)
	}

	return data, nil
}

// GetAllCurrentBasis 获取所有交易对的当前价差
func (bm *BasisMonitor) GetAllCurrentBasis() []*storage.BasisData {
	bm.dataMutex.RLock()
	defer bm.dataMutex.RUnlock()

	result := make([]*storage.BasisData, 0, len(bm.latestData))
	for _, data := range bm.latestData {
		result = append(result, data)
	}

	return result
}

// GetBasisHistory 获取价差历史数据
func (bm *BasisMonitor) GetBasisHistory(symbol string, limit int) ([]*storage.BasisData, error) {
	return bm.db.GetBasisHistory(symbol, bm.exchangeName, limit)
}

// GetBasisStatistics 获取价差统计数据
func (bm *BasisMonitor) GetBasisStatistics(symbol string, hours int) (*storage.BasisStats, error) {
	return bm.db.GetBasisStatistics(symbol, bm.exchangeName, hours)
}
