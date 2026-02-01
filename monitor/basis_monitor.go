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

// BasisMonitor 價差監控器
type BasisMonitor struct {
	db           storage.Storage
	exchange     exchange.IExchange
	exchangeName string
	symbols      []string
	interval     time.Duration
	ctx          context.Context
	cancel       context.CancelFunc

	// 缓存最新價差數據
	latestData map[string]*storage.BasisData
	dataMutex  sync.RWMutex

	// 事件回呼
	onBasisUpdate func(*storage.BasisData)
}

// NewBasisMonitor 創建價差監控器
func NewBasisMonitor(db storage.Storage, ex exchange.IExchange, symbols []string, intervalMinutes int) *BasisMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	// 默认監控主流交易對
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

// SetBasisUpdateCallback 設置價差更新回呼
func (bm *BasisMonitor) SetBasisUpdateCallback(callback func(*storage.BasisData)) {
	bm.onBasisUpdate = callback
}

// Start 啟动價差監控
func (bm *BasisMonitor) Start() {
	logger.Info("📊 啟动價差監控服務 (交易所: %s, 交易對: %v, 间隔: %v)",
		bm.exchangeName, bm.symbols, bm.interval)

	go bm.monitorLoop()
}

// Stop 停止價差監控
func (bm *BasisMonitor) Stop() {
	if bm.cancel != nil {
		bm.cancel()
	}
	logger.Info("⏹️ 價差監控服務已停止")
}

// monitorLoop 監控循环
func (bm *BasisMonitor) monitorLoop() {
	// 立即執行一次
	bm.checkAllBasis()

	// 創建定時器
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

// checkAllBasis 检查所有交易對的價差
func (bm *BasisMonitor) checkAllBasis() {
	logger.Info("🔍 开始检查價差...")

	// 使用 WaitGroup 並发獲取所有交易對的價差
	var wg sync.WaitGroup
	for _, symbol := range bm.symbols {
		wg.Add(1)
		go func(sym string) {
			defer wg.Done()
			if err := bm.checkBasis(sym); err != nil {
				logger.Warn("⚠️ [價差] %s 检查失败: %v", sym, err)
			}
		}(symbol)
	}
	wg.Wait()

	logger.Info("✅ 價差检查完成")
}

// checkBasis 检查單個交易對的價差
func (bm *BasisMonitor) checkBasis(symbol string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 並发獲取現貨價格、合約價格和资金费率
	var spotPrice, futuresPrice, fundingRate float64
	var spotErr, futuresErr, fundingErr error
	var wg sync.WaitGroup

	wg.Add(3)

	// 獲取現貨價格
	go func() {
		defer wg.Done()
		spotPrice, spotErr = bm.exchange.GetSpotPrice(ctx, symbol)
	}()

	// 獲取合約價格
	go func() {
		defer wg.Done()
		futuresPrice, futuresErr = bm.exchange.GetLatestPrice(ctx, symbol)
	}()

	// 獲取资金费率
	go func() {
		defer wg.Done()
		fundingRate, fundingErr = bm.exchange.GetFundingRate(ctx, symbol)
		if fundingErr != nil {
			// 资金费率獲取失败不影响價差计算
			fundingRate = 0
		}
	}()

	wg.Wait()

	// 检查必要的數據是否獲取成功
	if spotErr != nil {
		return fmt.Errorf("獲取現貨價格失败: %w", spotErr)
	}
	if futuresErr != nil {
		return fmt.Errorf("獲取合約價格失败: %w", futuresErr)
	}

	// 计算價差
	basis := futuresPrice - spotPrice
	basisPercent := (basis / spotPrice) * 100

	// 創建價差數據
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

	// 保存到數據库
	if err := bm.db.SaveBasisData(data); err != nil {
		logger.Warn("⚠️ 保存價差數據失败: %v", err)
	}

	// 更新缓存
	bm.dataMutex.Lock()
	bm.latestData[symbol] = data
	bm.dataMutex.Unlock()

	// 触发回呼
	if bm.onBasisUpdate != nil {
		bm.onBasisUpdate(data)
	}

	// 記錄日志
	logger.Info("💰 [價差] %s: 現貨=%.2f, 合約=%.2f, 價差=%.2f (%.4f%%), 资金费率=%.6f%%",
		symbol, spotPrice, futuresPrice, basis, basisPercent, fundingRate*100)

	return nil
}

// GetCurrentBasis 獲取當前價差（從缓存）
func (bm *BasisMonitor) GetCurrentBasis(symbol string) (*storage.BasisData, error) {
	bm.dataMutex.RLock()
	data, exists := bm.latestData[symbol]
	bm.dataMutex.RUnlock()

	if !exists {
		return nil, fmt.Errorf("未找到交易對 %s 的價差數據", symbol)
	}

	return data, nil
}

// GetAllCurrentBasis 獲取所有交易對的當前價差
func (bm *BasisMonitor) GetAllCurrentBasis() []*storage.BasisData {
	bm.dataMutex.RLock()
	defer bm.dataMutex.RUnlock()

	result := make([]*storage.BasisData, 0, len(bm.latestData))
	for _, data := range bm.latestData {
		result = append(result, data)
	}

	return result
}

// GetBasisHistory 獲取價差历史數據
func (bm *BasisMonitor) GetBasisHistory(symbol string, limit int) ([]*storage.BasisData, error) {
	return bm.db.GetBasisHistory(symbol, bm.exchangeName, limit)
}

// GetBasisStatistics 獲取價差统计數據
func (bm *BasisMonitor) GetBasisStatistics(symbol string, hours int) (*storage.BasisStats, error) {
	return bm.db.GetBasisStatistics(symbol, bm.exchangeName, hours)
}
