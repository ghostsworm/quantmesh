package poloniex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	PoloniexMainnetWSURL = "wss://ws.poloniex.com/ws/public"
	PoloniexTestnetWSURL = "wss://testnet-ws.poloniex.com/ws/public"
)

// WebSocketManager Poloniex WebSocket 管理器
type WebSocketManager struct {
	apiKey    string
	secretKey string
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(interface{})
	isRunning bool
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, isTestnet bool) *WebSocketManager {
	wsURL := PoloniexMainnetWSURL
	if isTestnet {
		wsURL = PoloniexTestnetWSURL
	}

	return &WebSocketManager{
		apiKey:    apiKey,
		secretKey: secretKey,
		wsURL:     wsURL,
		stopCh:    make(chan struct{}),
	}
}

// Start 启动 WebSocket
func (w *WebSocketManager) Start(ctx context.Context, symbol string, callback func(interface{})) error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("websocket already running")
	}
	w.callback = callback
	w.isRunning = true
	w.mu.Unlock()

	go w.connect(ctx, symbol)
	return nil
}

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.isRunning {
		return
	}

	w.isRunning = false
	close(w.stopCh)

	if w.conn != nil {
		w.conn.Close()
	}
}

// connect 连接 WebSocket
func (w *WebSocketManager) connect(ctx context.Context, symbol string) {
	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(w.wsURL, nil)
		if err != nil {
			logger.Error("Poloniex WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()

		logger.Info("Poloniex WebSocket connected")

		// 认证
		if err := w.authenticate(); err != nil {
			logger.Error("Poloniex WebSocket authenticate error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 订阅频道
		if err := w.subscribe(symbol); err != nil {
			logger.Error("Poloniex WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go w.heartbeat()

		// 读取消息
		w.readMessages()

		// 连接断开，重连
		logger.Warn("Poloniex WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// authenticate 认证
func (w *WebSocketManager) authenticate() error {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	message := "GET\n/ws\n" + timestamp

	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	signature := hex.EncodeToString(h.Sum(nil))

	authMsg := map[string]interface{}{
		"event":   "subscribe",
		"channel": []string{"auth"},
		"params": map[string]interface{}{
			"key":       w.apiKey,
			"signature": signature,
			"timestamp": timestamp,
		},
	}

	return w.sendMessage(authMsg)
}

// subscribe 订阅频道
func (w *WebSocketManager) subscribe(symbol string) error {
	subMsg := map[string]interface{}{
		"event":   "subscribe",
		"channel": []string{fmt.Sprintf("orders.%s", symbol)},
	}

	return w.sendMessage(subMsg)
}

// sendMessage 发送消息
func (w *WebSocketManager) sendMessage(msg interface{}) error {
	w.mu.RLock()
	conn := w.conn
	w.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("websocket not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return conn.WriteMessage(websocket.TextMessage, data)
}

// heartbeat 心跳
func (w *WebSocketManager) heartbeat() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"event": "ping",
			}
			w.sendMessage(pingMsg)
		}
	}
}

// readMessages 读取消息
func (w *WebSocketManager) readMessages() {
	w.mu.RLock()
	conn := w.conn
	w.mu.RUnlock()

	if conn == nil {
		return
	}

	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Error("Poloniex WebSocket read error: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("Poloniex WebSocket unmarshal error: %v", err)
		return
	}

	// 处理认证响应
	if event, ok := msg["event"].(string); ok && event == "subscribe" {
		logger.Info("Poloniex WebSocket subscribed")
		return
	}

	// 处理心跳响应
	if event, ok := msg["event"].(string); ok && event == "pong" {
		return
	}

	// 处理订单数据
	if channel, ok := msg["channel"].(string); ok {
		if data, ok := msg["data"].(interface{}); ok {
			logger.Debug("Poloniex WebSocket message: channel=%s", channel)
			if w.callback != nil {
				w.callback(data)
			}
		}
	}
}
