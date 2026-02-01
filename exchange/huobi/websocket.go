package huobi

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// decompressGzip 解压 gzip 数据
func decompressGzip(data []byte) ([]byte, error) {
	reader, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer reader.Close()

	return io.ReadAll(reader)
}

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey        string
	secretKey     string
	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(OrderUpdate)
	priceCallback func(float64)
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string) *WebSocketManager {
	return &WebSocketManager{
		apiKey:    apiKey,
		secretKey: secretKey,
		stopChan:  make(chan struct{}),
	}
}

// Start 启动订单流
func (w *WebSocketManager) Start(ctx context.Context, contractCode string, callback func(OrderUpdate)) error {
	if w.isRunning.Load() {
		return fmt.Errorf("WebSocket 已在运行")
	}

	w.orderCallback = callback

	conn, _, err := websocket.DefaultDialer.Dial(MainnetWsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 WebSocket 失败: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	w.isRunning.Store(true)

	// 订阅订单频道
	if err := w.subscribeOrders(contractCode); err != nil {
		conn.Close()
		return fmt.Errorf("订阅订单频道失败: %w", err)
	}

	go w.readMessages()
	go w.keepAlive()

	logger.Info("✅ [Huobi WebSocket] 订单流已启动")
	return nil
}

// subscribeOrders 订阅订单频道
func (w *WebSocketManager) subscribeOrders(contractCode string) error {
	subMsg := map[string]interface{}{
		"op":    "sub",
		"topic": fmt.Sprintf("orders.%s", contractCode),
	}

	return w.sendMessage(subMsg)
}

// StartPriceStream 启动价格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, contractCode string, callback func(float64)) error {
	w.priceCallback = callback

	subMsg := map[string]interface{}{
		"op":    "sub",
		"topic": fmt.Sprintf("public.%s.ticker", contractCode),
	}

	if err := w.sendMessage(subMsg); err != nil {
		return fmt.Errorf("订阅价格频道失败: %w", err)
	}

	logger.Info("✅ [Huobi WebSocket] 价格流已启动")
	return nil
}

// sendMessage 发送消息
func (w *WebSocketManager) sendMessage(msg interface{}) error {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.conn == nil {
		return fmt.Errorf("WebSocket 未连接")
	}

	return w.conn.WriteJSON(msg)
}

// readMessages 读取消息
func (w *WebSocketManager) readMessages() {
	defer func() {
		w.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [Huobi WebSocket] 消息处理 panic: %v", r)
		}
	}()

	for w.isRunning.Load() {
		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()

		if conn == nil {
			break
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if w.isRunning.Load() {
				logger.Warn("⚠️ [Huobi WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		// 解压 gzip
		decompressed, err := decompressGzip(message)
		if err != nil {
			logger.Warn("⚠️ [Huobi WebSocket] 解压消息失败: %v", err)
			continue
		}

		w.handleMessage(decompressed)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Huobi WebSocket] 解析消息失败: %v", err)
		return
	}

	// 处理 ping
	if ping, ok := msg["ping"].(float64); ok {
		pongMsg := map[string]interface{}{
			"pong": int64(ping),
		}
		w.sendMessage(pongMsg)
		return
	}

	// 处理订阅响应
	if op, ok := msg["op"].(string); ok {
		if op == "sub" {
			logger.Info("✅ [Huobi WebSocket] 订阅成功")
		}
		return
	}

	// 处理订单数据
	if topic, ok := msg["topic"].(string); ok {
		if len(topic) > 6 && topic[:6] == "orders" {
			w.handleOrderUpdate(msg)
		} else if len(topic) > 6 && topic[:6] == "public" {
			w.handlePriceUpdate(msg)
		}
	}
}

// handleOrderUpdate 处理订单更新
func (w *WebSocketManager) handleOrderUpdate(msg map[string]interface{}) {
	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		return
	}

	orderId, _ := strconv.ParseInt(getString(data, "order_id"), 10, 64)
	price, _ := strconv.ParseFloat(getString(data, "price"), 64)
	volume, _ := strconv.ParseFloat(getString(data, "volume"), 64)
	tradeVolume, _ := strconv.ParseFloat(getString(data, "trade_volume"), 64)
	tradeAvgPrice, _ := strconv.ParseFloat(getString(data, "trade_avg_price"), 64)
	createdAt, _ := strconv.ParseInt(getString(data, "created_at"), 10, 64)

	direction := getString(data, "direction")
	var side Side
	if direction == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	update := OrderUpdate{
		OrderID:       orderId,
		ClientOrderID: getString(data, "client_order_id"),
		Symbol:        getString(data, "contract_code"),
		Side:          side,
		Type:          OrderTypeLimit,
		Status:        OrderStatus(getString(data, "status")),
		Price:         price,
		Quantity:      volume,
		ExecutedQty:   tradeVolume,
		AvgPrice:      tradeAvgPrice,
		UpdateTime:    createdAt,
	}

	if w.orderCallback != nil {
		w.orderCallback(update)
	}
}

// handlePriceUpdate 处理价格更新
func (w *WebSocketManager) handlePriceUpdate(msg map[string]interface{}) {
	tick, ok := msg["tick"].(map[string]interface{})
	if !ok {
		return
	}

	if closePrice, ok := tick["close"].(float64); ok {
		w.lastPrice.Store(closePrice)
		if w.priceCallback != nil {
			w.priceCallback(closePrice)
		}
	}
}

// getString 安全获取字符串值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		switch val := v.(type) {
		case string:
			return val
		case float64:
			return strconv.FormatFloat(val, 'f', -1, 64)
		case int64:
			return strconv.FormatInt(val, 10)
		}
	}
	return ""
}

// keepAlive 保持连接
func (w *WebSocketManager) keepAlive() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !w.isRunning.Load() {
				return
			}
			// Huobi 使用 ping/pong 机制，在 handleMessage 中处理

		case <-w.stopChan:
			return
		}
	}
}

// GetLatestPrice 获取最新价格
func (w *WebSocketManager) GetLatestPrice() float64 {
	if price := w.lastPrice.Load(); price != nil {
		return price.(float64)
	}
	return 0
}

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() {
	if !w.isRunning.Load() {
		return
	}

	w.isRunning.Store(false)
	close(w.stopChan)

	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()

	logger.Info("🛑 [Huobi WebSocket] 已停止")
}
