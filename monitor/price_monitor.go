package monitor

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"opensqt/exchange"
	"opensqt/logger"
)

/*
PriceMonitor 架构说明：

1. **全局唯一的价格流**：
   - 整个系统中只有一个 PriceMonitor 实例（在 main.go 中创建）
   - 所有组件需要价格时，应该通过 priceMonitor.GetLastPrice() 获取
   - 不要在其他地方独立启动价格流

2. **价格获取方式**：
   - 必须使用 WebSocket 推送（毫秒级量化系统要求）
   - WebSocket 失败时系统将停止运行，不会降级
   - 价格缓存在内存中，读取无阻塞

3. **依赖关系**：
   - 依赖 exchange.IExchange 接口
   - 通过 exchange.StartPriceStream() 启动 WebSocket
   - WebSocket 是唯一的价格来源
*/

// PriceChange 价格变化事件
type PriceChange struct {
	OldPrice  float64
	NewPrice  float64
	Change    float64
	Timestamp time.Time
}

// PriceMonitor 价格监控器
type PriceMonitor struct {
	symbol        string
	exchange      exchange.IExchange // 依赖交易所接口
	lastPrice     atomic.Value       // float64
	lastPriceStr  atomic.Value       // string - 原始价格字符串（用于检测小数位数）
	lastPriceTime atomic.Value       // time.Time

	priceChangeCh     chan PriceChange
	latestPriceChange atomic.Value // *PriceChange - 保存最新的价格更新（不阻塞）
	isRunning         atomic.Bool
	ctx               context.Context
	cancel            context.CancelFunc

	// 时间配置
	priceSendInterval time.Duration
}

// NewPriceMonitor 创建价格监控器
// 参数说明：
// - ex: 交易所接口（用于启动价格流和轮询价格）
// - symbol: 交易对符号
// - priceSendInterval: 价格推送间隔（毫秒）
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

// Start 启动价格监控
func (pm *PriceMonitor) Start() error {
	if pm.isRunning.Load() {
		return fmt.Errorf("价格监控已在运行")
	}

	pm.isRunning.Store(true)

	// 启动价格流（WebSocket）- 这是唯一的价格来源
	// 注意：毫秒级量化系统不能容忍 REST API 轮询的延迟
	err := pm.exchange.StartPriceStream(pm.ctx, pm.symbol, func(price float64) {
		pm.updatePrice(price)
	})
	if err != nil {
		// WebSocket 失败时直接返回错误，系统将停止
		pm.isRunning.Store(false)
		return fmt.Errorf("启动价格流失败（WebSocket 是唯一价格来源）: %w", err)
	}

	logger.Info("✅ 价格监控已启动 (WebSocket 推送)")
	go pm.periodicPriceSender() // 启动定期发送协程

	return nil
}

// pollPrice 已移除 - 毫秒级量化系统不使用 REST API 轮询
// WebSocket 是唯一的价格来源，失败时系统应该停止运行

// updatePrice 更新价格状态
func (pm *PriceMonitor) updatePrice(newPrice float64) {
	if newPrice <= 0 {
		return
	}

	oldPrice := pm.GetLastPrice()

	// 存储新价格
	pm.lastPrice.Store(newPrice)
	pm.lastPriceStr.Store(fmt.Sprintf("%f", newPrice)) // 简单转换，精度由后续逻辑处理
	pm.lastPriceTime.Store(time.Now())

	// 如果价格有变化，生成事件
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

// periodicPriceSender 定期发送最新价格
func (pm *PriceMonitor) periodicPriceSender() {
	ticker := time.NewTicker(pm.priceSendInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pm.ctx.Done():
			return
		case <-ticker.C:
			// 获取最新价格更新
			if latestVal := pm.latestPriceChange.Load(); latestVal != nil {
				latestChange := latestVal.(*PriceChange)
				if latestChange != nil {
					// 尝试非阻塞发送
					select {
					case pm.priceChangeCh <- *latestChange:
						// 成功发送，清空latestPriceChange
						pm.latestPriceChange.Store((*PriceChange)(nil))
					default:
						// channel已满，保留最新价格等待下次机会
					}
				}
			}
		}
	}
}

// Stop 停止价格监控
func (pm *PriceMonitor) Stop() {
	pm.cancel()
	pm.isRunning.Store(false)
	// 使用select避免向已关闭的channel发送数据
	select {
	case <-pm.priceChangeCh:
		// channel已关闭或为空
	default:
		// channel未关闭，安全关闭
		close(pm.priceChangeCh)
	}
}

// GetLastPrice 获取最新价格
func (pm *PriceMonitor) GetLastPrice() float64 {
	if val := pm.lastPrice.Load(); val != nil {
		return val.(float64)
	}
	return 0
}

// GetLastPriceString 获取最新价格的原始字符串（用于检测小数位数）
func (pm *PriceMonitor) GetLastPriceString() string {
	if val := pm.lastPriceStr.Load(); val != nil {
		return val.(string)
	}
	return ""
}

// Subscribe 订阅价格变化
func (pm *PriceMonitor) Subscribe() <-chan PriceChange {
	outCh := make(chan PriceChange, 10)
	go func() {
		defer close(outCh)
		var latestChange *PriceChange // 保存最新的价格更新

		for {
			select {
			case <-pm.ctx.Done():
				// 尝试发送最后保存的更新（如果有）
				if latestChange != nil {
					select {
					case outCh <- *latestChange:
					default:
					}
				}
				return
			case change, ok := <-pm.priceChangeCh:
				if !ok {
					// priceChangeCh已关闭，尝试发送最后保存的更新（如果有）
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

				// 尝试非阻塞发送
				select {
				case outCh <- change:
					// 成功发送，清空latestChange
					latestChange = nil
				default:
					// outCh已满，保存最新的价格更新，丢弃旧数据
					// 这样确保消费者总是能收到最新的价格，而不是被旧数据阻塞
					latestChange = &change
				}
			}
		}
	}()
	return outCh
}
