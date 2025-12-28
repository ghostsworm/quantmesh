package kucoin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"quantmesh/logger"
)

// KlineWebSocketManager KuCoin K线 WebSocket 管理器
type KlineWebSocketManager struct {
	client         *KuCoinClient
	conn           *websocket.Conn
	symbols        []string
	interval       string
	token          *WebSocketToken
	callback       CandleUpdateCallback
	mu             sync.RWMutex
	stopChan       chan struct{}
	reconnectDelay time.Duration
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(client *KuCoinClient, symbols []string, interval string) (*KlineWebSocketManager, error) {
	return &KlineWebSocketManager{
		client:         client,
		symbols:        symbols,
		interval:       interval,
		reconnectDelay: 5 * time.Second,
		stopChan:       make(chan struct{}),
	}, nil
}

// Start 启动 K线流
func (k *KlineWebSocketManager) Start(ctx context.Context, callback CandleUpdateCallback) error {
	k.mu.Lock()
	k.callback = callback
	k.mu.Unlock()

	// 获取 WebSocket token（公共频道）
	token, err := k.client.GetWebSocketToken(ctx, false)
	if err != nil {
		return fmt.Errorf("get websocket token error: %w", err)
	}
	k.token = token

	// 连接 WebSocket
	wsURL := fmt.Sprintf("%s?token=%s&connectId=%d", token.Endpoint, token.Token, time.Now().UnixNano())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	k.conn = conn

	logger.Info("KuCoin K线 WebSocket connected: %s", wsURL)

	// 订阅 K线流
	granularity := convertIntervalToGranularity(k.interval)
	for _, symbol := range k.symbols {
		subscribeMsg := map[string]interface{}{
			"id":       time.Now().UnixMilli(),
			"type":     "subscribe",
			"topic":    fmt.Sprintf("/contractMarket/candle:%s_%dm", symbol, granularity),
			"response": true,
		}
		if err := conn.WriteJSON(subscribeMsg); err != nil {
			return fmt.Errorf("subscribe kline stream error: %w", err)
		}
		logger.Info("KuCoin subscribed to K线 stream: %s, interval: %s", symbol, k.interval)
	}

	// 启动消息处理
	go k.handleMessages(ctx)
	go k.ping(ctx)

	return nil
}

// handleMessages 处理 WebSocket 消息
func (k *KlineWebSocketManager) handleMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("KuCoin K线 WebSocket message handler panic: %v", r)
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
				logger.Error("KuCoin K线 WebSocket read error: %v", err)
				k.reconnect(ctx)
				return
			}

			k.processMessage(message)
		}
	}
}

// processMessage 处理消息
func (k *KlineWebSocketManager) processMessage(message []byte) {
	var baseMsg struct {
		Type    string          `json:"type"`
		Topic   string          `json:"topic"`
		Subject string          `json:"subject"`
		Data    json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		logger.Error("KuCoin K线 unmarshal message error: %v, message: %s", err, string(message))
		return
	}

	// 处理不同类型的消息
	switch baseMsg.Type {
	case "welcome":
		logger.Info("KuCoin K线 WebSocket welcome message received")
	case "ack":
		logger.Info("KuCoin K线 WebSocket subscription acknowledged")
	case "pong":
		// 心跳响应
	case "message":
		if strings.Contains(baseMsg.Topic, "/contractMarket/candle") {
			k.handleCandleUpdate(baseMsg.Data)
		}
	default:
		logger.Warn("KuCoin K线 unknown message type: %s", baseMsg.Type)
	}
}

// handleCandleUpdate 处理 K线更新
func (k *KlineWebSocketManager) handleCandleUpdate(data json.RawMessage) {
	k.mu.RLock()
	callback := k.callback
	k.mu.RUnlock()

	if callback == nil {
		return
	}

	var candleUpdate struct {
		Symbol    string  `json:"symbol"`
		Candles   []interface{} `json:"candles"` // [timestamp, open, close, high, low, volume, turnover]
		Time      int64   `json:"time"`
	}

	if err := json.Unmarshal(data, &candleUpdate); err != nil {
		logger.Error("KuCoin unmarshal candle update error: %v", err)
		return
	}

	if len(candleUpdate.Candles) < 7 {
		logger.Error("KuCoin invalid candle data: %v", candleUpdate.Candles)
		return
	}

	// 解析 K线数据
	timestamp, _ := candleUpdate.Candles[0].(float64)
	open, _ := parseFloat(candleUpdate.Candles[1])
	close, _ := parseFloat(candleUpdate.Candles[2])
	high, _ := parseFloat(candleUpdate.Candles[3])
	low, _ := parseFloat(candleUpdate.Candles[4])
	volume, _ := parseFloat(candleUpdate.Candles[5])

	candle := &Candle{
		Time:   int64(timestamp),
		Open:   open,
		High:   high,
		Low:    low,
		Close:  close,
		Volume: volume,
	}

	logger.Debug("KuCoin K线 update: %s, time: %d, open: %.2f, high: %.2f, low: %.2f, close: %.2f, volume: %.2f",
		candleUpdate.Symbol, candle.Time, candle.Open, candle.High, candle.Low, candle.Close, candle.Volume)

	callback(candle)
}

// parseFloat 解析浮点数
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		var f float64
		_, err := fmt.Sscanf(val, "%f", &f)
		return f, err
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// ping 发送心跳
func (k *KlineWebSocketManager) ping(ctx context.Context) {
	if k.token == nil {
		return
	}

	ticker := time.NewTicker(time.Duration(k.token.PingInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.stopChan:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"id":   time.Now().UnixMilli(),
				"type": "ping",
			}
			if err := k.conn.WriteJSON(pingMsg); err != nil {
				logger.Error("KuCoin K线 send ping error: %v", err)
				return
			}
		}
	}
}

// reconnect 重连
func (k *KlineWebSocketManager) reconnect(ctx context.Context) {
	logger.Info("KuCoin K线 WebSocket reconnecting...")
	time.Sleep(k.reconnectDelay)

	k.mu.RLock()
	callback := k.callback
	k.mu.RUnlock()

	if callback != nil {
		if err := k.Start(ctx, callback); err != nil {
			logger.Error("KuCoin K线 reconnect error: %v", err)
		}
	}
}

// Stop 停止 K线流
func (k *KlineWebSocketManager) Stop() {
	close(k.stopChan)
	if k.conn != nil {
		k.conn.Close()
	}
	logger.Info("KuCoin K线 WebSocket stopped")
}

