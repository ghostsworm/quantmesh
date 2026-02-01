package position

import (
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

// AllocationManager 资金分配管理器
type AllocationManager struct {
	cfg         *config.Config
	allocations map[string]*SymbolAllocationInfo // key: "exchange:symbol"
	eventBus    EventBus                         // 事件總線（用於发送通知）
	mu          sync.RWMutex
}

// SymbolAllocationInfo 币种分配信息
type SymbolAllocationInfo struct {
	Exchange   string
	Symbol     string
	MaxAmount  float64 // 最大允許金額（已计算好的值）
	UsedAmount float64 // 已使用金額
	
	// 分级限額状態
	IsEmergencyMode   bool      // 是否处於紧急模式
	EmergencyTriggeredAt time.Time // 紧急模式触发時间
	NormalLimit       float64   // 正常限額
	EmergencyLimit    float64   // 紧急限額
}

// AllocationStatus 资金使用状態
type AllocationStatus struct {
	Exchange        string  `json:"exchange"`
	Symbol          string  `json:"symbol"`
	MaxAmount       float64 `json:"max_amount"`
	UsedAmount      float64 `json:"used_amount"`
	AvailableAmount float64 `json:"available_amount"`
	UsagePercentage float64 `json:"usage_percentage"`
	
	// 分级限額状態
	IsEmergencyMode  bool    `json:"is_emergency_mode"`  // 是否处於紧急模式
	NormalLimit      float64 `json:"normal_limit"`       // 正常限額
	EmergencyLimit   float64 `json:"emergency_limit"`    // 紧急限額
	LimitMode        string  `json:"limit_mode"`         // 限額模式：normal/emergency
}

// NewAllocationManager 創建资金分配管理器
func NewAllocationManager(cfg *config.Config) *AllocationManager {
	am := &AllocationManager{
		cfg:         cfg,
		allocations: make(map[string]*SymbolAllocationInfo),
	}

	// 初始化分配配置
	if cfg.PositionAllocation.Enabled {
		for _, alloc := range cfg.PositionAllocation.Allocations {
			key := fmt.Sprintf("%s:%s", alloc.Exchange, alloc.Symbol)
			
			// 确定正常限額和紧急限額
			normalLimit := alloc.MaxAmountUSDT
			emergencyLimit := normalLimit
			if alloc.TieredLimits.Enabled && alloc.TieredLimits.EmergencyLimit > normalLimit {
				emergencyLimit = alloc.TieredLimits.EmergencyLimit
			}
			
			am.allocations[key] = &SymbolAllocationInfo{
				Exchange:         alloc.Exchange,
				Symbol:           alloc.Symbol,
				MaxAmount:        normalLimit, // 初始使用正常限額
				UsedAmount:       0,
				IsEmergencyMode:  false,
				NormalLimit:      normalLimit,
				EmergencyLimit:   emergencyLimit,
			}
			
			if alloc.TieredLimits.Enabled {
				logger.Info("📊 [资金分配] 初始化 %s:%s - 正常限額: %.2f USDT, 紧急限額: %.2f USDT (百分比: %.1f%%)",
					alloc.Exchange, alloc.Symbol, normalLimit, emergencyLimit, alloc.MaxPercentage)
			} else {
				logger.Info("📊 [资金分配] 初始化 %s:%s - 限額: %.2f USDT (百分比: %.1f%%)",
					alloc.Exchange, alloc.Symbol, normalLimit, alloc.MaxPercentage)
			}
		}
	}

	return am
}

// SetEventBus 設置事件總線（用於发送通知）
func (am *AllocationManager) SetEventBus(eventBus EventBus) {
	am.eventBus = eventBus
}

// CheckAndAdjustLimit 检查並調整限額（根據市场情况）
// 参數：
//   - currentPrice: 當前市場價格
//   - anchorPrice: 锚点價格（初始價格）
//   - positionLayers: 當前持倉层數
//   - unrealizedPnL: 未實現盈亏
func (am *AllocationManager) CheckAndAdjustLimit(exchange, symbol string, currentPrice, anchorPrice float64, positionLayers int, unrealizedPnL float64) {
	if !am.cfg.PositionAllocation.Enabled {
		return
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)
	configAlloc := am.getConfigAllocation(exchange, symbol)
	if configAlloc == nil || !configAlloc.TieredLimits.Enabled {
		return // 未配置分级限額
	}

	am.mu.Lock()
	defer am.mu.Unlock()

	alloc, exists := am.allocations[key]
	if !exists {
		return
	}

	triggers := configAlloc.TieredLimits.Triggers
	recovery := configAlloc.TieredLimits.Recovery
	
	// 计算價格下跌百分比
	priceDropPercent := 0.0
	if anchorPrice > 0 {
		priceDropPercent = ((anchorPrice - currentPrice) / anchorPrice) * 100
	}
	
	// 检查是否应該触发紧急限額
	shouldTriggerEmergency := false
	triggerReason := ""
	
	if !alloc.IsEmergencyMode {
		// 當前是正常模式，检查是否应該触发紧急模式
		if triggers.PriceDropPercent > 0 && priceDropPercent >= triggers.PriceDropPercent {
			shouldTriggerEmergency = true
			triggerReason = fmt.Sprintf("價格下跌 %.2f%% (触发阈值: %.2f%%)", priceDropPercent, triggers.PriceDropPercent)
		} else if triggers.PositionLayers > 0 && positionLayers >= triggers.PositionLayers {
			shouldTriggerEmergency = true
			triggerReason = fmt.Sprintf("持倉层數 %d (触发阈值: %d)", positionLayers, triggers.PositionLayers)
		} else if triggers.UnrealizedLossUSD > 0 && unrealizedPnL < -triggers.UnrealizedLossUSD {
			shouldTriggerEmergency = true
			triggerReason = fmt.Sprintf("未實現亏损 %.2f USDT (触发阈值: %.2f USDT)", unrealizedPnL, triggers.UnrealizedLossUSD)
		}
		
		if shouldTriggerEmergency {
			// 触发紧急限額
			alloc.MaxAmount = alloc.EmergencyLimit
			alloc.IsEmergencyMode = true
			alloc.EmergencyTriggeredAt = time.Now()
			
			logger.Warn("🚨 [资金分配] %s:%s 触发紧急限額: %.2f USDT -> %.2f USDT, 原因: %s",
				exchange, symbol, alloc.NormalLimit, alloc.EmergencyLimit, triggerReason)
			
			// 发送通知事件
			if am.eventBus != nil && configAlloc.TieredLimits.Notification.OnTrigger {
				am.eventBus.Publish(&event.Event{
					Type: event.EventTypeAllocationLimitChanged,
					Data: map[string]interface{}{
						"exchange":        exchange,
						"symbol":          symbol,
						"old_limit":       alloc.NormalLimit,
						"new_limit":       alloc.EmergencyLimit,
						"mode":            "emergency",
						"reason":          triggerReason,
						"price_drop":      priceDropPercent,
						"position_layers": positionLayers,
						"unrealized_pnl":  unrealizedPnL,
					},
				})
			}
		}
	} else {
		// 當前是紧急模式，检查是否应該恢複正常模式
		shouldRecover := false
		recoverReason := ""
		
		// 检查冷却時间
		cooldownSeconds := recovery.CooldownSeconds
		if cooldownSeconds <= 0 {
			cooldownSeconds = 300 // 默认5分钟
		}
		timeSinceTrigger := time.Since(alloc.EmergencyTriggeredAt).Seconds()
		if timeSinceTrigger < float64(cooldownSeconds) {
			// 还在冷却期内，不恢複
			return
		}
		
		// 检查價格是否恢複
		priceRecoverPercent := recovery.PriceRecoverPercent
		if priceRecoverPercent <= 0 {
			priceRecoverPercent = 5 // 默认價格恢複到下跌5%以内
		}
		
		if priceDropPercent <= priceRecoverPercent {
			shouldRecover = true
			recoverReason = fmt.Sprintf("價格已恢複，當前下跌 %.2f%% (恢複阈值: %.2f%%)", priceDropPercent, priceRecoverPercent)
		}
		
		if shouldRecover {
			// 恢複正常限額
			alloc.MaxAmount = alloc.NormalLimit
			alloc.IsEmergencyMode = false
			
			logger.Info("✅ [资金分配] %s:%s 恢複正常限額: %.2f USDT -> %.2f USDT, 原因: %s",
				exchange, symbol, alloc.EmergencyLimit, alloc.NormalLimit, recoverReason)
			
			// 发送通知事件
			if am.eventBus != nil && configAlloc.TieredLimits.Notification.OnRecovery {
				am.eventBus.Publish(&event.Event{
					Type: event.EventTypeAllocationLimitChanged,
					Data: map[string]interface{}{
						"exchange":        exchange,
						"symbol":          symbol,
						"old_limit":       alloc.EmergencyLimit,
						"new_limit":       alloc.NormalLimit,
						"mode":            "normal",
						"reason":          recoverReason,
						"price_drop":      priceDropPercent,
						"position_layers": positionLayers,
						"unrealized_pnl":  unrealizedPnL,
					},
				})
			}
		}
	}
}

// CheckAndReserve 检查並預留资金
func (am *AllocationManager) CheckAndReserve(exchange, symbol string, amount float64, accountBalance float64) error {
	if !am.cfg.PositionAllocation.Enabled {
		return nil // 未啟用，直接通過
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.Lock()
	defer am.mu.Unlock()

	alloc, exists := am.allocations[key]
	if !exists {
		// 未配置限制，允許通過
		return nil
	}

	// 计算實際限制（取固定金額和百分比的较小值）
	configAlloc := am.getConfigAllocation(exchange, symbol)
	if configAlloc != nil && accountBalance > 0 {
		percentageLimit := accountBalance * (configAlloc.MaxPercentage / 100.0)
		if percentageLimit > 0 && percentageLimit < alloc.MaxAmount {
			alloc.MaxAmount = percentageLimit
		}
	}

	// 检查是否超出限制
	if alloc.UsedAmount+amount > alloc.MaxAmount {
		limitType := "正常限額"
		if alloc.IsEmergencyMode {
			limitType = "紧急限額"
		}
		return fmt.Errorf("超出资金分配限制(%s): %s:%s 已用 %.2f USDT, 限額 %.2f USDT, 本次需要 %.2f USDT",
			limitType, exchange, symbol, alloc.UsedAmount, alloc.MaxAmount, amount)
	}

	// 預留资金
	alloc.UsedAmount += amount

	return nil
}

// Release 释放资金
func (am *AllocationManager) Release(exchange, symbol string, amount float64) {
	if !am.cfg.PositionAllocation.Enabled {
		return
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.Lock()
	defer am.mu.Unlock()

	if alloc, exists := am.allocations[key]; exists {
		alloc.UsedAmount -= amount
		if alloc.UsedAmount < 0 {
			alloc.UsedAmount = 0
		}
	}
}

// SetMaxAmount 設置最大允許金額（用於倉位计划等临時調整资金限制）
func (am *AllocationManager) SetMaxAmount(exchange, symbol string, amount float64) error {
	if !am.cfg.PositionAllocation.Enabled {
		return nil
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.Lock()
	defer am.mu.Unlock()

	alloc, exists := am.allocations[key]
	if !exists {
		return fmt.Errorf("未找到 %s:%s 的资金分配配置", exchange, symbol)
	}

	if amount < 0 {
		amount = 0
	}
	alloc.MaxAmount = amount
	return nil
}

// SetUsedAmount 直接設置已用资金（用於程序啟动時恢複持倉）
func (am *AllocationManager) SetUsedAmount(exchange, symbol string, amount float64) {
	if !am.cfg.PositionAllocation.Enabled {
		return
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.Lock()
	defer am.mu.Unlock()

	if alloc, exists := am.allocations[key]; exists {
		alloc.UsedAmount = amount
		if alloc.UsedAmount < 0 {
			alloc.UsedAmount = 0
		}
	}
}

// GetStatus 獲取资金使用状態
func (am *AllocationManager) GetStatus(exchange, symbol string) *AllocationStatus {
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.RLock()
	defer am.mu.RUnlock()

	alloc, exists := am.allocations[key]
	if !exists {
		return nil
	}

	availableAmount := alloc.MaxAmount - alloc.UsedAmount
	if availableAmount < 0 {
		availableAmount = 0
	}

	usagePercentage := 0.0
	if alloc.MaxAmount > 0 {
		usagePercentage = (alloc.UsedAmount / alloc.MaxAmount) * 100
	}
	
	limitMode := "normal"
	if alloc.IsEmergencyMode {
		limitMode = "emergency"
	}

	return &AllocationStatus{
		Exchange:        alloc.Exchange,
		Symbol:          alloc.Symbol,
		MaxAmount:       alloc.MaxAmount,
		UsedAmount:      alloc.UsedAmount,
		AvailableAmount: availableAmount,
		UsagePercentage: usagePercentage,
		IsEmergencyMode: alloc.IsEmergencyMode,
		NormalLimit:     alloc.NormalLimit,
		EmergencyLimit:  alloc.EmergencyLimit,
		LimitMode:       limitMode,
	}
}

// GetAllStatuses 獲取所有币种的资金使用状態
func (am *AllocationManager) GetAllStatuses() []*AllocationStatus {
	am.mu.RLock()
	defer am.mu.RUnlock()

	statuses := make([]*AllocationStatus, 0, len(am.allocations))
	for _, alloc := range am.allocations {
		availableAmount := alloc.MaxAmount - alloc.UsedAmount
		if availableAmount < 0 {
			availableAmount = 0
		}

		usagePercentage := 0.0
		if alloc.MaxAmount > 0 {
			usagePercentage = (alloc.UsedAmount / alloc.MaxAmount) * 100
		}
		
		limitMode := "normal"
		if alloc.IsEmergencyMode {
			limitMode = "emergency"
		}

		statuses = append(statuses, &AllocationStatus{
			Exchange:        alloc.Exchange,
			Symbol:          alloc.Symbol,
			MaxAmount:       alloc.MaxAmount,
			UsedAmount:      alloc.UsedAmount,
			AvailableAmount: availableAmount,
			UsagePercentage: usagePercentage,
			IsEmergencyMode: alloc.IsEmergencyMode,
			NormalLimit:     alloc.NormalLimit,
			EmergencyLimit:  alloc.EmergencyLimit,
			LimitMode:       limitMode,
		})
	}

	return statuses
}

// getConfigAllocation 獲取配置中的分配信息
func (am *AllocationManager) getConfigAllocation(exchange, symbol string) *config.SymbolAllocation {
	for _, alloc := range am.cfg.PositionAllocation.Allocations {
		if alloc.Exchange == exchange && alloc.Symbol == symbol {
			return &alloc
		}
	}
	return nil
}

