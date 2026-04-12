package bitget

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"quantmesh/logger"
)

// SpotPublicPriceWS Bitget 現貨公共 ticker（instType=SPOT）
type SpotPublicPriceWS struct {
	testnet       bool
	instID        string // BTCUSDT
	latestPrice   float64
	priceMu       sync.RWMutex
	priceCallback func(float64)
	stopC         chan struct{}
	stopOnce      sync.Once
}

// NewSpotPublicPriceWS 創建現貨價格流
func NewSpotPublicPriceWS(testnet bool) *SpotPublicPriceWS {
	return &SpotPublicPriceWS{
		testnet: testnet,
		stopC:   make(chan struct{}),
	}
}

// Start 訂閱 ticker 頻道
func (w *SpotPublicPriceWS) Start(ctx context.Context, instID string, callback func(float64)) error {
	w.instID = instID
	w.priceCallback = callback
	go w.loop(ctx)
	return nil
}

func (w *SpotPublicPriceWS) loop(ctx context.Context) {
	dialer := getProxyDialer()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopC:
			return
		default:
		}

		wsURL := BitgetWSPublic
		if w.testnet {
			wsURL = BitgetTestnetWSPublic
		}
		conn, _, err := dialer.Dial(wsURL, nil)
		if err != nil {
			logger.Error("❌ [Bitget Spot WS] 連接失敗: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-w.stopC:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		sub := map[string]interface{}{
			"op": "subscribe",
			"args": []WSSubscribeArg{
				{InstType: "SPOT", Channel: "ticker", InstId: w.instID},
			},
		}
		if err := conn.WriteJSON(sub); err != nil {
			conn.Close()
			time.Sleep(2 * time.Second)
			continue
		}

		for {
			select {
			case <-ctx.Done():
				conn.Close()
				return
			case <-w.stopC:
				conn.Close()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
			_, message, err := conn.ReadMessage()
			if err != nil {
				logger.Warn("⚠️ [Bitget Spot WS] 讀取失敗: %v", err)
				conn.Close()
				break
			}
			if string(message) == "pong" {
				continue
			}
			var msg struct {
				Arg    WSSubscribeArg  `json:"arg"`
				Action string          `json:"action"`
				Data   json.RawMessage `json:"data"`
			}
			if err := json.Unmarshal(message, &msg); err != nil {
				continue
			}
			if msg.Arg.Channel != "ticker" || msg.Arg.InstType != "SPOT" {
				continue
			}
			if len(msg.Data) == 0 {
				continue
			}
			var rows []map[string]interface{}
			if err := json.Unmarshal(msg.Data, &rows); err != nil {
				continue
			}
			for _, row := range rows {
				lastStr, ok := row["lastPr"].(string)
				if !ok {
					lastStr, ok = row["last"].(string)
				}
				if !ok {
					continue
				}
				price, err := strconv.ParseFloat(lastStr, 64)
				if err != nil || price <= 0 {
					continue
				}
				w.priceMu.Lock()
				w.latestPrice = price
				w.priceMu.Unlock()
				if w.priceCallback != nil {
					w.priceCallback(price)
				}
			}
		}
	}
}

// GetLatestPrice 緩存
func (w *SpotPublicPriceWS) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}

// Stop 停止
func (w *SpotPublicPriceWS) Stop() {
	w.stopOnce.Do(func() { close(w.stopC) })
}
