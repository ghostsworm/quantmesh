package bitget

/*
Bitget WebSocket 架構說明：

1. **WebSocket下單**：Bitget不支援WebSocket下單，所有下單操作请使用REST API

2. **WebSocket用途**：
   - 公共频道：订阅價格推送 (ticker)
   - 私有频道：订阅订單更新 (orders)

3. **啟动流程**：
   - main.go 中通過 PriceMonitor.Start() 啟動價格流
   - main.go 中通過 ex.StartOrderStream() 啟動訂單流
   - 價格流和訂單流共用同一個 WebSocketManager 實例
   - 公共频道和私有频道是两個独立的WebSocket连接

4. **價格獲取方式**：
   - 优先從 WebSocket 缓存獲取 (GetLatestPrice)
   - 如果缓存為空，降级使用 REST API
*/

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	// Bitget V2 WebSocket 地址
	BitgetWSPrivate = "wss://ws.bitget.com/v2/ws/private"
	BitgetWSPublic  = "wss://ws.bitget.com/v2/ws/public"

	// Bitget 測試網 WebSocket 地址
	BitgetTestnetWSPrivate = "wss://testnetws.bitget.com/v2/ws/private"
	BitgetTestnetWSPublic  = "wss://testnetws.bitget.com/v2/ws/public"

	// API Code - 重要：不要丢失！
	BitgetAPICode = "3xh1b"
)

// WebSocketManager Bitget WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	passphrase string

	// 连接管理
	privateConn *websocket.Conn
	publicConn  *websocket.Conn
	mu          sync.RWMutex

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

	// 🔥 標記消息处理是否已啟动
	privateHandlerStarted bool
	publicHandlerStarted  bool

	// 🔥 重连控制
	publicReconnectChan  chan struct{}
	privateReconnectChan chan struct{}
	reconnectDelay       time.Duration
	subscribedSymbol     string // 記錄订阅的交易對，用於重连后重新订阅
	testnet              bool   // 是否使用測試網

	// WebSocket Dialer（支援代理）
	dialer *websocket.Dialer
}

// getProxyDialer 創建支援代理的 WebSocket Dialer
func getProxyDialer() *websocket.Dialer {
	dialer := &websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
	}

	// 從环境变量读取代理配置
	proxyURL := getProxyFromEnv()
	if proxyURL != nil {
		logger.Info("🌐 [Bitget WS] 使用代理: %s", proxyURL.String())
		dialer.Proxy = http.ProxyURL(proxyURL)
	} else {
		logger.Debug("🌐 [Bitget WS] 未配置代理，使用直连")
	}

	return dialer
}

// getProxyFromEnv 從环境变量读取代理配置
// 优先级: all_proxy > https_proxy > http_proxy
func getProxyFromEnv() *url.URL {
	var proxyStr string

	// 优先使用 all_proxy（支援 socks5）
	if proxyStr = os.Getenv("all_proxy"); proxyStr != "" {
		logger.Debug("🌐 [Bitget WS] 從 all_proxy 读取代理: %s", proxyStr)
	} else if proxyStr = os.Getenv("ALL_PROXY"); proxyStr != "" {
		logger.Debug("🌐 [Bitget WS] 從 ALL_PROXY 读取代理: %s", proxyStr)
	} else if proxyStr = os.Getenv("https_proxy"); proxyStr != "" {
		logger.Debug("🌐 [Bitget WS] 從 https_proxy 读取代理: %s", proxyStr)
	} else if proxyStr = os.Getenv("HTTPS_PROXY"); proxyStr != "" {
		logger.Debug("🌐 [Bitget WS] 從 HTTPS_PROXY 读取代理: %s", proxyStr)
	} else if proxyStr = os.Getenv("http_proxy"); proxyStr != "" {
		logger.Debug("🌐 [Bitget WS] 從 http_proxy 读取代理: %s", proxyStr)
	} else if proxyStr = os.Getenv("HTTP_PROXY"); proxyStr != "" {
		logger.Debug("🌐 [Bitget WS] 從 HTTP_PROXY 读取代理: %s", proxyStr)
	}

	if proxyStr == "" {
		return nil
	}

	// 解析代理 URL
	proxyURL, err := url.Parse(proxyStr)
	if err != nil {
		logger.Warn("⚠️ [Bitget WS] 代理 URL 解析失败: %v, 將使用直连", err)
		return nil
	}

	// 如果协议是 socks5，需要轉换為 http（gorilla/websocket 不支援 socks5）
	// 但我们可以尝試使用，如果失败會回退到直连
	if proxyURL.Scheme == "socks5" || proxyURL.Scheme == "socks5h" {
		logger.Warn("⚠️ [Bitget WS] 检测到 socks5 代理，gorilla/websocket 可能不支援，建议使用 http/https 代理")
		// 尝試轉换為 http（某些代理工具支援）
		// 如果不行，可能需要使用其他库如 golang.org/x/net/proxy
	}

	return proxyURL
}

// SetPriceCallback 設置價格回呼
func (w *WebSocketManager) SetPriceCallback(callback func(string, float64)) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.priceCallback = callback
}

// IsRunning 检查 WebSocket 是否运行中
func (w *WebSocketManager) IsRunning() bool {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.publicConn != nil || w.privateConn != nil
}

// OrderResponse 订單响应
type OrderResponse struct {
	Success   bool
	OrderID   string
	ClientOid string
	Code      string
	Msg       string
}

// WebSocket 消息結構
type WSMessage struct {
	Op   string          `json:"op"`
	Args []interface{}   `json:"args,omitempty"`
	Data json.RawMessage `json:"data,omitempty"`
	Code json.RawMessage `json:"code,omitempty"` // 可能是字符串或數字
	Msg  string          `json:"msg,omitempty"`
}

// GetCodeString 獲取 code 的字符串值
func (m *WSMessage) GetCodeString() string {
	if len(m.Code) == 0 {
		return ""
	}
	// 尝試解析為數字
	var codeNum int
	if err := json.Unmarshal(m.Code, &codeNum); err == nil {
		return fmt.Sprintf("%d", codeNum)
	}
	// 尝試解析為字符串
	var codeStr string
	if err := json.Unmarshal(m.Code, &codeStr); err == nil {
		return codeStr
	}
	return ""
}

// WebSocket 订阅参數
type WSSubscribeArg struct {
	InstType string `json:"instType"`
	Channel  string `json:"channel"`
	InstId   string `json:"instId,omitempty"`
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey, passphrase string, testnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:               apiKey,
		secretKey:            secretKey,
		passphrase:           passphrase,
		publicReconnectChan:  make(chan struct{}, 1),
		privateReconnectChan: make(chan struct{}, 1),
		reconnectDelay:       5 * time.Second,
		testnet:              testnet,
		dialer:               getProxyDialer(), // 初始化支援代理的 Dialer
	}
}

// publicConnectLoop 公共频道连接循环（自动重连）
func (w *WebSocketManager) publicConnectLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Bitget WS公共] 停止连接循环")
			return
		default:
		}

		logger.Info("🔗 [Bitget WS公共] 正在连接...")

		// 连接公共频道（使用支援代理的 Dialer）
		var wsURL string
		if w.testnet {
			wsURL = BitgetTestnetWSPublic
			logger.Info("🌐 [Bitget WS公共] 使用測試網 WebSocket: %s", wsURL)
		} else {
			wsURL = BitgetWSPublic
		}
		conn, _, err := w.dialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Bitget WS公共] 连接失败: %v，%v后重試", err, w.reconnectDelay)
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-w.ctx.Done():
				logger.Info("✅ [Bitget WS公共] 停止连接循环")
				return
			case <-time.After(w.reconnectDelay):
			}
			continue
		}

		w.mu.Lock()
		w.publicConn = conn
		symbol := w.subscribedSymbol
		w.mu.Unlock()

		logger.Info("✅ [Bitget WS公共] 已连接")

		// 订阅價格更新
		logger.Info("📡 [Bitget WS公共] 正在订阅價格更新: %s", symbol)
		if err := w.subscribeTicker(symbol); err != nil {
			logger.Error("❌ [Bitget WS公共] 订阅失败: %v", err)
			conn.Close()
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-w.ctx.Done():
				logger.Info("✅ [Bitget WS公共] 停止连接循环")
				return
			case <-time.After(w.reconnectDelay):
			}
			continue
		}

		// 啟动 ping 和读取协程
		done := make(chan struct{})
		go func() {
			w.keepAlive(conn, "公共", w.publicReconnectChan)
			close(done)
		}()

		// 啟动读取循环（阻塞直到连接断开）
		w.handlePublicMessages(conn)

		// 等待 keepAlive 退出（同時監听 context 取消）
		select {
		case <-done:
			// keepAlive 正常退出
		case <-w.ctx.Done():
			// context 取消，不等待 keepAlive
			logger.Info("✅ [Bitget WS公共] 停止连接循环")
			return
		}

		// 连接断开，清理
		w.mu.Lock()
		if w.publicConn == conn {
			w.publicConn = nil
		}
		w.mu.Unlock()
		conn.Close()

		// 检查是否因為 context 取消而断开，如果是则直接退出
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Bitget WS公共] 停止连接循环")
			return
		default:
		}

		logger.Warn("⚠️ [Bitget WS公共] 连接断开，%v后重连...", w.reconnectDelay)
		// 使用 select 等待，可以立即响应 context 取消
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Bitget WS公共] 停止连接循环")
			return
		case <-time.After(w.reconnectDelay):
		}
	}
}

// privateConnectLoop 私有频道连接循环（自动重连）
func (w *WebSocketManager) privateConnectLoop() {
	defer w.wg.Done()

	for {
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Bitget WS私有] 停止连接循环")
			return
		default:
		}

		logger.Info("🔗 [Bitget WS私有] 正在连接...")

		// 连接私有频道
		if err := w.connectPrivate(); err != nil {
			logger.Error("❌ [Bitget WS私有] 连接失败: %v，%v后重試", err, w.reconnectDelay)
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-w.ctx.Done():
				logger.Info("✅ [Bitget WS私有] 停止连接循环")
				return
			case <-time.After(w.reconnectDelay):
			}
			continue
		}

		w.mu.Lock()
		conn := w.privateConn
		symbol := w.subscribedSymbol
		w.mu.Unlock()

		// 订阅订單更新
		if err := w.subscribeOrders(symbol); err != nil {
			logger.Error("❌ [Bitget WS私有] 订阅失败: %v", err)
			conn.Close()
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-w.ctx.Done():
				logger.Info("✅ [Bitget WS私有] 停止连接循环")
				return
			case <-time.After(w.reconnectDelay):
			}
			continue
		}

		// 啟动 ping 和读取协程
		done := make(chan struct{})
		go func() {
			w.keepAlive(conn, "私有", w.privateReconnectChan)
			close(done)
		}()

		// 啟动读取循环（阻塞直到连接断开）
		w.handlePrivateMessages(conn)

		// 等待 keepAlive 退出（同時監听 context 取消）
		select {
		case <-done:
			// keepAlive 正常退出
		case <-w.ctx.Done():
			// context 取消，不等待 keepAlive
			logger.Info("✅ [Bitget WS私有] 停止连接循环")
			return
		}

		// 连接断开，清理
		w.mu.Lock()
		if w.privateConn == conn {
			w.privateConn = nil
		}
		w.mu.Unlock()
		conn.Close()

		// 检查是否因為 context 取消而断开，如果是则直接退出
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Bitget WS私有] 停止连接循环")
			return
		default:
		}

		logger.Warn("⚠️ [Bitget WS私有] 连接断开，%v后重连...", w.reconnectDelay)
		// 使用 select 等待，可以立即响应 context 取消
		select {
		case <-w.ctx.Done():
			logger.Info("✅ [Bitget WS私有] 停止连接循环")
			return
		case <-time.After(w.reconnectDelay):
		}
	}
}

// ConnectAndLogin 已廢棄 - 请使用 Start() 方法
// 保留該方法以兼容舊代碼，但建议直接調用 Start()
func (w *WebSocketManager) ConnectAndLogin(ctx context.Context, symbol string) error {
	// 直接調用 Start 方法
	return w.Start(ctx, symbol, nil)
}

// Start 啟动 WebSocket 连接（公共频道+私有频道）
// 订阅價格更新(ticker)和订單更新(orders)
// callback: 订單更新回呼函數，為nil時不订阅订單频道
func (w *WebSocketManager) Start(ctx context.Context, symbol string, callback func(interface{})) error {
	w.mu.Lock()
	w.ctx, w.cancel = context.WithCancel(ctx)
	w.orderCallback = callback
	w.subscribedSymbol = symbol // 記錄订阅的交易對
	w.mu.Unlock()

	// 🔥 啟动公共频道重连循环
	if !w.publicHandlerStarted {
		w.wg.Add(1)
		go w.publicConnectLoop()
		w.publicHandlerStarted = true
	}

	// 🔥 啟动私有频道重连循环（如果有订單回呼）
	if callback != nil && !w.privateHandlerStarted {
		w.wg.Add(1)
		go w.privateConnectLoop()
		w.privateHandlerStarted = true
	}

	if callback != nil {
		logger.Info("✅ [Bitget WebSocket] 啟动成功，將订阅 %s 的價格和订單更新", symbol)
	} else {
		logger.Info("✅ [Bitget WebSocket] 啟动成功，將订阅 %s 的價格更新", symbol)
	}
	return nil
}

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() {
	// 🔥 第一步：取消 context 並关闭连接（需要加鎖）
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
	}

	if w.privateConn != nil {
		w.privateConn.Close()
	}
	if w.publicConn != nil {
		w.publicConn.Close()
	}
	w.mu.Unlock()

	// 🔥 第二步：等待所有 goroutine 退出（不能持有鎖，避免死鎖）
	w.wg.Wait()
	logger.Info("✅ [Bitget WebSocket] 已停止")
}

// connectPrivate 连接私有 WebSocket
func (w *WebSocketManager) connectPrivate() error {
	var wsURL string
	if w.testnet {
		wsURL = BitgetTestnetWSPrivate
		logger.Info("🌐 [Bitget WS私有] 使用測試網 WebSocket: %s", wsURL)
	} else {
		wsURL = BitgetWSPrivate
	}
	conn, _, err := w.dialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	w.privateConn = conn

	// 发送登錄认证
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	sign := w.generateSign(timestamp, "GET", "/user/verify")

	loginMsg := map[string]interface{}{
		"op": "login",
		"args": []map[string]string{
			{
				"apiKey":     w.apiKey,
				"passphrase": w.passphrase,
				"timestamp":  timestamp,
				"sign":       sign,
			},
		},
	}

	if err := conn.WriteJSON(loginMsg); err != nil {
		return fmt.Errorf("发送登錄消息失败: %w", err)
	}

	// 等待登錄响应
	var resp WSMessage
	if err := conn.ReadJSON(&resp); err != nil {
		return fmt.Errorf("读取登錄响应失败: %w", err)
	}

	codeStr := resp.GetCodeString()
	if codeStr != "0" && codeStr != "" {
		return fmt.Errorf("登錄失败: code=%s, msg=%s", codeStr, resp.Msg)
	}

	logger.Info("✅ [Bitget WebSocket] 私有频道登錄成功")
	return nil
}

// connectPublic 连接公共 WebSocket
func (w *WebSocketManager) connectPublic() error {
	var wsURL string
	if w.testnet {
		wsURL = BitgetTestnetWSPublic
		logger.Info("🌐 [Bitget WS公共] 使用測試網 WebSocket: %s", wsURL)
	} else {
		wsURL = BitgetWSPublic
	}
	conn, _, err := w.dialer.Dial(wsURL, nil)
	if err != nil {
		return err
	}
	w.publicConn = conn
	logger.Info("✅ [Bitget WebSocket] 公共频道连接成功")
	return nil
}

// subscribeOrders 订阅订單更新
func (w *WebSocketManager) subscribeOrders(symbol string) error {
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []WSSubscribeArg{
			{
				InstType: "USDT-FUTURES",
				Channel:  "orders",
				InstId:   "default", // 订阅所有交易對
			},
		},
	}

	logger.Info("📡 [Bitget WS] 订阅私有频道: orders")
	return w.privateConn.WriteJSON(subMsg)
}

// subscribeTicker 订阅價格更新
func (w *WebSocketManager) subscribeTicker(symbol string) error {
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []WSSubscribeArg{
			{
				InstType: "USDT-FUTURES",
				Channel:  "ticker",
				InstId:   symbol,
			},
		},
	}

	return w.publicConn.WriteJSON(subMsg)
}

// handlePrivateMessages 处理私有频道消息（订單更新和成交明细）
func (w *WebSocketManager) handlePrivateMessages(conn *websocket.Conn) {
	// 🔥 設置读取超時：90秒
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Warn("⚠️ [Bitget WebSocket] 读取私有消息失败: %v", err)
				// 🔥 关键：触发重连
				select {
				case w.privateReconnectChan <- struct{}{}:
				default:
				}
				return
			}

			// 🔥 收到消息后更新读取超時
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))

			// 忽略 pong 响应
			if string(message) == "pong" {
				logger.Debug("💓 [Bitget WS私有] 收到 pong")
				continue
			}

			var msg struct {
				Event  string          `json:"event"`  // subscribe / error / login
				Op     string          `json:"op"`     // trade (下單响应)
				Action string          `json:"action"` // snapshot / update
				Arg    WSSubscribeArg  `json:"arg"`
				Data   json.RawMessage `json:"data"`
				Code   json.RawMessage `json:"code"`
				Msg    string          `json:"msg"`
			}

			if err := json.Unmarshal(message, &msg); err != nil {
				logger.Warn("⚠️ [Bitget WebSocket] 解析私有消息失败: %v", err)
				continue
			}

			// 🔍 調試：打印收到的消息類型
			logger.Debug("🔍 [Bitget WS私有] event=%s, op=%s, action=%s, channel=%s",
				msg.Event, msg.Op, msg.Action, msg.Arg.Channel)

			// 处理订阅确认
			if msg.Event == "subscribe" {
				logger.Debug("✅ [Bitget WS] 订阅成功: %s", msg.Arg.Channel)
				continue
			}

			// 处理錯误消息
			if msg.Event == "error" {
				logger.Error("❌ [Bitget WS] 錯误: %s", msg.Msg)
				continue
			}

			// 处理订單推送 (channel="orders")
			if msg.Arg.Channel == "orders" && len(msg.Data) > 0 {
				logger.Debug("🔍 [Bitget WS订單] 推送數據: %s", string(msg.Data))
				w.handleOrderUpdate(msg.Data)
				continue
			}
		}
	}
}

// handlePublicMessages 处理公共频道消息（價格更新）
func (w *WebSocketManager) handlePublicMessages(conn *websocket.Conn) {
	// 🔥 設置读取超時：90秒（大於3倍ping间隔）
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))

	logger.Debug("📥 [Bitget WS公共] 开始監听消息...")

	for {
		select {
		case <-w.ctx.Done():
			logger.Debug("📥 [Bitget WS公共] 停止監听消息（上下文取消）")
			return
		default:
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Warn("⚠️ [Bitget WebSocket] 读取公共消息失败: %v", err)
				// 🔥 关键：触发重连
				select {
				case w.publicReconnectChan <- struct{}{}:
				default:
				}
				return
			}

			// 🔥 收到消息后更新读取超時
			conn.SetReadDeadline(time.Now().Add(90 * time.Second))

			// 忽略 pong 响应
			if string(message) == "pong" {
				logger.Debug("💓 [Bitget WS公共] 收到 pong")
				continue
			}

			logger.Debug("📨 [Bitget WS公共] 收到消息: %s", string(message))

			var msg struct {
				Arg    WSSubscribeArg  `json:"arg"`
				Action string          `json:"action"`
				Data   json.RawMessage `json:"data"`
			}

			if err := json.Unmarshal(message, &msg); err != nil {
				logger.Warn("⚠️ [Bitget WebSocket] 解析公共消息失败: %v, 原始消息: %s", err, string(message))
				continue
			}

			// 記錄订阅确认
			if msg.Action == "subscribe" && msg.Arg.Channel == "ticker" {
				logger.Info("✅ [Bitget WS公共] 订阅确认: %s/%s", msg.Arg.InstType, msg.Arg.InstId)
			}

			// 处理價格更新
			// Bitget V2 推送格式: {"action":"snapshot","arg":{"instType":"USDT-FUTURES","channel":"ticker","instId":"ETHUSDT"},"data":[...]}
			if msg.Arg.Channel == "ticker" {
				if len(msg.Data) > 0 {
					logger.Debug("📊 [Bitget WS公共] 收到 ticker 數據，action=%s, instId=%s", msg.Action, msg.Arg.InstId)
					w.handlePriceUpdate(msg.Data)
				} else {
					logger.Debug("⚠️ [Bitget WS公共] ticker 消息數據為空")
				}
			} else {
				logger.Debug("🔍 [Bitget WS公共] 收到其他频道消息: channel=%s, action=%s", msg.Arg.Channel, msg.Action)
			}
		}
	}
}

// handleOrderUpdate 处理订單更新
func (w *WebSocketManager) handleOrderUpdate(data json.RawMessage) {
	var updates []map[string]interface{}
	if err := json.Unmarshal(data, &updates); err != nil {
		logger.Warn("⚠️ [Bitget WebSocket] 解析订單更新失败: %v", err)
		return
	}

	//logger.Info("🔍 [Bitget WS] 收到 %d 条订單更新", len(updates))

	for _, update := range updates {
		// 🔍 調試：打印原始订單數據的关键字段
		orderID, _ := update["orderId"].(string)
		status, _ := update["status"].(string)
		side, _ := update["side"].(string)
		accBaseVolume, _ := update["accBaseVolume"].(string)

		// 🔥 关键诊断：如果订單被撤销，打印完整的原始數據
		if status == "cancelled" || status == "canceled" {
			//updateBytes, _ := json.Marshal(update)
			//logger.Warn("⚠️ [Bitget WS订單撤销] 完整數據: %s", string(updateBytes))
			//2025/12/07 20:46:12 [WARN] ⚠️ [Bitget WS订單撤销] 完整數據: {"accBaseVolume":"0","cTime":"1765101259950","cancelReason":"normal_cancel","clientOid":"sqt_302711_B_1765101259932571318","enterPointSource":"API","feeDetail":[{"fee":"0.00000000","feeCoin":"USDT"}],"force":"post_only","instId":"ETHUSDT","leverage":"10","marginCoin":"USDT","marginMode":"crossed","notionalUsd":"30.2711","orderId":"1381500303017938945","orderType":"limit","posMode":"hedge_mode","posSide":"long","presetStopLossExecutePrice":"","presetStopLossType":"","presetStopSurplusExecutePrice":"","presetStopSurplusType":"","price":"3027.11","reduceOnly":"no","side":"buy","size":"0.01","status":"canceled","stpMode":"none","totalProfits":"0","tradeSide":"open","uTime":"1765111572322"}
			logger.Warn("⚠️ [Bitget 订單被交易所撤销] ")
		}

		logger.Debug("🔍 [Bitget WS订單] ID=%s, 状態=%s, 方向=%s, 成交量=%s",
			orderID, status, side, accBaseVolume)

		if w.orderCallback != nil {
			// 轉换為 OrderUpdate 格式
			orderUpdate := w.parseOrderUpdate(update)
			if orderUpdate != nil {
				logger.Debug("🔍 [Bitget WS订單] 解析后: ID=%d, Status=%s, ExecutedQty=%.4f",
					orderUpdate.OrderID, orderUpdate.Status, orderUpdate.ExecutedQty)
				w.orderCallback(orderUpdate)
			}
		}
	}
}

// handlePriceUpdate 处理價格更新
func (w *WebSocketManager) handlePriceUpdate(data json.RawMessage) {
	var updates []map[string]interface{}
	if err := json.Unmarshal(data, &updates); err != nil {
		logger.Warn("⚠️ [Bitget WebSocket] 解析價格更新失败: %v", err)
		return
	}

	if len(updates) == 0 {
		logger.Warn("⚠️ [Bitget WebSocket] 收到空的價格更新數據")
		return
	}

	logger.Debug("📊 [Bitget WS] 收到價格更新，數據条數: %d", len(updates))

	for _, update := range updates {
		// Bitget V2 Ticker 字段是 lastPr
		lastStr, ok := update["lastPr"].(string)
		if !ok {
			// 尝試兼容舊字段
			lastStr, ok = update["last"].(string)
		}

		if !ok {
			logger.Debug("⚠️ [Bitget WS] 價格更新中未找到 lastPr 或 last 欄位，數據: %+v", update)
			continue
		}

		price, err := strconv.ParseFloat(lastStr, 64)
		if err != nil {
			logger.Warn("⚠️ [Bitget WS] 解析價格失败: lastPr=%s, error=%v", lastStr, err)
			continue
		}

		if price > 0 {
			w.priceMu.Lock()
			oldPrice := w.latestPrice
			w.latestPrice = price
			w.priceMu.Unlock()

			// 記錄首次價格或價格變化
			if oldPrice == 0 {
				symbol, _ := update["instId"].(string)
				logger.Info("✅ [Bitget WS] 收到首個價格: %s = %.2f", symbol, price)
			}

			if w.priceCallback != nil {
				// instId 是交易對名称
				symbol, _ := update["instId"].(string)
				w.priceCallback(symbol, price)
			}
		} else {
			logger.Warn("⚠️ [Bitget WS] 收到無效價格: %.2f", price)
		}
	}
}

// parseOrderUpdate 解析订單更新
func (w *WebSocketManager) parseOrderUpdate(data map[string]interface{}) *OrderUpdate {
	orderIDStr, _ := data["orderId"].(string)
	orderID, _ := strconv.ParseInt(orderIDStr, 10, 64)

	clientOrderID, _ := data["clientOid"].(string) // 🔥 解析 ClientOrderID

	symbol, _ := data["instId"].(string)
	sideStr, _ := data["side"].(string)
	statusStr, _ := data["status"].(string)
	priceStr, _ := data["price"].(string)
	qtyStr, _ := data["size"].(string)
	filledQtyStr, _ := data["accBaseVolume"].(string)
	avgPriceStr, _ := data["priceAvg"].(string)
	updateTimeStr, _ := data["uTime"].(string)
	tradeSideStr, _ := data["tradeSide"].(string)
	posSideStr, _ := data["posSide"].(string)

	// 🔍 調試：打印关键字段的原始值
	logger.Debug("🔍 [parseOrderUpdate] accBaseVolume=%v (type=%T), priceAvg=%v (type=%T)",
		data["accBaseVolume"], data["accBaseVolume"], data["priceAvg"], data["priceAvg"])

	price, _ := strconv.ParseFloat(priceStr, 64)
	quantity, _ := strconv.ParseFloat(qtyStr, 64)
	executedQty, _ := strconv.ParseFloat(filledQtyStr, 64)
	avgPrice, _ := strconv.ParseFloat(avgPriceStr, 64)
	updateTime, _ := strconv.ParseInt(updateTimeStr, 10, 64)

	// 解析手續費：feeDetail 格式為 [{"fee":"0.00000000","feeCoin":"USDT"}]
	commission := 0.0
	commissionAsset := "USDT"
	if feeDetailRaw, ok := data["feeDetail"].([]interface{}); ok && len(feeDetailRaw) > 0 {
		if feeItem, ok := feeDetailRaw[0].(map[string]interface{}); ok {
			if feeStr, ok := feeItem["fee"].(string); ok {
				commission, _ = strconv.ParseFloat(feeStr, 64)
			}
			if feeCoin, ok := feeItem["feeCoin"].(string); ok && feeCoin != "" {
				commissionAsset = feeCoin
			}
		}
	}

	// 🔍 調試：打印解析后的值
	logger.Debug("🔍 [parseOrderUpdate] 解析結果: executedQty=%.4f, avgPrice=%.2f, Price=%.2f, Commission=%.8f %s", executedQty, avgPrice, price, commission, commissionAsset)

	side := SideBuy
	lowerSide := strings.ToLower(strings.TrimSpace(sideStr))
	if lowerSide == "sell" {
		side = SideSell
	} else if lowerSide == "buy" {
		side = SideBuy
	} else {
		lowerTrade := strings.ToLower(strings.TrimSpace(tradeSideStr))
		lowerPos := strings.ToLower(strings.TrimSpace(posSideStr))
		if strings.Contains(lowerTrade, "close") || lowerPos == "short" {
			side = SideSell
		} else if strings.Contains(lowerTrade, "open") || lowerPos == "long" {
			side = SideBuy
		} else if lowerSide != "" {
			logger.Warn("⚠️ [Bitget WS] 未知 side 值: %s (tradeSide=%s, posSide=%s), 默认按買單处理", sideStr, tradeSideStr, posSideStr)
		}
	}

	// 🔥 关键修複：Bitget V2 WebSocket 订單推送的状態值
	// 根據官方文檔：live=挂單中, partially_filled=部分成交, filled=完全成交, cancelled=已撤销
	var status OrderStatus = "NEW"
	switch statusStr {
	case "new", "live": // live 表示订單挂單中
		status = "NEW"
	case "partial_filled", "partial-fill", "partially_filled":
		status = "PARTIALLY_FILLED"
	case "filled", "full-fill":
		status = "FILLED"
	case "cancelled", "canceled":
		status = "CANCELED"
	default:
		// 🔍 如果遇到未知状態，記錄日志
		logger.Warn("⚠️ [Bitget WS] 未知订單状態: %s, 订單ID: %s", statusStr, orderIDStr)
		status = OrderStatus(statusStr) // 保留原始状態
	}

	// 🔥 解析已實現盈虧（Bitget 返回 totalProfits 字段）
	totalProfitsStr, _ := data["totalProfits"].(string)
	realizedPnL, _ := strconv.ParseFloat(totalProfitsStr, 64)

	return &OrderUpdate{
		OrderID:         orderID,
		ClientOrderID:   clientOrderID, // 🔥 包含 ClientOrderID
		Symbol:          symbol,
		Side:            side,
		Type:            OrderTypeLimit,
		Status:          status,
		Price:           price,
		Quantity:        quantity,
		ExecutedQty:     executedQty,
		AvgPrice:        avgPrice,
		UpdateTime:      updateTime,
		Commission:      commission,
		CommissionAsset: commissionAsset,
		RealizedPnL:     realizedPnL,
	}
}

// PlaceOrderWS 已廢棄 - Bitget不支援WebSocket下單，请使用REST API
// 保留方法签名以兼容舊代碼，但返回錯误
func (w *WebSocketManager) PlaceOrderWS(symbol string, side string, price, quantity float64, priceDecimals int) (string, error) {
	return "", fmt.Errorf("Bitget不支援WebSocket下單，请使用REST API")
}

// GetLatestPrice 獲取最新價格
func (w *WebSocketManager) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}

// keepAlive WebSocket 保活（每15秒发送 ping）
func (w *WebSocketManager) keepAlive(conn *websocket.Conn, connType string, reconnectChan chan struct{}) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-w.ctx.Done():
			return
		case <-ticker.C:
			if conn != nil {
				conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
				err := conn.WriteMessage(websocket.TextMessage, []byte("ping"))
				if err != nil {
					logger.Warn("⚠️ [Bitget WS%s] 发送 ping 失败: %v", connType, err)
					// 🔥 关键：ping 失败說明连接已断开，触发重连並退出
					select {
					case reconnectChan <- struct{}{}:
					default:
					}
					return
				}
				logger.Debug("💓 [Bitget WS%s] Ping已发送", connType)
			}
		}
	}
}

// generateSign 生成签名
func (w *WebSocketManager) generateSign(timestamp, method, requestPath string) string {
	message := timestamp + method + requestPath
	mac := hmac.New(sha256.New, []byte(w.secretKey))
	mac.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}
