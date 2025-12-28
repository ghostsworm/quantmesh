package deribit

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
	DeribitMainnetWSURL = "wss://www.deribit.com/ws/api/v2"
	DeribitTestnetWSURL = "wss://test.deribit.com/ws/api/v2"
)

// WebSocketManager Deribit WebSocket 管理器
type WebSocketManager struct {
	apiKey    string
	secretKey string
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(interface{})
	isRunning bool
	requestID int64
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, isTestnet bool) *WebSocketManager {
	wsURL := DeribitMainnetWSURL
	if isTestnet {
		wsURL = DeribitTestnetWSURL
	}

	return &WebSocketManager{
		apiKey:    apiKey,
		secretKey: secretKey,
		wsURL:     wsURL,
		stopCh:    make(chan struct{}),
		requestID: 1,
	}
}

// Start 启动 WebSocket
func (w *WebSocketManager) Start(ctx context.Context, instrumentName string, callback func(interface{})) error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("websocket already running")
	}
	w.callback = callback
	w.isRunning = true
	w.mu.Unlock()

	go w.connect(ctx, instrumentName)
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
func (w *WebSocketManager) connect(ctx context.Context, instrumentName string) {
	for {
		select {
		case <-w.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(w.wsURL, nil)
		if err != nil {
			logger.Error("Deribit WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()

		logger.Info("Deribit WebSocket connected")

		// 认证
		if err := w.authenticate(); err != nil {
			logger.Error("Deribit WebSocket authenticate error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 订阅频道
		if err := w.subscribe(instrumentName); err != nil {
			logger.Error("Deribit WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go w.heartbeat()

		// 读取消息
		w.readMessages()

		// 连接断开，重连
		logger.Warn("Deribit WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// authenticate 认证
func (w *WebSocketManager) authenticate() error {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := timestamp
	data := ""

	// 签名字符串：timestamp + "\n" + nonce + "\n" + data
	signStr := timestamp + "\n" + nonce + "\n" + data
	
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(signStr))
	signature := hex.EncodeToString(h.Sum(nil))

	authMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      w.getNextRequestID(),
		"method":  "public/auth",
		"params": map[string]interface{}{
			"grant_type": "client_signature",
			"client_id":  w.apiKey,
			"timestamp":  timestamp,
			"signature":  signature,
			"nonce":      nonce,
			"data":       data,
		},
	}

	return w.sendMessage(authMsg)
}

// subscribe 订阅频道
func (w *WebSocketManager) subscribe(instrumentName string) error {
	// 订阅用户订单
	subMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      w.getNextRequestID(),
		"method":  "private/subscribe",
		"params": map[string]interface{}{
			"channels": []string{
				"user.orders." + instrumentName + ".raw",
				"user.portfolio." + instrumentName,
			},
		},
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
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopCh:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      w.getNextRequestID(),
				"method":  "public/test",
			}
			if err := w.sendMessage(pingMsg); err != nil {
				logger.Error("Deribit WebSocket ping error: %v", err)
				return
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
			logger.Error("Deribit WebSocket read error: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("Deribit WebSocket unmarshal error: %v", err)
		return
	}

	// 处理认证响应
	if method, ok := msg["method"].(string); ok && method == "public/auth" {
		if result, ok := msg["result"].(map[string]interface{}); ok {
			logger.Info("Deribit WebSocket auth success: %v", result)
		}
		return
	}

	// 处理订阅响应
	if method, ok := msg["method"].(string); ok && method == "subscription" {
		if params, ok := msg["params"].(map[string]interface{}); ok {
			if data, ok := params["data"].(interface{}); ok {
				logger.Debug("Deribit WebSocket subscription data: %v", data)
				if w.callback != nil {
					w.callback(data)
				}
			}
		}
		return
	}

	// 处理心跳响应
	if result, ok := msg["result"].(string); ok && result == "pong" {
		return
	}
}

// getNextRequestID 获取下一个请求 ID
func (w *WebSocketManager) getNextRequestID() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := w.requestID
	w.requestID++
	return id
}

