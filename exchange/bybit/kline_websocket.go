package bybit

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

// KlineWebSocketManager K線 WebSocket 管理器
type KlineWebSocketManager struct {
	useTestnet    bool
	useSpotPublic bool // true 時使用 v5/public/spot 與現貨 kline 間隔格式
	conn          *websocket.Conn
	mu            sync.RWMutex
	writeMu       sync.Mutex // 串行化 conn 的寫操作，避免 gorilla/websocket 並發寫競態
	stopChan      chan struct{}
	isRunning     atomic.Bool
	callback      CandleUpdateCallback
}

// NewKlineWebSocketManager 創建 K線 WebSocket 管理器（合約 linear）
func NewKlineWebSocketManager(useTestnet bool) *KlineWebSocketManager {
	return &KlineWebSocketManager{
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// NewSpotKlineWebSocketManager 創建現貨 K 線 WebSocket（v5/public/spot）
func NewSpotKlineWebSocketManager(useTestnet bool) *KlineWebSocketManager {
	return &KlineWebSocketManager{
		useTestnet:    useTestnet,
		useSpotPublic: true,
		stopChan:      make(chan struct{}),
	}
}

// Start 啟动 K線流
func (k *KlineWebSocketManager) Start(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if k.isRunning.Load() {
		return fmt.Errorf("K線 WebSocket 已在运行")
	}

	k.callback = callback

	// 连接公共 WebSocket
	var wsURL string
	if k.useSpotPublic {
		wsURL = PublicSpotWsURL
		if k.useTestnet {
			wsURL = PublicSpotTestnetWsURL
		}
	} else {
		wsURL = PublicWsURL
		if k.useTestnet {
			wsURL = PublicTestnetWsURL
		}
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 K線 WebSocket 失败: %w", err)
	}

	k.mu.Lock()
	k.conn = conn
	k.mu.Unlock()

	k.isRunning.Store(true)

	// 订阅 K線频道
	if err := k.subscribeKlines(symbols, interval); err != nil {
		conn.Close()
		return fmt.Errorf("订阅 K線频道失败: %w", err)
	}

	// 啟动消息处理
	go k.readMessages()
	go k.keepAlive()

	if k.useSpotPublic {
		logger.Info("✅ [Bybit Spot K線 WebSocket] 已啟动，订阅 %d 個交易對", len(symbols))
	} else {
		logger.Info("✅ [Bybit K線 WebSocket] 已啟动，订阅 %d 個交易對", len(symbols))
	}
	return nil
}

// intervalToBybitSpot 將 1m/1h 等轉為 Bybit 現貨 K 線間隔（1,3,60,D…）
func intervalToBybitSpot(interval string) string {
	switch interval {
	case "1m", "1":
		return "1"
	case "3m", "3":
		return "3"
	case "5m", "5":
		return "5"
	case "15m", "15":
		return "15"
	case "30m", "30":
		return "30"
	case "1h", "60m", "60":
		return "60"
	case "2h", "120m", "120":
		return "120"
	case "4h", "240m", "240":
		return "240"
	case "6h", "360":
		return "360"
	case "12h", "720":
		return "720"
	case "1d", "1D", "d", "D":
		return "D"
	case "1w", "1W", "w", "W":
		return "W"
	case "1M", "M":
		return "M"
	default:
		return "1"
	}
}

// subscribeKlines 订阅 K線频道
func (k *KlineWebSocketManager) subscribeKlines(symbols []string, interval string) error {
	args := make([]string, len(symbols))
	for i, symbol := range symbols {
		iv := interval
		if k.useSpotPublic {
			iv = intervalToBybitSpot(interval)
		}
		args[i] = fmt.Sprintf("kline.%s.%s", iv, symbol)
	}

	subMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}

	k.mu.RLock()
	conn := k.conn
	k.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("WebSocket 未连接")
	}

	k.writeMu.Lock()
	defer k.writeMu.Unlock()
	return conn.WriteJSON(subMsg)
}

// readMessages 读取消息
func (k *KlineWebSocketManager) readMessages() {
	defer func() {
		k.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [Bybit K線 WebSocket] 消息处理 panic: %v", r)
		}
	}()

	for k.isRunning.Load() {
		k.mu.RLock()
		conn := k.conn
		k.mu.RUnlock()

		if conn == nil {
			break
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if k.isRunning.Load() {
				logger.Warn("⚠️ [Bybit K線 WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Bybit K線 WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查操作類型
	if op, ok := msg["op"].(string); ok {
		if op == "subscribe" {
			logger.Info("✅ [Bybit K線 WebSocket] 订阅成功")
		}
		return
	}

	// 处理 K線數據
	if topic, ok := msg["topic"].(string); ok {
		if len(topic) > 5 && topic[:5] == "kline" {
			k.handleKlineUpdate(msg)
		}
	}
}

// handleKlineUpdate 处理 K線更新
func (k *KlineWebSocketManager) handleKlineUpdate(msg map[string]interface{}) {
	data, ok := msg["data"].([]interface{})
	if !ok || len(data) == 0 {
		return
	}

	for _, item := range data {
		klineData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}

		timestamp, _ := strconv.ParseInt(getString(klineData, "start"), 10, 64)
		open, _ := strconv.ParseFloat(getString(klineData, "open"), 64)
		high, _ := strconv.ParseFloat(getString(klineData, "high"), 64)
		low, _ := strconv.ParseFloat(getString(klineData, "low"), 64)
		close, _ := strconv.ParseFloat(getString(klineData, "close"), 64)
		volume, _ := strconv.ParseFloat(getString(klineData, "volume"), 64)

		// 判断是否已完結
		confirm, _ := klineData["confirm"].(bool)

		candle := Candle{
			Symbol:    getString(klineData, "symbol"),
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: timestamp,
			IsClosed:  confirm,
		}

		if k.callback != nil {
			k.callback(candle)
		}
	}
}

// keepAlive 保持连接
func (k *KlineWebSocketManager) keepAlive() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if !k.isRunning.Load() {
				return
			}

			pingMsg := map[string]interface{}{
				"op": "ping",
			}

			k.mu.RLock()
			conn := k.conn
			k.mu.RUnlock()

			if conn != nil {
				k.writeMu.Lock()
				err := conn.WriteJSON(pingMsg)
				k.writeMu.Unlock()
				if err != nil {
					logger.Warn("⚠️ [Bybit K線 WebSocket] 发送 ping 失败: %v", err)
				}
			}

		case <-k.stopChan:
			return
		}
	}
}

// Stop 停止 K線 WebSocket
func (k *KlineWebSocketManager) Stop() {
	if !k.isRunning.Load() {
		return
	}

	k.isRunning.Store(false)
	close(k.stopChan)

	k.mu.Lock()
	if k.conn != nil {
		k.conn.Close()
		k.conn = nil
	}
	k.mu.Unlock()

	logger.Info("🛑 [Bybit K線 WebSocket] 已停止")
}
