package okx

import (
	"bytes"
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
	useTestnet bool
	conn       *websocket.Conn
	mu         sync.RWMutex
	writeMu    sync.Mutex // 串行化 conn 的寫操作，避免 gorilla/websocket 並發寫競態
	stopChan   chan struct{}
	isRunning  atomic.Bool
	callback   CandleUpdateCallback
}

// NewKlineWebSocketManager 創建 K線 WebSocket 管理器
func NewKlineWebSocketManager(useTestnet bool) *KlineWebSocketManager {
	return &KlineWebSocketManager{
		useTestnet: useTestnet,
		stopChan:   make(chan struct{}),
	}
}

// Start 啟动 K線流
func (k *KlineWebSocketManager) Start(ctx context.Context, instIds []string, interval string, callback CandleUpdateCallback) error {
	if k.isRunning.Load() {
		return fmt.Errorf("K線 WebSocket 已在运行")
	}

	k.callback = callback

	// 连接公共 WebSocket（根據是否使用測試網选擇不同的地址）
	publicWsURL := MainnetPublicWsURL
	if k.useTestnet {
		publicWsURL = TestnetPublicWsURL
	}
	conn, _, err := websocket.DefaultDialer.Dial(publicWsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 K線 WebSocket 失败: %w", err)
	}

	k.mu.Lock()
	k.conn = conn
	k.mu.Unlock()

	k.isRunning.Store(true)

	// 订阅 K線频道
	if err := k.subscribeKlines(instIds, interval); err != nil {
		conn.Close()
		return fmt.Errorf("订阅 K線频道失败: %w", err)
	}

	// 啟动消息处理
	go k.readMessages()
	go k.keepAlive()

	logger.Info("✅ [OKX K線 WebSocket] 已啟动，订阅 %d 個交易對", len(instIds))
	return nil
}

// subscribeKlines 订阅 K線频道
func (k *KlineWebSocketManager) subscribeKlines(instIds []string, interval string) error {
	// 轉换時间周期格式
	bar := convertInterval(interval)

	args := make([]map[string]string, len(instIds))
	for i, instId := range instIds {
		args[i] = map[string]string{
			"channel": fmt.Sprintf("candle%s", bar),
			"instId":  instId,
		}
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

// convertInterval 轉换時间周期格式
// 1m -> 1m, 5m -> 5m, 1h -> 1H, 1d -> 1D
func convertInterval(interval string) string {
	switch interval {
	case "1m":
		return "1m"
	case "3m":
		return "3m"
	case "5m":
		return "5m"
	case "15m":
		return "15m"
	case "30m":
		return "30m"
	case "1h":
		return "1H"
	case "2h":
		return "2H"
	case "4h":
		return "4H"
	case "6h":
		return "6H"
	case "12h":
		return "12H"
	case "1d":
		return "1D"
	case "1w":
		return "1W"
	case "1M":
		return "1M"
	default:
		return "1m"
	}
}

// readMessages 读取消息
func (k *KlineWebSocketManager) readMessages() {
	defer func() {
		k.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [OKX K線 WebSocket] 消息处理 panic: %v", r)
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
				logger.Warn("⚠️ [OKX K線 WebSocket] 读取消息失败: %v", err)
			}
			break
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	trim := bytes.TrimSpace(message)
	if len(trim) == 0 || (trim[0] != '{' && trim[0] != '[') {
		// 服務端可能下發純文本 ping/pong，非 JSON，避免「invalid character 'p'」告警
		return
	}
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [OKX K線 WebSocket] 解析消息失败: %v", err)
		return
	}

	// 检查事件類型
	if event, ok := msg["event"].(string); ok {
		if event == "subscribe" {
			logger.Info("✅ [OKX K線 WebSocket] 订阅成功")
		} else if event == "error" {
			logger.Error("❌ [OKX K線 WebSocket] 錯误: %v", msg["msg"])
		}
		return
	}

	// 处理 K線數據
	if arg, ok := msg["arg"].(map[string]interface{}); ok {
		if channel, ok := arg["channel"].(string); ok {
			if len(channel) > 6 && channel[:6] == "candle" {
				k.handleKlineUpdate(msg)
			}
		}
	}
}

// handleKlineUpdate 处理 K線更新
func (k *KlineWebSocketManager) handleKlineUpdate(msg map[string]interface{}) {
	data, ok := msg["data"].([]interface{})
	if !ok || len(data) == 0 {
		return
	}

	arg, ok := msg["arg"].(map[string]interface{})
	if !ok {
		return
	}

	instId := getString(arg, "instId")

	for _, item := range data {
		klineData, ok := item.([]interface{})
		if !ok || len(klineData) < 7 {
			continue
		}

		timestamp, _ := strconv.ParseInt(fmt.Sprintf("%v", klineData[0]), 10, 64)
		open, _ := strconv.ParseFloat(fmt.Sprintf("%v", klineData[1]), 64)
		high, _ := strconv.ParseFloat(fmt.Sprintf("%v", klineData[2]), 64)
		low, _ := strconv.ParseFloat(fmt.Sprintf("%v", klineData[3]), 64)
		close, _ := strconv.ParseFloat(fmt.Sprintf("%v", klineData[4]), 64)
		volume, _ := strconv.ParseFloat(fmt.Sprintf("%v", klineData[5]), 64)

		// 判断是否已完結（OKX 的 K線數據中第8個字段表示是否确认）
		isClosed := true
		if len(klineData) >= 9 {
			if confirm, ok := klineData[8].(string); ok {
				isClosed = (confirm == "1")
			}
		}

		candle := Candle{
			Symbol:    instId,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: timestamp,
			IsClosed:  isClosed,
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

			pingMsg := "ping"
			k.mu.RLock()
			conn := k.conn
			k.mu.RUnlock()

			if conn != nil {
				k.writeMu.Lock()
				err := conn.WriteMessage(websocket.TextMessage, []byte(pingMsg))
				k.writeMu.Unlock()
				if err != nil {
					logger.Warn("⚠️ [OKX K線 WebSocket] 发送 ping 失败: %v", err)
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

	logger.Info("🛑 [OKX K線 WebSocket] 已停止")
}
