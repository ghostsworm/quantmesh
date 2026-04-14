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

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	apiKey     string
	secretKey  string
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	orderCallback func(OrderUpdate)
	priceCallback func(float64)

	// 公共行情（tickers）單獨連線；與 OKX 相同需可取消、可重連，避免断線後 last 卡死
	muPrice        sync.Mutex
	priceTickerKey string // 如 BTCUSDT，校驗 topic tickers.{symbol}
	priceRunCancel context.CancelFunc
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(apiKey, secretKey string, useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		apiKey:     apiKey,
		secretKey:  secretKey,
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// sign 生成签名
func (w *WebSocketManager) sign(expires string) string {
	message := "GET/realtime" + expires
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// Start 啟動訂單流
func (w *WebSocketManager) Start(ctx context.Context, symbol string, callback func(OrderUpdate)) error {
	if w.isRunning.Load() {
		return fmt.Errorf("WebSocket 已在运行")
	}

	w.orderCallback = callback

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
	if err := w.auth(); err != nil {
		conn.Close()
		return fmt.Errorf("WebSocket 认证失败: %w", err)
	}

	// 订阅订單频道
	if err := w.subscribeOrders(); err != nil {
		conn.Close()
		return fmt.Errorf("订阅订單频道失败: %w", err)
	}

	// 啟动消息处理
	go w.readMessages()
	go w.keepAlive()

	logger.Info("✅ [Bybit WebSocket] 訂單流已啟动")
	return nil
}

// auth 认证
func (w *WebSocketManager) auth() error {
	expires := strconv.FormatInt(time.Now().Add(10*time.Second).UnixMilli(), 10)
	signature := w.sign(expires)

	authMsg := map[string]interface{}{
		"op": "auth",
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
		"op": "subscribe",
		"args": []string{
			"order",
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
	defer w.mu.RUnlock()

	if w.conn == nil {
		return fmt.Errorf("WebSocket 未连接")
	}

	return w.conn.WriteJSON(msg)
}

// readMessages 读取消息
func (w *WebSocketManager) readMessages() {
	defer func() {
		w.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [Bybit WebSocket] 消息处理 panic: %v", r)
		}
	}()

	for w.isRunning.Load() {
		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()

		if conn == nil {
			break
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if w.isRunning.Load() {
				logger.Warn("⚠️ [Bybit WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		w.handleMessage(message)
	}
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
		if op == "auth" {
			if success, ok := msg["success"].(bool); ok && success {
				logger.Info("✅ [Bybit WebSocket] 认证成功")
			} else {
				logger.Error("❌ [Bybit WebSocket] 认证失败: %v", msg["ret_msg"])
			}
		} else if op == "subscribe" {
			logger.Info("✅ [Bybit WebSocket] 订阅成功")
		}
		return
	}

	// 处理订單數據
	if topic, ok := msg["topic"].(string); ok && topic == "order" {
		w.handleOrderUpdate(msg)
	}
}

// handleOrderUpdate 处理订單更新
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

		orderId, _ := strconv.ParseInt(getString(orderData, "orderId"), 10, 64)
		price, _ := strconv.ParseFloat(getString(orderData, "price"), 64)
		quantity, _ := strconv.ParseFloat(getString(orderData, "qty"), 64)
		executedQty, _ := strconv.ParseFloat(getString(orderData, "cumExecQty"), 64)
		avgPrice, _ := strconv.ParseFloat(getString(orderData, "avgPrice"), 64)
		updateTime, _ := strconv.ParseInt(getString(orderData, "updatedTime"), 10, 64)

		side := getString(orderData, "side")
		var orderSide Side
		if side == "Buy" {
			orderSide = SideBuy
		} else {
			orderSide = SideSell
		}

		// 🔥 解析已實現盈虧（Bybit 返回 closedPnl 字段，僅平倉訂單有效）
		realizedPnL, _ := strconv.ParseFloat(getString(orderData, "closedPnl"), 64)

		// Bybit WebSocket 訂單更新消息中通常不包含手續費，需要從交易歷史獲取
		// 這裡先設為 0，後續可通過查詢交易歷史補充
		update := OrderUpdate{
			OrderID:         orderId,
			ClientOrderID:   getString(orderData, "orderLinkId"),
			Symbol:          getString(orderData, "symbol"),
			Side:            orderSide,
			Type:            OrderType(getString(orderData, "orderType")),
			Status:          OrderStatus(getString(orderData, "orderStatus")),
			Price:           price,
			Quantity:        quantity,
			ExecutedQty:     executedQty,
			AvgPrice:        avgPrice,
			UpdateTime:      updateTime,
			Commission:      0, // Bybit WebSocket 不提供手續費，需從交易歷史查詢
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

// keepAlive 保持连接
func (w *WebSocketManager) keepAlive() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !w.isRunning.Load() {
				return
			}

			pingMsg := map[string]interface{}{
				"op": "ping",
			}

			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()

			if conn != nil {
				if err := conn.WriteJSON(pingMsg); err != nil {
					logger.Warn("⚠️ [Bybit WebSocket] 发送 ping 失败: %v", err)
				}
			}

		case <-w.stopChan:
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

// Stop 停止 WebSocket
func (w *WebSocketManager) Stop() {
	w.muPrice.Lock()
	if w.priceRunCancel != nil {
		w.priceRunCancel()
		w.priceRunCancel = nil
	}
	w.muPrice.Unlock()

	if !w.isRunning.Load() {
		return
	}

	w.isRunning.Store(false)
	close(w.stopChan)

	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()

	logger.Info("🛑 [Bybit WebSocket] 已停止")
}
