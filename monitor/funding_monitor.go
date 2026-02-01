package monitor

import (
	"context"
	"fmt"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/storage"
)

// FundingMonitor 资金费率监控服务
type FundingMonitor struct {
	storage      storage.Storage
	exchange     exchange.IExchange
	exchangeName string
	symbols      []string
	interval     time.Duration
	ctx          context.Context
	cancel       context.CancelFunc
}

// NewFundingMonitor 创建资金费率监控服务
func NewFundingMonitor(storage storage.Storage, ex exchange.IExchange, symbols []string, intervalHours int) *FundingMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	// 默认监控主流交易对
	if len(symbols) == 0 {
		symbols = []string{
			"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT",
			"ADAUSDT", "DOGEUSDT", "DOTUSDT", "MATICUSDT", "AVAXUSDT",
		}
	}

	interval := time.Duration(intervalHours) * time.Hour
	if interval <= 0 {
		interval = 8 * time.Hour // 默认8小时
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

// Start 启动资金费率监控
func (fm *FundingMonitor) Start() {
	logger.Info("📊 启动资金费率监控服务 (交易所: %s, 交易对: %v, 间隔: %v)",
		fm.exchangeName, fm.symbols, fm.interval)

	go fm.monitorLoop()
}

// Stop 停止资金费率监控
func (fm *FundingMonitor) Stop() {
	if fm.cancel != nil {
		fm.cancel()
	}
	logger.Info("⏹️ 资金费率监控服务已停止")
}

// monitorLoop 监控循环
func (fm *FundingMonitor) monitorLoop() {
	// 立即执行一次
	fm.checkFundingRates()

	// 创建定时器
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

// checkFundingRates 检查所有交易对的资金费率
func (fm *FundingMonitor) checkFundingRates() {
	logger.Info("🔍 开始检查资金费率...")

	for _, symbol := range fm.symbols {
		if err := fm.checkSymbolFundingRate(symbol); err != nil {
			logger.Warn("⚠️ [资金费率] %s 检查失败: %v", symbol, err)
			// 单个交易对失败不影响其他交易对
			continue
		}
	}

	logger.Info("✅ 资金费率检查完成")
}

// checkSymbolFundingRate 检查单个交易对的资金费率
func (fm *FundingMonitor) checkSymbolFundingRate(symbol string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 获取资金费率
	rate, err := fm.exchange.GetFundingRate(ctx, symbol)
	if err != nil {
		return fmt.Errorf("获取资金费率失败: %w", err)
	}

	// 获取当前时间（UTC）
	timestamp := time.Now().UTC()

	// 保存到数据库（仅在变动时存储）
	if err := fm.storage.SaveFundingRate(symbol, fm.exchangeName, rate, timestamp); err != nil {
		return fmt.Errorf("保存资金费率失败: %w", err)
	}

	// 记录日志（仅在费率变化时）
	logger.Info("💰 [资金费率] %s: %.6f%% (交易所: %s)", symbol, rate*100, fm.exchangeName)

	return nil
}

// GetCurrentFundingRates 获取当前所有监控交易对的资金费率
func (fm *FundingMonitor) GetCurrentFundingRates() (map[string]float64, error) {
	rates := make(map[string]float64)

	for _, symbol := range fm.symbols {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		rate, err := fm.exchange.GetFundingRate(ctx, symbol)
		cancel()

		if err != nil {
			logger.Warn("⚠️ 获取 %s 资金费率失败: %v", symbol, err)
			continue
		}

		rates[symbol] = rate
	}

	return rates, nil
}
