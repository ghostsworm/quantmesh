package binance

import (
	"errors"
	"sync"

	"github.com/adshao/go-binance/v2/common"
)

// 連線狀態事件（R2 遺留：熔斷器訂閱了 WS 斷線 / 認證失敗事件，但交易所層從未發布）。
// 本包不能引用 event 包的總線實例，由 main 通過 SetConnectivityEventHandler 注入轉發函數。

// ConnectivityEventType 連線事件類型
type ConnectivityEventType string

const (
	// ConnectivityDisconnected 用戶數據流斷開或連接失敗（一次斷線期間只上報一次）
	ConnectivityDisconnected ConnectivityEventType = "disconnected"
	// ConnectivityReconnected 斷線後重新連上
	ConnectivityReconnected ConnectivityEventType = "reconnected"
	// ConnectivityStopped 斷線期間訂單流被主動停止（外部應清除斷線計時，避免誤判長時間斷線）
	ConnectivityStopped ConnectivityEventType = "stopped"
	// ConnectivityAuthFailed API Key / 簽名 / IP 白名單等認證失敗
	ConnectivityAuthFailed ConnectivityEventType = "auth_failed"
)

const (
	// connectivityExchangeName 事件中的交易所名稱
	connectivityExchangeName = "binance"
	// connectivityStreamUserData 事件中的流名稱：合約用戶數據流
	connectivityStreamUserData = "futures_user_data"

	// Binance 認證類錯誤碼
	errCodeUnauthorized     = -1002 // 未授權
	errCodeInvalidSignature = -1022 // 簽名無效
	errCodeBadAPIKeyFormat  = -2014 // API Key 格式錯誤
	errCodeRejectedMBXKey   = -2015 // API Key 無效 / IP 不在白名單 / 無權限
)

// ConnectivityEvent 連線事件
type ConnectivityEvent struct {
	Type     ConnectivityEventType
	Exchange string
	Stream   string
	Symbol   string
	Testnet  bool
	Reason   string
}

// ConnectivityEventHandler 連線事件處理函數，需快速返回（在 WS 協程內同步調用）
type ConnectivityEventHandler func(ConnectivityEvent)

var (
	connectivityHandlerMu sync.RWMutex
	connectivityHandler   ConnectivityEventHandler
)

// SetConnectivityEventHandler 設置進程級連線事件處理函數；傳 nil 取消
func SetConnectivityEventHandler(fn ConnectivityEventHandler) {
	connectivityHandlerMu.Lock()
	defer connectivityHandlerMu.Unlock()
	connectivityHandler = fn
}

func emitConnectivityEvent(evt ConnectivityEvent) {
	connectivityHandlerMu.RLock()
	fn := connectivityHandler
	connectivityHandlerMu.RUnlock()
	if fn == nil {
		return
	}
	if evt.Exchange == "" {
		evt.Exchange = connectivityExchangeName
	}
	fn(evt)
}

// isBinanceAuthError 判斷是否為認證類錯誤（-1002 / -1022 / -2014 / -2015）
func isBinanceAuthError(err error) bool {
	var apiErr *common.APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Code {
	case errCodeUnauthorized, errCodeInvalidSignature, errCodeBadAPIKeyFormat, errCodeRejectedMBXKey:
		return true
	}
	return false
}
