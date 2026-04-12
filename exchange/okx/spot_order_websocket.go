package okx

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/logger"

	"github.com/gorilla/websocket"
)

// SpotOrderWebSocketManager 現貨私有訂單流（orders，instType=SPOT）
type SpotOrderWebSocketManager struct {
	apiKey     string
	secretKey  string
	passphrase string
	useTestnet bool
	instId     string

	conn          *websocket.Conn
	mu            sync.RWMutex
	stopChan      chan struct{}
	stopOnce      sync.Once
	isRunning     atomic.Bool
	orderCallback func(OrderUpdate)
}

// NewSpotOrderWebSocketManager 創建現貨訂單流管理器
func NewSpotOrderWebSocketManager(apiKey, secretKey, passphrase string, useTestnet bool, instId string) *SpotOrderWebSocketManager {
	return &SpotOrderWebSocketManager{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		useTestnet: useTestnet,
		instId:     instId,
		stopChan:   make(chan struct{}),
	}
}

func (w *SpotOrderWebSocketManager) sign(timestamp string) string {
	message := timestamp + "GET" + "/users/self/verify"
	h := hmac.New(sha256.New, []byte(w.secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// Start 啟動現貨訂單 WebSocket
func (w *SpotOrderWebSocketManager) Start(ctx context.Context, callback func(OrderUpdate)) error {
	if w.isRunning.Load() {
		return fmt.Errorf("OKX 現貨訂單流已在运行")
	}
	w.stopOnce = sync.Once{}
	w.stopChan = make(chan struct{})
	w.orderCallback = callback

	wsURL := MainnetWsURL
	if w.useTestnet {
		wsURL = TestnetWsURL
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return fmt.Errorf("连接 OKX 現貨訂單 WebSocket 失败: %w", err)
	}

	w.mu.Lock()
	w.conn = conn
	w.mu.Unlock()
	w.isRunning.Store(true)

	if err := w.login(); err != nil {
		conn.Close()
		w.isRunning.Store(false)
		return fmt.Errorf("WebSocket 登錄失败: %w", err)
	}

	subMsg := map[string]interface{}{
		"op": "subscribe",
		"args": []map[string]string{
			{
				"channel":  "orders",
				"instType": "SPOT",
				"instId":   w.instId,
			},
		},
	}
	if err := w.sendMessage(subMsg); err != nil {
		conn.Close()
		w.isRunning.Store(false)
		return fmt.Errorf("订阅現貨订單频道失败: %w", err)
	}

	go w.readMessages()
	go w.keepAlive()

	logger.Info("✅ [OKX Spot WS] 訂單流已啟动 instId=%s", w.instId)
	return nil
}

func (w *SpotOrderWebSocketManager) login() error {
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	sign := w.sign(timestamp)
	loginMsg := map[string]interface{}{
		"op": "login",
		"args": []map[string]string{
			{
				"apiKey":     w.apiKey,
				"passphrase": w.passphrase,
				"timestamp":  timestamp,
				"sign":       sign,
			},
		},
	}
	return w.sendMessage(loginMsg)
}

func (w *SpotOrderWebSocketManager) sendMessage(msg interface{}) error {
	w.mu.RLock()
	defer w.mu.RUnlock()
	if w.conn == nil {
		return fmt.Errorf("WebSocket 未连接")
	}
	return w.conn.WriteJSON(msg)
}

func (w *SpotOrderWebSocketManager) readMessages() {
	defer func() {
		w.isRunning.Store(false)
		if r := recover(); r != nil {
			logger.Error("❌ [OKX Spot WS] panic: %v", r)
		}
	}()

	for w.isRunning.Load() {
		w.mu.RLock()
		conn := w.conn
		w.mu.RUnlock()
		if conn == nil {
			break
		}
		_, message, err := conn.ReadMessage()
		if err != nil {
			if w.isRunning.Load() {
				logger.Warn("⚠️ [OKX Spot WS] 读取失败: %v", err)
			}
			break
		}
		w.handleMessage(message)
	}
}

func (w *SpotOrderWebSocketManager) handleMessage(message []byte) {
	var msg map[string]interface{}
	if err := json.Unmarshal(message, &msg); err != nil {
		return
	}
	if event, ok := msg["event"].(string); ok {
		if event == "login" {
			if code, ok := msg["code"].(string); ok && code == "0" {
				logger.Info("✅ [OKX Spot WS] 登錄成功")
			}
		} else if event == "error" {
			logger.Error("❌ [OKX Spot WS] %v", msg["msg"])
		}
		return
	}
	if arg, ok := msg["arg"].(map[string]interface{}); ok {
		if channel, ok := arg["channel"].(string); ok && channel == "orders" {
			w.handleOrderUpdate(msg)
		}
	}
}

func (w *SpotOrderWebSocketManager) handleOrderUpdate(msg map[string]interface{}) {
	data, ok := msg["data"].([]interface{})
	if !ok || len(data) == 0 {
		return
	}
	for _, item := range data {
		orderData, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		orderId, _ := strconv.ParseInt(getString(orderData, "ordId"), 10, 64)
		price, _ := strconv.ParseFloat(getString(orderData, "px"), 64)
		quantity, _ := strconv.ParseFloat(getString(orderData, "sz"), 64)
		executedQty, _ := strconv.ParseFloat(getString(orderData, "accFillSz"), 64)
		avgPrice, _ := strconv.ParseFloat(getString(orderData, "avgPx"), 64)
		updateTime, _ := strconv.ParseInt(getString(orderData, "uTime"), 10, 64)
		side := getString(orderData, "side")
		var orderSide Side
		if side == "buy" {
			orderSide = SideBuy
		} else {
			orderSide = SideSell
		}
		realizedPnL, _ := strconv.ParseFloat(getString(orderData, "pnl"), 64)
		update := OrderUpdate{
			OrderID:         orderId,
			ClientOrderID:   getString(orderData, "clOrdId"),
			Symbol:          getString(orderData, "instId"),
			Side:            orderSide,
			Type:            OrderType(getString(orderData, "ordType")),
			Status:          OrderStatus(getString(orderData, "state")),
			Price:           price,
			Quantity:        quantity,
			ExecutedQty:     executedQty,
			AvgPrice:        avgPrice,
			UpdateTime:      updateTime,
			Commission:      0,
			CommissionAsset: "USDT",
			RealizedPnL:     realizedPnL,
		}
		if w.orderCallback != nil {
			w.orderCallback(update)
		}
	}
}

func (w *SpotOrderWebSocketManager) keepAlive() {
	ticker := time.NewTicker(20 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			if !w.isRunning.Load() {
				return
			}
			w.mu.RLock()
			conn := w.conn
			w.mu.RUnlock()
			if conn != nil {
				_ = conn.WriteMessage(websocket.TextMessage, []byte("ping"))
			}
		case <-w.stopChan:
			return
		}
	}
}

// Stop 停止現貨訂單流
func (w *SpotOrderWebSocketManager) Stop() {
	if !w.isRunning.Load() {
		return
	}
	w.isRunning.Store(false)
	w.stopOnce.Do(func() { close(w.stopChan) })
	w.mu.Lock()
	if w.conn != nil {
		w.conn.Close()
		w.conn = nil
	}
	w.mu.Unlock()
	logger.Info("🛑 [OKX Spot WS] 訂單流已停止")
}
