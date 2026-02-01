package coinex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// KlineWebSocketManager CoinEx K线 WebSocket 管理器
type KlineWebSocketManager struct {
	wsURL     string
	conn      *websocket.Conn
	mu        sync.RWMutex
	stopCh    chan struct{}
	callback  func(*Kline)
	isRunning bool
	reqID     int64
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(isTestnet bool) *KlineWebSocketManager {
	wsURL := CoinExMainnetWSURL
	if isTestnet {
		wsURL = CoinExTestnetWSURL
	}

	return &KlineWebSocketManager{
		wsURL:  wsURL,
		stopCh: make(chan struct{}),
		reqID:  1,
	}
}

// Start 启动 K线 WebSocket
func (k *KlineWebSocketManager) Start(ctx context.Context, market, period string, callback func(*Kline)) error {
	k.mu.Lock()
	if k.isRunning {
		k.mu.Unlock()
		return fmt.Errorf("kline websocket already running")
	}
	k.callback = callback
	k.isRunning = true
	k.mu.Unlock()

	go k.connect(ctx, market, period)
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
func (k *KlineWebSocketManager) connect(ctx context.Context, market, period string) {
	for {
		select {
		case <-k.stopCh:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(k.wsURL, nil)
		if err != nil {
			logger.Error("CoinEx Kline WebSocket dial error: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("CoinEx Kline WebSocket connected")

		// 订阅 K线
		if err := k.subscribe(market, period); err != nil {
			logger.Error("CoinEx Kline WebSocket subscribe error: %v", err)
			conn.Close()
			time.Sleep(5 * time.Second)
			continue
		}

		// 启动心跳
		go k.heartbeat()

		// 读取消息
		k.readMessages()

		// 连接断开，重连
		logger.Warn("CoinEx Kline WebSocket disconnected, reconnecting...")
		time.Sleep(5 * time.Second)
	}
}

// subscribe 订阅 K线
func (k *KlineWebSocketManager) subscribe(market, period string) error {
	subMsg := map[string]interface{}{
		"method": "kline.subscribe",
		"params": []interface{}{market, period},
		"id":     k.getNextReqID(),
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
			pingMsg := map[string]interface{}{
				"method": "server.ping",
				"params": []interface{}{},
				"id":     k.getNextReqID(),
			}
			k.sendMessage(pingMsg)
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
			logger.Error("CoinEx Kline WebSocket read error: %v", err)
			return
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Error("CoinEx Kline WebSocket unmarshal error: %v", err)
		return
	}

	// 处理订阅响应
	if method, ok := msg["method"].(string); ok && method == "kline.subscribe" {
		logger.Info("CoinEx Kline WebSocket subscribed")
		return
	}

	// 处理心跳响应
	if method, ok := msg["method"].(string); ok && method == "server.ping" {
		return
	}

	// 处理 K线数据
	if method, ok := msg["method"].(string); ok && method == "kline.update" {
		if params, ok := msg["params"].([]interface{}); ok && len(params) > 0 {
			if klineData, ok := params[0].([]interface{}); ok && len(klineData) >= 7 {
				kline := k.parseKline(klineData)
				if kline != nil && k.callback != nil {
					k.callback(kline)
				}
			}
		}
	}
}

// parseKline 解析 K线数据
func (k *KlineWebSocketManager) parseKline(data []interface{}) *Kline {
	if len(data) < 7 {
		return nil
	}

	kline := &Kline{}

	if timestamp, ok := data[0].(float64); ok {
		kline.Timestamp = int64(timestamp)
	}
	if open, ok := data[1].(string); ok {
		kline.Open = open
	}
	if close, ok := data[2].(string); ok {
		kline.Close = close
	}
	if high, ok := data[3].(string); ok {
		kline.High = high
	}
	if low, ok := data[4].(string); ok {
		kline.Low = low
	}
	if volume, ok := data[5].(string); ok {
		kline.Volume = volume
	}
	if market, ok := data[6].(string); ok {
		kline.Market = market
	}

	return kline
}

// getNextReqID 获取下一个请求 ID
func (k *KlineWebSocketManager) getNextReqID() int64 {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reqID++
	return k.reqID
}
