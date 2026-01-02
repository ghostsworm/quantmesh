package bitrue

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// KlineWebSocketManager Bitrue K线 WebSocket 管理器
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
	wsURL := BitrueMainnetWSURL
	if isTestnet {
		wsURL = BitrueTestnetWSURL
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
			logger.Error("Bitrue Kline WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("Bitrue Kline WebSocket connected")

		// 订阅 K线
		if err := k.subscribe(symbol, interval); err != nil {
			logger.Error("Bitrue Kline WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go k.heartbeat()

		// 读取消息
		k.readMessages()

		// 连接断开，重连
		logger.Warn("Bitrue Kline WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// subscribe 订阅 K线
func (k *KlineWebSocketManager) subscribe(symbol, interval string) error {
	subMsg := map[string]interface{}{
		"method": "SUBSCRIBE",
		"params": []string{
			fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), interval),
		},
		"id": 1,
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
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-k.stopCh:
			return
		case <-ticker.C:
			k.mu.RLock()
			conn := k.conn
			k.mu.RUnlock()
			if conn != nil {
				conn.WriteMessage(websocket.PingMessage, []byte{})
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
			logger.Error("Bitrue Kline WebSocket read error: %v", err)
			return
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("Bitrue Kline WebSocket unmarshal error: %v", err)
		return
	}

	// 处理订阅响应
	if result, ok := msg["result"].(interface{}); ok && result == nil {
		logger.Info("Bitrue Kline WebSocket subscribed")
		return
	}

	// 处理 K线数据
	if data, ok := msg["data"].(map[string]interface{}); ok {
		if klineData, ok := data["k"].(map[string]interface{}); ok {
			kline := k.parseKline(klineData)
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
		kline.OpenTime = int64(t)
	}
	if o, ok := data["o"].(string); ok {
		kline.Open = o
	}
	if h, ok := data["h"].(string); ok {
		kline.High = h
	}
	if l, ok := data["l"].(string); ok {
		kline.Low = l
	}
	if c, ok := data["c"].(string); ok {
		kline.Close = c
	}
	if v, ok := data["v"].(string); ok {
		kline.Volume = v
	}

	return kline
}
