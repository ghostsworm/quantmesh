package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ServiceStatusItem 單項服務狀態（前端用 id / message_key + message_params 做 i18n，name 僅作兼容）
type ServiceStatusItem struct {
	ID            string            `json:"id"`
	Name          string            `json:"name,omitempty"`
	Ok            bool              `json:"ok"`
	Message       string            `json:"message,omitempty"`        // 兼容舊版，前端優先使用 message_key
	MessageKey    string            `json:"message_key,omitempty"`   // 對應 i18n servicesStatus.message.*
	MessageParams map[string]string `json:"message_params,omitempty"` // 如 type, path
}

// ServicesStatusResponse 服務狀態響應
type ServicesStatusResponse struct {
	Services []ServiceStatusItem `json:"services"`
}

// getServicesStatus 返回各後台服務狀態（存儲、回測等）
// GET /api/services/status
func getServicesStatus(c *gin.Context) {
	var items []ServiceStatusItem

	// 1. 存儲（SQLite）
	storageOk := false
	storageMsgKey := ""
	storageParams := map[string]string{}
	if globalConfig != nil {
		if !globalConfig.Storage.Enabled {
			storageMsgKey = "storageDisabled"
		} else if globalConfig.Storage.Type == "" {
			storageMsgKey = "storageNotConfigured"
		} else if globalConfig.Storage.Type == "sqlite" && globalConfig.Storage.Path == "" {
			storageMsgKey = "storageNotConfigured"
		} else if globalConfig.Storage.Type == "mysql" && globalConfig.Storage.Path == "" && globalConfig.Database.DSN == "" {
			storageMsgKey = "storageNotConfigured"
		} else if storageServiceProvider == nil {
			storageMsgKey = "storageProviderNil"
		} else if storageServiceProvider.GetStorage() == nil {
			storageMsgKey = "storageInstanceNil"
		} else {
			storageOk = true
			if globalConfig.Storage.Path != "" {
				storageMsgKey = "normalWithPath"
				storageParams = map[string]string{"type": globalConfig.Storage.Type, "path": globalConfig.Storage.Path}
			} else {
				// MySQL 使用 database.dsn 時 path 為空
				storageMsgKey = "normalWithType"
				storageParams = map[string]string{"type": globalConfig.Storage.Type}
			}
		}
	} else {
		storageMsgKey = "configNotLoaded"
	}
	items = append(items, ServiceStatusItem{
		ID:            "storage",
		Ok:            storageOk,
		MessageKey:    storageMsgKey,
		MessageParams: storageParams,
	})

	// 2. 回測任務管理器
	backtestOk := backtestTaskManager != nil
	backtestMsgKey := "normal"
	if !backtestOk {
		backtestMsgKey = "backtestNotInit"
	}
	items = append(items, ServiceStatusItem{
		ID:         "backtest",
		Ok:         backtestOk,
		MessageKey: backtestMsgKey,
	})

	// 3. 智能參數推薦（可選，不影響回測提交）
	smartOk := smartParamsService != nil
	smartMsgKey := "normal"
	if !smartOk {
		smartMsgKey = "smartParamsNotInit"
	}
	items = append(items, ServiceStatusItem{
		ID:         "smart_params",
		Ok:         smartOk,
		MessageKey: smartMsgKey,
	})

	c.JSON(http.StatusOK, ServicesStatusResponse{Services: items})
}
