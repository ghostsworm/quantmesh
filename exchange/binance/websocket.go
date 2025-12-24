package binance

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensqt/logger"

	"github.com/adshao/go-binance/v2/futures"
	"github.com/gorilla/websocket"
)

// WebSocketManager 币安 WebSocket 订单流管理器
type WebSocketManager struct {
	client    *futures.Client
	apiKey    string
	secretKey string
	listenKey string
	doneC     chan struct{}
	stopC     chan struct{}
	mu        sync.RWMutex
	callbacks []OrderUpdateCallback
	isRunning bool

	// 价格缓存
	latestPrice float64
	priceMu     sync.RWMutex

	// 时间配置
	reconnectDelay    time.Duration
	keepAliveInterval time.Duration
	closeTimeout      time.Duration
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string) *WebSocketManager {
	return &WebSocketManager{
		client:            futures.NewClient(apiKey, secretKey),
		apiKey:            apiKey,
		secretKey:         secretKey,
		doneC:             make(chan struct{}),
		stopC:             make(chan struct{}),
		callbacks:         make([]OrderUpdateCallback, 0),
		reconnectDelay:    5 * time.Second,
		keepAliveInterval: 30 * time.Minute,
		closeTimeout:      10 * time.Second,
	}
}

// Start 启动WebSocket连接
func (w *WebSocketManager) Start(ctx context.Context, callback OrderUpdateCallback) error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("订单流已在运行")
	}
	w.isRunning = true
	w.callbacks = append(w.callbacks, callback)
	w.mu.Unlock()

	// 获取listenKey
	listenKey, err := w.client.NewStartUserStreamService().Do(ctx)
	if err != nil {
		return fmt.Errorf("获取listenKey失败: %v", err)
	}
	w.listenKey = listenKey
	logger.Debug("✅ [Binance] 已获取订单流listenKey: %s", listenKey)

	// 启动listenKey保活协程
	go w.keepAliveListenKey(ctx)

	// 启动WebSocket监听
	go w.listenUserDataStream(ctx)

	return nil
}

// StartPriceStream 启动价格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 使用原生 WebSocket 连接（go-binance 的 WsAggTradeServe 有 Bug）
	// 格式: wss://fstream.binance.com/ws/<symbol>@aggTrade

	symbolLower := strings.ToLower(symbol)
	url := fmt.Sprintf("wss://fstream.binance.com/ws/%s@aggTrade", symbolLower)

	// 使用通道等待首个价格
	firstPriceCh := make(chan struct{})
	firstPriceReceived := false

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("✅ [Binance] 价格流已停止")
				return
			default:
			}

			logger.Debug("🔗 [Binance] 正在连接 WebSocket: %s", url)

			// 导入 gorilla/websocket
			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				logger.Error("❌ [Binance] WebSocket 连接失败: %v，5秒后重试", err)
				time.Sleep(5 * time.Second)
				continue
			}

			logger.Info("✅ [Binance] WebSocket 已连接: %s", url) // 读取消息循环
			for {
				select {
				case <-ctx.Done():
					conn.Close()
					logger.Info("✅ [Binance] 价格流已停止")
					return
				default:
				}

				_, message, err := conn.ReadMessage()
				if err != nil {
					logger.Warn("⚠️ [Binance] WebSocket 读取错误: %v，正在重连", err)
					conn.Close()
					time.Sleep(2 * time.Second)
					break // 跳出内层循环，重新连接
				}

				// 解析消息（只提取必要字段）
				var event struct {
					Symbol string `json:"s"`
					Price  string `json:"p"`
				}

				if err := json.Unmarshal(message, &event); err != nil {
					logger.Debug("解析消息失败: %v", err)
					continue
				}

				price, err := strconv.ParseFloat(event.Price, 64)
				if err != nil {
					logger.Debug("解析价格失败: %v", err)
					continue
				} // 更新价格缓存
				w.priceMu.Lock()
				w.latestPrice = price
				w.priceMu.Unlock()

				// 通知首个价格已接收
				if !firstPriceReceived {
					firstPriceReceived = true
					logger.Debug("✅ [Binance] 收到首个价格: %.2f", price)
					close(firstPriceCh)
				}

				// 调用回调
				callback(price)
			}
		}
	}()

	// 等待接收首个价格（最多10秒）
	select {
	case <-firstPriceCh:
		logger.Debug("✅ [Binance] 价格流已启动: %s@aggTrade", symbolLower)
		return nil
	case <-time.After(10 * time.Second):
		return fmt.Errorf("等待首个价格超时（10秒）")
	case <-ctx.Done():
		return fmt.Errorf("上下文已取消")
	}
} // Stop 停止WebSocket
func (w *WebSocketManager) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.isRunning {
		return
	}

	close(w.stopC)

	// 等待关闭完成或超时
	select {
	case <-w.doneC:
		logger.Info("✅ [Binance] 订单流已停止")
	case <-time.After(w.closeTimeout):
		logger.Warn("⚠️ [Binance] 订单流停止超时")
	}

	w.isRunning = false
}

// keepAliveListenKey 保持listenKey有效
func (w *WebSocketManager) keepAliveListenKey(ctx context.Context) {
	ticker := time.NewTicker(w.keepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopC:
			return
		case <-ticker.C:
			if err := w.client.NewKeepaliveUserStreamService().ListenKey(w.listenKey).Do(ctx); err != nil {
				logger.Error("❌ [Binance] listenKey保活失败: %v", err)
			} else {
				logger.Debug("✅ [Binance] listenKey保活成功")
			}
		}
	}
}

// listenUserDataStream 监听用户数据流
func (w *WebSocketManager) listenUserDataStream(ctx context.Context) {
	defer close(w.doneC)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopC:
			return
		default:
		}

		logger.Info("🔗 [Binance] 连接WebSocket订单流...")

		doneC, stopC, err := futures.WsUserDataServe(w.listenKey, w.handleUserDataEvent, w.handleError)
		if err != nil {
			logger.Error("❌ [Binance] WebSocket连接失败: %v", err)
			time.Sleep(w.reconnectDelay)
			continue
		}

		logger.Info("✅ [Binance] WebSocket订单流已连接")

		// 等待断开或停止信号
		select {
		case <-ctx.Done():
			stopC <- struct{}{}
			return
		case <-w.stopC:
			stopC <- struct{}{}
			return
		case <-doneC:
			logger.Warn("⚠️ [Binance] WebSocket连接断开，等待重连...")
			time.Sleep(w.reconnectDelay)
		}
	}
}

// handleUserDataEvent 处理用户数据事件
func (w *WebSocketManager) handleUserDataEvent(event *futures.WsUserDataEvent) {
	if event.Event != futures.UserDataEventTypeOrderTradeUpdate {
		return
	}

	order := event.OrderTradeUpdate

	executedQty, _ := strconv.ParseFloat(order.AccumulatedFilledQty, 64)
	price, _ := strconv.ParseFloat(order.OriginalPrice, 64)
	avgPrice, _ := strconv.ParseFloat(order.AveragePrice, 64)

	update := OrderUpdate{
		OrderID:       order.ID,
		ClientOrderID: order.ClientOrderID, // 🔥 添加 ClientOrderID
		Symbol:        order.Symbol,
		Status:        OrderStatus(order.Status),
		ExecutedQty:   executedQty,
		Price:         price,
		AvgPrice:      avgPrice,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		UpdateTime:    order.TradeTime,
	}

	// 🔍 调试日志：记录收到的订单更新
	logger.Debug("🔍 [WebSocket回调] 收到订单更新: ID=%d, ClientOID=%s, Side=%s, Status=%s, ExecutedQty=%.4f, Price=%.2f",
		update.OrderID, update.ClientOrderID, update.Side, update.Status, update.ExecutedQty, update.Price)

	// 调用所有注册的回调
	w.mu.RLock()
	callbacks := w.callbacks
	w.mu.RUnlock()

	for _, callback := range callbacks {
		callback(update)
	}
}

// handleError 处理错误
func (w *WebSocketManager) handleError(err error) {
	logger.Error("❌ [Binance] WebSocket错误: %v", err)
}

// GetLatestPrice 获取最新价格（从缓存读取）
func (w *WebSocketManager) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}
