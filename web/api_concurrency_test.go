package web

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentProviderAccess 测试并发访问 provider maps
func TestConcurrentProviderAccess(t *testing.T) {
	// 重置全局状态
	statusBySymbol = make(map[string]*SystemStatus)
	priceProviders = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders = make(map[string]RiskMonitorProvider)
	storageProviders = make(map[string]StorageServiceProvider)
	fundingProviders = make(map[string]FundingMonitorProvider)

	var wg sync.WaitGroup
	numGoroutines := 10
	numOperations := 100

	// 模拟并发写入（注册 providers）
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				exchange := "binance"
				symbol := "BTCUSDT"
				
				status := &SystemStatus{
					Running:      true,
					Exchange:     exchange,
					Symbol:       symbol,
					CurrentPrice: 50000.0,
				}

				providers := &SymbolScopedProviders{
					Status: status,
				}

				RegisterSymbolProviders(exchange, symbol, providers)
				
				// 短暂休眠以增加并发冲突的可能性
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// 模拟并发读取（访问 providers）
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := makeSymbolKey("binance", "BTCUSDT")
				
				// 读取 statusBySymbol
				statusMu.RLock()
				_ = statusBySymbol[key]
				statusMu.RUnlock()

				// 读取 priceProviders
				providersMu.RLock()
				_ = priceProviders[key]
				_ = exchangeProviders[key]
				_ = positionProviders[key]
				_ = riskProviders[key]
				_ = storageProviders[key]
				_ = fundingProviders[key]
				providersMu.RUnlock()

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// 模拟并发遍历
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				statusMu.RLock()
				for range statusBySymbol {
					// 遍历 map
				}
				statusMu.RUnlock()

				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// 等待所有 goroutine 完成
	wg.Wait()

	t.Log("并发测试完成，没有发生数据竞争")
}

// TestConcurrentFundingProviderRegistration 测试并发注册资金费率提供者
func TestConcurrentFundingProviderRegistration(t *testing.T) {
	fundingProviders = make(map[string]FundingMonitorProvider)

	var wg sync.WaitGroup
	numGoroutines := 5

	// 模拟多个 goroutine 同时注册不同的 funding providers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			// 每个 goroutine 注册多个 providers
			for j := 0; j < 10; j++ {
				RegisterFundingProvider("binance", "BTCUSDT", nil)
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// 同时读取
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			
			for j := 0; j < 10; j++ {
				key := makeSymbolKey("binance", "BTCUSDT")
				providersMu.RLock()
				_ = fundingProviders[key]
				providersMu.RUnlock()
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	wg.Wait()
	t.Log("资金费率提供者并发注册测试完成")
}

// TestConcurrentSymbolIteration 测试并发遍历交易对列表
func TestConcurrentSymbolIteration(t *testing.T) {
	// 初始化一些测试数据
	statusBySymbol = make(map[string]*SystemStatus)
	
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT"}
	for _, symbol := range symbols {
		key := makeSymbolKey("binance", symbol)
		statusBySymbol[key] = &SystemStatus{
			Running:      true,
			Exchange:     "binance",
			Symbol:       symbol,
			CurrentPrice: 50000.0,
		}
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	// 并发遍历
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				statusMu.RLock()
				count := 0
				for _, st := range statusBySymbol {
					if st != nil {
						count++
					}
				}
				statusMu.RUnlock()
				
				// 由于并发写入,数量可能会增加(XRPUSDT被添加)
				if count < len(symbols) {
					t.Errorf("期望至少 %d 个交易对，实际 %d 个", len(symbols), count)
				}
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// 同时进行写入操作
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				key := makeSymbolKey("binance", "XRPUSDT")
				statusMu.Lock()
				statusBySymbol[key] = &SystemStatus{
					Running:      true,
					Exchange:     "binance",
					Symbol:       "XRPUSDT",
					CurrentPrice: 1.0,
				}
				statusMu.Unlock()
				time.Sleep(time.Microsecond * 2)
			}
		}(i)
	}

	wg.Wait()
	t.Log("并发遍历测试完成")
}

