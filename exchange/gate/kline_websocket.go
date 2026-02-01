package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// KlineWebSocketManager Gate.io K線WebSocket管理器
type KlineWebSocketManager struct {
	conn           *websocket.Conn
	mu             sync.RWMutex
	done           chan struct{}
	callback       func(candle interface{})
	symbols        []string
	interval       string
	reconnectDelay time.Duration
	pingInterval   time.Duration
	isRunning      bool
	settle         string // usdt 或 btc
	testnet        bool   // 是否使用測試網
}

// NewKlineWebSocketManager 創建K線WebSocket管理器
func NewKlineWebSocketManager(settle string, testnet bool) *KlineWebSocketManager {
	if settle == "" {
		settle = "usdt" // 預設 USDT 永续合約
	}
	return &KlineWebSocketManager{
		done:           make(chan struct{}),
		reconnectDelay: 5 * time.Second,
		pingInterval:   15 * time.Second,
		settle:         settle,
		testnet:        testnet,
	}
}

// Start 啟動K線流（带自动重连）
func (k *KlineWebSocketManager) Start(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	k.mu.Lock()
	if k.isRunning {
		k.mu.Unlock()
		return fmt.Errorf("K線流已在运行")
	}
	k.callback = callback
	k.symbols = symbols
	k.interval = interval
	k.isRunning = true
	k.mu.Unlock()

	// 啟动连接和重连协程
	go k.connectLoop(ctx)

	return nil
}

// connectLoop 连接循环（自动重连）
func (k *KlineWebSocketManager) connectLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("✅ [Gate K線] WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ [Gate K線] WebSocket已停止")
			return
		default:
		}

		// Gate.io WebSocket URL
		var wsURL string
		if k.testnet {
			wsURL = fmt.Sprintf("wss://fx-ws-testnet.gateio.ws/v4/ws/%s", k.settle)
			logger.Info("🌐 [Gate K線] 使用測試網 WebSocket: %s", wsURL)
		} else {
			wsURL = fmt.Sprintf("wss://fx-ws.gateio.ws/v4/ws/%s", k.settle)
		}

		logger.Info("🔗 [Gate K線] 正在连接 WebSocket...")

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Gate K線] WebSocket连接失败: %v，%v后重試", err, k.reconnectDelay)
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-ctx.Done():
				logger.Info("✅ [Gate K線] WebSocket已停止（上下文取消）")
				return
			case <-k.done:
				logger.Info("✅ [Gate K線] WebSocket已停止")
				return
			case <-time.After(k.reconnectDelay):
			}
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("✅ [Gate K線] WebSocket已连接")

		// 订阅K線
		if err := k.subscribe(k.symbols, k.interval); err != nil {
			logger.Error("❌ [Gate K線] 订阅失败: %v", err)
			conn.Close()
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-ctx.Done():
				logger.Info("✅ [Gate K線] WebSocket已停止（上下文取消）")
				return
			case <-k.done:
				logger.Info("✅ [Gate K線] WebSocket已停止")
				return
			case <-time.After(k.reconnectDelay):
			}
			continue
		}

		// 啟动ping协程
		go k.pingLoop(ctx, conn)

		// 啟动读取循环（阻塞直到连接断开）
		k.readLoop(ctx, conn)

		// 连接断开，清理並准备重连
		k.mu.Lock()
		if k.conn == conn {
			k.conn = nil
		}
		k.mu.Unlock()

		// 检查是否因為 context 取消而断开，如果是则直接退出
		select {
		case <-ctx.Done():
			logger.Info("✅ [Gate K線] WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ [Gate K線] WebSocket已停止")
			return
		default:
		}

		logger.Warn("⚠️ [Gate K線] WebSocket连接断开，%v后重连...", k.reconnectDelay)
		// 使用 select 等待，可以立即响应 context 取消
		select {
		case <-ctx.Done():
			logger.Info("✅ [Gate K線] WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ [Gate K線] WebSocket已停止")
			return
		case <-time.After(k.reconnectDelay):
		}
	}
}

// subscribe 订阅K線
func (k *KlineWebSocketManager) subscribe(symbols []string, interval string) error {
	// Gate.io K線订阅格式: 每個交易對單独订阅
	// {"time": 1234567890, "channel": "futures.candlesticks", "event": "subscribe", "payload": ["1m", "BTC_USDT"]}

	k.mu.RLock()
	conn := k.conn
	k.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("连接未建立")
	}

	// 每個交易對單独订阅
	for _, symbol := range symbols {
		gateSymbol := convertToGateSymbol(symbol)

		subMsg := map[string]interface{}{
			"time":    time.Now().Unix(),
			"channel": "futures.candlesticks",
			"event":   "subscribe",
			"payload": []string{interval, gateSymbol},
		}

		if err := conn.WriteJSON(subMsg); err != nil {
			return fmt.Errorf("订阅 %s 失败: %w", symbol, err)
		}

		// 避免发送太快
		time.Sleep(100 * time.Millisecond)
	}

	logger.Info("✅ [Gate K線] 已订阅: %v, 周期: %s", symbols, interval)
	return nil
}

// pingLoop 定期发送 ping
func (k *KlineWebSocketManager) pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(k.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.done:
			return
		case <-ticker.C:
			k.mu.RLock()
			currentConn := k.conn
			k.mu.RUnlock()

			if currentConn != conn {
				return // 连接已更换，退出
			}

			// Gate.io K線 WebSocket 不需要客戶端发送 ping
			// 服務器會自动管理连接保活
			// 我们只需要保持 ticker 用於检测连接是否有效
			continue
		}
	}
}

// readLoop 读取消息循环
func (k *KlineWebSocketManager) readLoop(ctx context.Context, conn *websocket.Conn) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-k.done:
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			logger.Warn("⚠️ [Gate K線] 读取消息失败: %v", err)
			return
		}

		k.handleMessage(message)
	}
}

// handleMessage 处理WebSocket消息
func (k *KlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		logger.Warn("⚠️ [Gate K線] 解析消息失败: %v", err)
		return
	}

	// 調試：打印原始消息
	logger.Debug("[Gate K線] 收到消息: %s", string(message))

	// 检查錯误
	if errObj, ok := msg["error"].(map[string]interface{}); ok {
		logger.Error("❌ [Gate K線] 錯误: %v", errObj)
		return
	}

	// 处理不同類型的消息
	event, _ := msg["event"].(string)
	channel, _ := msg["channel"].(string)

	switch event {
	case "subscribe":
		// 订阅确认
		if result, ok := msg["result"].(map[string]interface{}); ok {
			if status, _ := result["status"].(string); status == "success" {
				// 尝試從payload中提取订阅的交易對信息
				subInfo := ""
				if payload, ok := msg["payload"].([]interface{}); ok && len(payload) >= 2 {
					if symbol, ok := payload[1].(string); ok {
						subInfo = fmt.Sprintf(" [%s]", convertFromGateSymbol(symbol))
					}
				}
				logger.Info("✅ [Gate K線] 订阅成功%s", subInfo)
			}
		}

	case "update":
		// K線數據更新
		if channel == "futures.candlesticks" {
			k.handleCandleUpdate(msg)
		}

	case "pong":
		// Pong 响应（静默处理）

	default:
		// 空事件可能是正常的update消息，检查channel
		if channel == "futures.candlesticks" {
			k.handleCandleUpdate(msg)
		} else if event != "" {
			// 有事件但不认识才打印
			logger.Info("[Gate K線] 未知事件: %s, channel: %s", event, channel)
		}
	}
}

// handleCandleUpdate 处理K線更新
func (k *KlineWebSocketManager) handleCandleUpdate(msg map[string]interface{}) {
	// Gate.io返回的result是數组: result: [{"t": ..., "o": ..., "n": "1m_ETH_USDT", ...}]
	resultArray, ok := msg["result"].([]interface{})
	if !ok || len(resultArray) == 0 {
		logger.Warn("⚠️ [Gate K線] result字段不是數组或為空")
		return
	}

	// 取第一個元素
	result, ok := resultArray[0].(map[string]interface{})
	if !ok {
		logger.Warn("⚠️ [Gate K線] result[0]不是對象")
		return
	}

	// Gate.io K線數據格式:
	// {"t": 1765624080, "o": 3122.03, "h": 3122.32, "l": 3121.21, "c": 3121.5, "v": 90265, "n": "1m_ETH_USDT", "w": false}
	// n字段格式是 "1m_ETH_USDT"，包含了周期信息
	nameField, ok := result["n"].(string)
	if !ok {
		logger.Warn("⚠️ [Gate K線] 交易對字段 n 不存在或類型錯误")
		return
	}

	// 從 "1m_ETH_USDT" 中提取交易對 "ETH_USDT"
	// 格式是: {interval}_{symbol}
	parts := splitAfterFirst(nameField, "_")
	if len(parts) < 2 {
		logger.Warn("⚠️ [Gate K線] n字段格式錯误: %s", nameField)
		return
	}
	gateSymbol := parts[1] // "ETH_USDT"
	symbol := convertFromGateSymbol(gateSymbol)

	timestamp, _ := result["t"].(float64)
	open, _ := parseFloat(result["o"])
	high, _ := parseFloat(result["h"])
	low, _ := parseFloat(result["l"])
	close, _ := parseFloat(result["c"])
	volume, _ := parseFloat(result["v"]) // 判断K線是否完結：根據時间戳和K線间隔判断
	// timestamp 是K線的开始時间（秒级）
	klineStartTime := int64(timestamp)

	// 计算K線间隔（秒）
	var intervalSeconds int64
	switch k.interval {
	case "1m":
		intervalSeconds = 60
	case "5m":
		intervalSeconds = 300
	case "15m":
		intervalSeconds = 900
	case "1h":
		intervalSeconds = 3600
	default:
		intervalSeconds = 60 // 默认1分钟
	}

	// K線結束時间 = 开始時间 + 间隔
	klineEndTime := klineStartTime + intervalSeconds
	currentTime := time.Now().Unix()

	// 如果當前時间已過K線結束時间，则认為已完結
	isClosed := currentTime >= klineEndTime

	candle := &Candle{
		Symbol:    symbol,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     close,
		Volume:    volume,
		Timestamp: klineStartTime,
		IsClosed:  isClosed,
	}

	k.mu.RLock()
	callback := k.callback
	k.mu.RUnlock()

	if callback != nil {
		callback(candle)
	}
}

// Stop 停止K線流
func (k *KlineWebSocketManager) Stop() {
	k.mu.Lock()
	defer k.mu.Unlock()

	if !k.isRunning {
		return
	}

	k.isRunning = false
	close(k.done)

	if k.conn != nil {
		k.conn.Close()
		k.conn = nil
	}
}

// convertToGateSymbol 轉换交易對格式為 Gate.io 格式
// 例如: BTCUSDT -> BTC_USDT
func convertToGateSymbol(symbol string) string {
	// 如果已經包含下划線，直接返回
	if strings.Contains(symbol, "_") {
		return symbol
	}
	// 简單實現：在倒數第4個字符前插入下划線（假設都是 XXX_USDT 格式）
	if len(symbol) > 4 && symbol[len(symbol)-4:] == "USDT" {
		return symbol[:len(symbol)-4] + "_" + symbol[len(symbol)-4:]
	}
	return symbol
}

// convertFromGateSymbol 從 Gate.io 格式轉换回標准格式
// 例如: BTC_USDT -> BTCUSDT
func convertFromGateSymbol(symbol string) string {
	// 移除下划線
	result := ""
	for _, c := range symbol {
		if c != '_' {
			result += string(c)
		}
	}
	return result
}

// parseFloat 解析浮点數（支援字符串和數字）
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(val, 64)
	default:
		return 0, fmt.Errorf("無法解析為浮点數: %v", v)
	}
}

// splitAfterFirst 從第一個分隔符后分割字符串
// 例如: "1m_ETH_USDT" -> ["1m", "ETH_USDT"]
func splitAfterFirst(s string, sep string) []string {
	idx := 0
	for i := 0; i < len(s); i++ {
		if s[i:i+len(sep)] == sep {
			idx = i
			break
		}
	}
	if idx == 0 {
		return []string{s}
	}
	return []string{s[:idx], s[idx+len(sep):]}
}
