package web

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
	"quantmesh/storage"
)

// ConfigManagerAPI 配置管理 API
type ConfigManagerAPI struct {
	configStorage storage.ConfigStorage
}

// NewConfigManagerAPI 创建配置管理 API
func NewConfigManagerAPI(configStorage storage.ConfigStorage) *ConfigManagerAPI {
	return &ConfigManagerAPI{
		configStorage: configStorage,
	}
}

// ConfigEntryResponse 配置条目响应
type ConfigEntryResponse struct {
	storage.ConfigEntry
	TypedValue interface{} `json:"typed_value,omitempty"` // 类型化的值
}

// ConfigListResponse 配置列表响应
type ConfigListResponse struct {
	Total int                         `json:"total"`
	Items []*ConfigEntryResponse       `json:"items"`
}

// ConfigSetRequest 设置配置请求
type ConfigSetRequest struct {
	Key      string      `json:"key" binding:"required"`
	Scope    string      `json:"scope" binding:"required"`
	ScopeID  string      `json:"scope_id"`
	Type     string      `json:"type" binding:"required"`
	Value    interface{} `json:"value"`
	Reason   string      `json:"reason"`
}

// ConfigBatchSetRequest 批量设置配置请求
type ConfigBatchSetRequest struct {
	Entries []*ConfigSetRequest `json:"entries" binding:"required"`
	Reason  string              `json:"reason"`
}

// RegisterRoutes 注册路由
func (api *ConfigManagerAPI) RegisterRoutes(router *gin.RouterGroup) {
	config := router.Group("/config")
	{
		// 获取配置列表
		config.GET("", api.getConfigs)

		// 按作用域获取配置
		config.GET("/scope/:scope/:scope_id", api.getConfigsByScope)

		// 按分类获取配置
		config.GET("/category/:category", api.getConfigsByCategory)

		// 获取单个配置
		config.GET("/:scope/:scope_id/:key", api.getConfig)

		// 设置配置
		config.POST("", api.setConfig)

		// 批量设置配置
		config.POST("/batch", api.setConfigs)

		// 删除配置（恢复默认值）
		config.DELETE("/:scope/:scope_id/:key", api.deleteConfig)

		// 获取配置变更历史
		config.GET("/history/:scope/:scope_id/:key", api.getConfigHistory)
	}

	// 通知规则快捷设置
	notifications := router.Group("/notifications")
	{
		notifications.GET("/rules", api.getNotificationRules)
		notifications.PUT("/rules", api.setNotificationRules)
	}
}

// getConfigs 获取配置列表
func (api *ConfigManagerAPI) getConfigs(c *gin.Context) {
	scope := c.DefaultQuery("scope", "")
	scopeID := c.DefaultQuery("scope_id", "")
	category := c.DefaultQuery("category", "")

	var entries []*storage.ConfigEntry
	var err error

	if scope != "" && scopeID != "" {
		entries, err = api.configStorage.GetConfigsByScope(c, storage.ConfigScope(scope), scopeID)
	} else if scope != "" && category != "" {
		entries, err = api.configStorage.GetConfigsByCategory(c, storage.ConfigScope(scope), category)
	} else {
		entries, err = api.configStorage.GetAllConfigs(c)
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为响应格式
	response := &ConfigListResponse{
		Total: len(entries),
		Items: make([]*ConfigEntryResponse, len(entries)),
	}

	for i, entry := range entries {
		typedValue, _ := entry.GetTypedValue()
		response.Items[i] = &ConfigEntryResponse{
			ConfigEntry: *entry,
			TypedValue:  typedValue,
		}
	}

	c.JSON(http.StatusOK, response)
}

// getConfigsByScope 按作用域获取配置
func (api *ConfigManagerAPI) getConfigsByScope(c *gin.Context) {
	scope := c.Param("scope")
	scopeID := c.Param("scope_id")

	entries, err := api.configStorage.GetConfigsByScope(c, storage.ConfigScope(scope), scopeID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := make([]*ConfigEntryResponse, len(entries))
	for i, entry := range entries {
		typedValue, _ := entry.GetTypedValue()
		response[i] = &ConfigEntryResponse{
			ConfigEntry: *entry,
			TypedValue:  typedValue,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(response),
		"items": response,
	})
}

// getConfigsByCategory 按分类获取配置
func (api *ConfigManagerAPI) getConfigsByCategory(c *gin.Context) {
	scope := c.DefaultQuery("scope", "global")
	category := c.Param("category")

	entries, err := api.configStorage.GetConfigsByCategory(c, storage.ConfigScope(scope), category)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := make([]*ConfigEntryResponse, len(entries))
	for i, entry := range entries {
		typedValue, _ := entry.GetTypedValue()
		response[i] = &ConfigEntryResponse{
			ConfigEntry: *entry,
			TypedValue:  typedValue,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(response),
		"items": response,
	})
}

// getConfig 获取单个配置
func (api *ConfigManagerAPI) getConfig(c *gin.Context) {
	scope := c.Param("scope")
	scopeID := c.Param("scope_id")
	key := c.Param("key")

	entry, err := api.configStorage.GetConfig(c, storage.ConfigScope(scope), scopeID, key)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	if entry == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "配置不存在"})
		return
	}

	typedValue, _ := entry.GetTypedValue()

	response := &ConfigEntryResponse{
		ConfigEntry: *entry,
		TypedValue:  typedValue,
	}

	c.JSON(http.StatusOK, response)
}

// setConfig 设置配置
func (api *ConfigManagerAPI) setConfig(c *gin.Context) {
	var req ConfigSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	username := c.GetString("username")
	if username == "" {
		username = "unknown"
	}

	entry, err := storage.NewConfigEntry(
		storage.ConfigScope(req.Scope),
		req.ScopeID,
		req.Key,
		req.Value,
		"",
		"",
		"",
	)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if err := api.configStorage.SetConfig(c, entry, username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("配置已更新: %s.%s.%s = %v (操作人: %s)", req.Scope, req.ScopeID, req.Key, req.Value, username)

	c.JSON(http.StatusOK, gin.H{
		"message": "配置已更新",
		"entry":  entry,
	})
}

// setConfigs 批量设置配置
func (api *ConfigManagerAPI) setConfigs(c *gin.Context) {
	var req ConfigBatchSetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	username := c.GetString("username")
	if username == "" {
		username = "unknown"
	}

	entries := make([]*storage.ConfigEntry, len(req.Entries))
	for i, item := range req.Entries {
		entry, err := storage.NewConfigEntry(
			storage.ConfigScope(item.Scope),
			item.ScopeID,
			item.Key,
			item.Value,
			"",
			"",
			"",
		)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		entries[i] = entry
	}

	if err := api.configStorage.SetConfigs(c, entries, username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("批量更新配置: %d 条 (操作人: %s)", len(entries), username)

	c.JSON(http.StatusOK, gin.H{
		"message": "配置已批量更新",
		"total":   len(entries),
	})
}

// deleteConfig 删除配置
func (api *ConfigManagerAPI) deleteConfig(c *gin.Context) {
	scope := c.Param("scope")
	scopeID := c.Param("scope_id")
	key := c.Param("key")

	username := c.GetString("username")
	if username == "" {
		username = "unknown"
	}

	if err := api.configStorage.DeleteConfig(c, storage.ConfigScope(scope), scopeID, key); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("配置已删除: %s.%s.%s (操作人: %s)", scope, scopeID, key, username)

	c.JSON(http.StatusOK, gin.H{
		"message": "配置已删除，将恢复默认值",
	})
}

// getConfigHistory 获取配置变更历史
func (api *ConfigManagerAPI) getConfigHistory(c *gin.Context) {
	scope := c.Param("scope")
	scopeID := c.Param("scope_id")
	key := c.Param("key")

	limit := 50
	if l := c.Query("limit"); l != "" {
		if n, err := parseLimitParam(l); err == nil && n > 0 {
			limit = n
		}
	}

	history, err := api.configStorage.GetConfigHistoryByKey(c, storage.ConfigScope(scope), scopeID, key, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"total": len(history),
		"items": history,
	})
}

// getNotificationRules 获取通知规则
func (api *ConfigManagerAPI) getNotificationRules(c *gin.Context) {
	rules := []string{
		"notifications.enabled",
		"notifications.rules.order_placed",
		"notifications.rules.order_filled",
		"notifications.rules.error",
		"notifications.rules.margin_insufficient",
		"notifications.rules.risk_triggered",
		"notifications.rules.stop_loss",
		"notifications.rules.allocation_exceeded",
		"notifications.rules.inspector_report",
	}

	entries, err := api.configStorage.GetConfigByKeys(c, storage.ScopeGlobal, "", rules)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	result := make(map[string]interface{})
	for _, entry := range entries {
		val, _ := entry.GetTypedValue()
		result[entry.Key] = val
	}

	c.JSON(http.StatusOK, result)
}

// NotificationRulesRequest 通知规则请求
type NotificationRulesRequest struct {
	Enabled              *bool `json:"enabled"`
	OrderPlaced          *bool `json:"order_placed"`
	OrderFilled          *bool `json:"order_filled"`
	Error                *bool `json:"error"`
	MarginInsufficient   *bool `json:"margin_insufficient"`
	RiskTriggered        *bool `json:"risk_triggered"`
	StopLoss             *bool `json:"stop_loss"`
	AllocationExceeded   *bool `json:"allocation_exceeded"`
	InspectorReport      *bool `json:"inspector_report"`
}

// setNotificationRules 设置通知规则
func (api *ConfigManagerAPI) setNotificationRules(c *gin.Context) {
	var req NotificationRulesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	username := c.GetString("username")
	if username == "" {
		username = "unknown"
	}

	// 构建配置条目列表
	var entries []*storage.ConfigEntry

	if req.Enabled != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.enabled", *req.Enabled, "notifications", "启用通知", "")
		entries = append(entries, entry)
	}
	if req.OrderPlaced != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.order_placed", *req.OrderPlaced, "notifications", "下单时通知", "")
		entries = append(entries, entry)
	}
	if req.OrderFilled != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.order_filled", *req.OrderFilled, "notifications", "成交时通知", "")
		entries = append(entries, entry)
	}
	if req.Error != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.error", *req.Error, "notifications", "错误时通知", "")
		entries = append(entries, entry)
	}
	if req.MarginInsufficient != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.margin_insufficient", *req.MarginInsufficient, "notifications", "保证金不足通知", "")
		entries = append(entries, entry)
	}
	if req.RiskTriggered != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.risk_triggered", *req.RiskTriggered, "notifications", "风控触发通知", "")
		entries = append(entries, entry)
	}
	if req.StopLoss != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.stop_loss", *req.StopLoss, "notifications", "止损通知", "")
		entries = append(entries, entry)
	}
	if req.AllocationExceeded != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.allocation_exceeded", *req.AllocationExceeded, "notifications", "分配超限通知", "")
		entries = append(entries, entry)
	}
	if req.InspectorReport != nil {
		entry, _ := storage.NewConfigEntry(storage.ScopeGlobal, "", "notifications.rules.inspector_report", *req.InspectorReport, "notifications", "巡检报告通知", "")
		entries = append(entries, entry)
	}

	if len(entries) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "没有要更新的配置"})
		return
	}

	if err := api.configStorage.SetConfigs(c, entries, username); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("通知规则已更新: %d 条 (操作人: %s)", len(entries), username)

	c.JSON(http.StatusOK, gin.H{
		"message": "通知规则已更新",
		"total":   len(entries),
	})
}

// parseLimitParam 解析限制参数
func parseLimitParam(s string) (int, error) {
	var result int
	if _, err := fmt.Sscanf(s, "%d", &result); err != nil {
		return 0, err
	}
	return result, nil
}
