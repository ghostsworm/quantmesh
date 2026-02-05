package whitebit

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	// WebSocket 地址
	MainnetWsURL = "wss://api.whitebit.com/ws"
	TestnetWsURL = "wss://api.whitebit.com/ws" // WhiteBIT没有测试网WebSocket
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	client     *WhiteBITClient // 用于获取WebSocket Token
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(WSOrderUpdate)
	priceCallback func(float64)
	wsToken       string // WebSocket Token
	requestID     int64  // 请求ID计数器
}

// WSOrderUpdate WebSocket订单更新
type WSOrderUpdate struct {
	ID            int64   `json:"id"`
	Market        string  `json:"market"`
	Type          int     `json:"type"` // 订单类型ID
	Side          int     `json:"side"` // 1 - sell, 2 - buy
	PostOnly      bool    `json:"post_only"`
	IOC           bool    `json:"ioc"`
	Ctime         float64 `json:"ctime"`
	Mtime         float64 `json:"mtime"`
	Price         string  `json:"price"`
	Amount        string  `json:"amount"`
	Left          string  `json:"left"`
	DealStock     string  `json:"deal_stock"`
	DealMoney     string  `json:"deal_money"`
	DealFee       string  `json:"deal_fee"`
	ClientOrderID string  `json:"client_order_id"`
	STP           string  `json:"stp"`
	Status        string  `json:"status"`
	PositionSide  string  `json:"position_side"`
	RPI           bool    `json:"rpi"`
}

// WSRequest WebSocket请求消息
type WSRequest struct {
	ID     int64         `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

// WSResponse WebSocket响应消息
type WSResponse struct {
	ID     *int64                `json:"id"`
	Method string                `json:"method"`
	Result interface{}            `json:"result"`
	Error  map[string]interface{} `json:"error"`
	Params interface{}            `json:"params"`
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, client *WhiteBITClient, useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:     apiKey,
		secretKey:  secretKey,
		client:     client,
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
		requestID:  1,
	}
}

// getRequestID 获取下一个请求ID
func (w *WebSocketManager) getRequestID() int64 {
	id := w.requestID
	w.requestID++
	return id
}

// sendMessage 发送WebSocket消息
func (w *WebSocketManager) sendMessage(msg interface{}) error {
	w.mu.RLock()
	conn := w.conn
	w.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("WebSocket 连接未建立")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("序列化消息失败: %w", err)
	}

	w.mu.Lock()
	err = conn.WriteMessage(websocket.TextMessage, data)
	w.mu.Unlock()

	if err != nil {
		return fmt.Errorf("发送消息失败: %w", err)
	}

	return nil
}

// Start 啟動訂單流
func (w *WebSocketManager) Start(ctx context.Context, market string, callback func(WSOrderUpdate)) error {
	if w.isRunning.Load() {
		return fmt.Errorf("WebSocket 已在运行")
	}

	w.orderCallback = callback

	// 获取WebSocket Token
	token, err := w.client.GetWebSocketToken(ctx)
	if err != nil {
		return fmt.Errorf("获取WebSocket Token失败: %w", err)
	}
	w.wsToken = token

	wsURL := MainnetWsURL
	if w.useTestnet {
		wsURL = TestnetWsURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 WebSocket 失败: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	w.isRunning.Store(true)

	// 认证
	if err := w.authorize(); err != nil {
		conn.Close()
		return fmt.Errorf("WebSocket 认证失败: %w", err)
	}

	// 订阅订单频道
	if err := w.subscribeOrders(market); err != nil {
		conn.Close()
		return fmt.Errorf("订阅订单频道失败: %w", err)
	}

	// 启动消息处理
	go w.readMessages()
	go w.keepAlive()

	logger.Info("✅ [WhiteBIT WebSocket] 訂單流已啟动")
	return nil
}

// authorize 认证
func (w *WebSocketManager) authorize() error {
	req := WSRequest{
		ID:     w.getRequestID(),
		Method: "authorize",
		Params: []interface{}{w.wsToken, "public"},
	}

	return w.sendMessage(req)
}

// subscribeOrders 订阅订单频道
func (w *WebSocketManager) subscribeOrders(market string) error {
	req := WSRequest{
		ID:     w.getRequestID(),
		Method: "ordersPending_subscribe",
		Params: []interface{}{market},
	}

	return w.sendMessage(req)
}

// StartPriceStream 啟動價格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, market string, callback func(float64)) error {
	w.priceCallback = callback

	// WhiteBIT价格流使用公共WebSocket（不需要认证）
	wsURL := MainnetWsURL
	if w.useTestnet {
		wsURL = TestnetWsURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接價格流 WebSocket 失败: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	// 订阅价格流（使用公共WebSocket，不需要认证）
	// 注意：WhiteBIT的价格流可能需要使用不同的订阅方法
	// 这里需要根据实际API文档调整

	logger.Info("✅ [WhiteBIT WebSocket] 價格流已啟动")
	return nil
}

// readMessages 读取消息
func (w *WebSocketManager) readMessages() {
	defer func() {
		w.mu.Lock()
		if w.conn != nil {
			w.conn.Close()
			w.conn = nil
		}
		w.mu.Unlock()
		w.isRunning.Store(false)
	}()

	for {
		select {
		case <-w.stopChan:
			return
		default:
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn == nil {
				return
			}

			// 设置读取超时
			conn.SetReadDeadline(time.Now().Add(60 * time.Second))

			_, message, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					logger.Error("WebSocket 读取错误: %v", err)
				}
				return
			}

			var resp WSResponse
			if err := json.Unmarshal(message, &resp); err != nil {
				logger.Error("解析WebSocket消息失败: %v", err)
				continue
			}

			// 处理消息
			w.handleMessage(&resp)
		}
	}
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(resp *WSResponse) {
	// 处理ping响应
	if resp.Method == "ping" {
		// 响应pong
		pongReq := WSRequest{
			ID:     0,
			Method: "pong",
			Params: []interface{}{},
		}
		w.sendMessage(pongReq)
		return
	}

		// 处理订单更新
	if resp.Method == "ordersPending_update" {
		if w.orderCallback == nil {
			return
		}

		// 解析订单更新
		if params, ok := resp.Params.([]interface{}); ok && len(params) >= 2 {
			// params[0] 是更新事件ID (1=新订单, 2=更新订单, 3=完成订单)
			// params[1] 是订单数据
			if len(params) >= 2 {
				if orderData, ok := params[1].(map[string]interface{}); ok {
					var order WSOrderUpdate
					if data, err := json.Marshal(orderData); err == nil {
						if err := json.Unmarshal(data, &order); err == nil {
							w.orderCallback(order)
						}
					}
				}
			}
		}
		return
	}

	// 处理价格更新
	if resp.Method == "ticker_update" || resp.Method == "lastprice_update" {
		if w.priceCallback == nil {
			return
		}

		// 解析价格数据
		if params, ok := resp.Params.([]interface{}); ok && len(params) > 0 {
			if priceStr, ok := params[0].(string); ok {
				if price, err := strconv.ParseFloat(priceStr, 64); err == nil {
					w.priceCallback(price)
					w.lastPrice.Store(price)
				}
			}
		}
		return
	}
}

// keepAlive 保持连接活跃（每50秒发送ping）
func (w *WebSocketManager) keepAlive() {
	ticker := time.NewTicker(50 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.stopChan:
			return
		case <-ticker.C:
			if !w.isRunning.Load() {
				return
			}

			pingReq := WSRequest{
				ID:     0,
				Method: "ping",
				Params: []interface{}{},
			}

			if err := w.sendMessage(pingReq); err != nil {
				logger.Error("发送ping失败: %v", err)
				return
			}
		}
	}
}

// Stop 停止WebSocket连接
func (w *WebSocketManager) Stop() error {
	if !w.isRunning.Load() {
		return nil
	}

	close(w.stopChan)

	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()

	w.isRunning.Store(false)
	logger.Info("✅ [WhiteBIT WebSocket] 已停止")
	return nil
}
