package coinex

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	CoinExMainnetWSURL = "wss://socket.coinex.com/"
	CoinExTestnetWSURL = "wss://socket.coinex.com/"
)

// WebSocketManager CoinEx WebSocket 管理器
type WebSocketManager struct {
	apiKey    string
	secretKey string
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(interface{})
	isRunning bool
	reqID     int64
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, isTestnet bool) *WebSocketManager {
	wsURL := CoinExMainnetWSURL
	if isTestnet {
		wsURL = CoinExTestnetWSURL
	}

	return &WebSocketManager{
		apiKey:    apiKey,
		secretKey: secretKey,
		wsURL:     wsURL,
		stopCh:    make(chan struct{}),
		reqID:     1,
	}
}

// Start 启动 WebSocket
func (w *WebSocketManager) Start(ctx context.Context, market string, callback func(interface{})) error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("websocket already running")
	}
	w.callback = callback
	w.isRunning = true
	w.mu.Unlock()

	go w.connect(ctx, market)
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
func (w *WebSocketManager) connect(ctx context.Context, market string) {
	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(w.wsURL, nil)
		if err != nil {
			logger.Error("CoinEx WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()

		logger.Info("CoinEx WebSocket connected")

		// 认证
		if err := w.authenticate(); err != nil {
			logger.Error("CoinEx WebSocket authenticate error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 订阅频道
		if err := w.subscribe(market); err != nil {
			logger.Error("CoinEx WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go w.heartbeat()

		// 读取消息
		w.readMessages()

		// 连接断开，重连
		logger.Warn("CoinEx WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// authenticate 认证
func (w *WebSocketManager) authenticate() error {
	timestamp := time.Now().Unix()
	message := fmt.Sprintf("access_id=%s&timestamp=%d&secret_key=%s", w.apiKey, timestamp, w.secretKey)

	h := md5.New()
	h.Write([]byte(message))
	signature := hex.EncodeToString(h.Sum(nil))

	authMsg := map[string]interface{}{
		"method": "server.sign",
		"params": []interface{}{
			w.apiKey,
			signature,
			timestamp,
		},
		"id": w.getNextReqID(),
	}

	return w.sendMessage(authMsg)
}

// subscribe 订阅频道
func (w *WebSocketManager) subscribe(market string) error {
	subMsg := map[string]interface{}{
		"method": "order.subscribe",
		"params": []interface{}{market},
		"id":     w.getNextReqID(),
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
				"method": "server.ping",
				"params": []interface{}{},
				"id":     w.getNextReqID(),
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
			logger.Error("CoinEx WebSocket read error: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("CoinEx WebSocket unmarshal error: %v", err)
		return
	}

	// 处理认证响应
	if method, ok := msg["method"].(string); ok && method == "server.sign" {
		if result, ok := msg["result"].(map[string]interface{}); ok {
			if status, ok := result["status"].(string); ok && status == "success" {
				logger.Info("CoinEx WebSocket authenticated")
			}
		}
		return
	}

	// 处理心跳响应
	if method, ok := msg["method"].(string); ok && method == "server.ping" {
		return
	}

	// 处理订单更新
	if method, ok := msg["method"].(string); ok && method == "order.update" {
		if params, ok := msg["params"].([]interface{}); ok && len(params) > 0 {
			logger.Debug("CoinEx WebSocket order update")
			if w.callback != nil {
				w.callback(params)
			}
		}
	}
}

// getNextReqID 获取下一个请求 ID
func (w *WebSocketManager) getNextReqID() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.reqID++
	return w.reqID
}
