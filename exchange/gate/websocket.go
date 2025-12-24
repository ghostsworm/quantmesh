package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"opensqt/logger"
	"opensqt/utils"

	"github.com/gorilla/websocket"
)

// WebSocketManager Gate.io WebSocket 管理器（用于交易和私有数据）
type WebSocketManager struct {
	apiKey    string
	secretKey string
	signer    *Signer

	// 连接管理
	conn *websocket.Conn
	mu   sync.RWMutex

	// 回调函数
	orderCallback func(interface{})
	priceCallback func(string, float64) // symbol, price

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 价格缓存
	latestPrice float64
	priceMu     sync.RWMutex

	// 重连控制
	reconnectChan    chan struct{}
	reconnectDelay   time.Duration
	subscribedSymbol string // 记录订阅的交易对，用于重连后重新订阅
	settle           string // usdt 或 btc
	isAuthenticated  bool   // 标记是否已认证
}

// NewWebSocketManager 创建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey, settle string) *WebSocketManager {
	if settle == "" {
		settle = "usdt"
	}
	return &WebSocketManager{
		apiKey:         apiKey,
		secretKey:      secretKey,
		signer:         NewSigner(apiKey, secretKey),
		reconnectChan:  make(chan struct{}, 1),
		reconnectDelay: 5 * time.Second,
		settle:         settle,
	}
}

// SetPriceCallback 设置价格回调
func (w *WebSocketManager) SetPriceCallback(callback func(string, float64)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.priceCallback = callback
}

// SetOrderCallback 设置订单回调
func (w *WebSocketManager) SetOrderCallback(callback func(interface{})) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.orderCallback = callback
}

// IsRunning 检查 WebSocket 是否运行中
func (w *WebSocketManager) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.conn != nil
}

// GetLatestPrice 获取最新价格（从缓存）
func (w *WebSocketManager) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}

// Start 启动 WebSocket（自动重连）
func (w *WebSocketManager) Start(ctx context.Context, symbol string) error {
	w.mu.Lock()
	if w.ctx != nil {
		w.mu.Unlock()
		return fmt.Errorf("WebSocket 已在运行")
	}
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.subscribedSymbol = symbol
	w.mu.Unlock()

	w.wg.Add(1)
	go w.connectLoop()

	return nil
}

// connectLoop 连接循环（自动重连）
func (w *WebSocketManager) connectLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Gate WS] 停止连接循环")
			return
		default:
		}

		logger.Info("🔗 [Gate WS] 正在连接...")

		// 连接 Gate.io WebSocket
		wsURL := fmt.Sprintf("wss://fx-ws.gateio.ws/v4/ws/%s", w.settle)
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Gate WS] 连接失败: %v，%v后重试", err, w.reconnectDelay)
			time.Sleep(w.reconnectDelay)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.isAuthenticated = false
		symbol := w.subscribedSymbol
		w.mu.Unlock()

		logger.Info("✅ [Gate WS] 已连接")

		// Gate.io 不需要单独登录,直接在订阅时携带认证信息
		// 订阅频道
		if err := w.subscribeChannels(symbol); err != nil {
			logger.Error("❌ [Gate WS] 订阅失败: %v", err)
			conn.Close()
			time.Sleep(w.reconnectDelay)
			continue
		}

		// 启动 ping 和读取协程
		done := make(chan struct{})
		go func() {
			w.keepAlive(conn)
			close(done)
		}()

		// 启动读取循环（阻塞直到连接断开）
		w.handleMessages(conn)

		// 等待 keepAlive 退出
		<-done

		// 连接断开，清理
		w.mu.Lock()
		if w.conn == conn {
			w.conn = nil
			w.isAuthenticated = false
		}
		w.mu.Unlock()

		logger.Warn("⚠️ [Gate WS] 连接断开，%v后重连...", w.reconnectDelay)
		time.Sleep(w.reconnectDelay)
	}
}

// login 登录认证
func (w *WebSocketManager) login() error {
	timestamp := time.Now().Unix()
	channel := "futures.login"
	event := "api"

	// 生成签名
	signature := w.signer.SignWebSocket(channel, event, timestamp)

	// 根据 Gate.io 官方文档,认证信息应该在 auth 字段中
	loginMsg := map[string]interface{}{
		"time":    timestamp,
		"channel": channel,
		"event":   event,
		"auth": map[string]interface{}{
			"method": "api_key",
			"KEY":    w.apiKey,
			"SIGN":   signature,
		},
		"req_header": map[string]string{
			"X-Gate-Channel-Id": GateChannelID, // 渠道返佣 ID
		},
	}

	w.mu.RLock()
	conn := w.conn
	w.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	if err := conn.WriteJSON(loginMsg); err != nil {
		return fmt.Errorf("发送登录消息失败: %w", err)
	}

	logger.Info("✅ [Gate WS] 已发送登录请求")
	return nil
}

// subscribeChannels 订阅频道
func (w *WebSocketManager) subscribeChannels(symbol string) error {
	gateSymbol := convertToGateSymbol(symbol)
	timestamp := time.Now().Unix()

	// 订阅订单更新（私有频道需要认证）
	ordersSign := w.signer.SignWebSocket("futures.orders", "subscribe", timestamp)
	ordersMsg := map[string]interface{}{
		"time":    timestamp,
		"channel": "futures.orders",
		"event":   "subscribe",
		"auth": map[string]interface{}{
			"method": "api_key",
			"KEY":    w.apiKey,
			"SIGN":   ordersSign,
		},
		"req_header": map[string]string{
			"X-Gate-Channel-Id": GateChannelID,
		},
		"payload": []string{w.apiKey, gateSymbol},
	}

	// 订阅余额更新（私有频道需要认证）
	balanceSign := w.signer.SignWebSocket("futures.balances", "subscribe", timestamp+1)
	balanceMsg := map[string]interface{}{
		"time":    timestamp + 1,
		"channel": "futures.balances",
		"event":   "subscribe",
		"auth": map[string]interface{}{
			"method": "api_key",
			"KEY":    w.apiKey,
			"SIGN":   balanceSign,
		},
		"req_header": map[string]string{
			"X-Gate-Channel-Id": GateChannelID,
		},
		"payload": []string{w.apiKey},
	}

	// 订阅价格更新（ticker）
	tickerMsg := map[string]interface{}{
		"time":    timestamp + 2,
		"channel": "futures.tickers",
		"event":   "subscribe",
		"payload": []string{gateSymbol},
	}

	w.mu.RLock()
	conn := w.conn
	w.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	// 发送订阅消息
	if err := conn.WriteJSON(ordersMsg); err != nil {
		return fmt.Errorf("订阅订单频道失败: %w", err)
	}

	if err := conn.WriteJSON(balanceMsg); err != nil {
		return fmt.Errorf("订阅余额频道失败: %w", err)
	}

	if err := conn.WriteJSON(tickerMsg); err != nil {
		return fmt.Errorf("订阅价格频道失败: %w", err)
	}

	logger.Info("✅ [Gate WS] 已订阅频道: orders, balances, tickers")
	return nil
}

// keepAlive 保持连接活跃
func (w *WebSocketManager) keepAlive(conn *websocket.Conn) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			w.mu.RLock()
			currentConn := w.conn
			w.mu.RUnlock()

			if currentConn != conn {
				return // 连接已更换，退出
			}

			// Gate.io 使用 ping 消息
			pingMsg := map[string]interface{}{
				"time":    time.Now().Unix(),
				"channel": "futures.ping",
			}

			if err := conn.WriteJSON(pingMsg); err != nil {
				logger.Warn("⚠️ [Gate WS] Ping 失败: %v", err)
				return
			}
		}
	}
}

// handleMessages 处理消息循环
func (w *WebSocketManager) handleMessages(conn *websocket.Conn) {
	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Warn("⚠️ [Gate WS] 读取消息失败: %v", err)
			return
		}

		w.handleMessage(message)
	}
}

// handleMessage 处理单条消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Gate WS] 解析消息失败: %v", err)
		return
	}

	// 检查错误
	if errObj, ok := msg["error"].(map[string]interface{}); ok {
		logger.Error("❌ [Gate WS] 错误: %v", errObj)
		return
	}

	// 处理不同类型的消息
	event, _ := msg["event"].(string)
	channel, _ := msg["channel"].(string)

	switch event {
	case "subscribe":
		// 订阅确认
		if result, ok := msg["result"].(map[string]interface{}); ok {
			if status, _ := result["status"].(string); status == "success" {
				logger.Info("✅ [Gate WS] 订阅成功: %s", channel)
			}
		}

	case "update":
		// 数据更新
		switch channel {
		case "futures.orders":
			w.handleOrderUpdate(msg)
		case "futures.balances":
			// 余额更新（可选实现）
			logger.Debug("[Gate WS] 余额更新")
		case "futures.tickers":
			w.handleTickerUpdate(msg)
		}

	case "pong":
		// Pong 响应（静默处理）

	default:
		// 检查是否是登录响应
		if channel == "futures.login" {
			// Gate.io 登录响应在 header.status 中
			if header, ok := msg["header"].(map[string]interface{}); ok {
				status, _ := header["status"].(string)
				if status == "200" {
					w.mu.Lock()
					w.isAuthenticated = true
					w.mu.Unlock()
					logger.Info("✅ [Gate WS] 登录成功")
				} else {
					// 解析错误信息
					errMsg := status
					if data, ok := msg["data"].(map[string]interface{}); ok {
						if errs, ok := data["errs"].(map[string]interface{}); ok {
							if message, ok := errs["message"].(string); ok {
								errMsg = message
							}
						}
					}
					logger.Warn("⚠️ [Gate WS] 登录失败: %s", errMsg)
				}
			}
		} else {
			// 打印未处理的事件用于调试
			logger.Debug("[Gate WS] 未处理的事件: event=%s, channel=%s", event, channel)
		}
	}
}

// handleOrderUpdate 处理订单更新
func (w *WebSocketManager) handleOrderUpdate(msg map[string]interface{}) {
	result, ok := msg["result"].([]interface{})
	if !ok || len(result) == 0 {
		return
	}

	for _, item := range result {
		orderData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		// 解析订单数据
		orderID, _ := orderData["id"].(float64)
		contract, _ := orderData["contract"].(string)
		status, _ := orderData["status"].(string)
		size, _ := orderData["size"].(float64)
		left, _ := orderData["left"].(float64) // 未成交数量
		price, _ := parseFloat(orderData["price"])
		fillPrice, _ := parseFloat(orderData["fill_price"])
		text, _ := orderData["text"].(string)
		finishTime, _ := orderData["finish_time"].(float64)

		// 使用统一的 utils 包去掉 Gate.io 的 t- 前缀
		clientOrderID := utils.RemoveBrokerPrefix("gate", text)

		// 计算成交数量 = 总数量 - 未成交数量
		executedQty := abs(size) - abs(left)
		if executedQty < 0 {
			executedQty = 0
		}

		// 转换为标准格式
		update := OrderUpdate{
			OrderID:       int64(orderID),
			ClientOrderID: clientOrderID,
			Symbol:        convertFromGateSymbol(contract),
			Side:          convertSide(size),
			Status:        convertStatus(status),
			Price:         price,
			Quantity:      abs(size),
			ExecutedQty:   executedQty, // 成交数量 = size - left
			AvgPrice:      fillPrice,
			UpdateTime:    int64(finishTime * 1000), // 转换为毫秒
		}

		w.mu.RLock()
		callback := w.orderCallback
		w.mu.RUnlock()

		if callback != nil {
			callback(update)
		}
	}
}

// handleTickerUpdate 处理价格更新
func (w *WebSocketManager) handleTickerUpdate(msg map[string]interface{}) {
	result, ok := msg["result"].([]interface{})
	if !ok || len(result) == 0 {
		return
	}

	for _, item := range result {
		tickerData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		contract, _ := tickerData["contract"].(string)
		last, _ := parseFloat(tickerData["last"])

		symbol := convertFromGateSymbol(contract)

		// 更新缓存
		w.priceMu.Lock()
		w.latestPrice = last
		w.priceMu.Unlock()

		// 触发回调
		w.mu.RLock()
		callback := w.priceCallback
		w.mu.RUnlock()

		if callback != nil {
			callback(symbol, last)
		}
	}
}

// PlaceOrder 通过 WebSocket 下单（带渠道码）
func (w *WebSocketManager) PlaceOrder(order map[string]interface{}) error {
	timestamp := time.Now().Unix()

	// 🔥 重要：构造带渠道码的 Payload
	payload := map[string]interface{}{
		"req_header": map[string]string{
			"X-Gate-Channel-Id": GateChannelID, // 渠道返佣标识
		},
		"req_id":    fmt.Sprintf("order_%d", timestamp),
		"req_param": order,
	}

	orderMsg := map[string]interface{}{
		"time":    timestamp,
		"channel": "futures.order_place",
		"event":   "api",
		"payload": payload,
	}

	w.mu.RLock()
	conn := w.conn
	authenticated := w.isAuthenticated
	w.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	if !authenticated {
		return fmt.Errorf("未认证")
	}

	if err := conn.WriteJSON(orderMsg); err != nil {
		return fmt.Errorf("发送下单消息失败: %w", err)
	}

	return nil
}

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() error {
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()

	w.wg.Wait()
	return nil
}

// convertSide 根据 size 判断方向
func convertSide(size float64) Side {
	if size > 0 {
		return SideBuy
	}
	return SideSell
}

// convertStatus 转换订单状态
func convertStatus(status string) OrderStatus {
	switch status {
	case "open":
		return "NEW"
	case "finished":
		return "FILLED"
	default:
		return OrderStatus(status)
	}
}

// abs 返回绝对值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
