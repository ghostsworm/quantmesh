package bitfinex

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"quantmesh/logger"
)

const (
	BitfinexWSURL = "wss://api.bitfinex.com/ws/2"
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
	channelMap     map[int]string // chanID -> channel type
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(client *BitfinexClient, symbol string) (*WebSocketManager, error) {
	return &WebSocketManager{
		client:         client,
		symbol:         symbol,
		reconnectDelay: 5 * time.Second,
		stopChan:       make(chan struct{}),
		channelMap:     make(map[int]string),
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

	// 认证
	if err := w.authenticate(); err != nil {
		return fmt.Errorf("authenticate error: %w", err)
	}

	// 启动消息处理
	go w.handleMessages(ctx)

	return nil
}

// StartPriceStream 启动价格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, callback func(float64)) error {
	w.mu.Lock()
	w.priceCallback = callback
	w.mu.Unlock()

	// 如果已经连接，订阅 ticker
	if w.conn != nil {
		return w.subscribeTicker()
	}

	// 连接 WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(BitfinexWSURL, nil)
	if err != nil {
		return fmt.Errorf("dial websocket error: %w", err)
	}
	w.conn = conn

	logger.Info("Bitfinex WebSocket connected for price stream: %s", BitfinexWSURL)

	// 订阅 ticker
	if err := w.subscribeTicker(); err != nil {
		return err
	}

	// 启动消息处理
	go w.handleMessages(ctx)

	return nil
}

// authenticate Bitfinex WebSocket 认证
func (w *WebSocketManager) authenticate() error {
	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)
	authPayload := "AUTH" + nonce

	h := hmac.New(sha512.New384, []byte(w.client.secretKey))
	h.Write([]byte(authPayload))
	signature := hex.EncodeToString(h.Sum(nil))

	authMsg := map[string]interface{}{
		"event":       "auth",
		"apiKey":      w.client.apiKey,
		"authSig":     signature,
		"authNonce":   nonce,
		"authPayload": authPayload,
	}

	if err := w.conn.WriteJSON(authMsg); err != nil {
		return fmt.Errorf("send auth message error: %w", err)
	}

	logger.Info("Bitfinex WebSocket authentication sent")
	return nil
}

// subscribeTicker 订阅 ticker
func (w *WebSocketManager) subscribeTicker() error {
	subscribeMsg := map[string]interface{}{
		"event":   "subscribe",
		"channel": "ticker",
		"symbol":  "t" + w.symbol,
	}

	if err := w.conn.WriteJSON(subscribeMsg); err != nil {
		return fmt.Errorf("subscribe ticker error: %w", err)
	}

	logger.Info("Bitfinex subscribed to ticker: %s", w.symbol)
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
	// Bitfinex WebSocket 消息格式：
	// 1. 事件消息：{"event": "...", ...}
	// 2. 数据消息：[CHANNEL_ID, DATA]

	// 尝试解析为事件消息
	var eventMsg map[string]interface{}
	if err := json.Unmarshal(message, &eventMsg); err == nil {
		w.handleEventMessage(eventMsg)
		return
	}

	// 尝试解析为数据消息
	var dataMsg []interface{}
	if err := json.Unmarshal(message, &dataMsg); err == nil {
		w.handleDataMessage(dataMsg)
		return
	}

	logger.Warn("Bitfinex unknown message format: %s", string(message))
}

// handleEventMessage 处理事件消息
func (w *WebSocketManager) handleEventMessage(msg map[string]interface{}) {
	event, ok := msg["event"].(string)
	if !ok {
		return
	}

	switch event {
	case "info":
		logger.Info("Bitfinex WebSocket info: %v", msg)
	case "auth":
		status, _ := msg["status"].(string)
		if status == "OK" {
			logger.Info("Bitfinex WebSocket authenticated successfully")
		} else {
			logger.Error("Bitfinex WebSocket authentication failed: %v", msg)
		}
	case "subscribed":
		// 订阅成功，记录 channel ID
		chanID, _ := msg["chanId"].(float64)
		channel, _ := msg["channel"].(string)
		w.mu.Lock()
		w.channelMap[int(chanID)] = channel
		w.mu.Unlock()
		logger.Info("Bitfinex subscribed to %s, chanID: %d", channel, int(chanID))
	case "unsubscribed":
		chanID, _ := msg["chanId"].(float64)
		logger.Info("Bitfinex unsubscribed from chanID: %d", int(chanID))
	case "error":
		logger.Error("Bitfinex WebSocket error: %v", msg)
	}
}

// handleDataMessage 处理数据消息
func (w *WebSocketManager) handleDataMessage(msg []interface{}) {
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

	// 获取 channel 类型
	w.mu.RLock()
	channelType := w.channelMap[int(chanID)]
	w.mu.RUnlock()

	switch channelType {
	case "ticker":
		w.handleTickerData(msg[1])
	default:
		// 认证频道的消息
		w.handleAuthChannelData(msg)
	}
}

// handleTickerData 处理 ticker 数据
func (w *WebSocketManager) handleTickerData(data interface{}) {
	w.mu.RLock()
	callback := w.priceCallback
	w.mu.RUnlock()

	if callback == nil {
		return
	}

	// Ticker 格式：[BID, BID_SIZE, ASK, ASK_SIZE, DAILY_CHANGE, DAILY_CHANGE_RELATIVE, LAST_PRICE, ...]
	tickerArray, ok := data.([]interface{})
	if !ok || len(tickerArray) < 7 {
		return
	}

	lastPrice, ok := tickerArray[6].(float64)
	if !ok {
		return
	}

	callback(lastPrice)
}

// handleAuthChannelData 处理认证频道数据
func (w *WebSocketManager) handleAuthChannelData(msg []interface{}) {
	if len(msg) < 2 {
		return
	}

	// 订单更新格式：[0, "on", [ORDER_DATA]]
	// 订单取消格式：[0, "oc", [ORDER_DATA]]
	// 订单执行格式：[0, "ou", [ORDER_DATA]]

	msgType, ok := msg[1].(string)
	if !ok {
		return
	}

	switch msgType {
	case "on", "ou", "oc": // order new, order update, order cancel
		w.handleOrderUpdate(msg)
	}
}

// handleOrderUpdate 处理订单更新
func (w *WebSocketManager) handleOrderUpdate(msg []interface{}) {
	w.mu.RLock()
	callback := w.orderCallback
	w.mu.RUnlock()

	if callback == nil {
		return
	}

	if len(msg) < 3 {
		return
	}

	orderData, ok := msg[2].([]interface{})
	if !ok {
		return
	}

	order := parseOrderArray(orderData)
	logger.Info("Bitfinex order update: %s, type: %s, amount: %.8f", order.ID, msg[1], order.Amount)
	callback(order)
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
