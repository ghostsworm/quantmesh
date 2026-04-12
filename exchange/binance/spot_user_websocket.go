package binance

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"

	binancesdk "github.com/adshao/go-binance/v2"
)

// SpotUserDataWebSocketManager 幣安現貨 User Data Stream（listenKey + executionReport）
type SpotUserDataWebSocketManager struct {
	client     *binancesdk.Client
	useTestnet bool
	listenKey  string

	mu        sync.Mutex
	isRunning bool
	stopC     chan struct{}
	stopOnce  sync.Once
	doneC     chan struct{}
	onOrder   func(OrderUpdate)
}

// NewSpotUserDataWebSocketManager 創建現貨訂單流管理器
func NewSpotUserDataWebSocketManager(client *binancesdk.Client, useTestnet bool) *SpotUserDataWebSocketManager {
	return &SpotUserDataWebSocketManager{
		client:     client,
		useTestnet: useTestnet,
		stopC:      make(chan struct{}),
	}
}

// Start 啟動現貨訂單推送
func (w *SpotUserDataWebSocketManager) Start(ctx context.Context, callback func(OrderUpdate)) error {
	w.mu.Lock()
	if w.isRunning {
		w.mu.Unlock()
		return fmt.Errorf("現貨訂單流已在运行")
	}
	w.onOrder = callback
	w.isRunning = true
	w.mu.Unlock()

	listenKey, err := w.client.NewStartUserStreamService().Do(ctx)
	if err != nil {
		w.mu.Lock()
		w.isRunning = false
		w.onOrder = nil
		w.mu.Unlock()
		return fmt.Errorf("獲取 listenKey 失敗: %w", err)
	}
	w.listenKey = listenKey
	logger.Debug("✅ [Binance Spot] listenKey: %s", listenKey)

	w.mu.Lock()
	w.doneC = make(chan struct{})
	w.mu.Unlock()

	go w.keepAliveListenKey(ctx)
	go w.listenLoop(ctx)

	return nil
}

func (w *SpotUserDataWebSocketManager) keepAliveListenKey(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopC:
			return
		case <-ticker.C:
			if err := w.client.NewKeepaliveUserStreamService().ListenKey(w.listenKey).Do(ctx); err != nil {
				logger.Error("❌ [Binance Spot] listenKey 保活失敗: %v", err)
			} else {
				logger.Debug("✅ [Binance Spot] listenKey 保活成功")
			}
		}
	}
}

func (w *SpotUserDataWebSocketManager) listenLoop(ctx context.Context) {
	defer close(w.doneC)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopC:
			return
		default:
		}

		prev := binancesdk.UseTestnet
		binancesdk.UseTestnet = w.useTestnet

		handler := func(ev *binancesdk.WsUserDataEvent) {
			if ev.Event != binancesdk.UserDataEventTypeExecutionReport {
				return
			}
			o := ev.OrderUpdate
			price, _ := strconv.ParseFloat(o.Price, 64)
			qty, _ := strconv.ParseFloat(o.Volume, 64)
			filled, _ := strconv.ParseFloat(o.FilledVolume, 64)
			avgPx, _ := strconv.ParseFloat(o.LatestPrice, 64)
			comm, _ := strconv.ParseFloat(o.FeeCost, 64)

			up := OrderUpdate{
				OrderID:         o.Id,
				ClientOrderID:   o.ClientOrderId,
				Symbol:          o.Symbol,
				Side:            Side(strings.ToUpper(o.Side)),
				Type:            OrderType(strings.ToUpper(o.Type)),
				Status:          OrderStatus(o.Status),
				Price:           price,
				Quantity:        qty,
				ExecutedQty:     filled,
				AvgPrice:        avgPx,
				UpdateTime:      o.TransactionTime,
				Commission:      comm,
				CommissionAsset: o.FeeAsset,
				RealizedPnL:     0,
			}

			w.mu.Lock()
			cb := w.onOrder
			w.mu.Unlock()
			if cb != nil {
				cb(up)
			}
		}

		errHandler := func(err error) {
			logger.Error("❌ [Binance Spot] User Data WebSocket 錯誤: %v", err)
		}

		doneC, stopC, err := binancesdk.WsUserDataServe(w.listenKey, handler, errHandler)
		binancesdk.UseTestnet = prev

		if err != nil {
			logger.Error("❌ [Binance Spot] WebSocket 啟動失敗: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-w.stopC:
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}

		logger.Info("✅ [Binance Spot] 訂單流 WebSocket 已連接")

		select {
		case <-ctx.Done():
			stopC <- struct{}{}
			return
		case <-w.stopC:
			stopC <- struct{}{}
			return
		case <-doneC:
			logger.Warn("⚠️ [Binance Spot] 訂單流斷開，5s 後重連")
			time.Sleep(5 * time.Second)
		}
	}
}

// Stop 停止訂單流
func (w *SpotUserDataWebSocketManager) Stop() {
	w.mu.Lock()
	if !w.isRunning {
		w.mu.Unlock()
		return
	}
	w.isRunning = false
	w.onOrder = nil
	doneWait := w.doneC
	w.mu.Unlock()

	w.stopOnce.Do(func() { close(w.stopC) })

	if doneWait != nil {
		select {
		case <-doneWait:
		case <-time.After(15 * time.Second):
			logger.Warn("⚠️ [Binance Spot] 訂單流停止超時")
		}
	}
}
