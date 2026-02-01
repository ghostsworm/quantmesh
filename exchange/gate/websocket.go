package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"

	"github.com/gorilla/websocket"
)

// WebSocketManager Gate.io WebSocket 管理器（用於交易和私有數據）
type WebSocketManager struct {
	apiKey    string
	secretKey string
	signer    *Signer

	// 连接管理
	conn *websocket.Conn
	mu   sync.RWMutex

	// 回呼函數
	orderCallback func(interface{})
	priceCallback func(string, float64) // symbol, price

	// 控制
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// 價格缓存
	latestPrice float64
	priceMu     sync.RWMutex

	// 重连控制
	reconnectChan    chan struct{}
	reconnectDelay   time.Duration
	subscribedSymbol string // 記錄订阅的交易對，用於重连后重新订阅
	settle           string // usdt 或 btc
	isAuthenticated  bool   // 標記是否已认证
	testnet          bool   // 是否使用測試網
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey, settle string, testnet bool) *WebSocketManager {
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
		testnet:        testnet,
	}
}

// SetPriceCallback 設置價格回呼
func (w *WebSocketManager) SetPriceCallback(callback func(string, float64)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.priceCallback = callback
}

// SetOrderCallback 設置订單回呼
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

// GetLatestPrice 獲取最新價格（從缓存）
func (w *WebSocketManager) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}

// Start 啟动 WebSocket（自动重连）
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
		var wsURL string
		if w.testnet {
			wsURL = fmt.Sprintf("wss://fx-ws-testnet.gateio.ws/v4/ws/%s", w.settle)
			logger.Info("🌐 [Gate WS] 使用測試網 WebSocket: %s", wsURL)
		} else {
			wsURL = fmt.Sprintf("wss://fx-ws.gateio.ws/v4/ws/%s", w.settle)
		}
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Gate WS] 连接失败: %v，%v后重試", err, w.reconnectDelay)
			time.Sleep(w.reconnectDelay)
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.isAuthenticated = false
		symbol := w.subscribedSymbol
		w.mu.Unlock()

		logger.Info("✅ [Gate WS] 已连接")

		// Gate.io 測試網可能需要先登錄认证，然后再订阅私有频道
		// 注意：根據 Gate.io 文檔，測試網和主网都支援在订阅時直接携带认证信息
		// 但如果遇到 "Invalid key provided" 錯误，可能是：
		// 1. 測試網需要使用专门的測試網 API Key（不是主网的 API Key）
		// 2. 或者需要先調用 login() 進行认证
		// 这里先尝試直接订阅，如果失败再尝試先登錄
		if w.testnet {
			logger.Info("ℹ️ [Gate WS] 測試網模式：將先尝試登錄认证")
			// 測試網模式下先登錄
			if err := w.login(); err != nil {
				logger.Warn("⚠️ [Gate WS] 測試網登錄发送失败: %v，將直接尝試订阅", err)
			} else {
				// 等待登錄响应（最多等待1.5秒）
				loginSuccess := false
				for i := 0; i < 30; i++ { // 最多检查30次（约1.5秒）
					w.mu.RLock()
					authenticated := w.isAuthenticated
					w.mu.RUnlock()
					if authenticated {
						loginSuccess = true
						logger.Info("✅ [Gate WS] 測試網登錄成功")
						break
					}
					time.Sleep(50 * time.Millisecond)
				}
				if !loginSuccess {
					logger.Warn("⚠️ [Gate WS] 測試網登錄响应超時，继续尝試订阅（可能測試網不需要先登錄）")
				}
			}
		}

		// 订阅频道
		if err := w.subscribeChannels(symbol); err != nil {
			logger.Error("❌ [Gate WS] 订阅失败: %v", err)
			conn.Close()
			time.Sleep(w.reconnectDelay)
			continue
		}

		// 啟动 ping 和读取协程
		done := make(chan struct{})
		go func() {
			w.keepAlive(conn)
			close(done)
		}()

		// 啟动读取循环（阻塞直到连接断开）
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

// login 登錄认证
func (w *WebSocketManager) login() error {
	timestamp := time.Now().Unix()
	channel := "futures.login"
	event := "api"

	// 生成签名
	signature := w.signer.SignWebSocket(channel, event, timestamp)

	// 根據 Gate.io 官方文檔,认证信息应該在 auth 字段中
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
		return fmt.Errorf("发送登錄消息失败: %w", err)
	}

	logger.Info("✅ [Gate WS] 已发送登錄请求")
	return nil
}

// subscribeChannels 订阅频道
func (w *WebSocketManager) subscribeChannels(symbol string) error {
	gateSymbol := convertToGateSymbol(symbol)
	timestamp := time.Now().Unix()

	// 订阅订單更新（私有频道需要认证）
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

	// 订阅餘額更新（私有频道需要认证）
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

	// 订阅價格更新（ticker）
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
		return fmt.Errorf("订阅订單频道失败: %w", err)
	}

	if err := conn.WriteJSON(balanceMsg); err != nil {
		return fmt.Errorf("订阅餘額频道失败: %w", err)
	}

	if err := conn.WriteJSON(tickerMsg); err != nil {
		return fmt.Errorf("订阅價格频道失败: %w", err)
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

// handleMessage 处理單条消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Gate WS] 解析消息失败: %v", err)
		return
	}

	// 检查錯误（可能是錯误對象或 code 字段）
	if errObj, ok := msg["error"].(map[string]interface{}); ok {
		logger.Error("❌ [Gate WS] 錯误: %v", errObj)
		return
	}
	// Gate.io 測試網可能使用 code 字段表示錯误
	if code, ok := msg["code"].(float64); ok && code != 0 {
		if message, ok := msg["message"].(string); ok {
			// 检查是否是 "Invalid key provided" 錯误
			isInvalidKey := code == 4 || (message != "" && (message == "Invalid key provided" || message == `{"message":"Invalid key provided","label":"INVALID_KEY"}`))
			
			if isInvalidKey {
				if w.testnet {
					logger.Error("❌ [Gate WS] 測試網认证失败: Invalid key provided")
					logger.Error("💡 [Gate WS] 提示：Gate.io 測試網需要使用专门的測試網 API Key")
					logger.Error("💡 [Gate WS] 请访问 https://api-testnet.gateapi.io/ 申请測試網 API Key")
					logger.Error("💡 [Gate WS] 注意：測試網 API Key 與主网 API Key 是独立的，不能混用")
				} else {
					logger.Error("❌ [Gate WS] 认证失败: Invalid key provided (code=%v)", code)
				}
			} else {
				logger.Error("❌ [Gate WS] 錯误: code=%v, message=%s", code, message)
			}
			
			// 如果是认证錯误，標記為未认证
			if isInvalidKey {
				w.mu.Lock()
				w.isAuthenticated = false
				w.mu.Unlock()
			}
		} else {
			logger.Error("❌ [Gate WS] 錯误: code=%v, msg=%v", code, msg)
		}
		return
	}

	// 处理不同類型的消息
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
		// 數據更新
		switch channel {
		case "futures.orders":
			w.handleOrderUpdate(msg)
		case "futures.balances":
			// 餘額更新（可選實現）
			logger.Debug("[Gate WS] 餘額更新")
		case "futures.tickers":
			w.handleTickerUpdate(msg)
		}

	case "pong":
		// Pong 响应（静默处理）

	default:
		// 检查是否是登錄响应
		if channel == "futures.login" {
			// Gate.io 登錄响应在 header.status 中
			if header, ok := msg["header"].(map[string]interface{}); ok {
				status, _ := header["status"].(string)
				if status == "200" {
					w.mu.Lock()
					w.isAuthenticated = true
					w.mu.Unlock()
					logger.Info("✅ [Gate WS] 登錄成功")
				} else {
					// 解析錯误信息
					errMsg := status
					if data, ok := msg["data"].(map[string]interface{}); ok {
						if errs, ok := data["errs"].(map[string]interface{}); ok {
							if message, ok := errs["message"].(string); ok {
								errMsg = message
							}
						}
					}
					logger.Warn("⚠️ [Gate WS] 登錄失败: %s", errMsg)
				}
			}
		} else {
			// 打印未处理的事件用於調試
			logger.Debug("[Gate WS] 未处理的事件: event=%s, channel=%s", event, channel)
		}
	}
}

// handleOrderUpdate 处理订單更新
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

		// 解析订單數據
		orderID, _ := orderData["id"].(float64)
		contract, _ := orderData["contract"].(string)
		status, _ := orderData["status"].(string)
		size, _ := orderData["size"].(float64)
		left, _ := orderData["left"].(float64) // 未成交數量
		price, _ := parseFloat(orderData["price"])
		fillPrice, _ := parseFloat(orderData["fill_price"])
		text, _ := orderData["text"].(string)
		finishTime, _ := orderData["finish_time"].(float64)

		// 使用统一的 utils 包去掉 Gate.io 的 t- 前缀
		clientOrderID := utils.RemoveBrokerPrefix("gate", text)

		// 计算成交數量 = 總數量 - 未成交數量
		executedQty := abs(size) - abs(left)
		if executedQty < 0 {
			executedQty = 0
		}

		// 轉换為標准格式
		update := OrderUpdate{
			OrderID:       int64(orderID),
			ClientOrderID: clientOrderID,
			Symbol:        convertFromGateSymbol(contract),
			Side:          convertSide(size),
			Status:        convertStatus(status),
			Price:         price,
			Quantity:      abs(size),
			ExecutedQty:   executedQty, // 成交數量 = size - left
			AvgPrice:      fillPrice,
			UpdateTime:    int64(finishTime * 1000), // 轉换為毫秒
		}

		w.mu.RLock()
		callback := w.orderCallback
		w.mu.RUnlock()

		if callback != nil {
			callback(update)
		}
	}
}

// handleTickerUpdate 处理價格更新
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

		// 触发回呼
		w.mu.RLock()
		callback := w.priceCallback
		w.mu.RUnlock()

		if callback != nil {
			callback(symbol, last)
		}
	}
}

// PlaceOrder 通過 WebSocket 下單（带渠道碼）
func (w *WebSocketManager) PlaceOrder(order map[string]interface{}) error {
	timestamp := time.Now().Unix()

	// 🔥 重要：構造带渠道碼的 Payload
	payload := map[string]interface{}{
		"req_header": map[string]string{
			"X-Gate-Channel-Id": GateChannelID, // 渠道返佣標识
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
		return fmt.Errorf("发送下單消息失败: %w", err)
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

// convertSide 根據 size 判断方向
func convertSide(size float64) Side {
	if size > 0 {
		return SideBuy
	}
	return SideSell
}

// convertStatus 轉换订單状態
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

// abs 返回绝對值
func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
