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

// KlineWebSocketManager K线 WebSocket 管理器
type KlineWebSocketManager struct {
	useTestnet bool
	conn       *websocket.Conn
	mu         sync.RWMutex
	stopChan   chan struct{}
	isRunning  atomic.Bool
	callback   CandleUpdateCallback
}

// NewKlineWebSocketManager 创建 K线 WebSocket 管理器
func NewKlineWebSocketManager(useTestnet bool) *KlineWebSocketManager {
	return &KlineWebSocketManager{
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// Start 启动 K线流
func (k *KlineWebSocketManager) Start(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if k.isRunning.Load() {
		return fmt.Errorf("K线 WebSocket 已在运行")
	}

	k.callback = callback

	// 连接公共 WebSocket
	wsURL := PublicWsURL
	if k.useTestnet {
		wsURL = PublicTestnetWsURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 K线 WebSocket 失败: %w", err)
	}

	k.mu.Lock()
	k.conn = conn
	k.mu.Unlock()

	k.isRunning.Store(true)

	// 订阅 K线频道
	if err := k.subscribeKlines(symbols, interval); err != nil {
		conn.Close()
		return fmt.Errorf("订阅 K线频道失败: %w", err)
	}

	// 启动消息处理
	go k.readMessages()
	go k.keepAlive()

	logger.Info("✅ [Bybit K线 WebSocket] 已启动，订阅 %d 个交易对", len(symbols))
	return nil
}

// subscribeKlines 订阅 K线频道
func (k *KlineWebSocketManager) subscribeKlines(symbols []string, interval string) error {
	args := make([]string, len(symbols))
	for i, symbol := range symbols {
		args[i] = fmt.Sprintf("kline.%s.%s", interval, symbol)
	}

	subMsg := map[string]interface{}{
		"op":   "subscribe",
		"args": args,
	}

	k.mu.RLock()
	defer k.mu.RUnlock()

	if k.conn == nil {
		return fmt.Errorf("WebSocket 未连接")
	}

	return k.conn.WriteJSON(subMsg)
}

// readMessages 读取消息
func (k *KlineWebSocketManager) readMessages() {
	defer func() {
		k.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [Bybit K线 WebSocket] 消息处理 panic: %v", r)
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
				logger.Warn("⚠️ [Bybit K线 WebSocket] 读取消息失败: %v", err)
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
		logger.Warn("⚠️ [Bybit K线 WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查操作类型
	if op, ok := msg["op"].(string); ok {
		if op == "subscribe" {
			logger.Info("✅ [Bybit K线 WebSocket] 订阅成功")
		}
		return
	}

	// 处理 K线数据
	if topic, ok := msg["topic"].(string); ok {
		if len(topic) > 5 && topic[:5] == "kline" {
			k.handleKlineUpdate(msg)
		}
	}
}

// handleKlineUpdate 处理 K线更新
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

		// 判断是否已完结
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
				if err := conn.WriteJSON(pingMsg); err != nil {
					logger.Warn("⚠️ [Bybit K线 WebSocket] 发送 ping 失败: %v", err)
				}
			}

		case <-k.stopChan:
			return
		}
	}
}

// Stop 停止 K线 WebSocket
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

	logger.Info("🛑 [Bybit K线 WebSocket] 已停止")
}
