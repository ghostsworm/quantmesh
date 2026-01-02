package bitmex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// KlineWebSocketManager BitMEX K线 WebSocket 管理器
type KlineWebSocketManager struct {
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(*TradeBucket)
	isRunning bool
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(isTestnet bool) *KlineWebSocketManager {
	wsURL := BitMEXMainnetWSURL
	if isTestnet {
		wsURL = BitMEXTestnetWSURL
	}

	return &KlineWebSocketManager{
		wsURL:  wsURL,
		stopCh: make(chan struct{}),
	}
}

// Start 启动 K线 WebSocket
func (k *KlineWebSocketManager) Start(ctx context.Context, symbol, binSize string, callback func(*TradeBucket)) error {
	k.mu.Lock()
	if k.isRunning {
		k.mu.Unlock()
		return fmt.Errorf("kline websocket already running")
	}
	k.callback = callback
	k.isRunning = true
	k.mu.Unlock()

	go k.connect(ctx, symbol, binSize)
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
func (k *KlineWebSocketManager) connect(ctx context.Context, symbol, binSize string) {
	for {
		select {
		case <-k.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(k.wsURL, nil)
		if err != nil {
			logger.Error("BitMEX Kline WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("BitMEX Kline WebSocket connected")

		// 订阅 K线
		if err := k.subscribe(symbol, binSize); err != nil {
			logger.Error("BitMEX Kline WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go k.heartbeat()

		// 读取消息
		k.readMessages()

		// 连接断开，重连
		logger.Warn("BitMEX Kline WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// subscribe 订阅 K线
func (k *KlineWebSocketManager) subscribe(symbol, binSize string) error {
	subMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": []string{"tradeBin" + binSize + ":" + symbol},
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
	ticker := time.NewTicker(5 * time.Second)
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
				conn.WriteMessage(websocket.TextMessage, []byte("ping"))
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
			logger.Error("BitMEX Kline WebSocket read error: %v", err)
			return
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	// 处理 pong
	if string(message) == "pong" {
		return
	}

	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("BitMEX Kline WebSocket unmarshal error: %v", err)
		return
	}

	// 处理订阅响应
	if subscribe, ok := msg["subscribe"].(string); ok {
		logger.Info("BitMEX Kline WebSocket subscribed: %s", subscribe)
		return
	}

	// 处理 K线数据
	if _, ok := msg["table"].(string); ok {
		if data, ok := msg["data"].([]interface{}); ok && len(data) > 0 {
			for _, item := range data {
				if bucketData, ok := item.(map[string]interface{}); ok {
					bucket := k.parseTradeBucket(bucketData)
					if bucket != nil && k.callback != nil {
						k.callback(bucket)
					}
				}
			}
		}
	}
}

// parseTradeBucket 解析 K线数据
func (k *KlineWebSocketManager) parseTradeBucket(data map[string]interface{}) *TradeBucket {
	bucket := &TradeBucket{}

	if timestamp, ok := data["timestamp"].(string); ok {
		t, _ := time.Parse(time.RFC3339, timestamp)
		bucket.Timestamp = t
	}
	if symbol, ok := data["symbol"].(string); ok {
		bucket.Symbol = symbol
	}
	if open, ok := data["open"].(float64); ok {
		bucket.Open = open
	}
	if high, ok := data["high"].(float64); ok {
		bucket.High = high
	}
	if low, ok := data["low"].(float64); ok {
		bucket.Low = low
	}
	if close, ok := data["close"].(float64); ok {
		bucket.Close = close
	}
	if volume, ok := data["volume"].(float64); ok {
		bucket.Volume = volume
	}

	return bucket
}
