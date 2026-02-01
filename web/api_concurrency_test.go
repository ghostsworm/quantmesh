package web

import (
	"sync"
	"testing"
	"time"
)

// TestConcurrentProviderAccess 测試並发访问 provider maps
func TestConcurrentProviderAccess(t *testing.T) {
	// 重置全局状態
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

	// 模拟並发写入（注册 providers）
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

				// 短暂休眠以增加並行衝突的可能性
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// 模拟並发读取（访问 providers）
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

	// 模拟並发遍历
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

	t.Log("並发测試完成，没有发生數據竞争")
}

// TestConcurrentFundingProviderRegistration 测試並发注册资金费率提供者
func TestConcurrentFundingProviderRegistration(t *testing.T) {
	fundingProviders = make(map[string]FundingMonitorProvider)

	var wg sync.WaitGroup
	numGoroutines := 5

	// 模拟多個 goroutine 同時注册不同的 funding providers
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()

			// 每個 goroutine 注册多個 providers
			for j := 0; j < 10; j++ {
				RegisterFundingProvider("binance", "BTCUSDT", nil)
				time.Sleep(time.Microsecond)
			}
		}(i)
	}

	// 同時读取
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
	t.Log("资金费率提供者並发注册测試完成")
}

// TestConcurrentSymbolIteration 测試並发遍历交易對列表
func TestConcurrentSymbolIteration(t *testing.T) {
	// 初始化一些测試數據
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

	// 並发遍历
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

				// 由於並发写入,數量可能會增加(XRPUSDT被添加)
				if count < len(symbols) {
					t.Errorf("期望至少 %d 個交易對，實際 %d 個", len(symbols), count)
				}
				time.Sleep(time.Microsecond)
			}
		}()
	}

	// 同時進行写入操作
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
	t.Log("並发遍历测試完成")
}
