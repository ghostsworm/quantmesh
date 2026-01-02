package bingx

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// KlineWebSocketManager BingX K线 WebSocket 管理器
type KlineWebSocketManager struct {
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(*Kline)
	isRunning bool
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(isTestnet bool) *KlineWebSocketManager {
	wsURL := BingXMainnetWSURL
	if isTestnet {
		wsURL = BingXTestnetWSURL
	}

	return &KlineWebSocketManager{
		wsURL:  wsURL,
		stopCh: make(chan struct{}),
	}
}

// Start 启动 K线 WebSocket
func (k *KlineWebSocketManager) Start(ctx context.Context, symbol, interval string, callback func(*Kline)) error {
	k.mu.Lock()
	if k.isRunning {
		k.mu.Unlock()
		return fmt.Errorf("kline websocket already running")
	}
	k.callback = callback
	k.isRunning = true
	k.mu.Unlock()

	go k.connect(ctx, symbol, interval)
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
func (k *KlineWebSocketManager) connect(ctx context.Context, symbol, interval string) {
	for {
		select {
		case <-k.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(k.wsURL, nil)
		if err != nil {
			logger.Error("BingX Kline WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("BingX Kline WebSocket connected")

		// 订阅 K线
		if err := k.subscribe(symbol, interval); err != nil {
			logger.Error("BingX Kline WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go k.heartbeat()

		// 读取消息
		k.readMessages()

		// 连接断开，重连
		logger.Warn("BingX Kline WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// subscribe 订阅 K线
func (k *KlineWebSocketManager) subscribe(symbol, interval string) error {
	subMsg := map[string]interface{}{
		"id":       "kline_sub",
		"reqType":  "sub",
		"dataType": symbol + "@kline_" + interval,
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
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-k.stopCh:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"id":      "ping",
				"reqType": "ping",
			}
			if err := k.sendMessage(pingMsg); err != nil {
				logger.Error("BingX Kline WebSocket ping error: %v", err)
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
			logger.Error("BingX Kline WebSocket read error: %v", err)
			return
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("BingX Kline WebSocket unmarshal error: %v", err)
		return
	}

	// 处理 pong
	if reqType, ok := msg["reqType"].(string); ok && reqType == "pong" {
		return
	}

	// 处理订阅响应
	if id, ok := msg["id"].(string); ok && id == "kline_sub" {
		if code, ok := msg["code"].(float64); ok && code == 0 {
			logger.Info("BingX Kline WebSocket subscribe success")
		} else {
			logger.Error("BingX Kline WebSocket subscribe failed: %v", msg)
		}
		return
	}

	// 处理 K线数据
	if _, ok := msg["dataType"].(string); ok {
		if data, ok := msg["data"].(map[string]interface{}); ok {
			kline := k.parseKline(data)
			if kline != nil && k.callback != nil {
				k.callback(kline)
			}
		}
	}
}

// parseKline 解析 K线数据
func (k *KlineWebSocketManager) parseKline(data map[string]interface{}) *Kline {
	kline := &Kline{}

	if t, ok := data["t"].(float64); ok {
		kline.Time = int64(t)
	}
	if o, ok := data["o"].(string); ok {
		fmt.Sscanf(o, "%f", &kline.Open)
	}
	if h, ok := data["h"].(string); ok {
		fmt.Sscanf(h, "%f", &kline.High)
	}
	if l, ok := data["l"].(string); ok {
		fmt.Sscanf(l, "%f", &kline.Low)
	}
	if c, ok := data["c"].(string); ok {
		fmt.Sscanf(c, "%f", &kline.Close)
	}
	if v, ok := data["v"].(string); ok {
		fmt.Sscanf(v, "%f", &kline.Volume)
	}

	return kline
}
