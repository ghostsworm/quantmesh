package mexc

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
	MEXCMainnetWSURL = "wss://contract.mexc.com/ws"
	MEXCTestnetWSURL = "wss://contract-testnet.mexc.com/ws"
)

// WebSocketManager MEXC WebSocket 管理器
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
	wsURL := MEXCMainnetWSURL
	if isTestnet {
		wsURL = MEXCTestnetWSURL
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
			logger.Error("MEXC WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()

		logger.Info("MEXC WebSocket connected")

		// 登录认证
		if err := w.login(); err != nil {
			logger.Error("MEXC WebSocket login error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 订阅频道
		if err := w.subscribe(symbol); err != nil {
			logger.Error("MEXC WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go w.heartbeat()

		// 读取消息
		w.readMessages()

		// 连接断开，重连
		logger.Warn("MEXC WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// login 登录认证
func (w *WebSocketManager) login() error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	
	// 签名：HMAC-SHA256(apiKey + timestamp)
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(w.apiKey + timestamp))
	signature := hex.EncodeToString(h.Sum(nil))

	loginMsg := map[string]interface{}{
		"method": "login",
		"param": map[string]string{
			"apiKey":    w.apiKey,
			"reqTime":   timestamp,
			"signature": signature,
		},
	}

	return w.sendMessage(loginMsg)
}

// subscribe 订阅频道
func (w *WebSocketManager) subscribe(symbol string) error {
	// 订阅订单更新
	subMsg := map[string]interface{}{
		"method": "sub.personal.order",
		"param": map[string]string{
			"symbol": symbol,
		},
	}

	if err := w.sendMessage(subMsg); err != nil {
		return err
	}

	// 订阅持仓更新
	subMsg = map[string]interface{}{
		"method": "sub.personal.position",
		"param": map[string]string{
			"symbol": symbol,
		},
	}

	if err := w.sendMessage(subMsg); err != nil {
		return err
	}

	// 订阅 Ticker
	subMsg = map[string]interface{}{
		"method": "sub.ticker",
		"param": map[string]string{
			"symbol": symbol,
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
				"method": "ping",
			}
			if err := w.sendMessage(pingMsg); err != nil {
				logger.Error("MEXC WebSocket ping error: %v", err)
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
			logger.Error("MEXC WebSocket read error: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("MEXC WebSocket unmarshal error: %v", err)
		return
	}

	// 处理 pong
	if method, ok := msg["method"].(string); ok && method == "pong" {
		return
	}

	// 处理登录响应
	if method, ok := msg["method"].(string); ok && method == "login" {
		if code, ok := msg["code"].(float64); ok && code == 0 {
			logger.Info("MEXC WebSocket login success")
		} else {
			logger.Error("MEXC WebSocket login failed: %v", msg)
		}
		return
	}

	// 处理订阅响应
	if method, ok := msg["method"].(string); ok && (method == "sub.personal.order" || method == "sub.personal.position" || method == "sub.ticker") {
		if code, ok := msg["code"].(float64); ok && code == 0 {
			logger.Info("MEXC WebSocket subscribe success: %s", method)
		} else {
			logger.Error("MEXC WebSocket subscribe failed: %v", msg)
		}
		return
	}

	// 处理数据推送
	if channel, ok := msg["channel"].(string); ok {
		if data, ok := msg["data"].(interface{}); ok {
			logger.Debug("MEXC WebSocket message: channel=%s, data=%v", channel, data)
			if w.callback != nil {
				w.callback(data)
			}
		}
	}
}

