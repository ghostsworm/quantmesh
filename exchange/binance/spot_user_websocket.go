package binance

import (
	"context"
	"fmt"
	"math"
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

	mu            sync.Mutex
	isRunning     bool
	stopC         chan struct{}
	stopOnce      sync.Once
	doneC         chan struct{}
	onOrder       func(OrderUpdate)
	fillMu        sync.Mutex
	filledByOrder map[string]float64
}

// NewSpotUserDataWebSocketManager 創建現貨訂單流管理器
func NewSpotUserDataWebSocketManager(client *binancesdk.Client, useTestnet bool) *SpotUserDataWebSocketManager {
	return &SpotUserDataWebSocketManager{
		client:        client,
		useTestnet:    useTestnet,
		stopC:         make(chan struct{}),
		filledByOrder: make(map[string]float64),
	}
}

// spotFeeCoversFill reports whether the latest-trade commission covers the
// entire newly observed cumulative quantity. Binance's n/N fields describe
// only l/L, while z/Z are cumulative for the order.
func (w *SpotUserDataWebSocketManager) spotFeeCoversFill(symbol string, orderID int64, cumulativeQty, latestQty float64) bool {
	if orderID <= 0 || cumulativeQty <= 0 || latestQty <= 0 || math.IsNaN(cumulativeQty) || math.IsInf(cumulativeQty, 0) || math.IsNaN(latestQty) || math.IsInf(latestQty, 0) {
		return false
	}
	key := fmt.Sprintf("%s:%d", symbol, orderID)
	w.fillMu.Lock()
	defer w.fillMu.Unlock()
	previous := w.filledByOrder[key]
	delta := cumulativeQty - previous
	tolerance := math.Max(1e-10, math.Abs(cumulativeQty)*1e-9)
	if cumulativeQty > previous {
		w.filledByOrder[key] = cumulativeQty
	}
	return delta > 0 && math.Abs(delta-latestQty) <= tolerance
}

func (w *SpotUserDataWebSocketManager) clearSpotOrderFill(symbol string, orderID int64) {
	if orderID <= 0 {
		return
	}
	w.fillMu.Lock()
	delete(w.filledByOrder, fmt.Sprintf("%s:%d", symbol, orderID))
	w.fillMu.Unlock()
}

func parseSpotCommission(raw, asset string) (float64, bool) {
	commission, err := strconv.ParseFloat(raw, 64)
	known := raw != "" && err == nil && !math.IsNaN(commission) && !math.IsInf(commission, 0) && (commission == 0 || asset != "")
	return commission, known
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
			latestQty, _ := strconv.ParseFloat(o.LatestVolume, 64)
			// LatestPrice is Binance's last-fill price (L), not the order's
			// cumulative average. Use cumulative quote / executed quantity (Z / z).
			filledQuote, _ := strconv.ParseFloat(o.FilledQuoteVolume, 64)
			avgPx := cumulativeAveragePrice(filledQuote, filled)
			comm, commissionKnown := parseSpotCommission(o.FeeCost, o.FeeAsset)
			commissionIncomplete := o.ExecutionType == "TRADE" && !w.spotFeeCoversFill(o.Symbol, o.Id, filled, latestQty)
			if commissionIncomplete {
				commissionKnown = false
				comm = 0
			}

			up := OrderUpdate{
				OrderID:              o.Id,
				ClientOrderID:        o.ClientOrderId,
				Symbol:               o.Symbol,
				Side:                 Side(strings.ToUpper(o.Side)),
				Type:                 OrderType(strings.ToUpper(o.Type)),
				Status:               OrderStatus(o.Status),
				Price:                price,
				Quantity:             qty,
				ExecutedQty:          filled,
				AvgPrice:             avgPx,
				UpdateTime:           o.TransactionTime,
				Commission:           comm,
				CommissionAsset:      o.FeeAsset,
				CommissionKnown:      commissionKnown,
				CommissionIncomplete: commissionIncomplete,
				RealizedPnL:          0,
			}

			w.mu.Lock()
			cb := w.onOrder
			w.mu.Unlock()
			if cb != nil {
				cb(up)
			}
			if o.Status == "FILLED" || o.Status == "CANCELED" || o.Status == "REJECTED" || o.Status == "EXPIRED" {
				w.clearSpotOrderFill(o.Symbol, o.Id)
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

func cumulativeAveragePrice(cumulativeQuote, executedQty float64) float64 {
	if cumulativeQuote <= 0 || executedQty <= 0 {
		return 0
	}
	return cumulativeQuote / executedQty
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
