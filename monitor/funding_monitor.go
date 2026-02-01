package monitor

import (
	"context"
	"fmt"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
)

// FundingMonitor 资金费率監控服務
type FundingMonitor struct {
	storage      storage.Storage
	exchange     exchange.IExchange
	exchangeName string
	symbols      []string
	interval     time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewFundingMonitor 創建资金费率監控服務
func NewFundingMonitor(storage storage.Storage, ex exchange.IExchange, symbols []string, intervalHours int) *FundingMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	// 默认監控主流交易對
	if len(symbols) == 0 {
		symbols = []string{
			"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
			"ADAUSDT", "DOGEUSDT", "DOTUSDT", "MATICUSDT", "AVAXUSDT",
		}
	}

	interval := time.Duration(intervalHours) * time.Hour
	if interval <= 0 {
		interval = 8 * time.Hour // 默认8小時
	}

	return &FundingMonitor{
		storage:      storage,
		exchange:     ex,
		exchangeName: ex.GetName(),
		symbols:      symbols,
		interval:     interval,
		ctx:          ctx,
		cancel:       cancel,
	}
}

// Start 啟动资金费率監控
func (fm *FundingMonitor) Start() {
	logger.Info("📊 啟动资金费率監控服務 (交易所: %s, 交易對: %v, 间隔: %v)",
		fm.exchangeName, fm.symbols, fm.interval)

	go fm.monitorLoop()
}

// Stop 停止资金费率監控
func (fm *FundingMonitor) Stop() {
	if fm.cancel != nil {
		fm.cancel()
	}
	logger.Info("⏹️ 资金费率監控服務已停止")
}

// monitorLoop 監控循环
func (fm *FundingMonitor) monitorLoop() {
	// 立即執行一次
	fm.checkFundingRates()

	// 創建定時器
	ticker := time.NewTicker(fm.interval)
	defer ticker.Stop()

	for {
		select {
		case <-fm.ctx.Done():
			return
		case <-ticker.C:
			fm.checkFundingRates()
		}
	}
}

// checkFundingRates 检查所有交易對的资金费率
func (fm *FundingMonitor) checkFundingRates() {
	logger.Info("🔍 开始检查资金费率...")

	for _, symbol := range fm.symbols {
		if err := fm.checkSymbolFundingRate(symbol); err != nil {
			logger.Warn("⚠️ [资金费率] %s 检查失败: %v", symbol, err)
			// 單個交易對失败不影响其他交易對
			continue
		}
	}

	logger.Info("✅ 资金费率检查完成")
}

// checkSymbolFundingRate 检查單個交易對的资金费率
func (fm *FundingMonitor) checkSymbolFundingRate(symbol string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 獲取资金费率
	rate, err := fm.exchange.GetFundingRate(ctx, symbol)
	if err != nil {
		return fmt.Errorf("獲取资金费率失败: %w", err)
	}

	// 獲取當前時间（UTC）
	timestamp := time.Now().UTC()

	// 保存到數據库（僅在变动時存儲）
	if err := fm.storage.SaveFundingRate(symbol, fm.exchangeName, rate, timestamp); err != nil {
		return fmt.Errorf("保存资金费率失败: %w", err)
	}

	// 記錄日志（僅在费率变化時）
	logger.Info("💰 [资金费率] %s: %.6f%% (交易所: %s)", symbol, rate*100, fm.exchangeName)

	return nil
}

// GetCurrentFundingRates 獲取當前所有監控交易對的资金费率
func (fm *FundingMonitor) GetCurrentFundingRates() (map[string]float64, error) {
	rates := make(map[string]float64)

	for _, symbol := range fm.symbols {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rate, err := fm.exchange.GetFundingRate(ctx, symbol)
		cancel()

		if err != nil {
			logger.Warn("⚠️ 獲取 %s 资金费率失败: %v", symbol, err)
			continue
		}

		rates[symbol] = rate
	}

	return rates, nil
}
