package okx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	// WebSocket 地址
	MainnetWsURL       = "wss://ws.okx.com:8443/ws/v5/private"
	TestnetWsURL       = "wss://wspap.okx.com:8443/ws/v5/private"
	MainnetPublicWsURL = "wss://ws.okx.com:8443/ws/v5/public"
	TestnetPublicWsURL = "wss://wspap.okx.com:8443/ws/v5/public"
	// OKX 已将 K线/candle 等频道迁移到 business 端点，public 上不再提供（订阅会报
	// "Wrong URL or channel:candle1m ... doesn't exist"）。
	MainnetBusinessWsURL = "wss://ws.okx.com:8443/ws/v5/business"
	TestnetBusinessWsURL = "wss://wspap.okx.com:8443/ws/v5/business"
)

const (
	// privateWsHandshakeTimeout 登錄/訂閱確認的等待上限
	privateWsHandshakeTimeout = 10 * time.Second
	// privateWsPingInterval 心跳間隔（OKX 30 秒無消息會斷開）
	privateWsPingInterval = 20 * time.Second
	// privateWsReadTimeout 讀超時：心跳每 20 秒一次必有 pong，超過即視為連接已死
	privateWsReadTimeout = 3 * privateWsPingInterval
	// privateWsReconnectInitialBackoff / privateWsReconnectMaxBackoff 重連指數退避區間
	privateWsReconnectInitialBackoff = 1 * time.Second
	privateWsReconnectMaxBackoff     = 60 * time.Second

	okxPingMessage = "ping"
	okxPongMessage = "pong"

	okxEventLogin     = "login"
	okxEventSubscribe = "subscribe"
	okxEventError     = "error"
	okxSuccessCode    = "0"
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	passphrase string
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	writeMu       sync.Mutex // 串行化私有 conn 的寫操作，避免 gorilla/websocket 並發寫競態
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(OrderUpdate)
	priceCallback func(float64)

	// 私有訂單流生命週期：runCancel 取消後讀循環/心跳/重連全部退出，runWg 等待其結束
	privateURL              string // 為空時按 useTestnet 選擇（測試可覆寫）
	privateInstType         string // orders 頻道的 instType（SWAP/SPOT），為空時按 SWAP
	reconnectInitialBackoff time.Duration
	reconnectMaxBackoff     time.Duration
	runMu                   sync.Mutex
	runCancel               context.CancelFunc
	runWg                   sync.WaitGroup

	// 公共行情（tickers）單獨連接，與私有訂單 conn 分離；需可取消、可重連，避免断線後價格永遠卡死在舊值
	muPrice        sync.Mutex
	priceInstID    string
	priceRunCancel context.CancelFunc
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey, passphrase string, useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:                  apiKey,
		secretKey:               secretKey,
		passphrase:              passphrase,
		useTestnet:              useTestnet,
		reconnectInitialBackoff: privateWsReconnectInitialBackoff,
		reconnectMaxBackoff:     privateWsReconnectMaxBackoff,
	}
}

// sign 生成签名
func (w *WebSocketManager) sign(timestamp string) string {
	message := timestamp + "GET" + "/users/self/verify"
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// SetOrderInstType 設置私有 orders 頻道訂閱的 instType（SWAP/SPOT）。需在 Start 前調用。
func (w *WebSocketManager) SetOrderInstType(instType string) {
	w.privateInstType = instType
}

func (w *WebSocketManager) orderInstType() string {
	if w.privateInstType != "" {
		return w.privateInstType
	}
	return okxInstTypeSwap
}

func (w *WebSocketManager) privateWsURL() string {
	if w.privateURL != "" {
		return w.privateURL
	}
	if w.useTestnet {
		return TestnetWsURL
	}
	return MainnetWsURL
}

// Start 啟動訂單流：首次連接同步完成（登錄確認 → 訂閱確認），之後斷線自動重連並重新登錄、訂閱
func (w *WebSocketManager) Start(ctx context.Context, instId string, callback func(OrderUpdate)) error {
	w.runMu.Lock()
	defer w.runMu.Unlock()

	if w.isRunning.Load() {
		return fmt.Errorf("WebSocket 已在运行")
	}

	w.orderCallback = callback
	runCtx, cancel := context.WithCancel(ctx)

	conn, err := w.connectPrivate(runCtx, instId)
	if err != nil {
		cancel()
		return fmt.Errorf("OKX 私有訂單流啟動失败(instId=%s): %w", instId, err)
	}

	w.runCancel = cancel
	w.isRunning.Store(true)
	w.runWg.Add(2)
	go w.runPrivateLoop(runCtx, instId, conn)
	go w.keepAlive(runCtx)

	logger.Info("✅ [OKX WebSocket] 訂單流已啟动 instId=%s", instId)
	return nil
}

// connectPrivate 建立私有連接並完成「登錄 → 等登錄確認 → 訂閱 → 等訂閱確認」
func (w *WebSocketManager) connectPrivate(ctx context.Context, instId string) (*websocket.Conn, error) {
	dialCtx, cancel := context.WithTimeout(ctx, privateWsHandshakeTimeout)
	defer cancel()
	conn, _, err := websocket.DefaultDialer.DialContext(dialCtx, w.privateWsURL(), nil)
	if err != nil {
		return nil, fmt.Errorf("连接 WebSocket 失败: %w", err)
	}
	if !w.setConn(ctx, conn) {
		return nil, fmt.Errorf("WebSocket 已停止")
	}

	fail := func(step string, err error) (*websocket.Conn, error) {
		w.closeConn(conn)
		return nil, fmt.Errorf("%s: %w", step, err)
	}

	if err := w.login(); err != nil {
		return fail("发送登錄请求失败", err)
	}
	// OKX 要求登錄成功後才能訂閱私有頻道，否則訂閱被拒且不會自動補訂
	if err := w.awaitEvent(conn, okxEventLogin); err != nil {
		return fail("WebSocket 登錄失败", err)
	}
	if err := w.subscribeOrders(instId); err != nil {
		return fail("发送订阅请求失败", err)
	}
	if err := w.awaitEvent(conn, okxEventSubscribe); err != nil {
		return fail("订阅订單频道失败", err)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return conn, nil
}

// setConn 設置當前連接；若已停止則關閉新連接並返回 false（避免 Stop 之後遺留連接）
func (w *WebSocketManager) setConn(ctx context.Context, conn *websocket.Conn) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if ctx.Err() != nil {
		_ = conn.Close()
		return false
	}
	w.conn = conn
	return true
}

// closeConn 關閉指定連接，若它仍是當前連接則清空
func (w *WebSocketManager) closeConn(conn *websocket.Conn) {
	w.mu.Lock()
	if w.conn == conn {
		w.conn = nil
	}
	w.mu.Unlock()
	_ = conn.Close()
}

// awaitEvent 在握手階段同步讀取，直到收到指定 event 的確認；收到 error 事件或超時則失败
func (w *WebSocketManager) awaitEvent(conn *websocket.Conn, want string) error {
	deadline := time.Now().Add(privateWsHandshakeTimeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("等待 %s 确认失败: %w", want, err)
		}
		if string(message) == okxPongMessage {
			continue
		}
		var ev struct {
			Event string `json:"event"`
			Code  string `json:"code"`
			Msg   string `json:"msg"`
		}
		if err := json.Unmarshal(message, &ev); err != nil {
			continue
		}
		switch ev.Event {
		case want:
			if ev.Code != "" && ev.Code != okxSuccessCode {
				return fmt.Errorf("OKX 返回 %s 失败 code=%s msg=%s", want, ev.Code, ev.Msg)
			}
			return nil
		case okxEventError:
			return fmt.Errorf("OKX 返回錯误 code=%s msg=%s", ev.Code, ev.Msg)
		case "":
			w.safeHandleMessage(message)
		}
	}
}

// runPrivateLoop 讀取私有推送；斷線後按指數退避重連，ctx 取消時退出
func (w *WebSocketManager) runPrivateLoop(ctx context.Context, instId string, conn *websocket.Conn) {
	defer w.runWg.Done()
	for {
		err := w.readLoop(conn)
		w.closeConn(conn)
		if ctx.Err() != nil {
			return
		}
		logger.Warn("⚠️ [OKX WebSocket] 私有訂單流斷開(instId=%s): %v，开始重連", instId, err)

		conn = w.reconnect(ctx, instId)
		if conn == nil {
			return
		}
		logger.Info("✅ [OKX WebSocket] 私有訂單流重連成功(instId=%s)，已重新登錄並訂閱", instId)
	}
}

// reconnect 指數退避重連，直到成功或 ctx 取消（返回 nil）
func (w *WebSocketManager) reconnect(ctx context.Context, instId string) *websocket.Conn {
	backoff := w.reconnectInitialBackoff
	if backoff <= 0 {
		backoff = privateWsReconnectInitialBackoff
	}
	maxBackoff := w.reconnectMaxBackoff
	if maxBackoff < backoff {
		maxBackoff = backoff
	}
	for attempt := 1; ; attempt++ {
		timer := time.NewTimer(backoff)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}

		conn, err := w.connectPrivate(ctx, instId)
		if err == nil {
			return conn
		}
		if ctx.Err() != nil {
			return nil
		}
		next := backoff * 2
		if next > maxBackoff {
			next = maxBackoff
		}
		logger.Warn("⚠️ [OKX WebSocket] 第 %d 次重連失败(instId=%s): %v，%v 後重試", attempt, instId, err, next)
		backoff = next
	}
}

// readLoop 讀取消息直到出錯
func (w *WebSocketManager) readLoop(conn *websocket.Conn) error {
	for {
		_ = conn.SetReadDeadline(time.Now().Add(privateWsReadTimeout))
		_, message, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if string(message) == okxPongMessage {
			continue
		}
		w.safeHandleMessage(message)
	}
}

// safeHandleMessage 處理消息並吞掉單條消息引發的 panic，避免整條訂單流停擺
func (w *WebSocketManager) safeHandleMessage(message []byte) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("❌ [OKX WebSocket] 消息处理 panic: %v", r)
		}
	}()
	w.handleMessage(message)
}

// login 登錄认证
func (w *WebSocketManager) login() error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sign := w.sign(timestamp)

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

	return w.sendMessage(loginMsg)
}

// subscribeOrders 订阅订單频道
func (w *WebSocketManager) subscribeOrders(instId string) error {
	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []map[string]string{
			{
				"channel":  "orders",
				"instType": w.orderInstType(),
				"instId":   instId,
			},
		},
	}

	return w.sendMessage(subMsg)
}

// StartPriceStream 啟動價格流（可重複調用：會取消上一路公共行情协程並關閉舊連接，避免多連接競寫 lastPrice）
func (w *WebSocketManager) StartPriceStream(ctx context.Context, instId string, callback func(float64)) error {
	w.priceCallback = callback
	w.priceInstID = instId

	w.muPrice.Lock()
	if w.priceRunCancel != nil {
		w.priceRunCancel()
		w.priceRunCancel = nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.priceRunCancel = cancel
	w.muPrice.Unlock()

	go w.runPublicPriceLoop(runCtx, instId)
	logger.Info("✅ [OKX WebSocket] 價格流已啟动 instId=%s", instId)
	return nil
}

func (w *WebSocketManager) runPublicPriceLoop(ctx context.Context, instId string) {
	publicWsURL := MainnetPublicWsURL
	if w.useTestnet {
		publicWsURL = TestnetPublicWsURL
	}

	backoff := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(publicWsURL, nil)
		if err != nil {
			logger.Warn("⚠️ [OKX WebSocket] 公共行情連接失敗: %v，%v 後重試", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}

		subMsg := map[string]interface{}{
			"op": "subscribe",
			"args": []map[string]string{
				{
					"channel": "tickers",
					"instId":  instId,
				},
			},
		}
		if err := conn.WriteJSON(subMsg); err != nil {
			logger.Warn("⚠️ [OKX WebSocket] 訂閱 tickers 失敗: %v", err)
			_ = conn.Close()
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}

	readLoop:
		for {
			select {
			case <-ctx.Done():
				_ = conn.Close()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				if ctx.Err() != nil {
					_ = conn.Close()
					return
				}
				logger.Warn("⚠️ [OKX WebSocket] 讀取價格消息失敗: %v，重連", err)
				_ = conn.Close()
				break readLoop
			}
			w.handlePriceMessage(message)
		}

		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

// sendMessage 发送消息
func (w *WebSocketManager) sendMessage(msg interface{}) error {
	w.mu.RLock()
	conn := w.conn
	w.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("WebSocket 未连接")
	}

	w.writeMu.Lock()
	defer w.writeMu.Unlock()
	return conn.WriteJSON(msg)
}

// handleMessage 处理消息
func (w *WebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [OKX WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查事件類型
	if event, ok := msg["event"].(string); ok {
		if event == okxEventLogin {
			if code, ok := msg["code"].(string); ok && code == okxSuccessCode {
				logger.Info("✅ [OKX WebSocket] 登錄成功")
			} else {
				logger.Error("❌ [OKX WebSocket] 登錄失败: %v", msg["msg"])
			}
		} else if event == okxEventSubscribe {
			logger.Info("✅ [OKX WebSocket] 订阅成功")
		} else if event == okxEventError {
			logger.Error("❌ [OKX WebSocket] 錯误: code=%v msg=%v", msg["code"], msg["msg"])
		}
		return
	}

	// 处理订單數據
	if arg, ok := msg["arg"].(map[string]interface{}); ok {
		if channel, ok := arg["channel"].(string); ok && channel == "orders" {
			w.handleOrderUpdate(msg)
		}
	}
}

// handleOrderUpdate 处理订單更新。
// 這裡保留 OKX 原生值（instId、張數、buy/sell、live/filled），由適配器統一換算成內部口徑。
func (w *WebSocketManager) handleOrderUpdate(msg map[string]interface{}) {
	data, ok := msg["data"].([]interface{})
	if !ok || len(data) == 0 {
		return
	}

	for _, item := range data {
		orderData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		orderId, _ := strconv.ParseInt(getString(orderData, "ordId"), 10, 64)
		price, _ := strconv.ParseFloat(getString(orderData, "px"), 64)
		quantity, _ := strconv.ParseFloat(getString(orderData, "sz"), 64)
		executedQty, _ := strconv.ParseFloat(getString(orderData, "accFillSz"), 64)
		avgPrice, _ := strconv.ParseFloat(getString(orderData, "avgPx"), 64)
		updateTime, _ := strconv.ParseInt(getString(orderData, "uTime"), 10, 64)

		// 🔥 解析已實現盈虧（OKX 返回 pnl 字段，僅平倉訂單有效）
		realizedPnL, _ := strconv.ParseFloat(getString(orderData, "pnl"), 64)

		// 本次成交手續費：fillFee 扣費為負、返佣為正，轉為「支出為正」
		fillFeeRaw, fillFeePresent := orderData["fillFee"].(string)
		fillFee, fillFeeErr := strconv.ParseFloat(fillFeeRaw, 64)
		commissionKnown := fillFeePresent && fillFeeErr == nil && !math.IsNaN(fillFee) && !math.IsInf(fillFee, 0)
		fillPx, _ := strconv.ParseFloat(getString(orderData, "fillPx"), 64)
		feeCcy := getString(orderData, "fillFeeCcy")
		if feeCcy == "" {
			feeCcy = okxDefaultCommissionAsset
		}

		update := OrderUpdate{
			OrderID:         orderId,
			ClientOrderID:   getString(orderData, "clOrdId"),
			Symbol:          getString(orderData, "instId"),
			Side:            Side(getString(orderData, "side")),
			Type:            OrderType(getString(orderData, "ordType")),
			Status:          OrderStatus(getString(orderData, "state")),
			Price:           price,
			Quantity:        quantity,
			ExecutedQty:     executedQty,
			AvgPrice:        avgPrice,
			UpdateTime:      updateTime,
			Commission:      okxFeeToCommission(fillFee),
			CommissionAsset: feeCcy,
			CommissionKnown: commissionKnown,
			RealizedPnL:     realizedPnL,
			FillPrice:       fillPx,
		}

		if w.orderCallback != nil {
			w.orderCallback(update)
		}
	}
}

// handlePriceMessage 处理價格消息（只採用訂閱的 instId，避免多標的推送時誤用 data[0]）
func (w *WebSocketManager) handlePriceMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	if arg, ok := msg["arg"].(map[string]interface{}); ok {
		if channel, ok := arg["channel"].(string); !ok || channel != "tickers" {
			return
		}
		if want := w.priceInstID; want != "" {
			if got, ok := arg["instId"].(string); ok && got != "" && got != want {
				return
			}
		}
		data, ok := msg["data"].([]interface{})
		if !ok || len(data) == 0 {
			return
		}
		want := w.priceInstID
		for _, row := range data {
			ticker, ok := row.(map[string]interface{})
			if !ok {
				continue
			}
			if want != "" {
				if id, ok := ticker["instId"].(string); ok && id != "" && id != want {
					continue
				}
			}
			lastStr, ok := ticker["last"].(string)
			if !ok {
				continue
			}
			price, err := strconv.ParseFloat(lastStr, 64)
			if err != nil || price <= 0 {
				continue
			}
			w.lastPrice.Store(price)
			if w.priceCallback != nil {
				w.priceCallback(price)
			}
			return
		}
	}
}

// getString 安全獲取字符串值
func getString(m map[string]interface{}, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// keepAlive 保持连接（對當前私有連接發 ping；ctx 取消時退出）
func (w *WebSocketManager) keepAlive(ctx context.Context) {
	defer w.runWg.Done()
	ticker := time.NewTicker(privateWsPingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn != nil {
				w.writeMu.Lock()
				err := conn.WriteMessage(websocket.TextMessage, []byte(okxPingMessage))
				w.writeMu.Unlock()
				if err != nil {
					logger.Warn("⚠️ [OKX WebSocket] 发送 ping 失败: %v", err)
				}
			}

		case <-ctx.Done():
			return
		}
	}
}

// GetLatestPrice 獲取最新價格
func (w *WebSocketManager) GetLatestPrice() float64 {
	if price := w.lastPrice.Load(); price != nil {
		return price.(float64)
	}
	return 0
}

// Stop 停止 WebSocket（等待讀循環、心跳、重連協程全部退出）
func (w *WebSocketManager) Stop() {
	w.muPrice.Lock()
	if w.priceRunCancel != nil {
		w.priceRunCancel()
		w.priceRunCancel = nil
	}
	w.muPrice.Unlock()

	w.runMu.Lock()
	defer w.runMu.Unlock()

	if !w.isRunning.Load() {
		return
	}

	w.isRunning.Store(false)
	if w.runCancel != nil {
		w.runCancel()
		w.runCancel = nil
	}

	// 先取消 ctx 再關連接：setConn 會拒絕 Stop 之後才建立的新連接
	w.mu.Lock()
	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()

	w.runWg.Wait()

	logger.Info("🛑 [OKX WebSocket] 已停止")
}
