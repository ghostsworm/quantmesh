package event

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"quantmesh/database"
	"quantmesh/logger"
)

// EventCenter 事件中心
type EventCenter struct {
	db                       database.Database
	eventBus                 *EventBus
	notifier                 NotificationService
	config                   *EventCenterConfig
	ctx                      context.Context
	cancel                   context.CancelFunc
	wg                       sync.WaitGroup
	priceVolatilityThreshold float64
	monitoredSymbols         map[string]bool
}

// EventCenterConfig 事件中心配置
type EventCenterConfig struct {
	Enabled                  bool
	PriceVolatilityThreshold float64
	MonitoredSymbols         []string
	CleanupInterval          int
	Retention                RetentionConfig
}

// RetentionConfig 保留策略配置
type RetentionConfig struct {
	CriticalDays     int
	WarningDays      int
	InfoDays         int
	CriticalMaxCount int
	WarningMaxCount  int
	InfoMaxCount     int
}

// NotificationService 通知服务接口
type NotificationService interface {
	Send(event *Event)
}

// NewEventCenter 创建事件中心
func NewEventCenter(db database.Database, eventBus *EventBus, notifier NotificationService, config *EventCenterConfig) *EventCenter {
	ctx, cancel := context.WithCancel(context.Background())
	
	// 构建监控交易对映射
	monitoredSymbols := make(map[string]bool)
	for _, symbol := range config.MonitoredSymbols {
		monitoredSymbols[symbol] = true
	}
	
	ec := &EventCenter{
		db:                       db,
		eventBus:                 eventBus,
		notifier:                 notifier,
		config:                   config,
		ctx:                      ctx,
		cancel:                   cancel,
		priceVolatilityThreshold: config.PriceVolatilityThreshold,
		monitoredSymbols:         monitoredSymbols,
	}
	
	return ec
}

// Start 启动事件中心
func (ec *EventCenter) Start() error {
	if !ec.config.Enabled {
		logger.Info("⏸️ 事件中心未启用")
		return nil
	}
	
	logger.Info("🚀 启动事件中心...")
	
	// 启动事件处理协程
	ec.wg.Add(1)
	go ec.processEvents()
	
	// 启动清理任务
	ec.wg.Add(1)
	go ec.cleanupTask()
	
	logger.Info("✅ 事件中心已启动")
	return nil
}

// Stop 停止事件中心
func (ec *EventCenter) Stop() {
	logger.Info("🛑 停止事件中心...")
	ec.cancel()
	ec.wg.Wait()
	logger.Info("✅ 事件中心已停止")
}

// processEvents 处理事件
func (ec *EventCenter) processEvents() {
	defer ec.wg.Done()
	
	eventCh := ec.eventBus.Subscribe()
	
	for {
		select {
		case <-ec.ctx.Done():
			return
		case event, ok := <-eventCh:
			if !ok {
				return
			}
			ec.handleEvent(event)
		}
	}
}

// handleEvent 处理单个事件
func (ec *EventCenter) handleEvent(event *Event) {
	if event == nil {
		return
	}
	
	// 获取事件元数据
	severity := GetEventSeverity(event.Type)
	source := GetEventSource(event.Type)
	title := GetEventTitle(event.Type)
	
	// 提取交易所和交易对信息
	exchange := ec.extractString(event.Data, "exchange")
	symbol := ec.extractString(event.Data, "symbol")
	
	// 构建消息
	message := ec.buildMessage(event)
	
	// 序列化详细信息
	detailsJSON, err := json.Marshal(event.Data)
	if err != nil {
		logger.Warn("⚠️ 序列化事件详情失败: %v", err)
		detailsJSON = []byte("{}")
	}
	
	// 保存到数据库
	record := &database.EventRecord{
		Type:      string(event.Type),
		Severity:  string(severity),
		Source:    string(source),
		Exchange:  exchange,
		Symbol:    symbol,
		Title:     title,
		Message:   message,
		Details:   string(detailsJSON),
		CreatedAt: event.Timestamp,
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	if err := ec.db.SaveEvent(ctx, record); err != nil {
		logger.Error("❌ 保存事件失败: %v", err)
		return
	}
	
	// 触发通知（如果需要）
	if ec.shouldNotify(event.Type, severity) {
		ec.notifier.Send(event)
	}
}

// extractString 从事件数据中提取字符串字段
func (ec *EventCenter) extractString(data map[string]interface{}, key string) string {
	if val, ok := data[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}

// buildMessage 构建事件消息
func (ec *EventCenter) buildMessage(event *Event) string {
	switch event.Type {
	case EventTypeOrderPlaced, EventTypeOrderFilled, EventTypeOrderCanceled, EventTypeOrderFailed:
		return ec.buildOrderMessage(event)
	case EventTypeWebSocketDisconnected, EventTypeWebSocketReconnected:
		return ec.buildWebSocketMessage(event)
	case EventTypeAPIRateLimited, EventTypeAPIServerError, EventTypeAPIRequestFailed:
		return ec.buildAPIMessage(event)
	case EventTypePriceVolatility:
		return ec.buildPriceVolatilityMessage(event)
	case EventTypeSystemCPUHigh, EventTypeSystemMemoryHigh:
		return ec.buildSystemResourceMessage(event)
	case EventTypePrecisionAdjustment:
		return ec.buildPrecisionAdjustmentMessage(event)
	default:
		if msg, ok := event.Data["message"].(string); ok {
			return msg
		}
		if err, ok := event.Data["error"].(string); ok {
			return err
		}
		return fmt.Sprintf("事件类型: %s", event.Type)
	}
}

// buildOrderMessage 构建订单消息
func (ec *EventCenter) buildOrderMessage(event *Event) string {
	symbol := ec.extractString(event.Data, "symbol")
	side := ec.extractString(event.Data, "side")
	price := event.Data["price"]
	quantity := event.Data["quantity"]
	
	return fmt.Sprintf("%s %s %.8f @ %.2f", symbol, side, quantity, price)
}

// buildWebSocketMessage 构建 WebSocket 消息
func (ec *EventCenter) buildWebSocketMessage(event *Event) string {
	exchange := ec.extractString(event.Data, "exchange")
	symbol := ec.extractString(event.Data, "symbol")
	reason := ec.extractString(event.Data, "reason")
	
	if reason != "" {
		return fmt.Sprintf("%s %s WebSocket: %s", exchange, symbol, reason)
	}
	return fmt.Sprintf("%s %s WebSocket 连接状态变化", exchange, symbol)
}

// buildAPIMessage 构建 API 消息
func (ec *EventCenter) buildAPIMessage(event *Event) string {
	exchange := ec.extractString(event.Data, "exchange")
	endpoint := ec.extractString(event.Data, "endpoint")
	errorMsg := ec.extractString(event.Data, "error")
	
	if endpoint != "" {
		return fmt.Sprintf("%s API [%s]: %s", exchange, endpoint, errorMsg)
	}
	return fmt.Sprintf("%s API 错误: %s", exchange, errorMsg)
}

// buildPriceVolatilityMessage 构建价格波动消息
func (ec *EventCenter) buildPriceVolatilityMessage(event *Event) string {
	symbol := ec.extractString(event.Data, "symbol")
	oldPrice := event.Data["old_price"]
	newPrice := event.Data["new_price"]
	changePercent := event.Data["change_percent"]
	
	return fmt.Sprintf("%s 价格波动: %.2f → %.2f (%.2f%%)", 
		symbol, oldPrice, newPrice, changePercent)
}

// buildSystemResourceMessage 构建系统资源消息
func (ec *EventCenter) buildSystemResourceMessage(event *Event) string {
	resourceType := ec.extractString(event.Data, "resource_type")
	usage := event.Data["usage"]
	threshold := event.Data["threshold"]
	
	return fmt.Sprintf("%s 使用率 %.2f%% (阈值: %.2f%%)", 
		resourceType, usage, threshold)
}

// buildPrecisionAdjustmentMessage 构建精度调整消息
func (ec *EventCenter) buildPrecisionAdjustmentMessage(event *Event) string {
	symbol := ec.extractString(event.Data, "symbol")
	calculatedQty := event.Data["calculated_qty"]
	minQty := event.Data["min_qty"]
	action := ec.extractString(event.Data, "action")
	
	if action == "pause" {
		return fmt.Sprintf("[%s] 下单数量 %.8f 低于最小精度 %.8f，交易已自动暂停", 
			symbol, calculatedQty, minQty)
	}
	return fmt.Sprintf("[%s] 下单数量精度调整: %.8f -> %.8f", 
		symbol, calculatedQty, minQty)
}

// shouldNotify 判断是否需要发送通知
func (ec *EventCenter) shouldNotify(eventType EventType, severity EventSeverity) bool {
	// Critical 级别的事件总是通知
	if severity == SeverityCritical {
		return true
	}
	
	// Warning 级别的某些重要事件需要通知
	if severity == SeverityWarning {
		switch eventType {
		case EventTypeAPIRateLimited, EventTypePriceVolatility, EventTypeAPIRequestFailed, EventTypePrecisionAdjustment:
			return true
		}
	}
	
	// Info 级别的事件通常不通知（除非在通知配置中明确启用）
	return false
}

// cleanupTask 清理任务
func (ec *EventCenter) cleanupTask() {
	defer ec.wg.Done()
	
	// 首次等待1小时后再开始清理
	timer := time.NewTimer(1 * time.Hour)
	defer timer.Stop()
	
	for {
		select {
		case <-ec.ctx.Done():
			return
		case <-timer.C:
			ec.performCleanup()
			// 重置定时器
			timer.Reset(time.Duration(ec.config.CleanupInterval) * time.Hour)
		}
	}
}

// performCleanup 执行清理
func (ec *EventCenter) performCleanup() {
	logger.Info("🧹 开始清理旧事件...")
	
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	
	// 清理 Critical 事件
	if err := ec.db.CleanupOldEvents(ctx, "critical", 
		ec.config.Retention.CriticalMaxCount, 
		ec.config.Retention.CriticalDays); err != nil {
		logger.Error("❌ 清理 Critical 事件失败: %v", err)
	} else {
		logger.Info("✅ Critical 事件清理完成")
	}
	
	// 清理 Warning 事件
	if err := ec.db.CleanupOldEvents(ctx, "warning", 
		ec.config.Retention.WarningMaxCount, 
		ec.config.Retention.WarningDays); err != nil {
		logger.Error("❌ 清理 Warning 事件失败: %v", err)
	} else {
		logger.Info("✅ Warning 事件清理完成")
	}
	
	// 清理 Info 事件
	if err := ec.db.CleanupOldEvents(ctx, "info", 
		ec.config.Retention.InfoMaxCount, 
		ec.config.Retention.InfoDays); err != nil {
		logger.Error("❌ 清理 Info 事件失败: %v", err)
	} else {
		logger.Info("✅ Info 事件清理完成")
	}
	
	logger.Info("✅ 事件清理完成")
}

// PublishEvent 发布事件（便捷方法）
func (ec *EventCenter) PublishEvent(eventType EventType, data map[string]interface{}) {
	event := &Event{
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
	ec.eventBus.Publish(event)
}

// CheckPriceVolatility 检查价格波动
func (ec *EventCenter) CheckPriceVolatility(symbol string, oldPrice, newPrice float64) {
	// 检查是否监控此交易对
	if len(ec.monitoredSymbols) > 0 && !ec.monitoredSymbols[symbol] {
		return
	}
	
	if oldPrice <= 0 || newPrice <= 0 {
		return
	}
	
	// 计算变化百分比
	changePercent := ((newPrice - oldPrice) / oldPrice) * 100
	absChangePercent := changePercent
	if absChangePercent < 0 {
		absChangePercent = -absChangePercent
	}
	
	// 检查是否超过阈值
	if absChangePercent >= ec.priceVolatilityThreshold {
		ec.PublishEvent(EventTypePriceVolatility, map[string]interface{}{
			"symbol":         symbol,
			"old_price":      oldPrice,
			"new_price":      newPrice,
			"change_percent": changePercent,
			"threshold":      ec.priceVolatilityThreshold,
		})
	}
}

