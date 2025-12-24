package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"opensqt/logger"

	"github.com/gorilla/websocket"
)

// Candle K线数据
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool // K线是否完结
}

// KlineWebSocketManager Bitget K线WebSocket管理器
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
}

// NewKlineWebSocketManager 创建K线WebSocket管理器
func NewKlineWebSocketManager() *KlineWebSocketManager {
	return &KlineWebSocketManager{
		done:           make(chan struct{}),
		reconnectDelay: 5 * time.Second,  // 重连延迟
		pingInterval:   15 * time.Second, // Ping间隔（Bitget官方SDK使用15秒）
	}
}

// Start 启动K线流（带自动重连）
func (k *KlineWebSocketManager) Start(ctx context.Context, symbols []string, interval string, callback func(candle interface{})) error {
	k.mu.Lock()
	if k.isRunning {
		k.mu.Unlock()
		return fmt.Errorf("K线流已在运行")
	}
	k.callback = callback
	k.symbols = symbols
	k.interval = interval
	k.isRunning = true
	k.mu.Unlock()

	// 启动连接和重连协程
	go k.connectLoop(ctx)

	return nil
}

// connectLoop 连接循环（自动重连）
func (k *KlineWebSocketManager) connectLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			logger.Info("✅ Bitget K线WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ Bitget K线WebSocket已停止")
			return
		default:
		}

		// Bitget WebSocket URL
		wsURL := "wss://ws.bitget.com/v2/ws/public"

		logger.Info("🔗 正在连接 Bitget K线WebSocket...")

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ Bitget K线WebSocket连接失败: %v，%v后重试", err, k.reconnectDelay)
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-ctx.Done():
				logger.Info("✅ Bitget K线WebSocket已停止（上下文取消）")
				return
			case <-k.done:
				logger.Info("✅ Bitget K线WebSocket已停止")
				return
			case <-time.After(k.reconnectDelay):
			}
			continue
		}

		k.mu.Lock()
		k.conn = conn
		k.mu.Unlock()

		logger.Info("✅ Bitget K线WebSocket已连接")

		// 订阅K线
		if err := k.subscribe(k.symbols, k.interval); err != nil {
			logger.Error("❌ Bitget K线订阅失败: %v", err)
			conn.Close()
			// 使用 select 等待，可以立即响应 context 取消
			select {
			case <-ctx.Done():
				logger.Info("✅ Bitget K线WebSocket已停止（上下文取消）")
				return
			case <-k.done:
				logger.Info("✅ Bitget K线WebSocket已停止")
				return
			case <-time.After(k.reconnectDelay):
			}
			continue
		}

		// 启动ping协程
		go k.pingLoop(ctx, conn)

		// 启动读取循环（阻塞直到连接断开）
		k.readLoop(ctx, conn)

		// 连接断开，清理并准备重连
		k.mu.Lock()
		if k.conn == conn {
			k.conn = nil
		}
		k.mu.Unlock()

		// 检查是否因为 context 取消而断开，如果是则直接退出
		select {
		case <-ctx.Done():
			logger.Info("✅ Bitget K线WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ Bitget K线WebSocket已停止")
			return
		default:
		}

		logger.Warn("⚠️ Bitget K线WebSocket连接断开，%v后重连...", k.reconnectDelay)
		// 使用 select 等待，可以立即响应 context 取消
		select {
		case <-ctx.Done():
			logger.Info("✅ Bitget K线WebSocket已停止（上下文取消）")
			return
		case <-k.done:
			logger.Info("✅ Bitget K线WebSocket已停止")
			return
		case <-time.After(k.reconnectDelay):
		}
	}
}

// subscribe 订阅K线
func (k *KlineWebSocketManager) subscribe(symbols []string, interval string) error {
	// Bitget V2 订阅格式
	// {"op": "subscribe", "args": [{"instType": "USDT-FUTURES", "channel": "candle1m", "instId": "BTCUSDT"}]}

	// 转换interval格式：1m -> candle1m
	channel := fmt.Sprintf("candle%s", interval)

	args := make([]map[string]string, len(symbols))
	for i, symbol := range symbols {
		// 转换为Bitget格式
		bitgetSymbol := convertToBitgetSymbol(symbol)
		args[i] = map[string]string{
			"instType": "USDT-FUTURES",
			"channel":  channel,
			"instId":   bitgetSymbol,
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
		return fmt.Errorf("WebSocket连接未建立")
	}

	data, _ := json.Marshal(subMsg)
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		return fmt.Errorf("发送订阅消息失败: %w", err)
	}

	logger.Debug("已发送K线订阅请求: %d个币种", len(symbols))
	return nil
}

// Stop 停止K线流
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

	logger.Info("✅ Bitget K线WebSocket已停止")
}

// pingLoop ping循环
func (k *KlineWebSocketManager) pingLoop(ctx context.Context, conn *websocket.Conn) {
	ticker := time.NewTicker(k.pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-k.done:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			k.mu.RLock()
			currentConn := k.conn
			k.mu.RUnlock()

			// 检查连接是否还是当前连接
			if currentConn != conn {
				return
			}

			// Bitget 使用纯文本 "ping"，服务器返回纯文本 "pong"
			// 参考官方SDK: https://github.com/BitgetLimited/v3-bitget-api-sdk/blob/master/bitget-golang-sdk-api/internal/common/bitgetwsclient.go
			conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
				logger.Warn("⚠️ Bitget K线WebSocket发送Ping失败: %v", err)
				conn.Close()
				return
			}
			logger.Debug("💓 Bitget K线WebSocket Ping已发送")
		}
	}
}

// readLoop 读取消息循环
func (k *KlineWebSocketManager) readLoop(ctx context.Context, conn *websocket.Conn) {
	defer func() {
		if r := recover(); r != nil {
			logger.Error("❌ Bitget K线WebSocket读取协程panic: %v", r)
		}
		conn.Close()
	}()

	// 🔥 设置 pong handler，收到 pong 时更新读取超时
	conn.SetPongHandler(func(string) error {
		logger.Debug("💓 收到 K线WebSocket pong，更新读取超时")
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		return nil
	})

	// 🔥 初始读取超时：设置为90秒（大于ping间隔的3倍）
	conn.SetReadDeadline(time.Now().Add(90 * time.Second))

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
				logger.Warn("⚠️ Bitget K线WebSocket异常关闭: %v", err)
			} else {
				logger.Debug("Bitget K线WebSocket读取错误: %v", err)
			}
			return
		}

		// 🔥 收到任何消息都更新读取超时（延长到90秒）
		conn.SetReadDeadline(time.Now().Add(90 * time.Second))

		// 检查是否为纯文本 "pong" 响应
		if string(message) == "pong" {
			logger.Debug("💓 收到 K线WebSocket pong")
			continue
		}

		// 解析消息
		var msg struct {
			Event string `json:"event"` // subscribe, error, etc
			Op    string `json:"op"`    // pong
			Arg   struct {
				InstType string `json:"instType"`
				Channel  string `json:"channel"`
				InstId   string `json:"instId"`
			} `json:"arg"`
			Data [][]string `json:"data"` // [[timestamp, open, high, low, close, volume, amount]]
		}

		if err := json.Unmarshal(message, &msg); err != nil {
			logger.Debug("解析K线消息失败: %v", err)
			continue
		}

		// 跳过订阅确认消息
		if msg.Event == "subscribe" {
			logger.Debug("✅ K线订阅成功: %s %s", msg.Arg.InstId, msg.Arg.Channel)
			continue
		}

		// 处理K线数据
		if len(msg.Data) > 0 && msg.Arg.Channel != "" && strings.HasPrefix(msg.Arg.Channel, "candle") {
			for _, kline := range msg.Data {
				if len(kline) < 6 {
					continue
				}

				// Bitget: [timestamp, open, high, low, close, volume, amount]
				timestamp, _ := strconv.ParseInt(kline[0], 10, 64)
				open, _ := strconv.ParseFloat(kline[1], 64)
				high, _ := strconv.ParseFloat(kline[2], 64)
				low, _ := strconv.ParseFloat(kline[3], 64)
				close, _ := strconv.ParseFloat(kline[4], 64)
				volume, _ := strconv.ParseFloat(kline[5], 64)

				// V2 API 直接使用原始符号，不需要转换
				symbol := msg.Arg.InstId

				// 判断K线是否完结：根据时间戳和K线间隔判断
				// timestamp 是K线的开始时间（毫秒级）
				klineStartTime := timestamp / 1000 // 转为秒

				// 从 channel 中提取K线间隔（如 candle1m -> 1m）
				intervalStr := strings.TrimPrefix(msg.Arg.Channel, "candle")

				// 计算K线间隔（秒）
				var intervalSeconds int64
				switch intervalStr {
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

				// K线结束时间 = 开始时间 + 间隔
				klineEndTime := klineStartTime + intervalSeconds
				currentTime := time.Now().Unix()

				// 如果当前时间已过K线结束时间，则认为已完结
				isClosed := currentTime >= klineEndTime

				candle := &Candle{
					Symbol:    symbol,
					Open:      open,
					High:      high,
					Low:       low,
					Close:     close,
					Volume:    volume,
					Timestamp: timestamp,
					IsClosed:  isClosed,
				}

				// 调用回调
				if k.callback != nil {
					k.callback(candle)
				}
			}
		}
	}
}
