package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"quantmesh/logger"
)

// KlineWebSocketManager Kraken K線 WebSocket 管理器
type KlineWebSocketManager struct {
	client         *KrakenClient
	conn           *websocket.Conn
	symbols        []string
	interval       string
	callback       CandleUpdateCallback
	mu             sync.RWMutex
	stopChan       chan struct{}
	reconnectDelay time.Duration
}

// NewKlineWebSocketManager 創建 K線 WebSocket 管理器
func NewKlineWebSocketManager(client *KrakenClient, symbols []string, interval string) (*KlineWebSocketManager, error) {
	return &KlineWebSocketManager{
		client:         client,
		symbols:        symbols,
		interval:       interval,
		reconnectDelay: 5 * time.Second,
		stopChan:       make(chan struct{}),
	}, nil
}

// Start 啟动 K線流
func (k *KlineWebSocketManager) Start(ctx context.Context, callback CandleUpdateCallback) error {
	k.mu.Lock()
	k.callback = callback
	k.mu.Unlock()

	// 连接 WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(KrakenWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	k.conn = conn

	logger.Info("Kraken K線 WebSocket connected: %s", KrakenWSURL)

	// 订阅 K線流
	// Kraken 使用 "trade" feed 来獲取實時交易數據，然后自己聚合成 K線
	// 这里简化处理，订阅 ticker feed
	for _, symbol := range k.symbols {
		subscribeMsg := map[string]interface{}{
			"event":       "subscribe",
			"feed":        "trade",
			"product_ids": []string{symbol},
		}
		if err := conn.WriteJSON(subscribeMsg); err != nil {
			return fmt.Errorf("subscribe kline stream error: %w", err)
		}
		logger.Info("Kraken subscribed to K線 stream: %s, interval: %s", symbol, k.interval)
	}

	// 啟动消息处理
	go k.handleMessages(ctx)
	go k.ping(ctx)

	return nil
}

// handleMessages 处理 WebSocket 消息
func (k *KlineWebSocketManager) handleMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Kraken K線 WebSocket message handler panic: %v", r)
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
				logger.Error("Kraken K線 WebSocket read error: %v", err)
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
		Event string          `json:"event"`
		Feed  string          `json:"feed"`
		Data  json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		logger.Error("Kraken K線 unmarshal message error: %v, message: %s", err, string(message))
		return
	}

	// 处理不同類型的消息
	switch baseMsg.Event {
	case "info":
		logger.Info("Kraken K線 WebSocket info message received")
	case "subscribed":
		logger.Info("Kraken K線 WebSocket subscription confirmed: %s", baseMsg.Feed)
	case "heartbeat":
		// 心跳响应
	default:
		// 數據消息
		if strings.Contains(baseMsg.Feed, "trade") {
			k.handleTradeUpdate(message)
		}
	}
}

// handleTradeUpdate 处理交易更新（用於構建 K線）
func (k *KlineWebSocketManager) handleTradeUpdate(message []byte) {
	k.mu.RLock()
	callback := k.callback
	k.mu.RUnlock()

	if callback == nil {
		return
	}

	var tradeUpdate struct {
		Feed   string `json:"feed"`
		Symbol string `json:"product_id"`
		Trades []struct {
			Price   string `json:"price"`
			Qty     int    `json:"qty"`
			Side    string `json:"side"`
			Time    int64  `json:"time"`
			TradeID string `json:"uid"`
		} `json:"trades"`
	}

	if err := json.Unmarshal(message, &tradeUpdate); err != nil {
		logger.Error("Kraken unmarshal trade update error: %v", err)
		return
	}

	// 將交易數據轉换為 K線數據（简化处理）
	// 實際应該聚合多個交易到一個 K線週期
	for _, trade := range tradeUpdate.Trades {
		price, _ := strconv.ParseFloat(trade.Price, 64)

		candle := &Candle{
			Time:   trade.Time,
			Open:   trade.Price,
			High:   trade.Price,
			Low:    trade.Price,
			Close:  trade.Price,
			Volume: strconv.Itoa(trade.Qty),
		}

		logger.Debug("Kraken K線 update: %s, time: %d, price: %.2f, qty: %d",
			tradeUpdate.Symbol, trade.Time, price, trade.Qty)

		callback(candle)
	}
}

// ping 发送心跳
func (k *KlineWebSocketManager) ping(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.stopChan:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"event": "ping",
			}
			if err := k.conn.WriteJSON(pingMsg); err != nil {
				logger.Error("Kraken K線 send ping error: %v", err)
				return
			}
		}
	}
}

// reconnect 重连
func (k *KlineWebSocketManager) reconnect(ctx context.Context) {
	logger.Info("Kraken K線 WebSocket reconnecting...")
	time.Sleep(k.reconnectDelay)

	k.mu.RLock()
	callback := k.callback
	k.mu.RUnlock()

	if callback != nil {
		if err := k.Start(ctx, callback); err != nil {
			logger.Error("Kraken K線 reconnect error: %v", err)
		}
	}
}

// Stop 停止 K線流
func (k *KlineWebSocketManager) Stop() {
	close(k.stopChan)
	if k.conn != nil {
		k.conn.Close()
	}
	logger.Info("Kraken K線 WebSocket stopped")
}
