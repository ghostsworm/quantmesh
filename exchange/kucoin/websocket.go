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

// WebSocketManager KuCoin WebSocket 管理器
type WebSocketManager struct {
	client         *KuCoinClient
	conn           *websocket.Conn
	symbol         string
	token          *WebSocketToken
	orderCallback  func(interface{})
	priceCallback  func(float64)
	mu             sync.RWMutex
	stopChan       chan struct{}
	reconnectDelay time.Duration
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(client *KuCoinClient, symbol string) (*WebSocketManager, error) {
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

	// 获取 WebSocket token（私有频道）
	token, err := w.client.GetWebSocketToken(ctx, true)
	if err != nil {
		return fmt.Errorf("get websocket token error: %w", err)
	}
	w.token = token

	// 连接 WebSocket
	wsURL := fmt.Sprintf("%s?token=%s&connectId=%d", token.Endpoint, token.Token, time.Now().UnixNano())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	w.conn = conn

	logger.Info("KuCoin WebSocket connected: %s", wsURL)

	// 订阅订单更新
	subscribeMsg := map[string]interface{}{
		"id":             time.Now().UnixMilli(),
		"type":           "subscribe",
		"topic":          fmt.Sprintf("/contractMarket/tradeOrders:%s", w.symbol),
		"privateChannel": true,
		"response":       true,
	}
	if err := conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("subscribe order stream error: %w", err)
	}

	logger.Info("KuCoin subscribed to order stream: %s", w.symbol)

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

	// 获取 WebSocket token（公共频道）
	token, err := w.client.GetWebSocketToken(ctx, false)
	if err != nil {
		return fmt.Errorf("get websocket token error: %w", err)
	}
	w.token = token

	// 连接 WebSocket
	wsURL := fmt.Sprintf("%s?token=%s&connectId=%d", token.Endpoint, token.Token, time.Now().UnixNano())
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	w.conn = conn

	logger.Info("KuCoin WebSocket connected for price stream: %s", wsURL)

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
		"id":       time.Now().UnixMilli(),
		"type":     "subscribe",
		"topic":    fmt.Sprintf("/contractMarket/ticker:%s", w.symbol),
		"response": true,
	}
	if err := w.conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("subscribe price stream error: %w", err)
	}

	logger.Info("KuCoin subscribed to price stream: %s", w.symbol)
	return nil
}

// handleMessages 处理 WebSocket 消息
func (w *WebSocketManager) handleMessages(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("KuCoin WebSocket message handler panic: %v", r)
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
				logger.Error("KuCoin WebSocket read error: %v", err)
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
		Type    string          `json:"type"`
		Topic   string          `json:"topic"`
		Subject string          `json:"subject"`
		Data    json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(message, &baseMsg); err != nil {
		logger.Error("KuCoin unmarshal message error: %v, message: %s", err, string(message))
		return
	}

	// 处理不同类型的消息
	switch baseMsg.Type {
	case "welcome":
		logger.Info("KuCoin WebSocket welcome message received")
	case "ack":
		logger.Info("KuCoin WebSocket subscription acknowledged")
	case "pong":
		// 心跳响应
	case "message":
		w.handleDataMessage(baseMsg.Topic, baseMsg.Subject, baseMsg.Data)
	default:
		logger.Warn("KuCoin unknown message type: %s", baseMsg.Type)
	}
}

// handleDataMessage 处理数据消息
func (w *WebSocketManager) handleDataMessage(topic, subject string, data json.RawMessage) {
	// 订单更新
	if strings.Contains(topic, "/contractMarket/tradeOrders") {
		w.handleOrderUpdate(data)
		return
	}

	// 价格更新
	if strings.Contains(topic, "/contractMarket/ticker") {
		w.handlePriceUpdate(data)
		return
	}
}

// handleOrderUpdate 处理订单更新
func (w *WebSocketManager) handleOrderUpdate(data json.RawMessage) {
	w.mu.RLock()
	callback := w.orderCallback
	w.mu.RUnlock()

	if callback == nil {
		return
	}

	var orderUpdate struct {
		Symbol        string `json:"symbol"`
		OrderType     string `json:"orderType"`
		Side          string `json:"side"`
		OrderId       string `json:"orderId"`
		Type          string `json:"type"`
		Status        string `json:"status"`
		MatchSize     string `json:"matchSize"`
		MatchPrice    string `json:"matchPrice"`
		OrderTime     int64  `json:"orderTime"`
		Size          int    `json:"size"`
		FilledSize    int    `json:"filledSize"`
		Price         string `json:"price"`
		ClientOid     string `json:"clientOid"`
		RemainSize    int    `json:"remainSize"`
		Liquidity     string `json:"liquidity"`
		Ts            int64  `json:"ts"`
	}

	if err := json.Unmarshal(data, &orderUpdate); err != nil {
		logger.Error("KuCoin unmarshal order update error: %v", err)
		return
	}

	logger.Info("KuCoin order update: %s, status: %s, filled: %d/%d", orderUpdate.OrderId, orderUpdate.Status, orderUpdate.FilledSize, orderUpdate.Size)
	callback(orderUpdate)
}

// handlePriceUpdate 处理价格更新
func (w *WebSocketManager) handlePriceUpdate(data json.RawMessage) {
	w.mu.RLock()
	callback := w.priceCallback
	w.mu.RUnlock()

	if callback == nil {
		return
	}

	var priceUpdate struct {
		Symbol      string  `json:"symbol"`
		Sequence    int64   `json:"sequence"`
		Side        string  `json:"side"`
		Price       float64 `json:"price"`
		Size        int     `json:"size"`
		TradeId     string  `json:"tradeId"`
		BestBidSize int     `json:"bestBidSize"`
		BestBidPrice float64 `json:"bestBidPrice"`
		BestAskSize int     `json:"bestAskSize"`
		BestAskPrice float64 `json:"bestAskPrice"`
		Ts          int64   `json:"ts"`
	}

	if err := json.Unmarshal(data, &priceUpdate); err != nil {
		logger.Error("KuCoin unmarshal price update error: %v", err)
		return
	}

	// 使用最新成交价
	callback(priceUpdate.Price)
}

// ping 发送心跳
func (w *WebSocketManager) ping(ctx context.Context) {
	if w.token == nil {
		return
	}

	ticker := time.NewTicker(time.Duration(w.token.PingInterval) * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopChan:
			return
		case <-ticker.C:
			pingMsg := map[string]interface{}{
				"id":   time.Now().UnixMilli(),
				"type": "ping",
			}
			if err := w.conn.WriteJSON(pingMsg); err != nil {
				logger.Error("KuCoin send ping error: %v", err)
				return
			}
		}
	}
}

// reconnect 重连
func (w *WebSocketManager) reconnect(ctx context.Context) {
	logger.Info("KuCoin WebSocket reconnecting...")
	time.Sleep(w.reconnectDelay)

	w.mu.RLock()
	orderCallback := w.orderCallback
	priceCallback := w.priceCallback
	w.mu.RUnlock()

	if orderCallback != nil {
		if err := w.StartOrderStream(ctx, orderCallback); err != nil {
			logger.Error("KuCoin reconnect order stream error: %v", err)
		}
	}

	if priceCallback != nil {
		if err := w.StartPriceStream(ctx, priceCallback); err != nil {
			logger.Error("KuCoin reconnect price stream error: %v", err)
		}
	}
}

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() {
	close(w.stopChan)
	if w.conn != nil {
		w.conn.Close()
	}
	logger.Info("KuCoin WebSocket stopped")
}

