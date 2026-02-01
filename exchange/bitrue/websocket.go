package bitrue

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	BitrueMainnetWSURL = "wss://ws.bitrue.com/kline-api/ws"
	BitrueTestnetWSURL = "wss://testnet-ws.bitrue.com/kline-api/ws"
)

// WebSocketManager Bitrue WebSocket 管理器
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
	wsURL := BitrueMainnetWSURL
	if isTestnet {
		wsURL = BitrueTestnetWSURL
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
			logger.Error("Bitrue WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()

		logger.Info("Bitrue WebSocket connected")

		// 订阅频道
		if err := w.subscribe(symbol); err != nil {
			logger.Error("Bitrue WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go w.heartbeat()

		// 读取消息
		w.readMessages()

		// 连接断开，重连
		logger.Warn("Bitrue WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// subscribe 订阅频道
func (w *WebSocketManager) subscribe(symbol string) error {
	// Bitrue 订阅格式类似 Binance
	subMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{
			fmt.Sprintf("%s@trade", symbol),
		},
		"id": 1,
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
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()
			if conn != nil {
				conn.WriteMessage(websocket.PingMessage, []byte{})
			}
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
			logger.Error("Bitrue WebSocket read error: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("Bitrue WebSocket unmarshal error: %v", err)
		return
	}

	// 处理订阅响应
	if result, ok := msg["result"].(interface{}); ok && result == nil {
		logger.Info("Bitrue WebSocket subscribed")
		return
	}

	// 处理交易数据
	if stream, ok := msg["stream"].(string); ok {
		if data, ok := msg["data"].(interface{}); ok {
			logger.Debug("Bitrue WebSocket message: stream=%s", stream)
			if w.callback != nil {
				w.callback(data)
			}
		}
	}
}
