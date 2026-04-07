package storage

import (
	"sync"
	"time"

	"quantmesh/logger"
)

var (
	botRiskStorageMu     sync.RWMutex
	botRiskStorageGetter func() Storage
)

// SetBotRiskControlStorageGetter 在應用啟動後註冊（通常指向 StorageService.GetStorage()）。
func SetBotRiskControlStorageGetter(fn func() Storage) {
	botRiskStorageMu.Lock()
	defer botRiskStorageMu.Unlock()
	botRiskStorageGetter = fn
}

// AppendBotRiskControlEvent 異步寫入開倉風控暫停/恢復事件（失敗僅記錄調試日誌，不阻塞交易路徑）。
func AppendBotRiskControlEvent(botID, eventType, reason, source string) {
	if botID == "" {
		return
	}
	botRiskStorageMu.RLock()
	get := botRiskStorageGetter
	botRiskStorageMu.RUnlock()
	if get == nil {
		return
	}
	st := get()
	if st == nil {
		return
	}
	rec := &BotRiskControlEventRecord{
		BotID:     botID,
		EventType: eventType,
		Reason:    reason,
		Source:    source,
		CreatedAt: time.Now().UTC(),
	}
	go func() {
		if err := st.SaveBotRiskControlEvent(rec); err != nil {
			logger.Debug("保存 Bot 風控事件失敗: %v", err)
		}
	}()
}
