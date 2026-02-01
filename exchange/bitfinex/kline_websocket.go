package bitfinex

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"quantmesh/logger"
)

// KlineWebSocketManager Bitfinex K线 WebSocket 管理器
type KlineWebSocketManager struct {
	client         *BitfinexClient
	conn           *websocket.Conn
	symbols        []string
	interval       string
	callback       CandleUpdateCallback
	mu             sync.RWMutex
	stopChan       chan struct{}
	reconnectDelay time.Duration
	channelMap     map[int]string // chanID -> symbol
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(client *BitfinexClient, symbols []string, interval string) (*KlineWebSocketManager, error) {
	return &KlineWebSocketManager{
		client:         client,
		symbols:        symbols,
		interval:       interval,
		reconnectDelay: 5 * time.Second,
		stopChan:       make(chan struct{}),
		channelMap:     make(map[int]string),
	}, nil
}

// Start 启动 K线流
func (k *KlineWebSocketManager) Start(ctx context.Context, callback CandleUpdateCallback) error {
	k.mu.Lock()
	k.callback = callback
	k.mu.Unlock()

	// 连接 WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(BitfinexWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	k.conn = conn

	logger.Info("Bitfinex K线 WebSocket connected: %s", BitfinexWSURL)

	// 订阅 K线流
	timeframe := convertIntervalToTimeframe(k.interval)
	for _, symbol := range k.symbols {
		subscribeMsg := map[string]interface{}{
			"event":   "subscribe",
			"channel": "candles",
			"key":     fmt.Sprintf("trade:%s:t%s", timeframe, symbol),
		}
		if err := conn.WriteJSON(subscribeMsg); err != nil {
			return fmt.Errorf("subscribe kline stream error: %w", err)
		}
		logger.Info("Bitfinex subscribed to K线 stream: %s, interval: %s", symbol, k.interval)
	}

	// 启动消息处理
	go k.handleMessages(ctx)

	return nil
}

// handleMessages 处理 WebSocket 消息
func (k *KlineWebSocketManager) handleMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Bitfinex K线 WebSocket message handler panic: %v", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.stopChan:
			return
		default:
			_, message, err := k.conn.ReadMessage()
			if err != nil {
				logger.Error("Bitfinex K线 WebSocket read error: %v", err)
				k.reconnect(ctx)
				return
			}

			k.processMessage(message)
		}
	}
}

// processMessage 处理消息
func (k *KlineWebSocketManager) processMessage(message []byte) {
	// 尝试解析为事件消息
	var eventMsg map[string]interface{}
	if err := json.Unmarshal(message, &eventMsg); err == nil {
		k.handleEventMessage(eventMsg)
		return
	}

	// 尝试解析为数据消息
	var dataMsg []interface{}
	if err := json.Unmarshal(message, &dataMsg); err == nil {
		k.handleDataMessage(dataMsg)
		return
	}
}

// handleEventMessage 处理事件消息
func (k *KlineWebSocketManager) handleEventMessage(msg map[string]interface{}) {
	event, ok := msg["event"].(string)
	if !ok {
		return
	}

	switch event {
	case "subscribed":
		chanID, _ := msg["chanId"].(float64)
		key, _ := msg["key"].(string)
		k.mu.Lock()
		k.channelMap[int(chanID)] = key
		k.mu.Unlock()
		logger.Info("Bitfinex K线 subscribed, chanID: %d, key: %s", int(chanID), key)
	case "error":
		logger.Error("Bitfinex K线 WebSocket error: %v", msg)
	}
}

// handleDataMessage 处理数据消息
func (k *KlineWebSocketManager) handleDataMessage(msg []interface{}) {
	if len(msg) < 2 {
		return
	}

	chanID, ok := msg[0].(float64)
	if !ok {
		return
	}

	// 检查是否是心跳消息
	if hb, ok := msg[1].(string); ok && hb == "hb" {
		return
	}

	// K线数据
	k.handleCandleData(int(chanID), msg[1])
}

// handleCandleData 处理 K线数据
func (k *KlineWebSocketManager) handleCandleData(chanID int, data interface{}) {
	k.mu.RLock()
	callback := k.callback
	key := k.channelMap[chanID]
	k.mu.RUnlock()

	if callback == nil {
		return
	}

	// K线格式：[MTS, OPEN, CLOSE, HIGH, LOW, VOLUME]
	candleArray, ok := data.([]interface{})
	if !ok || len(candleArray) < 6 {
		return
	}

	timestamp, ok := candleArray[0].(float64)
	if !ok {
		return
	}

	candle := &Candle{
		Timestamp: int64(timestamp),
		Open:      parseFloat64(candleArray[1]),
		Close:     parseFloat64(candleArray[2]),
		High:      parseFloat64(candleArray[3]),
		Low:       parseFloat64(candleArray[4]),
		Volume:    parseFloat64(candleArray[5]),
	}

	logger.Debug("Bitfinex K线 update: key=%s, time=%d, open=%.2f, high=%.2f, low=%.2f, close=%.2f, volume=%.2f",
		key, candle.Timestamp, candle.Open, candle.High, candle.Low, candle.Close, candle.Volume)

	callback(candle)
}

// reconnect 重连
func (k *KlineWebSocketManager) reconnect(ctx context.Context) {
	logger.Info("Bitfinex K线 WebSocket reconnecting...")
	time.Sleep(k.reconnectDelay)

	k.mu.RLock()
	callback := k.callback
	k.mu.RUnlock()

	if callback != nil {
		if err := k.Start(ctx, callback); err != nil {
			logger.Error("Bitfinex K线 reconnect error: %v", err)
		}
	}
}

// Stop 停止 K线流
func (k *KlineWebSocketManager) Stop() {
	close(k.stopChan)
	if k.conn != nil {
		k.conn.Close()
	}
	logger.Info("Bitfinex K线 WebSocket stopped")
}
