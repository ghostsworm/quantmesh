package ascendex

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	AscendEXMainnetWSURL = "wss://ascendex.com/api/pro/v1/stream"
	AscendEXTestnetWSURL = "wss://testnet.ascendex.com/api/pro/v1/stream"
)

// WebSocketManager AscendEX WebSocket 管理器
type WebSocketManager struct {
	apiKey       string
	secretKey    string
	accountGroup string
	wsURL        string
	conn         *websocket.Conn
	mu           sync.RWMutex
	stopCh       chan struct{}
	callback     func(interface{})
	isRunning    bool
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey, accountGroup string, isTestnet bool) *WebSocketManager {
	wsURL := AscendEXMainnetWSURL
	if isTestnet {
		wsURL = AscendEXTestnetWSURL
	}

	return &WebSocketManager{
		apiKey:       apiKey,
		secretKey:    secretKey,
		accountGroup: accountGroup,
		wsURL:        wsURL,
		stopCh:       make(chan struct{}),
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
			logger.Error("AscendEX WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()

		logger.Info("AscendEX WebSocket connected")

		// 认证
		if err := w.authenticate(); err != nil {
			logger.Error("AscendEX WebSocket authenticate error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 订阅频道
		if err := w.subscribe(symbol); err != nil {
			logger.Error("AscendEX WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go w.heartbeat()

		// 读取消息
		w.readMessages()

		// 连接断开，重连
		logger.Warn("AscendEX WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// authenticate 认证
func (w *WebSocketManager) authenticate() error {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	message := timestamp + "+stream"

	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	authMsg := map[string]interface{}{
		"op": "auth",
		"data": map[string]interface{}{
			"apiKey":    w.apiKey,
			"timestamp": timestamp,
			"signature": signature,
		},
	}

	return w.sendMessage(authMsg)
}

// subscribe 订阅频道
func (w *WebSocketManager) subscribe(symbol string) error {
	subMsg := map[string]interface{}{
		"op": "sub",
		"ch": fmt.Sprintf("order:%s", w.accountGroup),
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
				"op": "ping",
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
			logger.Error("AscendEX WebSocket read error: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("AscendEX WebSocket unmarshal error: %v", err)
		return
	}

	// 处理认证响应
	if m, ok := msg["m"].(string); ok && m == "auth" {
		logger.Info("AscendEX WebSocket authenticated")
		return
	}

	// 处理心跳响应
	if m, ok := msg["m"].(string); ok && m == "pong" {
		return
	}

	// 处理订单数据
	if m, ok := msg["m"].(string); ok && m == "order" {
		if data, ok := msg["data"].(interface{}); ok {
			logger.Debug("AscendEX WebSocket order message")
			if w.callback != nil {
				w.callback(data)
			}
		}
	}
}
