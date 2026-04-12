package binance

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

// Candle K線數據
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool // K線是否完結
}

// KlineWebSocketManager Binance K線WebSocket管理器
type KlineWebSocketManager struct {
	conn           *websocket.Conn
	mu             sync.RWMutex
	done           chan struct{}
	callback       func(candle interface{})
	symbols        []string
	interval       string
	reconnectDelay time.Duration
	pingInterval   time.Duration
	pongWait       time.Duration
	isRunning      bool
	useTestnet     bool // 是否使用測試網
	spotMarket     bool // true=現貨 stream，false=合約 fstream
}

// NewKlineWebSocketManager 創建K線WebSocket管理器（合約）
func NewKlineWebSocketManager(useTestnet bool) *KlineWebSocketManager {
	return &KlineWebSocketManager{
		done:           make(chan struct{}),
		reconnectDelay: 5 * time.Second,  // 重连延迟
		pingInterval:   30 * time.Second, // Ping间隔
		pongWait:       60 * time.Second, // Pong等待超時
		useTestnet:     useTestnet,
		spotMarket:     false,
	}
}

// NewSpotKlineWebSocketManager 創建現貨 K 線 WebSocket（stream.binance.com）
func NewSpotKlineWebSocketManager(useTestnet bool) *KlineWebSocketManager {
	return &KlineWebSocketManager{
		done:           make(chan struct{}),
		reconnectDelay: 5 * time.Second,
		pingInterval:   30 * time.Second,
		pongWait:       60 * time.Second,
		useTestnet:     useTestnet,
		spotMarket:     true,
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
			logger.Info("✅ K線WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ K線WebSocket已停止")
			return
		default:
		}

		streams := make([]string, len(k.symbols))
		for i, symbol := range k.symbols {
			streams[i] = fmt.Sprintf("%s@kline_%s", strings.ToLower(symbol), k.interval)
		}
		var wsURL string
		if k.spotMarket {
			if k.useTestnet {
				wsURL = fmt.Sprintf("wss://stream.testnet.binance.vision/stream?streams=%s", strings.Join(streams, "/"))
				logger.Info("🌐 [Binance Spot Kline WS] 測試網")
			} else {
				wsURL = fmt.Sprintf("wss://stream.binance.com:443/stream?streams=%s", strings.Join(streams, "/"))
			}
			logger.Info("🔗 正在连接 Binance 現貨 K線 WebSocket...")
		} else {
			if k.useTestnet {
				wsURL = fmt.Sprintf("wss://stream.binancefuture.com/stream?streams=%s", strings.Join(streams, "/"))
				logger.Info("🌐 [Binance Kline WS] 使用測試網 WebSocket")
			} else {
				wsURL = fmt.Sprintf("wss://fstream.binance.com/stream?streams=%s", strings.Join(streams, "/"))
			}
			logger.Info("🔗 正在连接 Binance K線WebSocket...")
		}

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ K線WebSocket连接失败: %v，%v后重試", err, k.reconnectDelay)
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-ctx.Done():
				logger.Info("✅ K線WebSocket已停止（上下文取消）")
				return
			case <-k.done:
				logger.Info("✅ K線WebSocket已停止")
				return
			case <-time.After(k.reconnectDelay):
			}
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("✅ Binance K線WebSocket已连接")

		// 啟动心跳保活
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
			logger.Info("✅ K線WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ K線WebSocket已停止")
			return
		default:
		}

		logger.Warn("⚠️ K線WebSocket连接断开，%v后重连...", k.reconnectDelay)
		// 使用 select 等待，可以立即响应 context 取消
		select {
		case <-ctx.Done():
			logger.Info("✅ K線WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ K線WebSocket已停止")
			return
		case <-time.After(k.reconnectDelay):
		}
	}
}

// pingLoop 心跳保活循环
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

			// 检查连接是否还是當前连接
			if currentConn != conn {
				return
			}

			// 发送Ping
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				logger.Warn("⚠️ K線WebSocket发送Ping失败: %v", err)
				conn.Close()
				return
			}
			logger.Debug("💓 K線WebSocket Ping已发送")
		}
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

	logger.Info("✅ Binance K線WebSocket已停止")
}

// readLoop 读取消息循环
func (k *KlineWebSocketManager) readLoop(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("❌ K線WebSocket读取协程panic: %v", r)
		}
		conn.Close()
	}()

	// 設置Pong处理器
	conn.SetReadDeadline(time.Now().Add(k.pongWait))
	conn.SetPongHandler(func(string) error {
		logger.Debug("💓 K線WebSocket收到Pong")
		conn.SetReadDeadline(time.Now().Add(k.pongWait))
		return nil
	})

	for {
		select {
		case <-k.done:
			return
		case <-ctx.Done():
			return
		default:
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				logger.Warn("⚠️ K線WebSocket异常关闭: %v", err)
			} else {
				logger.Debug("K線WebSocket读取錯误: %v", err)
			}
			return
		}

		// 重置读取超時
		conn.SetReadDeadline(time.Now().Add(k.pongWait))

		// 首次收到消息時打印，确认WebSocket连接正常
		//logger.Debug("收到K線WebSocket原始消息: %s", string(message))

		// 解析消息
		var msg struct {
			Stream string `json:"stream"`
			Data   struct {
				EventType string `json:"e"` // 事件類型（"kline"）
				EventTime int64  `json:"E"` // 事件時间（毫秒時间戳）
				Symbol    string `json:"s"` // 交易對
				K         struct {
					T  int64  `json:"t"` // K線开始時间
					T2 int64  `json:"T"` // K線結束時间
					S  string `json:"s"` // 交易對
					I  string `json:"i"` // K線间隔
					F  int64  `json:"f"` // 第一笔交易ID
					L  int64  `json:"L"` // 最后一笔交易ID
					O  string `json:"o"` // 开盘價
					C  string `json:"c"` // 收盘價
					H  string `json:"h"` // 最高價
					L2 string `json:"l"` // 最低價
					V  string `json:"v"` // 成交量
					N  int64  `json:"n"` // 成交笔數
					X  bool   `json:"x"` // K線是否完結
					Q  string `json:"q"` // 成交額
					V2 string `json:"V"` // 主动買入成交量
					Q2 string `json:"Q"` // 主动買入成交額
				} `json:"k"`
			} `json:"data"`
		}

		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Warn("⚠️ 解析K線消息失败: %v, 原始消息: %s", err, string(message))
			continue
		}

		// 轉换為Candle（接收所有K線數據，包括未完結的）
		open, _ := strconv.ParseFloat(msg.Data.K.O, 64)
		high, _ := strconv.ParseFloat(msg.Data.K.H, 64)
		low, _ := strconv.ParseFloat(msg.Data.K.L2, 64)
		close, _ := strconv.ParseFloat(msg.Data.K.C, 64)
		volume, _ := strconv.ParseFloat(msg.Data.K.V, 64)

		candle := &Candle{
			Symbol:    msg.Data.K.S,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
			Timestamp: msg.Data.K.T,
			IsClosed:  msg.Data.K.X, // 設置K線是否完結
		}

		// 調用回呼（無論K線是否完結都回呼）
		if k.callback != nil {
			k.callback(candle)
		}
	}
}
