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

// SpotWebSocketManager 幣安現貨 WebSocket 管理器
type SpotWebSocketManager struct {
	useTestnet bool

	// 價格緩存
	latestPrice float64
	priceMu     sync.RWMutex

	// 控制
	stopC chan struct{}
	mu    sync.RWMutex
}

// NewSpotWebSocketManager 創建現貨 WebSocket 管理器
func NewSpotWebSocketManager(useTestnet bool) *SpotWebSocketManager {
	return &SpotWebSocketManager{
		useTestnet: useTestnet,
		stopC:      make(chan struct{}),
	}
}

// StartPriceStream 啟動現貨價格流（aggTrade）
func (w *SpotWebSocketManager) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	// 現貨 WebSocket 端點：
	// 主網: wss://stream.binance.com:9443/ws/<symbol>@aggTrade
	// 測試網: wss://stream.testnet.binance.vision/ws/<symbol>@aggTrade
	// 注意：測試網需要使用 stream.testnet.binance.vision，而非 testnet.binance.vision

	symbolLower := strings.ToLower(symbol)
	var url string
	if w.useTestnet {
		url = fmt.Sprintf("wss://stream.testnet.binance.vision/ws/%s@aggTrade", symbolLower)
		logger.Info("🌐 [Binance Spot WS] 使用測試網 WebSocket: %s", url)
	} else {
		url = fmt.Sprintf("wss://stream.binance.com:9443/ws/%s@aggTrade", symbolLower)
	}

	// 使用通道等待首個價格
	firstPriceCh := make(chan struct{})
	firstPriceReceived := false
	errCh := make(chan error, 1)

	go func() {
		for {
			select {
			case <-ctx.Done():
				logger.Info("✅ [Binance Spot] 價格流已停止（上下文取消）")
				return
			case <-w.stopC:
				logger.Info("✅ [Binance Spot] 價格流已停止（手動停止）")
				return
			default:
			}

			logger.Debug("🔗 [Binance Spot] 正在連接 WebSocket: %s", url)

			conn, _, err := websocket.DefaultDialer.Dial(url, nil)
			if err != nil {
				logger.Error("❌ [Binance Spot] WebSocket 連接失敗: %v，5秒後重試", err)
				if !firstPriceReceived {
					select {
					case errCh <- fmt.Errorf("WebSocket 連接失敗: %w", err):
					default:
					}
				}
				time.Sleep(5 * time.Second)
				continue
			}

			logger.Info("✅ [Binance Spot] WebSocket 已連接: %s", url)

			// 讀取消息循環
			for {
				select {
				case <-ctx.Done():
					conn.Close()
					logger.Info("✅ [Binance Spot] 價格流已停止")
					return
				case <-w.stopC:
					conn.Close()
					logger.Info("✅ [Binance Spot] 價格流已停止")
					return
				default:
				}

				_, message, err := conn.ReadMessage()
				if err != nil {
					logger.Warn("⚠️ [Binance Spot] WebSocket 讀取錯誤: %v，正在重連", err)
					conn.Close()
					time.Sleep(2 * time.Second)
					break // 跳出內層循環，重新連接
				}

				// 解析消息
				// aggTrade 消息格式：
				// {"e":"aggTrade","E":1234567890,"s":"PAXGUSDT","a":12345,"p":"2950.00","q":"0.5","f":100,"l":105,"T":1234567890,"m":false,"M":true}
				var event struct {
					Symbol string `json:"s"` // 交易對
					Price  string `json:"p"` // 價格
				}

				if err := json.Unmarshal(message, &event); err != nil {
					logger.Debug("[Binance Spot] 解析消息失敗: %v", err)
					continue
				}

				price, err := strconv.ParseFloat(event.Price, 64)
				if err != nil {
					logger.Debug("[Binance Spot] 解析價格失敗: %v", err)
					continue
				}

				// 更新價格緩存
				w.priceMu.Lock()
				w.latestPrice = price
				w.priceMu.Unlock()

				// 通知首個價格已接收
				if !firstPriceReceived {
					firstPriceReceived = true
					logger.Debug("✅ [Binance Spot] 收到首個價格: %.2f", price)
					close(firstPriceCh)
				}

				// 調用回呼
				callback(price)
			}
		}
	}()

	// 等待接收首個價格（最多10秒）
	select {
	case <-firstPriceCh:
		logger.Debug("✅ [Binance Spot] 價格流已啟動: %s@aggTrade", symbolLower)
		return nil
	case err := <-errCh:
		return err
	case <-time.After(10 * time.Second):
		return fmt.Errorf("等待首個價格超時（10秒）")
	case <-ctx.Done():
		return fmt.Errorf("上下文已取消")
	}
}

// Stop 停止 WebSocket
func (w *SpotWebSocketManager) Stop() {
	w.mu.Lock()
	defer w.mu.Unlock()

	select {
	case <-w.stopC:
		// 已經關閉
	default:
		close(w.stopC)
	}
}

// GetLatestPrice 獲取最新價格（從緩存讀取）
func (w *SpotWebSocketManager) GetLatestPrice() float64 {
	w.priceMu.RLock()
	defer w.priceMu.RUnlock()
	return w.latestPrice
}
