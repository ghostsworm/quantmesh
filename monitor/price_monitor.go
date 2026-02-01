package monitor

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/metrics"
)

/*
PriceMonitor 架構說明：

1. **全局唯一的價格流**：
   - 整個系统中只有一個 PriceMonitor 實例（在 main.go 中創建）
   - 所有组件需要價格時，应該通過 priceMonitor.GetLastPrice() 獲取
   - 不要在其他地方独立啟動價格流

2. **價格獲取方式**：
   - 必須使用 WebSocket 推送（毫秒级量化系统要求）
   - WebSocket 失败時系统將停止运行，不會降级
   - 價格缓存在記憶體中，读取無阻塞

3. **依赖关系**：
   - 依赖 exchange.IExchange 接口
   - 通過 exchange.StartPriceStream() 啟动 WebSocket
   - WebSocket 是唯一的價格来源
*/

// PriceChange 價格變化事件
type PriceChange struct {
	OldPrice  float64
	NewPrice  float64
	Change    float64
	Timestamp time.Time
}

// PriceMonitor 價格監控器
type PriceMonitor struct {
	symbol        string
	exchange      exchange.IExchange // 依赖交易所接口
	lastPrice     atomic.Value       // float64
	lastPriceStr  atomic.Value       // string - 原始價格字符串（用於检测小數位數）
	lastPriceTime atomic.Value       // time.Time

	priceChangeCh     chan PriceChange
	latestPriceChange atomic.Value // *PriceChange - 保存最新的價格更新（不阻塞）
	isRunning         atomic.Bool
	ctx               context.Context
	cancel            context.CancelFunc

	// 時间配置
	priceSendInterval time.Duration
}

// NewPriceMonitor 創建價格監控器
// 参數說明：
// - ex: 交易所介面（用於啟動價格流和輪詢價格）
// - symbol: 交易對符号
// - priceSendInterval: 價格推送间隔（毫秒）
func NewPriceMonitor(ex exchange.IExchange, symbol string, priceSendInterval int) *PriceMonitor {
	ctx, cancel := context.WithCancel(context.Background())
	pm := &PriceMonitor{
		symbol:            symbol,
		exchange:          ex,
		priceChangeCh:     make(chan PriceChange, 10),
		ctx:               ctx,
		cancel:            cancel,
		priceSendInterval: time.Duration(priceSendInterval) * time.Millisecond,
	}
	pm.lastPrice.Store(0.0)
	pm.lastPriceStr.Store("")
	pm.lastPriceTime.Store(time.Time{})
	pm.latestPriceChange.Store((*PriceChange)(nil))
	return pm
}

// Start 啟動價格監控
func (pm *PriceMonitor) Start() error {
	if pm.isRunning.Load() {
		return fmt.Errorf("價格監控已在运行")
	}

	pm.isRunning.Store(true)

	// 啟動價格流（WebSocket）- 这是唯一的價格来源
	// 注意：毫秒级量化系统不能容忍 REST API 輪詢的延迟
	err := pm.exchange.StartPriceStream(pm.ctx, pm.symbol, func(price float64) {
		pm.updatePrice(price)
	})
	if err != nil {
		// WebSocket 失败時直接返回錯误，系统將停止
		pm.isRunning.Store(false)
		return fmt.Errorf("啟動價格流失败（WebSocket 是唯一價格来源）: %w", err)
	}

	logger.Info("✅ 價格監控已啟动 (WebSocket 推送)")
	go pm.periodicPriceSender() // 啟动定期发送协程

	return nil
}

// pollPrice 已移除 - 毫秒级量化系统不使用 REST API 輪詢
// WebSocket 是唯一的價格来源，失败時系统应該停止运行

// updatePrice 更新價格状態
func (pm *PriceMonitor) updatePrice(newPrice float64) {
	if newPrice <= 0 {
		return
	}

	oldPrice := pm.GetLastPrice()

	// 存儲新價格
	pm.lastPrice.Store(newPrice)
	pm.lastPriceStr.Store(fmt.Sprintf("%f", newPrice)) // 简單轉换，精度由后续逻辑处理
	pm.lastPriceTime.Store(time.Now())

	// 記錄 Prometheus 指標
	promMetrics := metrics.GetPrometheusMetrics()
	promMetrics.SetCurrentPrice(pm.exchange.GetName(), pm.symbol, newPrice)
	promMetrics.RecordPriceUpdate(pm.exchange.GetName(), pm.symbol)

	// 如果價格有变化，生成事件
	if oldPrice > 0 && newPrice != oldPrice {
		change := newPrice - oldPrice
		event := &PriceChange{
			OldPrice:  oldPrice,
			NewPrice:  newPrice,
			Change:    change,
			Timestamp: time.Now(),
		}
		pm.latestPriceChange.Store(event)
	}
}

// periodicPriceSender 定期发送最新價格
func (pm *PriceMonitor) periodicPriceSender() {
	ticker := time.NewTicker(pm.priceSendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			// 獲取最新價格更新
			if latestVal := pm.latestPriceChange.Load(); latestVal != nil {
				latestChange := latestVal.(*PriceChange)
				if latestChange != nil {
					// 尝試非阻塞发送
					select {
					case pm.priceChangeCh <- *latestChange:
						// 成功发送，清空latestPriceChange
						pm.latestPriceChange.Store((*PriceChange)(nil))
					default:
						// channel已满，保留最新價格等待下次机會
					}
				}
			}
		}
	}
}

// Stop 停止價格監控
func (pm *PriceMonitor) Stop() {
	pm.cancel()
	pm.isRunning.Store(false)
	// 使用select避免向已关闭的channel发送數據
	select {
	case <-pm.priceChangeCh:
		// channel已关闭或為空
	default:
		// channel未关闭，安全关闭
		close(pm.priceChangeCh)
	}
}

// GetLastPrice 獲取最新價格
func (pm *PriceMonitor) GetLastPrice() float64 {
	if val := pm.lastPrice.Load(); val != nil {
		return val.(float64)
	}
	return 0
}

// GetLastPriceString 獲取最新價格的原始字符串（用於检测小數位數）
func (pm *PriceMonitor) GetLastPriceString() string {
	if val := pm.lastPriceStr.Load(); val != nil {
		return val.(string)
	}
	return ""
}

// Subscribe 订阅價格變化
func (pm *PriceMonitor) Subscribe() <-chan PriceChange {
	outCh := make(chan PriceChange, 10)
	go func() {
		defer close(outCh)
		var latestChange *PriceChange // 保存最新的價格更新

		for {
			select {
			case <-pm.ctx.Done():
				// 尝試发送最后保存的更新（如果有）
				if latestChange != nil {
					select {
					case outCh <- *latestChange:
					default:
					}
				}
				return
			case change, ok := <-pm.priceChangeCh:
				if !ok {
					// priceChangeCh已关闭，尝試发送最后保存的更新（如果有）
					if latestChange != nil {
						select {
						case outCh <- *latestChange:
						default:
						}
					}
					return
				}
				if change.NewPrice <= 0 {
					continue
				}

				// 尝試非阻塞发送
				select {
				case outCh <- change:
					// 成功发送，清空latestChange
					latestChange = nil
				default:
					// outCh已满，保存最新的價格更新，丢弃舊數據
					// 这样确保消费者總是能收到最新的價格，而不是被舊數據阻塞
					latestChange = &change
				}
			}
		}
	}()
	return outCh
}
