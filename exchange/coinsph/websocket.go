package coinsph

import (
	"context"
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
	MainnetWsURL = "wss://wsapi.pro.coins.ph"
	TestnetWsURL = "wss://wsapi.pro.coins.ph" // Coins.ph没有测试网
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	priceMu       sync.Mutex
	priceCancel   context.CancelFunc
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	priceCallback func(float64)
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		useTestnet: useTestnet,
	}
}

// sendMessage 发送WebSocket消息
func (w *WebSocketManager) sendMessage(msg map[string]interface{}) error {
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

// StartPriceStream 啟動價格流（断線自動重連；重複調用會取消上一路）
func (w *WebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	w.priceMu.Lock()
	if w.priceCancel != nil {
		w.priceCancel()
		w.priceCancel = nil
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.priceCancel = cancel
	w.priceMu.Unlock()

	w.priceCallback = callback

	go w.runPriceLoop(runCtx, symbol)

	logger.Info("✅ [Coins.ph WebSocket] 價格流已啟动（断線重連）: %s", symbol)
	return nil
}

func (w *WebSocketManager) runPriceLoop(ctx context.Context, symbol string) {
	wsURL := MainnetWsURL
	if w.useTestnet {
		wsURL = TestnetWsURL
	}
	symbolLower := strings.ToLower(symbol)
	streamName := fmt.Sprintf("%s@miniTicker", symbolLower)
	fullURL := fmt.Sprintf("%s/openapi/quote/ws/v3/%s", wsURL, streamName)

	backoff := 2 * time.Second
	for {
		select {
		case <-ctx.Done():
			logger.Info("✅ [Coins.ph WebSocket] 價格流已停止（上下文取消）")
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(fullURL, nil)
		if err != nil {
			logger.Error("[Coins.ph WebSocket] 連接失敗: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			continue
		}

		w.mu.Lock()
		w.conn = conn
		w.mu.Unlock()
		w.isRunning.Store(true)

		hbDone := make(chan struct{})
		go w.keepAliveConnection(ctx, hbDone)

		w.readLoop(ctx)

		close(hbDone)
		w.mu.Lock()
		if w.conn != nil {
			_ = w.conn.Close()
			w.conn = nil
		}
		w.mu.Unlock()
		w.isRunning.Store(false)

		if ctx.Err() != nil {
			return
		}
		logger.Warn("[Coins.ph WebSocket] 連接斷開，%v 後重連…", backoff)
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
	}
}

func (w *WebSocketManager) readLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()
		if conn == nil {
			return
		}

		conn.SetReadDeadline(time.Now().Add(60 * time.Second))

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Error("WebSocket 读取错误: %v", err)
			}
			return
		}

		var pongMsg struct {
			Pong int64 `json:"pong"`
		}
		if err := json.Unmarshal(message, &pongMsg); err == nil && pongMsg.Pong > 0 {
			continue
		}

		var tickerMsg struct {
			EventType string `json:"e"`
			EventTime int64  `json:"E"`
			Symbol    string `json:"s"`
			Close     string `json:"c"`
			Open      string `json:"o"`
			High      string `json:"h"`
			Low       string `json:"l"`
			Volume    string `json:"v"`
			QuoteVol  string `json:"q"`
		}

		if err := json.Unmarshal(message, &tickerMsg); err != nil {
			logger.Debug("[Coins.ph WebSocket] 解析消息失败: %v", err)
			continue
		}

		if w.priceCallback != nil && tickerMsg.Close != "" {
			price, err := strconv.ParseFloat(tickerMsg.Close, 64)
			if err == nil && price > 0 {
				w.priceCallback(price)
				w.lastPrice.Store(price)
			}
		}
	}
}

func (w *WebSocketManager) keepAliveConnection(ctx context.Context, hbDone <-chan struct{}) {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-hbDone:
			return
		case <-ticker.C:
			if !w.isRunning.Load() {
				return
			}
			pingMsg := map[string]interface{}{
				"ping": time.Now().UnixMilli(),
			}
			if err := w.sendMessage(pingMsg); err != nil {
				logger.Error("发送ping失败: %v", err)
				return
			}
		}
	}
}

// Stop 停止WebSocket连接
func (w *WebSocketManager) Stop() error {
	w.priceMu.Lock()
	if w.priceCancel != nil {
		w.priceCancel()
		w.priceCancel = nil
	}
	w.priceMu.Unlock()

	w.mu.Lock()
	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()

	w.isRunning.Store(false)
	logger.Info("✅ [Coins.ph WebSocket] 已停止")
	return nil
}

// GetLastPrice 獲取最後價格
func (w *WebSocketManager) GetLastPrice() float64 {
	if val := w.lastPrice.Load(); val != nil {
		if price, ok := val.(float64); ok {
			return price
		}
	}
	return 0
}
