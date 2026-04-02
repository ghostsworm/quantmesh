package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"quantmesh/database"
	"quantmesh/event"

	"github.com/gin-gonic/gin"

	// qmi18n "quantmesh/i18n" // TODO: 等待 RegisterMessages 實現后啟用
	"quantmesh/logger"
)

// EventProvider 事件數據提供者接口
type EventProvider interface {
	GetEvents(ctx context.Context, filter *database.EventFilter) ([]*database.EventRecord, error)
	GetEventByID(ctx context.Context, id int64) (*database.EventRecord, error)
	GetEventStats(ctx context.Context) (*database.EventStats, error)
}

// EventCenterController 事件中心控制器接口
type EventCenterController interface {
	Start() error
	Stop()
	IsRunning() bool
}

var eventProvider EventProvider
var globalEventCenterController EventCenterController

// SetEventProvider 設置事件提供者
func SetEventProvider(provider EventProvider) {
	eventProvider = provider
}

// SetEventCenterController 設置事件中心控制器
func SetEventCenterController(controller EventCenterController) {
	globalEventCenterController = controller
}

// handleGetEvents 獲取事件列表
// @Summary 獲取事件列表
// @Description 獲取系统事件列表，支援按類型、严重程度等筛选
// @Tags Events
// @Accept json
// @Produce json
// @Param type query string false "事件類型"
// @Param severity query string false "严重程度 (critical/warning/info)"
// @Param source query string false "事件源 (exchange/network/system/strategy/risk/api)"
// @Param exchange query string false "交易所"
// @Param symbol query string false "交易對"
// @Param start_time query string false "开始時间 (RFC3339)"
// @Param end_time query string false "結束時间 (RFC3339)"
// @Param limit query int false "限制數量" default(100)
// @Param offset query int false "偏移量" default(0)
// @Success 200 {object} map[string]interface{}
// @Router /api/events [get]
func handleGetEvents(c *gin.Context) {
	if eventProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.event_service_unavailable")
		return
	}

	// 解析查詢参數
	filter := &database.EventFilter{
		Type:     c.Query("type"),
		Severity: c.Query("severity"),
		Source:   c.Query("source"),
		Exchange: c.Query("exchange"),
		Symbol:   c.Query("symbol"),
	}

	// 解析時间範圍
	if startTimeStr := c.Query("start_time"); startTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, startTimeStr); err == nil {
			filter.StartTime = &t
		}
	}
	if endTimeStr := c.Query("end_time"); endTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, endTimeStr); err == nil {
			filter.EndTime = &t
		}
	}

	// 解析分页参數
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	filter.Limit = limit
	filter.Offset = offset

	// 查詢事件
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := eventProvider.GetEvents(ctx, filter)
	if err != nil {
		logger.Error("❌ 查詢事件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "errors.query_events_failed", err)
		return
	}

	for _, ev := range events {
		enrichEventRecord(ev)
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
	})
}

// handleGetEventDetail 獲取事件详情
// @Summary 獲取事件详情
// @Description 根據ID獲取事件详细信息
// @Tags Events
// @Accept json
// @Produce json
// @Param id path int true "事件ID"
// @Success 200 {object} database.EventRecord
// @Router /api/events/{id} [get]
func handleGetEventDetail(c *gin.Context) {
	if eventProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.event_service_unavailable")
		return
	}

	// 解析事件ID
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		respondError(c, http.StatusBadRequest, "errors.invalid_event_id")
		return
	}

	// 查詢事件
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	ev, err := eventProvider.GetEventByID(ctx, id)
	if err != nil {
		logger.Error("❌ 查詢事件详情失败: %v", err)
		respondError(c, http.StatusNotFound, "errors.event_not_found")
		return
	}

	enrichEventRecord(ev)
	c.JSON(http.StatusOK, ev)
}

// handleGetEventStats 獲取事件统计
// @Summary 獲取事件统计
// @Description 獲取事件中心统计信息
// @Tags Events
// @Accept json
// @Produce json
// @Success 200 {object} database.EventStats
// @Router /api/events/stats [get]
func handleGetEventStats(c *gin.Context) {
	if eventProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.event_service_unavailable")
		return
	}

	// 查詢统计（事件表大時單次聚合仍可能較慢，略放寬超時）
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	stats, err := eventProvider.GetEventStats(ctx)
	if err != nil {
		logger.Error("❌ 查詢事件统计失败: %v", err)
		respondError(c, http.StatusInternalServerError, "errors.query_stats_failed", err)
		return
	}

	c.JSON(http.StatusOK, stats)
}

// handleGetEventCenterStatus 獲取事件中心状態
// @Summary 獲取事件中心状態
// @Description 獲取事件中心是否啟用的状態
// @Tags Events
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{}
// @Router /api/events/center/status [get]
func handleGetEventCenterStatus(c *gin.Context) {
	enabled := false
	if globalEventCenterController != nil {
		enabled = globalEventCenterController.IsRunning()
	}
	c.JSON(http.StatusOK, gin.H{
		"enabled": enabled,
	})
}

// handleSetEventCenterStatus 設置事件中心状態
// @Summary 設置事件中心状態
// @Description 动態啟用或禁用事件中心
// @Tags Events
// @Accept json
// @Produce json
// @Param request body map[string]bool true "状態"
// @Success 200 {object} map[string]interface{}
// @Router /api/events/center/status [post]
func handleSetEventCenterStatus(c *gin.Context) {
	var req struct {
		Enabled bool `json:"enabled"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "errors.invalid_request")
		return
	}

	// 調用事件中心控制器
	if globalEventCenterController == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.event_center_unavailable")
		return
	}

	if globalEventCenterController != nil {
		if req.Enabled {
			if err := globalEventCenterController.Start(); err != nil {
				logger.Error("❌ 啟动事件中心失败: %v", err)
				respondError(c, http.StatusInternalServerError, "errors.start_event_center_failed", err)
				return
			}
			logger.Info("✅ 事件中心已啟动")
		} else {
			globalEventCenterController.Stop()
			logger.Info("⏸️ 事件中心已停止")
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"enabled": req.Enabled,
		"message": map[bool]string{true: "事件中心已啟动", false: "事件中心已停止"}[req.Enabled],
	})
}

// enrichEventRecord 補齊 Storage 寫入的舊事件（type/source/title/message 為空但 event_type/data 有值時）
func enrichEventRecord(record *database.EventRecord) {
	if record == nil {
		return
	}
	if record.Type != "" {
		return
	}
	if record.EventTypeRaw == "" {
		return
	}
	record.Type = record.EventTypeRaw
	et := event.EventType(record.EventTypeRaw)
	record.Severity = string(event.GetEventSeverity(et))
	record.Source = string(event.GetEventSource(et))
	record.Title = event.GetEventTitle(et)

	var data map[string]interface{}
	if record.DataRaw != "" {
		_ = json.Unmarshal([]byte(record.DataRaw), &data)
	}
	record.Message = event.BuildMessageFromData(et, data)
	if record.Message == "" {
		record.Message = "事件類型: " + record.Type
	}
	if record.Details == "" && record.DataRaw != "" {
		record.Details = record.DataRaw
	}
}

// registerEventRoutes 注册事件相关路由
func registerEventRoutes(r *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	events := r.Group("/events")
	events.Use(authMiddleware)
	{
		events.GET("", handleGetEvents)
		events.GET("/stats", handleGetEventStats)
		events.GET("/:id", handleGetEventDetail)
		events.GET("/center/status", handleGetEventCenterStatus)
		events.POST("/center/status", handleSetEventCenterStatus)
	}
}

// 添加国際化錯误消息
// TODO: 等待 qmi18n.RegisterMessages 函數實現后啟用
/*
func init() {
	// 注册中文錯误消息
	qmi18n.RegisterMessages("zh-CN", map[string]string{
		"errors.event_service_unavailable": "事件服務不可用",
		"errors.invalid_event_id":          "無效的事件ID",
		"errors.event_not_found":           "事件不存在",
		"errors.query_events_failed":       "查詢事件失败",
		"errors.query_stats_failed":        "查詢统计失败",
	})

	// 注册英文錯误消息
	qmi18n.RegisterMessages("en-US", map[string]string{
		"errors.event_service_unavailable": "Event service unavailable",
		"errors.invalid_event_id":          "Invalid event ID",
		"errors.event_not_found":           "Event not found",
		"errors.query_events_failed":       "Failed to query events",
		"errors.query_stats_failed":        "Failed to query statistics",
	})
}
*/
