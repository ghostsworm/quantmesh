package event

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/indicators"
	"quantmesh/logger"
)

// VolatilityAlertService 波动率预警服务
type VolatilityAlertService struct {
	detector     *indicators.VolatilityRegimeDetector
	mu           sync.RWMutex
	ctx          context.Context
	cancel       context.CancelFunc
	subscribers  map[string][]chan indicators.VolatilityRegimeEvent

	// 预警历史
	alertHistory []VolatilityAlertRecord
	maxHistory   int
}

// VolatilityAlertRecord 波动率预警记录
type VolatilityAlertRecord struct {
	ID              string
	Timestamp       time.Time
	OldRegime       indicators.VolatilityRegime
	NewRegime       indicators.VolatilityRegime
	TriggerReason   string
	Severity        string
	Recommendations []string
	Acknowledged    bool
	AcknowledgedBy  string
	AcknowledgedAt  *time.Time
}

// NewVolatilityAlertService 创建波动率预警服务
func NewVolatilityAlertService(config indicators.VolatilityRegimeConfig) *VolatilityAlertService {
	ctx, cancel := context.WithCancel(context.Background())

	return &VolatilityAlertService{
		detector:    indicators.NewVolatilityRegimeDetector(config),
		ctx:         ctx,
		cancel:      cancel,
		subscribers: make(map[string][]chan indicators.VolatilityRegimeEvent),
		alertHistory: make([]VolatilityAlertRecord, 0, 100),
		maxHistory:   100,
	}
}

// Start 启动预警服务
func (vas *VolatilityAlertService) Start() {
	// 设置回调函数
	vas.detector.SetRegimeChangeCallback(vas.handleRegimeChange)

	logger.Info("✅ 波动率预警服务已启动")
}

// Stop 停止预警服务（須用指針接收者，避免複製含 sync.RWMutex 的值）
func (vas *VolatilityAlertService) Stop() {
	if vas != nil && vas.cancel != nil {
		vas.cancel()
	}
	logger.Info("🛑 波动率预警服务已停止")
}

// UpdatePrice 更新价格数据
func (vas *VolatilityAlertService) UpdatePrice(price, high, low, volume float64) {
	vas.detector.UpdatePrice(price, high, low, volume)

	// 检查突变
	hasChange, message := vas.detector.DetectSuddenChange()
	if hasChange {
		logger.Warn("⚠️ %s", message)

		// 发送突变通知
		event := indicators.VolatilityRegimeEvent{
			Timestamp: time.Now(),
			Severity:  "warning",
			TriggerReason: message,
		}
		vas.notifySubscribers(event)
	}
}

// handleRegimeChange 处理区间变化
func (vas *VolatilityAlertService) handleRegimeChange(event indicators.VolatilityRegimeEvent) {
	vas.mu.Lock()
	defer vas.mu.Unlock()

	// 记录预警历史
	record := VolatilityAlertRecord{
		ID:              generateAlertID(),
		Timestamp:       event.Timestamp,
		OldRegime:       event.OldRegime,
		NewRegime:       event.NewRegime,
		TriggerReason:   event.TriggerReason,
		Severity:        event.Severity,
		Recommendations: event.Recommendations,
		Acknowledged:    false,
	}

	vas.alertHistory = append(vas.alertHistory, record)

	// 限制历史长度
	if len(vas.alertHistory) > vas.maxHistory {
		vas.alertHistory = vas.alertHistory[len(vas.alertHistory)-vas.maxHistory:]
	}

	// 记录日志
	switch event.Severity {
	case "critical":
		logger.Error("🚨 [波动率预警] %s", event.TriggerReason)
		for _, rec := range event.Recommendations {
			logger.Error("   → %s", rec)
		}
	case "warning":
		logger.Warn("⚠️ [波动率预警] %s", event.TriggerReason)
		for _, rec := range event.Recommendations {
			logger.Warn("   → %s", rec)
		}
	default:
		logger.Info("ℹ️ [波动率预警] %s", event.TriggerReason)
	}

	// 通知订阅者
	vas.notifySubscribers(event)
}

// notifySubscribers 通知所有订阅者
func (vas *VolatilityAlertService) notifySubscribers(event indicators.VolatilityRegimeEvent) {
	for _, channels := range vas.subscribers {
		for _, ch := range channels {
			select {
			case ch <- event:
				// 发送成功
			default:
				// 通道满了，丢弃
				logger.Warn("⚠️ 波动率预警通道已满，丢弃消息")
			}
		}
	}
}

// Subscribe 订阅波动率预警
func (vas *VolatilityAlertService) Subscribe(subscriberID string) <-chan indicators.VolatilityRegimeEvent {
	vas.mu.Lock()
	defer vas.mu.Unlock()

	ch := make(chan indicators.VolatilityRegimeEvent, 10) // 缓冲10条消息

	if _, exists := vas.subscribers[subscriberID]; !exists {
		vas.subscribers[subscriberID] = make([]chan indicators.VolatilityRegimeEvent, 0)
	}

	vas.subscribers[subscriberID] = append(vas.subscribers[subscriberID], ch)

	return ch
}

// Unsubscribe 取消订阅
func (vas *VolatilityAlertService) Unsubscribe(subscriberID string, ch <-chan indicators.VolatilityRegimeEvent) {
	vas.mu.Lock()
	defer vas.mu.Unlock()

	if channels, exists := vas.subscribers[subscriberID]; exists {
		for i, c := range channels {
			if c == ch {
				// 找到了，删除
				vas.subscribers[subscriberID] = append(channels[:i], channels[i+1:]...)
				close(c)
				break
			}
		}

		if len(vas.subscribers[subscriberID]) == 0 {
			delete(vas.subscribers, subscriberID)
		}
	}
}

// GetCurrentRegime 获取当前波动率区间
func (vas *VolatilityAlertService) GetCurrentRegime() indicators.VolatilityRegime {
	return vas.detector.GetCurrentRegime()
}

// GetLatestVolatility 获取最新波动率数据
func (vas *VolatilityAlertService) GetLatestVolatility() *indicators.VolatilityPoint {
	return vas.detector.GetLatestVolatility()
}

// GetVolatilityHistory 获取波动率历史
func (vas *VolatilityAlertService) GetVolatilityHistory(count int) []indicators.VolatilityPoint {
	return vas.detector.GetVolatilityHistory(count)
}

// IsGridFriendly 判断当前是否适合网格策略
func (vas *VolatilityAlertService) IsGridFriendly() bool {
	return vas.detector.IsGridFriendly()
}

// GetRiskLevel 获取当前风险等级
func (vas *VolatilityAlertService) GetRiskLevel() int {
	return vas.detector.GetRiskLevel()
}

// GetAlertHistory 获取预警历史
func (vas *VolatilityAlertService) GetAlertHistory(limit int) []VolatilityAlertRecord {
	vas.mu.RLock()
	defer vas.mu.RUnlock()

	if limit <= 0 || limit > len(vas.alertHistory) {
		limit = len(vas.alertHistory)
	}

	start := len(vas.alertHistory) - limit
	if start < 0 {
		start = 0
	}

	result := make([]VolatilityAlertRecord, limit)
	copy(result, vas.alertHistory[start:])
	return result
}

// AcknowledgeAlert 确认预警
func (vas *VolatilityAlertService) AcknowledgeAlert(alertID, acknowledgedBy string) error {
	vas.mu.Lock()
	defer vas.mu.Unlock()

	for i, record := range vas.alertHistory {
		if record.ID == alertID {
			if record.Acknowledged {
				return fmt.Errorf("预警 %s 已经被确认过了", alertID)
			}

			now := time.Now()
			vas.alertHistory[i].Acknowledged = true
			vas.alertHistory[i].AcknowledgedBy = acknowledgedBy
			vas.alertHistory[i].AcknowledgedAt = &now

			logger.Info("✅ 预警 %s 已被 %s 确认", alertID, acknowledgedBy)
			return nil
		}
	}

	return fmt.Errorf("未找到预警 %s", alertID)
}

// GetUnacknowledgedAlerts 获取未确认的预警
func (vas *VolatilityAlertService) GetUnacknowledgedAlerts() []VolatilityAlertRecord {
	vas.mu.RLock()
	defer vas.mu.RUnlock()

	result := make([]VolatilityAlertRecord, 0)
	for _, record := range vas.alertHistory {
		if !record.Acknowledged {
			result = append(result, record)
		}
	}

	return result
}

// GetStatistics 获取统计信息
func (vas *VolatilityAlertService) GetStatistics() map[string]interface{} {
	vas.mu.RLock()
	defer vas.mu.RUnlock()

	unackCount := 0
	for _, record := range vas.alertHistory {
		if !record.Acknowledged {
			unackCount++
		}
	}
	stats := map[string]interface{}{
		"current_regime":     vas.detector.GetCurrentRegime().String(),
		"current_risk_level": vas.detector.GetRiskLevel(),
		"grid_friendly":      vas.detector.IsGridFriendly(),
		"total_alerts":       len(vas.alertHistory),
		"unacknowledged":     unackCount,
		"subscribers":        len(vas.subscribers),
	}

	// 获取最新波动率数据
	if latest := vas.detector.GetLatestVolatility(); latest != nil {
		stats["short_volatility"] = latest.ShortVolatility
		stats["medium_volatility"] = latest.MediumVolatility
		stats["long_volatility"] = latest.LongVolatility
		stats["price_range"] = latest.PriceRange
	}

	return stats
}

// generateAlertID 生成预警ID
func generateAlertID() string {
	return fmt.Sprintf("VA_%d", time.Now().UnixNano())
}
