package bybit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	// WebSocket 地址
	MainnetWsURL = "wss://stream.bybit.com/v5/private"
	TestnetWsURL = "wss://stream-testnet.bybit.com/v5/private"

	PublicWsURL        = "wss://stream.bybit.com/v5/public/linear"
	PublicTestnetWsURL = "wss://stream-testnet.bybit.com/v5/public/linear"
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(OrderUpdate)
	priceCallback func(float64)
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:     apiKey,
		secretKey:  secretKey,
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// sign 生成签名
func (w *WebSocketManager) sign(expires string) string {
	message := "GET/realtime" + expires
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// Start 启动订单流
func (w *WebSocketManager) Start(ctx context.Context, symbol string, callback func(OrderUpdate)) error {
	if w.isRunning.Load() {
		return fmt.Errorf("WebSocket 已在运行")
	}

	w.orderCallback = callback

	wsURL := MainnetWsURL
	if w.useTestnet {
		wsURL = TestnetWsURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 WebSocket 失败: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	w.isRunning.Store(true)

	// 认证
	if err := w.auth(); err != nil {
		conn.Close()
		return fmt.Errorf("WebSocket 认证失败: %w", err)
	}

	// 订阅订单频道
	if err := w.subscribeOrders(); err != nil {
		conn.Close()
		return fmt.Errorf("订阅订单频道失败: %w", err)
	}

	// 启动消息处理
	go w.readMessages()
	go w.keepAlive()

	logger.Info("✅ [Bybit WebSocket] 订单流已启动")
	return nil
}

// auth 认证
func (w *WebSocketManager) auth() error {
	expires := strconv.FormatInt(time.Now().Add(10*time.Second).UnixMilli(), 10)
	signature := w.sign(expires)

	authMsg := map[string]interface{}{
		"op": "auth",
		"args": []string{
			w.apiKey,
			expires,
			signature,
		},
	}

	return w.sendMessage(authMsg)
}

// subscribeOrders 订阅订单频道
func (w *WebSocketManager) subscribeOrders() error {
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []string{
			"order",
		},
	}

	return w.sendMessage(subMsg)
}

// StartPriceStream 启动价格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(float64)) error {
	w.priceCallback = callback

	// 价格流使用公共 WebSocket
	wsURL := PublicWsURL
	if w.useTestnet {
		wsURL = PublicTestnetWsURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接价格流 WebSocket 失败: %w", err)
	}

	// 订阅行情频道
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []string{
			fmt.Sprintf("tickers.%s", symbol),
		},
	}

	if err := conn.WriteJSON(subMsg); err != nil {
		conn.Close()
		return fmt.Errorf("订阅价格频道失败: %w", err)
	}

	// 启动价格消息处理
	go w.readPriceMessages(conn)

	logger.Info("✅ [Bybit WebSocket] 价格流已启动")
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
			logger.Error("❌ [Bybit WebSocket] 消息处理 panic: %v", r)
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
				logger.Warn("⚠️ [Bybit WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		w.handleMessage(message)
	}
}

// readPriceMessages 读取价格消息
func (w *WebSocketManager) readPriceMessages(conn *websocket.Conn) {
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Warn("⚠️ [Bybit WebSocket] 读取价格消息失败: %v", err)
			break
		}

		w.handlePriceMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Bybit WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查操作类型
	if op, ok := msg["op"].(string); ok {
		if op == "auth" {
			if success, ok := msg["success"].(bool); ok && success {
				logger.Info("✅ [Bybit WebSocket] 认证成功")
			} else {
				logger.Error("❌ [Bybit WebSocket] 认证失败: %v", msg["ret_msg"])
			}
		} else if op == "subscribe" {
			logger.Info("✅ [Bybit WebSocket] 订阅成功")
		}
		return
	}

	// 处理订单数据
	if topic, ok := msg["topic"].(string); ok && topic == "order" {
		w.handleOrderUpdate(msg)
	}
}

// handleOrderUpdate 处理订单更新
func (w *WebSocketManager) handleOrderUpdate(msg map[string]interface{}) {
	data, ok := msg["data"].([]interface{})
	if !ok || len(data) == 0 {
		return
	}

	for _, item := range data {
		orderData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		orderId, _ := strconv.ParseInt(getString(orderData, "orderId"), 10, 64)
		price, _ := strconv.ParseFloat(getString(orderData, "price"), 64)
		quantity, _ := strconv.ParseFloat(getString(orderData, "qty"), 64)
		executedQty, _ := strconv.ParseFloat(getString(orderData, "cumExecQty"), 64)
		avgPrice, _ := strconv.ParseFloat(getString(orderData, "avgPrice"), 64)
		updateTime, _ := strconv.ParseInt(getString(orderData, "updatedTime"), 10, 64)

		side := getString(orderData, "side")
		var orderSide Side
		if side == "Buy" {
			orderSide = SideBuy
		} else {
			orderSide = SideSell
		}

		update := OrderUpdate{
			OrderID:       orderId,
			ClientOrderID: getString(orderData, "orderLinkId"),
			Symbol:        getString(orderData, "symbol"),
			Side:          orderSide,
			Type:          OrderType(getString(orderData, "orderType")),
			Status:        OrderStatus(getString(orderData, "orderStatus")),
			Price:         price,
			Quantity:      quantity,
			ExecutedQty:   executedQty,
			AvgPrice:      avgPrice,
			UpdateTime:    updateTime,
		}

		if w.orderCallback != nil {
			w.orderCallback(update)
		}
	}
}

// handlePriceMessage 处理价格消息
func (w *WebSocketManager) handlePriceMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	// 检查是否是行情数据
	if topic, ok := msg["topic"].(string); ok && len(topic) > 7 && topic[:7] == "tickers" {
		if data, ok := msg["data"].(map[string]interface{}); ok {
			if lastPriceStr, ok := data["lastPrice"].(string); ok {
				if price, err := strconv.ParseFloat(lastPriceStr, 64); err == nil {
					w.lastPrice.Store(price)
					if w.priceCallback != nil {
						w.priceCallback(price)
					}
				}
			}
		}
	}
}

// getString 安全获取字符串值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
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

			pingMsg := map[string]interface{}{
				"op": "ping",
			}

			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteJSON(pingMsg); err != nil {
					logger.Warn("⚠️ [Bybit WebSocket] 发送 ping 失败: %v", err)
				}
			}

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

	logger.Info("🛑 [Bybit WebSocket] 已停止")
}
