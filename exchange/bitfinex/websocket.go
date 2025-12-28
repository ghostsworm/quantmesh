package bitfinex

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

const (
	BitfinexWSURL = "wss://futures.bitfinex.com/ws/v1"
)

// WebSocketManager Bitfinex WebSocket 管理器
type WebSocketManager struct {
	client         *BitfinexClient
	conn           *websocket.Conn
	symbol         string
	orderCallback  func(interface{})
	priceCallback  func(float64)
	mu             sync.RWMutex
	stopChan       chan struct{}
	reconnectDelay time.Duration
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(client *BitfinexClient, symbol string) (*WebSocketManager, error) {
	return &WebSocketManager{
		client:         client,
		symbol:         symbol,
		reconnectDelay: 5 * time.Second,
		stopChan:       make(chan struct{}),
	}, nil
}

// StartOrderStream 启动订单流
func (w *WebSocketManager) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	w.mu.Lock()
	w.orderCallback = callback
	w.mu.Unlock()

	// 连接 WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(BitfinexWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	w.conn = conn

	logger.Info("Bitfinex WebSocket connected: %s", BitfinexWSURL)

	// 认证（如果需要私有频道）
	// Bitfinex 使用 challenge-response 认证机制
	// 这里简化处理，实际需要实现完整的认证流程

	// 订阅订单更新
	subscribeMsg := map[string]interface{}{
		"event": "subscribe",
		"feed":  "fills",
	}
	if err := conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("subscribe order stream error: %w", err)
	}

	logger.Info("Bitfinex subscribed to order stream")

	// 启动消息处理
	go w.handleMessages(ctx)
	go w.ping(ctx)

	return nil
}

// StartPriceStream 启动价格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, callback func(float64)) error {
	w.mu.Lock()
	w.priceCallback = callback
	w.mu.Unlock()

	// 如果已经连接，直接订阅价格流
	if w.conn != nil {
		return w.subscribePriceStream()
	}

	// 连接 WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(BitfinexWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	w.conn = conn

	logger.Info("Bitfinex WebSocket connected for price stream: %s", BitfinexWSURL)

	// 订阅价格流
	if err := w.subscribePriceStream(); err != nil {
		return err
	}

	// 启动消息处理
	go w.handleMessages(ctx)
	go w.ping(ctx)

	return nil
}

// subscribePriceStream 订阅价格流
func (w *WebSocketManager) subscribePriceStream() error {
	subscribeMsg := map[string]interface{}{
		"event":        "subscribe",
		"feed":         "ticker",
		"product_ids":  []string{w.symbol},
	}
	if err := w.conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("subscribe price stream error: %w", err)
	}

	logger.Info("Bitfinex subscribed to price stream: %s", w.symbol)
	return nil
}

// handleMessages 处理 WebSocket 消息
func (w *WebSocketManager) handleMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("Bitfinex WebSocket message handler panic: %v", r)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		default:
			_, message, err := w.conn.ReadMessage()
			if err != nil {
				logger.Error("Bitfinex WebSocket read error: %v", err)
				w.reconnect(ctx)
				return
			}

			w.processMessage(message)
		}
	}
}

// processMessage 处理消息
func (w *WebSocketManager) processMessage(message []byte) {
	var baseMsg struct {
		Event string          `json:"event"`
		Feed  string          `json:"feed"`
		Data  json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		logger.Error("Bitfinex unmarshal message error: %v, message: %s", err, string(message))
		return
	}

	// 处理不同类型的消息
	switch baseMsg.Event {
	case "info":
		logger.Info("Bitfinex WebSocket info message received")
	case "subscribed":
		logger.Info("Bitfinex WebSocket subscription confirmed: %s", baseMsg.Feed)
	case "heartbeat":
		// 心跳响应
	default:
		// 数据消息
		w.handleDataMessage(baseMsg.Feed, message)
	}
}

// handleDataMessage 处理数据消息
func (w *WebSocketManager) handleDataMessage(feed string, message []byte) {
	// 订单更新
	if strings.Contains(feed, "fills") {
		w.handleOrderUpdate(message)
		return
	}

	// 价格更新
	if strings.Contains(feed, "ticker") {
		w.handlePriceUpdate(message)
		return
	}
}

// handleOrderUpdate 处理订单更新
func (w *WebSocketManager) handleOrderUpdate(message []byte) {
	w.mu.RLock()
	callback := w.orderCallback
	w.mu.RUnlock()

	if callback == nil {
		return
	}

	var orderUpdate struct {
		Feed string `json:"feed"`
		Data []struct {
			OrderID    string  `json:"order_id"`
			CliOrdId   string  `json:"cliOrdId"`
			Symbol     string  `json:"instrument"`
			Side       string  `json:"side"`
			Quantity   int     `json:"qty"`
			Filled     int     `json:"filled"`
			Price      float64 `json:"price"`
			FillTime   string  `json:"fillTime"`
		} `json:"fills"`
	}

	if err := json.Unmarshal(message, &orderUpdate); err != nil {
		logger.Error("Bitfinex unmarshal order update error: %v", err)
		return
	}

	for _, fill := range orderUpdate.Data {
		logger.Info("Bitfinex order update: %s, filled: %d/%d", fill.OrderID, fill.Filled, fill.Quantity)
		callback(fill)
	}
}

// handlePriceUpdate 处理价格更新
func (w *WebSocketManager) handlePriceUpdate(message []byte) {
	w.mu.RLock()
	callback := w.priceCallback
	w.mu.RUnlock()

	if callback == nil {
		return
	}

	var priceUpdate struct {
		Feed   string `json:"feed"`
		Symbol string `json:"product_id"`
		Time   int64  `json:"time"`
		Bid    float64 `json:"bid"`
		Ask    float64 `json:"ask"`
		Last   float64 `json:"last"`
		Volume float64 `json:"volume"`
	}

	if err := json.Unmarshal(message, &priceUpdate); err != nil {
		logger.Error("Bitfinex unmarshal price update error: %v", err)
		return
	}

	// 使用最新成交价
	if priceUpdate.Last > 0 {
		callback(priceUpdate.Last)
	}
}

// ping 发送心跳
func (w *WebSocketManager) ping(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"event": "ping",
			}
			if err := w.conn.WriteJSON(pingMsg); err != nil {
				logger.Error("Bitfinex send ping error: %v", err)
				return
			}
		}
	}
}

// reconnect 重连
func (w *WebSocketManager) reconnect(ctx context.Context) {
	logger.Info("Bitfinex WebSocket reconnecting...")
	time.Sleep(w.reconnectDelay)

	w.mu.RLock()
	orderCallback := w.orderCallback
	priceCallback := w.priceCallback
	w.mu.RUnlock()

	if orderCallback != nil {
		if err := w.StartOrderStream(ctx, orderCallback); err != nil {
			logger.Error("Bitfinex reconnect order stream error: %v", err)
		}
	}

	if priceCallback != nil {
		if err := w.StartPriceStream(ctx, priceCallback); err != nil {
			logger.Error("Bitfinex reconnect price stream error: %v", err)
		}
	}
}

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() {
	close(w.stopChan)
	if w.conn != nil {
		w.conn.Close()
	}
	logger.Info("Bitfinex WebSocket stopped")
}

