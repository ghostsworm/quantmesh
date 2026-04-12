package gate

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// SpotKlineWebSocketManager 現貨公共 K 線（spot.candlesticks）
type SpotKlineWebSocketManager struct {
	testnet       bool
	gateSymbols   []string
	interval      string
	callback      func(interface{})
	mu            sync.RWMutex
	conn          *websocket.Conn
	stopC         chan struct{}
	stopOnce      sync.Once
	pingInterval  time.Duration
	reconnectDelay time.Duration
}

// NewSpotKlineWebSocketManager 創建現貨 K 線流
func NewSpotKlineWebSocketManager(testnet bool) *SpotKlineWebSocketManager {
	return &SpotKlineWebSocketManager{
		testnet:        testnet,
		stopC:          make(chan struct{}),
		pingInterval:   15 * time.Second,
		reconnectDelay: 5 * time.Second,
	}
}

// Start 訂閱 spot.candlesticks（payload: [interval, BTC_USDT]）
func (k *SpotKlineWebSocketManager) Start(ctx context.Context, gateSymbols []string, interval string, callback func(interface{})) error {
	k.mu.Lock()
	k.gateSymbols = append([]string(nil), gateSymbols...)
	k.interval = interval
	k.callback = callback
	k.mu.Unlock()

	go k.runLoop(ctx)
	return nil
}

func (k *SpotKlineWebSocketManager) runLoop(ctx context.Context) {
	wsURL := gateSpotWsMainnet
	if k.testnet {
		wsURL = gateSpotWsTestnet
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-k.stopC:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Gate Spot K線] 連接失敗: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-k.stopC:
				return
			case <-time.After(k.reconnectDelay):
			}
			continue
		}

		k.mu.Lock()
		k.conn = conn
		symbols := append([]string(nil), k.gateSymbols...)
		iv := k.interval
		k.mu.Unlock()

		for _, gs := range symbols {
			ts := time.Now().Unix()
			sub := map[string]interface{}{
				"time":    ts,
				"channel": "spot.candlesticks",
				"event":   "subscribe",
				"payload": []string{iv, gs},
			}
			if err := conn.WriteJSON(sub); err != nil {
				logger.Warn("⚠️ [Gate Spot K線] 訂閱失敗: %v", err)
				break
			}
			time.Sleep(80 * time.Millisecond)
		}

		logger.Info("✅ [Gate Spot K線] 已訂閱 %v 周期=%s", symbols, iv)

		pingStop := make(chan struct{})
		go k.pingLoop(conn, pingStop)

	readLoop:
		for {
			select {
			case <-ctx.Done():
				close(pingStop)
				conn.Close()
				return
			case <-k.stopC:
				close(pingStop)
				conn.Close()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Warn("⚠️ [Gate Spot K線] 讀取失敗: %v", err)
				close(pingStop)
				conn.Close()
				break readLoop
			}
			k.handleMessage(message)
		}

		k.mu.Lock()
		if k.conn == conn {
			k.conn = nil
		}
		k.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-k.stopC:
			return
		case <-time.After(k.reconnectDelay):
		}
	}
}

func (k *SpotKlineWebSocketManager) pingLoop(conn *websocket.Conn, done <-chan struct{}) {
	t := time.NewTicker(k.pingInterval)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			_ = conn.WriteJSON(map[string]interface{}{"time": time.Now().Unix(), "channel": "spot.ping"})
		}
	}
}

func (k *SpotKlineWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}
	ch, _ := msg["channel"].(string)
	ev, _ := msg["event"].(string)
	if ch == "spot.pong" {
		return
	}
	if ev == "update" && ch == "spot.candlesticks" {
		k.handleCandle(msg)
	}
}

func (k *SpotKlineWebSocketManager) handleCandle(msg map[string]interface{}) {
	arr, ok := msg["result"].([]interface{})
	if !ok || len(arr) == 0 {
		return
	}
	row, ok := arr[0].(map[string]interface{})
	if !ok {
		return
	}
	nameField, _ := row["n"].(string)
	parts := splitAfterFirst(nameField, "_")
	if len(parts) < 2 {
		return
	}
	gateSym := parts[1]
	symbol := convertFromGateSymbol(gateSym)

	timestamp, _ := row["t"].(float64)
	open, _ := parseFloat(row["o"])
	high, _ := parseFloat(row["h"])
	low, _ := parseFloat(row["l"])
	closep, _ := parseFloat(row["c"])
	volume, _ := parseFloat(row["v"])

	var w bool
	if x, ok := row["w"].(bool); ok {
		w = x
	}

	candle := &Candle{
		Symbol:    symbol,
		Open:      open,
		High:      high,
		Low:       low,
		Close:     closep,
		Volume:    volume,
		Timestamp: int64(timestamp),
		IsClosed:  w,
	}

	k.mu.RLock()
	cb := k.callback
	k.mu.RUnlock()
	if cb != nil {
		cb(candle)
	}
}

// Stop 停止
func (k *SpotKlineWebSocketManager) Stop() {
	k.stopOnce.Do(func() { close(k.stopC) })
	k.mu.Lock()
	if k.conn != nil {
		k.conn.Close()
		k.conn = nil
	}
	k.mu.Unlock()
	logger.Info("🛑 [Gate Spot K線] 已停止")
}
