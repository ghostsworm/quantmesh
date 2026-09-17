package okx

import (
	"context"
	"fmt"
)

// SpotOrderWebSocketManager 現貨私有訂單流（orders，instType=SPOT）。
// 直接複用合約的 WebSocketManager：登錄確認後才訂閱、訂閱確認、讀超時、斷線指數退避重連並重新登錄訂閱、
// Stop 等待協程退出，只把訂閱的 instType 換成 SPOT，避免兩套實現各自漂移。
type SpotOrderWebSocketManager struct {
	ws     *WebSocketManager
	instId string
}

// NewSpotOrderWebSocketManager 創建現貨訂單流管理器（instId 如 BTC-USDT）
func NewSpotOrderWebSocketManager(apiKey, secretKey, passphrase string, useTestnet bool, instId string) *SpotOrderWebSocketManager {
	ws := NewWebSocketManager(apiKey, secretKey, passphrase, useTestnet)
	ws.SetOrderInstType(okxInstTypeSpot)
	return &SpotOrderWebSocketManager{ws: ws, instId: instId}
}

// Start 啟動現貨訂單 WebSocket（首次連接同步完成登錄與訂閱確認，之後自動重連）
func (w *SpotOrderWebSocketManager) Start(ctx context.Context, callback func(OrderUpdate)) error {
	if err := w.ws.Start(ctx, w.instId, callback); err != nil {
		return fmt.Errorf("OKX 現貨訂單流啟動失败(instId=%s): %w", w.instId, err)
	}
	return nil
}

// Stop 停止現貨訂單流（等待讀循環、心跳、重連協程退出）
func (w *SpotOrderWebSocketManager) Stop() {
	w.ws.Stop()
}
