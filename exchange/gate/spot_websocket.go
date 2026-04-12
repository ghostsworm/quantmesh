package gate

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

const (
	gateSpotWsMainnet = "wss://api.gateio.ws/ws/v4/"
	gateSpotWsTestnet = "wss://api-testnet.gateio.ws/ws/v4/"
)

// SpotPriceWebSocketManager Gate.io 現貨公共 tickers（spot.tickers）
type SpotPriceWebSocketManager struct {
	testnet       bool
	gateSymbol    string // BTC_USDT
	latestPrice   float64
	priceMu       sync.RWMutex
	priceCallback func(float64)
	stopC         chan struct{}
	stopOnce      sync.Once
}

// NewSpotPriceWebSocketManager 創建現貨價格流管理器
func NewSpotPriceWebSocketManager(testnet bool) *SpotPriceWebSocketManager {
	return &SpotPriceWebSocketManager{
		testnet: testnet,
		stopC:   make(chan struct{}),
	}
}

// Start 訂閱 spot.tickers，推送 last 價
func (m *SpotPriceWebSocketManager) Start(ctx context.Context, gateSymbol string, callback func(float64)) error {
	m.gateSymbol = gateSymbol
	m.priceCallback = callback

	wsURL := gateSpotWsMainnet
	if m.testnet {
		wsURL = gateSpotWsTestnet
		logger.Info("🌐 [Gate Spot WS] 使用測試網: %s", wsURL)
	}

	go m.runLoop(ctx, wsURL)
	return nil
}

func (m *SpotPriceWebSocketManager) runLoop(ctx context.Context, wsURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.stopC:
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Gate Spot WS] 連接失敗: %v，5s 後重試", err)
			select {
			case <-ctx.Done():
				return
			case <-m.stopC:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		ts := time.Now().Unix()
		sub := map[string]interface{}{
			"time":    ts,
			"channel": "spot.tickers",
			"event":   "subscribe",
			"payload": []string{m.gateSymbol},
		}
		if err := conn.WriteJSON(sub); err != nil {
			logger.Warn("⚠️ [Gate Spot WS] 訂閱失敗: %v", err)
			conn.Close()
			time.Sleep(2 * time.Second)
			continue
		}

	readLoop:
		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return
			case <-m.stopC:
				conn.Close()
				return
			default:
			}

			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Warn("⚠️ [Gate Spot WS] 讀取失敗: %v，重連", err)
				conn.Close()
				break readLoop
			}

			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}
			ch, _ := msg["channel"].(string)
			ev, _ := msg["event"].(string)
			if ch == "spot.pong" {
				continue
			}
			if ev == "update" && ch == "spot.tickers" {
				m.handleTicker(msg)
			}
		}

		select {
		case <-ctx.Done():
			return
		case <-m.stopC:
			return
		case <-time.After(2 * time.Second):
		}
	}
}

func (m *SpotPriceWebSocketManager) handleTicker(msg map[string]interface{}) {
	var res map[string]interface{}
	if r, ok := msg["result"].(map[string]interface{}); ok {
		res = r
	} else if arr, ok := msg["result"].([]interface{}); ok && len(arr) > 0 {
		if r, ok := arr[0].(map[string]interface{}); ok {
			res = r
		}
	}
	if res == nil {
		return
	}
	lastStr, ok := res["last"].(string)
	if !ok {
		return
	}
	price, err := strconv.ParseFloat(lastStr, 64)
	if err != nil || price <= 0 {
		return
	}
	m.priceMu.Lock()
	m.latestPrice = price
	m.priceMu.Unlock()
	if m.priceCallback != nil {
		m.priceCallback(price)
	}
}

// GetLatestPrice 緩存價
func (m *SpotPriceWebSocketManager) GetLatestPrice() float64 {
	m.priceMu.RLock()
	defer m.priceMu.RUnlock()
	return m.latestPrice
}

// Stop 停止
func (m *SpotPriceWebSocketManager) Stop() {
	m.stopOnce.Do(func() { close(m.stopC) })
}
