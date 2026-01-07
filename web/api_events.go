package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/database"
	// qmi18n "quantmesh/i18n" // TODO: 等待 RegisterMessages 实现后启用
	"quantmesh/logger"
)

// EventProvider 事件数据提供者接口
type EventProvider interface {
	GetEvents(ctx context.Context, filter *database.EventFilter) ([]*database.EventRecord, error)
	GetEventByID(ctx context.Context, id int64) (*database.EventRecord, error)
	GetEventStats(ctx context.Context) (*database.EventStats, error)
}

var eventProvider EventProvider

// SetEventProvider 设置事件提供者
func SetEventProvider(provider EventProvider) {
	eventProvider = provider
}

// handleGetEvents 获取事件列表
// @Summary 获取事件列表
// @Description 获取系统事件列表，支持按类型、严重程度等筛选
// @Tags Events
// @Accept json
// @Produce json
// @Param type query string false "事件类型"
// @Param severity query string false "严重程度 (critical/warning/info)"
// @Param source query string false "事件源 (exchange/network/system/strategy/risk/api)"
// @Param exchange query string false "交易所"
// @Param symbol query string false "交易对"
// @Param start_time query string false "开始时间 (RFC3339)"
// @Param end_time query string false "结束时间 (RFC3339)"
// @Param limit query int false "限制数量" default(100)
// @Param offset query int false "偏移量" default(0)
// @Success 200 {object} map[string]interface{}
// @Router /api/events [get]
func handleGetEvents(c *gin.Context) {
	if eventProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.event_service_unavailable")
		return
	}

	// 解析查询参数
	filter := &database.EventFilter{
		Type:     c.Query("type"),
		Severity: c.Query("severity"),
		Source:   c.Query("source"),
		Exchange: c.Query("exchange"),
		Symbol:   c.Query("symbol"),
	}

	// 解析时间范围
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

	// 解析分页参数
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "100"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	filter.Limit = limit
	filter.Offset = offset

	// 查询事件
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	events, err := eventProvider.GetEvents(ctx, filter)
	if err != nil {
		logger.Error("❌ 查询事件失败: %v", err)
		respondError(c, http.StatusInternalServerError, "errors.query_events_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"events": events,
		"count":  len(events),
	})
}

// handleGetEventDetail 获取事件详情
// @Summary 获取事件详情
// @Description 根据ID获取事件详细信息
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

	// 查询事件
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	event, err := eventProvider.GetEventByID(ctx, id)
	if err != nil {
		logger.Error("❌ 查询事件详情失败: %v", err)
		respondError(c, http.StatusNotFound, "errors.event_not_found")
		return
	}

	c.JSON(http.StatusOK, event)
}

// handleGetEventStats 获取事件统计
// @Summary 获取事件统计
// @Description 获取事件中心统计信息
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

	// 查询统计
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stats, err := eventProvider.GetEventStats(ctx)
	if err != nil {
		logger.Error("❌ 查询事件统计失败: %v", err)
		respondError(c, http.StatusInternalServerError, "errors.query_stats_failed", err)
		return
	}

	c.JSON(http.StatusOK, stats)
}

// registerEventRoutes 注册事件相关路由
func registerEventRoutes(r *gin.RouterGroup, authMiddleware gin.HandlerFunc) {
	events := r.Group("/events")
	events.Use(authMiddleware)
	{
		events.GET("", handleGetEvents)
		events.GET("/stats", handleGetEventStats)
		events.GET("/:id", handleGetEventDetail)
	}
}

// 添加国际化错误消息
// TODO: 等待 qmi18n.RegisterMessages 函数实现后启用
/*
func init() {
	// 注册中文错误消息
	qmi18n.RegisterMessages("zh-CN", map[string]string{
		"errors.event_service_unavailable": "事件服务不可用",
		"errors.invalid_event_id":          "无效的事件ID",
		"errors.event_not_found":           "事件不存在",
		"errors.query_events_failed":       "查询事件失败",
		"errors.query_stats_failed":        "查询统计失败",
	})

	// 注册英文错误消息
	qmi18n.RegisterMessages("en-US", map[string]string{
		"errors.event_service_unavailable": "Event service unavailable",
		"errors.invalid_event_id":          "Invalid event ID",
		"errors.event_not_found":           "Event not found",
		"errors.query_events_failed":       "Failed to query events",
		"errors.query_stats_failed":        "Failed to query statistics",
	})
}
*/

