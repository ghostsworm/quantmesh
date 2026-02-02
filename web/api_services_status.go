package web

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ServiceStatusItem 單項服務狀態
type ServiceStatusItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Ok      bool   `json:"ok"`
	Message string `json:"message,omitempty"`
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
	storageMsg := ""
	if globalConfig != nil {
		if !globalConfig.Storage.Enabled {
			storageMsg = "配置中 storage.enabled 為關閉；請在「設置」-「存儲與 Web」中開啟「啟用數據存儲」並保存後重啟"
		} else if globalConfig.Storage.Path == "" || globalConfig.Storage.Type == "" {
			storageMsg = "配置中未設置 storage.path 或 storage.type"
		} else if storageServiceProvider == nil {
			storageMsg = "存儲服務提供者未注入（進程啟動時未初始化）"
		} else if storageServiceProvider.GetStorage() == nil {
			storageMsg = "存儲實例為空（可能初始化失敗，請查看啟動日誌中的 SQLite/創建表 錯誤）"
		} else {
			storageOk = true
			storageMsg = "正常 (" + globalConfig.Storage.Type + " @ " + globalConfig.Storage.Path + ")"
		}
	} else {
		storageMsg = "配置未加載"
	}
	items = append(items, ServiceStatusItem{
		ID:      "storage",
		Name:    "數據存儲 (SQLite)",
		Ok:      storageOk,
		Message: storageMsg,
	})

	// 2. 回測任務管理器
	backtestOk := backtestTaskManager != nil
	backtestMsg := ""
	if backtestOk {
		backtestMsg = "正常"
	} else {
		backtestMsg = "回測服務未初始化；請確保存儲已啟用且初始化成功（見上方「數據存儲」狀態），保存配置後重啟服務"
	}
	items = append(items, ServiceStatusItem{
		ID:      "backtest",
		Name:    "回測服務",
		Ok:      backtestOk,
		Message: backtestMsg,
	})

	// 3. 智能參數推薦（可選，不影響回測提交）
	smartOk := smartParamsService != nil
	smartMsg := "正常"
	if !smartOk {
		smartMsg = "未初始化（不影響回測提交，僅影響參數推薦）"
	}
	items = append(items, ServiceStatusItem{
		ID:      "smart_params",
		Name:    "智能參數推薦",
		Ok:      smartOk,
		Message: smartMsg,
	})

	c.JSON(http.StatusOK, ServicesStatusResponse{Services: items})
}
