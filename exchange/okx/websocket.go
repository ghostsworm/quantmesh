package okx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	MainnetWsURL      = "wss://ws.okx.com:8443/ws/v5/private"
	TestnetWsURL      = "wss://wspap.okx.com:8443/ws/v5/private"
	MainnetPublicWsURL = "wss://ws.okx.com:8443/ws/v5/public"
	TestnetPublicWsURL = "wss://wspap.okx.com:8443/ws/v5/public"
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	passphrase string
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(OrderUpdate)
	priceCallback func(float64)
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey, passphrase string, useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// sign 生成签名
func (w *WebSocketManager) sign(timestamp string) string {
	message := timestamp + "GET" + "/users/self/verify"
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Start 啟動訂單流
func (w *WebSocketManager) Start(ctx context.Context, instId string, callback func(OrderUpdate)) error {
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

	// 登錄认证
	if err := w.login(); err != nil {
		conn.Close()
		return fmt.Errorf("WebSocket 登錄失败: %w", err)
	}

	// 订阅订單频道
	if err := w.subscribeOrders(instId); err != nil {
		conn.Close()
		return fmt.Errorf("订阅订單频道失败: %w", err)
	}

	// 啟动消息处理
	go w.readMessages()
	go w.keepAlive()

	logger.Info("✅ [OKX WebSocket] 訂單流已啟动")
	return nil
}

// login 登錄认证
func (w *WebSocketManager) login() error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sign := w.sign(timestamp)

	loginMsg := map[string]interface{}{
		"op": "login",
		"args": []map[string]string{
			{
				"apiKey":     w.apiKey,
				"passphrase": w.passphrase,
				"timestamp":  timestamp,
				"sign":       sign,
			},
		},
	}

	return w.sendMessage(loginMsg)
}

// subscribeOrders 订阅订單频道
func (w *WebSocketManager) subscribeOrders(instId string) error {
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []map[string]string{
			{
				"channel":  "orders",
				"instType": "SWAP",
				"instId":   instId,
			},
		},
	}

	return w.sendMessage(subMsg)
}

// StartPriceStream 啟動價格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, instId string, callback func(float64)) error {
	w.priceCallback = callback

	// 價格流使用公共 WebSocket（根據是否使用測試網选擇不同的地址）
	publicWsURL := MainnetPublicWsURL
	if w.useTestnet {
		publicWsURL = TestnetPublicWsURL
	}
	conn, _, err := websocket.DefaultDialer.Dial(publicWsURL, nil)
	if err != nil {
		return fmt.Errorf("连接價格流 WebSocket 失败: %w", err)
	}

	// 订阅行情频道
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []map[string]string{
			{
				"channel": "tickers",
				"instId":  instId,
			},
		},
	}

	if err := conn.WriteJSON(subMsg); err != nil {
		conn.Close()
		return fmt.Errorf("订阅價格频道失败: %w", err)
	}

	// 啟動價格消息处理
	go w.readPriceMessages(conn)

	logger.Info("✅ [OKX WebSocket] 價格流已啟动")
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
			logger.Error("❌ [OKX WebSocket] 消息处理 panic: %v", r)
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
				logger.Warn("⚠️ [OKX WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		w.handleMessage(message)
	}
}

// readPriceMessages 读取價格消息
func (w *WebSocketManager) readPriceMessages(conn *websocket.Conn) {
	defer conn.Close()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Warn("⚠️ [OKX WebSocket] 读取價格消息失败: %v", err)
			break
		}

		w.handlePriceMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [OKX WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查事件類型
	if event, ok := msg["event"].(string); ok {
		if event == "login" {
			if code, ok := msg["code"].(string); ok && code == "0" {
				logger.Info("✅ [OKX WebSocket] 登錄成功")
			} else {
				logger.Error("❌ [OKX WebSocket] 登錄失败: %v", msg["msg"])
			}
		} else if event == "subscribe" {
			logger.Info("✅ [OKX WebSocket] 订阅成功")
		} else if event == "error" {
			logger.Error("❌ [OKX WebSocket] 錯误: %v", msg["msg"])
		}
		return
	}

	// 处理订單數據
	if arg, ok := msg["arg"].(map[string]interface{}); ok {
		if channel, ok := arg["channel"].(string); ok && channel == "orders" {
			w.handleOrderUpdate(msg)
		}
	}
}

// handleOrderUpdate 处理订單更新
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

		orderId, _ := strconv.ParseInt(getString(orderData, "ordId"), 10, 64)
		price, _ := strconv.ParseFloat(getString(orderData, "px"), 64)
		quantity, _ := strconv.ParseFloat(getString(orderData, "sz"), 64)
		executedQty, _ := strconv.ParseFloat(getString(orderData, "accFillSz"), 64)
		avgPrice, _ := strconv.ParseFloat(getString(orderData, "avgPx"), 64)
		updateTime, _ := strconv.ParseInt(getString(orderData, "uTime"), 10, 64)

		side := getString(orderData, "side")
		var orderSide Side
		if side == "buy" {
			orderSide = SideBuy
		} else {
			orderSide = SideSell
		}

		update := OrderUpdate{
			OrderID:       orderId,
			ClientOrderID: getString(orderData, "clOrdId"),
			Symbol:        getString(orderData, "instId"),
			Side:          orderSide,
			Type:          OrderType(getString(orderData, "ordType")),
			Status:        OrderStatus(getString(orderData, "state")),
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

// handlePriceMessage 处理價格消息
func (w *WebSocketManager) handlePriceMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	// 检查是否是行情數據
	if arg, ok := msg["arg"].(map[string]interface{}); ok {
		if channel, ok := arg["channel"].(string); ok && channel == "tickers" {
			if data, ok := msg["data"].([]interface{}); ok && len(data) > 0 {
				if ticker, ok := data[0].(map[string]interface{}); ok {
					if lastStr, ok := ticker["last"].(string); ok {
						if price, err := strconv.ParseFloat(lastStr, 64); err == nil {
							w.lastPrice.Store(price)
							if w.priceCallback != nil {
								w.priceCallback(price)
							}
						}
					}
				}
			}
		}
	}
}

// getString 安全獲取字符串值
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

			pingMsg := "ping"
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteMessage(websocket.TextMessage, []byte(pingMsg)); err != nil {
					logger.Warn("⚠️ [OKX WebSocket] 发送 ping 失败: %v", err)
				}
			}

		case <-w.stopChan:
			return
		}
	}
}

// GetLatestPrice 獲取最新價格
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

	logger.Info("🛑 [OKX WebSocket] 已停止")
}
