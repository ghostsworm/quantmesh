package bitkub

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	// WebSocket 地址
	MainnetWsURL = "wss://api.bitkub.com/websocket-api"
	TestnetWsURL = "wss://api.bitkub.com/websocket-api" // Bitkub没有测试网
)

// WebSocketManager WebSocket 管理器
type WebSocketManager struct {
	useTestnet bool

	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	isRunning     atomic.Bool
	lastPrice     atomic.Value
	priceCallback func(float64)
}

// NewWebSocketManager 創建 WebSocket 管理器
func NewWebSocketManager(useTestnet bool) *WebSocketManager {
	return &WebSocketManager{
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// StartPriceStream 啟動價格流
func (w *WebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	w.priceCallback = callback

	wsURL := MainnetWsURL
	if w.useTestnet {
		wsURL = TestnetWsURL
	}

	// Bitkub WebSocket格式: wss://api.bitkub.com/websocket-api/market.ticker.{symbol}
	// symbol需要转换为小写，格式如: btc_thb
	symbolLower := strings.ToLower(symbol)
	streamName := fmt.Sprintf("market.ticker.%s", symbolLower)
	fullURL := fmt.Sprintf("%s/%s", wsURL, streamName)

	conn, _, err := websocket.DefaultDialer.Dial(fullURL, nil)
	if err != nil {
		return fmt.Errorf("连接價格流 WebSocket 失败: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()

	w.isRunning.Store(true)

	// 启动消息处理
	go w.readMessages(ctx)

	logger.Info("✅ [Bitkub WebSocket] 價格流已啟动: %s", streamName)
	return nil
}

// readMessages 读取消息
func (w *WebSocketManager) readMessages(ctx context.Context) {
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
		case <-ctx.Done():
			logger.Info("✅ [Bitkub WebSocket] 價格流已停止（上下文取消）")
			return
		case <-w.stopChan:
			logger.Info("✅ [Bitkub WebSocket] 價格流已停止（手動停止）")
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

			// 解析消息
			var tickerMsg struct {
				Stream      string  `json:"stream"`
				ID          int     `json:"id"`
				Last        float64 `json:"last"`
				LowestAsk   float64 `json:"lowestAsk"`
				LowestAskSize float64 `json:"lowestAskSize"`
				HighestBid  float64 `json:"highestBid"`
				HighestBidSize float64 `json:"highestBidSize"`
				Change      float64 `json:"change"`
				PercentChange float64 `json:"percentChange"`
				BaseVolume  float64 `json:"baseVolume"`
				QuoteVolume float64 `json:"quoteVolume"`
				IsFrozen    int     `json:"isFrozen"`
				High24Hr    float64 `json:"high24hr"`
				Low24Hr     float64 `json:"low24hr"`
				Open        float64 `json:"open"`
				Close       float64 `json:"close"`
			}

			if err := json.Unmarshal(message, &tickerMsg); err != nil {
				logger.Debug("[Bitkub WebSocket] 解析消息失败: %v", err)
				continue
			}

			// 处理价格更新
			if w.priceCallback != nil && tickerMsg.Last > 0 {
				w.priceCallback(tickerMsg.Last)
				w.lastPrice.Store(tickerMsg.Last)
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
	logger.Info("✅ [Bitkub WebSocket] 已停止")
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
