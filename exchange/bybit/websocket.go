package bybit

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	// WebSocket 地址
	MainnetWsURL = "wss://stream.bybit.com/v5/private"
	TestnetWsURL = "wss://stream-testnet.bybit.com/v5/private"

	PublicWsURL        = "wss://stream.bybit.com/v5/public/linear"
	PublicTestnetWsURL = "wss://stream-testnet.bybit.com/v5/public/linear"

	// 現貨公共行情（與 linear 分屬不同路徑）
	PublicSpotWsURL        = "wss://stream.bybit.com/v5/public/spot"
	PublicSpotTestnetWsURL = "wss://stream-testnet.bybit.com/v5/public/spot"
)

const (
	// privateWsHandshakeTimeout 認證/訂閱確認的等待上限
	privateWsHandshakeTimeout = 10 * time.Second
	// privateWsPingInterval 心跳間隔（Bybit 建議 20 秒）
	privateWsPingInterval = 20 * time.Second
	// privateWsReadTimeout 讀超時：心跳必有 pong 回包，超過即視為連接已死
	privateWsReadTimeout = 3 * privateWsPingInterval
	// privateWsReconnectInitialBackoff / privateWsReconnectMaxBackoff 重連指數退避區間
	privateWsReconnectInitialBackoff = 1 * time.Second
	privateWsReconnectMaxBackoff     = 60 * time.Second
	// privateWsAuthExpiry 認證簽名有效期
	privateWsAuthExpiry = 10 * time.Second

	bybitOpAuth      = "auth"
	bybitOpSubscribe = "subscribe"
	bybitOpPing      = "ping"
	bybitTopicOrder  = "order"
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	writeMu       sync.Mutex // 串行化私有 conn 的寫操作，避免 gorilla/websocket 並發寫競態
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(OrderUpdate)
	priceCallback func(float64)

	// orderCategory 非空時只轉發該 category 的訂單推送（order topic 是全品類的，合約適配器需過濾掉現貨/期權）
	orderCategory string

	// 私有訂單流生命週期：runCancel 取消後讀循環/心跳/重連全部退出，runWg 等待其結束
	privateURL              string // 為空時按 useTestnet 選擇（測試可覆寫）
	reconnectInitialBackoff time.Duration
	reconnectMaxBackoff     time.Duration
	runMu                   sync.Mutex
	runCancel               context.CancelFunc
	runWg                   sync.WaitGroup

	// 公共行情（tickers）單獨連線；與 OKX 相同需可取消、可重連，避免断線後 last 卡死
	muPrice        sync.Mutex
	priceTickerKey string // 如 BTCUSDT，校驗 topic tickers.{symbol}
	priceRunCancel context.CancelFunc
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:                  apiKey,
		secretKey:               secretKey,
		useTestnet:              useTestnet,
		reconnectInitialBackoff: privateWsReconnectInitialBackoff,
		reconnectMaxBackoff:     privateWsReconnectMaxBackoff,
	}
}

// SetOrderCategory 設置訂單推送的 category 過濾（linear/spot；空字串表示不過濾）。需在 Start 前調用。
func (w *WebSocketManager) SetOrderCategory(category string) {
	w.orderCategory = category
}

// sign 生成签名
func (w *WebSocketManager) sign(expires string) string {
	message := "GET/realtime" + expires
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
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

// Start 啟動訂單流：首次連接同步完成（認證確認 → 訂閱確認），之後斷線自動重連並重新認證、訂閱
func (w *WebSocketManager) Start(ctx context.Context, symbol string, callback func(OrderUpdate)) error {
	w.runMu.Lock()
	defer w.runMu.Unlock()

	if w.isRunning.Load() {
		return fmt.Errorf("WebSocket 已在运行")
	}

	w.orderCallback = callback
	runCtx, cancel := context.WithCancel(ctx)

	conn, err := w.connectPrivate(runCtx)
	if err != nil {
		cancel()
		return fmt.Errorf("Bybit 私有訂單流啟動失败(symbol=%s): %w", symbol, err)
	}

	w.runCancel = cancel
	w.isRunning.Store(true)
	w.runWg.Add(2)
	go w.runPrivateLoop(runCtx, conn)
	go w.keepAlive(runCtx)

	logger.Info("✅ [Bybit WebSocket] 訂單流已啟动 symbol=%s", symbol)
	return nil
}

// connectPrivate 建立私有連接並完成「認證 → 等認證確認 → 訂閱 → 等訂閱確認」
func (w *WebSocketManager) connectPrivate(ctx context.Context) (*websocket.Conn, error) {
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

	if err := w.auth(); err != nil {
		return fail("发送认证请求失败", err)
	}
	// 認證成功前訂閱私有 topic 會被拒，必須等確認
	if err := w.awaitOp(conn, bybitOpAuth); err != nil {
		return fail("WebSocket 认证失败", err)
	}
	if err := w.subscribeOrders(); err != nil {
		return fail("发送订阅请求失败", err)
	}
	if err := w.awaitOp(conn, bybitOpSubscribe); err != nil {
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

// awaitOp 在握手階段同步讀取，直到收到指定 op 的回包；success=false 或超時則失败
func (w *WebSocketManager) awaitOp(conn *websocket.Conn, want string) error {
	deadline := time.Now().Add(privateWsHandshakeTimeout)
	for {
		_ = conn.SetReadDeadline(deadline)
		_, message, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("等待 %s 确认失败: %w", want, err)
		}
		var resp struct {
			Op      string `json:"op"`
			Success *bool  `json:"success"`
			RetMsg  string `json:"ret_msg"`
		}
		if err := json.Unmarshal(message, &resp); err != nil {
			continue
		}
		switch resp.Op {
		case want:
			if resp.Success == nil || !*resp.Success {
				return fmt.Errorf("Bybit 返回 %s 失败: %s", want, resp.RetMsg)
			}
			return nil
		case "":
			w.safeHandleMessage(message)
		}
	}
}

// runPrivateLoop 讀取私有推送；斷線後按指數退避重連，ctx 取消時退出
func (w *WebSocketManager) runPrivateLoop(ctx context.Context, conn *websocket.Conn) {
	defer w.runWg.Done()
	for {
		err := w.readLoop(conn)
		w.closeConn(conn)
		if ctx.Err() != nil {
			return
		}
		logger.Warn("⚠️ [Bybit WebSocket] 私有訂單流斷開: %v，开始重連", err)

		conn = w.reconnect(ctx)
		if conn == nil {
			return
		}
		logger.Info("✅ [Bybit WebSocket] 私有訂單流重連成功，已重新认证並訂閱")
	}
}

// reconnect 指數退避重連，直到成功或 ctx 取消（返回 nil）
func (w *WebSocketManager) reconnect(ctx context.Context) *websocket.Conn {
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

		conn, err := w.connectPrivate(ctx)
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
		logger.Warn("⚠️ [Bybit WebSocket] 第 %d 次重連失败: %v，%v 後重試", attempt, err, next)
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
		w.safeHandleMessage(message)
	}
}

// safeHandleMessage 處理消息並吞掉單條消息引發的 panic，避免整條訂單流停擺
func (w *WebSocketManager) safeHandleMessage(message []byte) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("❌ [Bybit WebSocket] 消息处理 panic: %v", r)
		}
	}()
	w.handleMessage(message)
}

// auth 认证
func (w *WebSocketManager) auth() error {
	expires := strconv.FormatInt(time.Now().Add(privateWsAuthExpiry).UnixMilli(), 10)
	signature := w.sign(expires)

	authMsg := map[string]interface{}{
		"op": bybitOpAuth,
		"args": []string{
			w.apiKey,
			expires,
			signature,
		},
	}

	return w.sendMessage(authMsg)
}

// subscribeOrders 订阅订單频道
func (w *WebSocketManager) subscribeOrders() error {
	subMsg := map[string]interface{}{
		"op": bybitOpSubscribe,
		"args": []string{
			bybitTopicOrder,
		},
	}

	return w.sendMessage(subMsg)
}

// StartPriceStream 啟動合約價格流（公共 linear）
func (w *WebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(float64)) error {
	wsURL := PublicWsURL
	if w.useTestnet {
		wsURL = PublicTestnetWsURL
	}
	return w.startPublicPriceStream(ctx, wsURL, symbol, callback)
}

// StartSpotPriceStream 啟動現貨價格流（公共 spot + tickers.{symbol}）
func (w *WebSocketManager) StartSpotPriceStream(ctx context.Context, symbol string, callback func(float64)) error {
	wsURL := PublicSpotWsURL
	if w.useTestnet {
		wsURL = PublicSpotTestnetWsURL
	}
	return w.startPublicPriceStream(ctx, wsURL, symbol, callback)
}

func (w *WebSocketManager) startPublicPriceStream(ctx context.Context, wsURL string, symbol string, callback func(float64)) error {
	w.priceCallback = callback
	w.priceTickerKey = symbol

	w.muPrice.Lock()
	if w.priceRunCancel != nil {
		w.priceRunCancel()
		w.priceRunCancel = nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.priceRunCancel = cancel
	w.muPrice.Unlock()

	go w.runPublicPriceLoop(runCtx, wsURL, symbol)
	logger.Info("✅ [Bybit WebSocket] 價格流已啟动 symbol=%s", symbol)
	return nil
}

func (w *WebSocketManager) runPublicPriceLoop(ctx context.Context, wsURL string, symbol string) {
	backoff := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Warn("⚠️ [Bybit WebSocket] 公共行情連接失敗: %v，%v 後重試", err, backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}

		subMsg := map[string]interface{}{
			"op": "subscribe",
			"args": []string{
				fmt.Sprintf("tickers.%s", symbol),
			},
		}
		if err := conn.WriteJSON(subMsg); err != nil {
			logger.Warn("⚠️ [Bybit WebSocket] 訂閱 tickers 失敗: %v", err)
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
				logger.Warn("⚠️ [Bybit WebSocket] 讀取價格消息失敗: %v，重連", err)
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
		logger.Warn("⚠️ [Bybit WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查操作類型
	if op, ok := msg["op"].(string); ok {
		if op == bybitOpAuth {
			if success, ok := msg["success"].(bool); ok && success {
				logger.Info("✅ [Bybit WebSocket] 认证成功")
			} else {
				logger.Error("❌ [Bybit WebSocket] 认证失败: %v", msg["ret_msg"])
			}
		} else if op == bybitOpSubscribe {
			if success, ok := msg["success"].(bool); ok && !success {
				logger.Error("❌ [Bybit WebSocket] 订阅失败: %v", msg["ret_msg"])
			}
		}
		return
	}

	// 处理订單數據
	if topic, ok := msg["topic"].(string); ok && topic == bybitTopicOrder {
		w.handleOrderUpdate(msg)
	}
}

// handleOrderUpdate 处理订單更新（保留 Bybit 原生 Side/Type/Status，由適配器映射為內部常量）
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

		if w.orderCategory != "" {
			if category := getString(orderData, "category"); category != "" && category != w.orderCategory {
				continue
			}
		}

		orderId, _ := strconv.ParseInt(getString(orderData, "orderId"), 10, 64)
		price, _ := strconv.ParseFloat(getString(orderData, "price"), 64)
		quantity, _ := strconv.ParseFloat(getString(orderData, "qty"), 64)
		executedQty, _ := strconv.ParseFloat(getString(orderData, "cumExecQty"), 64)
		avgPrice, _ := strconv.ParseFloat(getString(orderData, "avgPrice"), 64)
		updateTime, _ := strconv.ParseInt(getString(orderData, "updatedTime"), 10, 64)

		// 🔥 解析已實現盈虧（Bybit 返回 closedPnl 字段，僅平倉訂單有效）
		realizedPnL, _ := strconv.ParseFloat(getString(orderData, "closedPnl"), 64)

		// Bybit order topic 只有累計手續費 cumExecFee，沒有「本次成交」手續費；
		// 這裡設為 0，由上層通過 GetOrderFills（/v5/execution/list）補充
		update := OrderUpdate{
			OrderID:         orderId,
			ClientOrderID:   getString(orderData, "orderLinkId"),
			Symbol:          getString(orderData, "symbol"),
			Side:            Side(getString(orderData, "side")),
			Type:            OrderType(getString(orderData, "orderType")),
			Status:          OrderStatus(getString(orderData, "orderStatus")),
			Price:           price,
			Quantity:        quantity,
			ExecutedQty:     executedQty,
			AvgPrice:        avgPrice,
			UpdateTime:      updateTime,
			Commission:      0,
			CommissionAsset: "USDT",
			RealizedPnL:     realizedPnL,
		}

		if w.orderCallback != nil {
			w.orderCallback(update)
		}
	}
}

// handlePriceMessage 处理價格消息（校驗 topic 與當前訂閱的 tickers.{symbol} 一致）
func (w *WebSocketManager) handlePriceMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}

	topic, ok := msg["topic"].(string)
	if !ok || !strings.HasPrefix(topic, "tickers.") {
		return
	}
	want := "tickers." + w.priceTickerKey
	if topic != want && !strings.EqualFold(topic, want) {
		return
	}

	data, ok := msg["data"].(map[string]interface{})
	if !ok {
		return
	}
	lastPriceStr, ok := data["lastPrice"].(string)
	if !ok {
		return
	}
	price, err := strconv.ParseFloat(lastPriceStr, 64)
	if err != nil || price <= 0 {
		return
	}
	w.lastPrice.Store(price)
	if w.priceCallback != nil {
		w.priceCallback(price)
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
			pingMsg := map[string]interface{}{
				"op": bybitOpPing,
			}

			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn != nil {
				w.writeMu.Lock()
				err := conn.WriteJSON(pingMsg)
				w.writeMu.Unlock()
				if err != nil {
					logger.Warn("⚠️ [Bybit WebSocket] 发送 ping 失败: %v", err)
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

	logger.Info("🛑 [Bybit WebSocket] 已停止")
}
