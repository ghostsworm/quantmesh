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

// SpotOrderWebSocketManager Gate 現貨私有訂單流（api.gateio.ws/ws/v4，channel spot.orders）
type SpotOrderWebSocketManager struct {
	apiKey     string
	secretKey  string
	signer     *Signer
	gateSymbol string
	testnet    bool

	mu       sync.RWMutex
	writeMu  sync.Mutex // 串行化 conn 的寫操作，避免 gorilla/websocket 並發寫競態
	conn     *websocket.Conn
	ctx      context.Context
	cancel   context.CancelFunc
	callback func(interface{})
}

// NewSpotOrderWebSocketManager 創建現貨訂單流管理器
func NewSpotOrderWebSocketManager(apiKey, secretKey, gateSymbol string, testnet bool) *SpotOrderWebSocketManager {
	return &SpotOrderWebSocketManager{
		apiKey:     apiKey,
		secretKey:  secretKey,
		signer:     NewSigner(apiKey, secretKey),
		gateSymbol: gateSymbol,
		testnet:    testnet,
	}
}

// Start 啟動 spot.orders 訂閱
func (m *SpotOrderWebSocketManager) Start(ctx context.Context, callback func(interface{})) error {
	m.mu.Lock()
	if m.cancel != nil {
		m.mu.Unlock()
		return fmt.Errorf("Gate 現貨訂單流已在运行")
	}
	m.callback = callback
	m.ctx, m.cancel = context.WithCancel(ctx)
	m.mu.Unlock()

	go m.loop()
	return nil
}

func (m *SpotOrderWebSocketManager) wsURL() string {
	if m.testnet {
		return gateSpotWsTestnet
	}
	return gateSpotWsMainnet
}

func (m *SpotOrderWebSocketManager) loop() {
	for {
		select {
		case <-m.ctx.Done():
			return
		default:
		}

		conn, _, err := websocket.DefaultDialer.Dial(m.wsURL(), nil)
		if err != nil {
			logger.Error("❌ [Gate Spot Order WS] 連接失敗: %v", err)
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		m.mu.Lock()
		m.conn = conn
		m.mu.Unlock()

		ts := time.Now().Unix()
		sign := m.signer.SignWebSocket("spot.orders", "subscribe", ts)
		sub := map[string]interface{}{
			"time":    ts,
			"channel": "spot.orders",
			"event":   "subscribe",
			"auth": map[string]interface{}{
				"method": "api_key",
				"KEY":    m.apiKey,
				"SIGN":   sign,
			},
			"req_header": map[string]string{
				"X-Gate-Channel-Id": GateChannelID,
			},
			"payload": []string{m.gateSymbol},
		}
		m.writeMu.Lock()
		err = conn.WriteJSON(sub)
		m.writeMu.Unlock()
		if err != nil {
			logger.Warn("⚠️ [Gate Spot Order WS] 訂閱失敗: %v", err)
			conn.Close()
			select {
			case <-m.ctx.Done():
				return
			case <-time.After(3 * time.Second):
			}
			continue
		}

		logger.Info("✅ [Gate Spot Order WS] 已訂閱 spot.orders %s", m.gateSymbol)

		pingStop := make(chan struct{})
		go m.pingLoop(conn, pingStop)

	readLoop:
		for {
			select {
			case <-m.ctx.Done():
				close(pingStop)
				conn.Close()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Warn("⚠️ [Gate Spot Order WS] 讀取失敗: %v", err)
				close(pingStop)
				conn.Close()
				break readLoop
			}
			m.handleMessage(message)
		}

		m.mu.Lock()
		if m.conn == conn {
			m.conn = nil
		}
		m.mu.Unlock()

		select {
		case <-m.ctx.Done():
			return
		case <-time.After(3 * time.Second):
		}
	}
}

// pingLoop 與現貨 K 線流一致定期發送 spot.ping，降低長連線被服務端 idle 斷開（websocket 1006）的概率
func (m *SpotOrderWebSocketManager) pingLoop(conn *websocket.Conn, done <-chan struct{}) {
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-done:
			return
		case <-t.C:
			m.writeMu.Lock()
			_ = conn.WriteJSON(map[string]interface{}{"time": time.Now().Unix(), "channel": "spot.ping"})
			m.writeMu.Unlock()
		}
	}
}

func (m *SpotOrderWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}
	ch, _ := msg["channel"].(string)
	ev, _ := msg["event"].(string)
	if ch == "spot.pong" {
		return
	}
	if ev != "update" || ch != "spot.orders" {
		return
	}
	res := msg["result"]
	if res == nil {
		return
	}
	// result 可能為單條 object 或數組
	switch v := res.(type) {
	case map[string]interface{}:
		m.emitOrder(v)
	case []interface{}:
		for _, item := range v {
			if row, ok := item.(map[string]interface{}); ok {
				m.emitOrder(row)
			}
		}
	}
}

func (m *SpotOrderWebSocketManager) emitOrder(row map[string]interface{}) {
	idStr, _ := row["id"].(string)
	orderID, _ := strconv.ParseInt(idStr, 10, 64)
	price := cellFloat(row, "price")
	amount := cellFloat(row, "amount")
	filled := cellFloat(row, "filled_amount")
	avg := cellFloat(row, "avg_deal_price")
	sideStr, _ := row["side"].(string)
	var side Side
	if strings.EqualFold(sideStr, "buy") {
		side = SideBuy
	} else {
		side = SideSell
	}
	statusStr, _ := row["status"].(string)
	uTimeStr, _ := row["update_time_ms"].(string)
	if uTimeStr == "" {
		uTimeStr, _ = row["update_time"].(string)
	}
	uTime, _ := strconv.ParseInt(uTimeStr, 10, 64)
	pair, _ := row["currency_pair"].(string)
	sym := pair
	if sym == "" {
		sym = m.gateSymbol
	}

	up := OrderUpdate{
		OrderID:         orderID,
		ClientOrderID:   getStr(row, "text"),
		Symbol:          sym,
		Side:            side,
		Type:            OrderType(getStr(row, "type")),
		Status:          OrderStatus(statusStr),
		Price:           price,
		Quantity:        amount,
		ExecutedQty:     filled,
		AvgPrice:        avg,
		UpdateTime:      uTime,
		Commission:      0,
		CommissionAsset: "USDT",
	}
	if m.callback != nil {
		m.callback(up)
	}
}

func getStr(m map[string]interface{}, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func cellFloat(m map[string]interface{}, k string) float64 {
	v, ok := m[k]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case string:
		f, _ := strconv.ParseFloat(t, 64)
		return f
	case float64:
		return t
	default:
		return 0
	}
}

// Stop 停止
func (m *SpotOrderWebSocketManager) Stop() {
	m.mu.Lock()
	if m.cancel != nil {
		m.cancel()
		m.cancel = nil
	}
	if m.conn != nil {
		m.conn.Close()
		m.conn = nil
	}
	m.mu.Unlock()
	logger.Info("🛑 [Gate Spot Order WS] 已停止")
}
