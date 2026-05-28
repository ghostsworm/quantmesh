package huobi

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

const (
	PublicWsURL = "wss://api.hbdm.com/linear-swap-ws"
)

// KlineWebSocketManager K線 WebSocket 管理器
type KlineWebSocketManager struct {
	conn      *websocket.Conn
	mu        sync.RWMutex
	writeMu   sync.Mutex // 串行化 conn 的寫操作，避免 gorilla/websocket 並發寫競態
	stopChan  chan struct{}
	isRunning atomic.Bool
	callback  CandleUpdateCallback
}

// NewKlineWebSocketManager 創建 K線 WebSocket 管理器
func NewKlineWebSocketManager() *KlineWebSocketManager {
	return &KlineWebSocketManager{
		stopChan: make(chan struct{}),
	}
}

// Start 啟动 K線流
func (k *KlineWebSocketManager) Start(ctx context.Context, contractCodes []string, interval string, callback CandleUpdateCallback) error {
	if k.isRunning.Load() {
		return fmt.Errorf("K線 WebSocket 已在运行")
	}

	k.callback = callback

	conn, _, err := websocket.DefaultDialer.Dial(PublicWsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 K線 WebSocket 失败: %w", err)
	}

	k.mu.Lock()
	k.conn = conn
	k.mu.Unlock()

	k.isRunning.Store(true)

	// 订阅 K線频道
	if err := k.subscribeKlines(contractCodes, interval); err != nil {
		conn.Close()
		return fmt.Errorf("订阅 K線频道失败: %w", err)
	}

	go k.readMessages()
	go k.keepAlive()

	logger.Info("✅ [Huobi K線 WebSocket] 已啟动，订阅 %d 個交易對", len(contractCodes))
	return nil
}

// subscribeKlines 订阅 K線频道
func (k *KlineWebSocketManager) subscribeKlines(contractCodes []string, interval string) error {
	for _, contractCode := range contractCodes {
		subMsg := map[string]interface{}{
			"sub": fmt.Sprintf("market.%s.kline.%s", contractCode, interval),
			"id":  fmt.Sprintf("kline_%s", contractCode),
		}

		k.mu.RLock()
		conn := k.conn
		k.mu.RUnlock()
		if conn == nil {
			return fmt.Errorf("WebSocket 未连接")
		}
		k.writeMu.Lock()
		err := conn.WriteJSON(subMsg)
		k.writeMu.Unlock()

		if err != nil {
			return err
		}

		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

// readMessages 读取消息
func (k *KlineWebSocketManager) readMessages() {
	defer func() {
		k.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [Huobi K線 WebSocket] 消息处理 panic: %v", r)
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
				logger.Warn("⚠️ [Huobi K線 WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		// 解压 gzip
		decompressed, err := decompressGzip(message)
		if err != nil {
			logger.Warn("⚠️ [Huobi K線 WebSocket] 解压消息失败: %v", err)
			continue
		}

		k.handleMessage(decompressed)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Huobi K線 WebSocket] 解析消息失败: %v", err)
		return
	}

	// 处理 ping
	if ping, ok := msg["ping"].(float64); ok {
		pongMsg := map[string]interface{}{
			"pong": int64(ping),
		}
		k.mu.RLock()
		conn := k.conn
		k.mu.RUnlock()
		if conn != nil {
			k.writeMu.Lock()
			_ = conn.WriteJSON(pongMsg)
			k.writeMu.Unlock()
		}
		return
	}

	// 处理订阅响应
	if _, ok := msg["subbed"]; ok {
		logger.Info("✅ [Huobi K線 WebSocket] 订阅成功")
		return
	}

	// 处理 K線數據
	if ch, ok := msg["ch"].(string); ok {
		if len(ch) > 6 && ch[:6] == "market" {
			k.handleKlineUpdate(msg)
		}
	}
}

// handleKlineUpdate 处理 K線更新
func (k *KlineWebSocketManager) handleKlineUpdate(msg map[string]interface{}) {
	tick, ok := msg["tick"].(map[string]interface{})
	if !ok {
		return
	}

	ch, _ := msg["ch"].(string)

	timestamp, _ := strconv.ParseInt(getString(tick, "id"), 10, 64)
	open, _ := strconv.ParseFloat(getString(tick, "open"), 64)
	high, _ := strconv.ParseFloat(getString(tick, "high"), 64)
	low, _ := strconv.ParseFloat(getString(tick, "low"), 64)
	close, _ := strconv.ParseFloat(getString(tick, "close"), 64)
	vol, _ := strconv.ParseFloat(getString(tick, "vol"), 64)

	candle := Candle{
		Symbol:    ch,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    vol,
		Timestamp: timestamp * 1000,
		IsClosed:  true,
	}

	if k.callback != nil {
		k.callback(candle)
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
			// Huobi 使用 ping/pong 机制，在 handleMessage 中处理

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

	logger.Info("🛑 [Huobi K線 WebSocket] 已停止")
}
