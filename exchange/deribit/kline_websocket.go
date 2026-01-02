package deribit

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// KlineWebSocketManager Deribit K线 WebSocket 管理器
type KlineWebSocketManager struct {
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(tick int64, open, high, low, close, volume float64)
	isRunning bool
	requestID int64
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(isTestnet bool) *KlineWebSocketManager {
	wsURL := DeribitMainnetWSURL
	if isTestnet {
		wsURL = DeribitTestnetWSURL
	}

	return &KlineWebSocketManager{
		wsURL:     wsURL,
		stopCh:    make(chan struct{}),
		requestID: 1,
	}
}

// Start 启动 K线 WebSocket
func (k *KlineWebSocketManager) Start(ctx context.Context, instrumentName, resolution string, callback func(tick int64, open, high, low, close, volume float64)) error {
	k.mu.Lock()
	if k.isRunning {
		k.mu.Unlock()
		return fmt.Errorf("kline websocket already running")
	}
	k.callback = callback
	k.isRunning = true
	k.mu.Unlock()

	go k.connect(ctx, instrumentName, resolution)
	return nil
}

// Stop 停止 K线 WebSocket
func (k *KlineWebSocketManager) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.isRunning {
		return
	}

	k.isRunning = false
	close(k.stopCh)

	if k.conn != nil {
		k.conn.Close()
	}
}

// connect 连接 WebSocket
func (k *KlineWebSocketManager) connect(ctx context.Context, instrumentName, resolution string) {
	for {
		select {
		case <-k.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(k.wsURL, nil)
		if err != nil {
			logger.Error("Deribit Kline WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("Deribit Kline WebSocket connected")

		// 订阅 K线
		if err := k.subscribe(instrumentName, resolution); err != nil {
			logger.Error("Deribit Kline WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go k.heartbeat()

		// 读取消息
		k.readMessages()

		// 连接断开，重连
		logger.Warn("Deribit Kline WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// subscribe 订阅 K线
func (k *KlineWebSocketManager) subscribe(instrumentName, resolution string) error {
	subMsg := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      k.getNextRequestID(),
		"method":  "public/subscribe",
		"params": map[string]interface{}{
			"channels": []string{
				"chart.trades." + instrumentName + "." + resolution,
			},
		},
	}

	return k.sendMessage(subMsg)
}

// sendMessage 发送消息
func (k *KlineWebSocketManager) sendMessage(msg interface{}) error {
	k.mu.RLock()
	conn := k.conn
	k.mu.RUnlock()

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
func (k *KlineWebSocketManager) heartbeat() {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-k.stopCh:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"jsonrpc": "2.0",
				"id":      k.getNextRequestID(),
				"method":  "public/test",
			}
			if err := k.sendMessage(pingMsg); err != nil {
				logger.Error("Deribit Kline WebSocket ping error: %v", err)
				return
			}
		}
	}
}

// readMessages 读取消息
func (k *KlineWebSocketManager) readMessages() {
	k.mu.RLock()
	conn := k.conn
	k.mu.RUnlock()

	if conn == nil {
		return
	}

	for {
		select {
		case <-k.stopCh:
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Error("Deribit Kline WebSocket read error: %v", err)
			return
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("Deribit Kline WebSocket unmarshal error: %v", err)
		return
	}

	// 处理订阅响应
	if result, ok := msg["result"].([]interface{}); ok {
		logger.Info("Deribit Kline WebSocket subscribe success: %v", result)
		return
	}

	// 处理心跳响应
	if result, ok := msg["result"].(string); ok && result == "pong" {
		return
	}

	// 处理 K线数据
	if method, ok := msg["method"].(string); ok && method == "subscription" {
		if params, ok := msg["params"].(map[string]interface{}); ok {
			if data, ok := params["data"].(map[string]interface{}); ok {
				k.parseKline(data)
			}
		}
	}
}

// parseKline 解析 K线数据
func (k *KlineWebSocketManager) parseKline(data map[string]interface{}) {
	var tick int64
	var open, high, low, close, volume float64

	if t, ok := data["tick"].(float64); ok {
		tick = int64(t)
	}
	if o, ok := data["open"].(float64); ok {
		open = o
	}
	if h, ok := data["high"].(float64); ok {
		high = h
	}
	if l, ok := data["low"].(float64); ok {
		low = l
	}
	if c, ok := data["close"].(float64); ok {
		close = c
	}
	if v, ok := data["volume"].(float64); ok {
		volume = v
	}

	if k.callback != nil && tick > 0 {
		k.callback(tick, open, high, low, close, volume)
	}
}

// getNextRequestID 获取下一个请求 ID
func (k *KlineWebSocketManager) getNextRequestID() int64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	id := k.requestID
	k.requestID++
	return id
}
