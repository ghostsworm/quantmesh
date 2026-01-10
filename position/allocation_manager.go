package position

import (
	"fmt"
	"sync"

	"quantmesh/config"
	"quantmesh/logger"
)

// AllocationManager 资金分配管理器
type AllocationManager struct {
	cfg         *config.Config
	allocations map[string]*SymbolAllocationInfo // key: "exchange:symbol"
	mu          sync.RWMutex
}

// SymbolAllocationInfo 币种分配信息
type SymbolAllocationInfo struct {
	Exchange   string
	Symbol     string
	MaxAmount  float64 // 最大允许金额（已计算好的值）
	UsedAmount float64 // 已使用金额
}

// AllocationStatus 资金使用状态
type AllocationStatus struct {
	Exchange        string  `json:"exchange"`
	Symbol          string  `json:"symbol"`
	MaxAmount       float64 `json:"max_amount"`
	UsedAmount      float64 `json:"used_amount"`
	AvailableAmount float64 `json:"available_amount"`
	UsagePercentage float64 `json:"usage_percentage"`
}

// NewAllocationManager 创建资金分配管理器
func NewAllocationManager(cfg *config.Config) *AllocationManager {
	am := &AllocationManager{
		cfg:         cfg,
		allocations: make(map[string]*SymbolAllocationInfo),
	}

	// 初始化分配配置
	if cfg.PositionAllocation.Enabled {
		for _, alloc := range cfg.PositionAllocation.Allocations {
			key := fmt.Sprintf("%s:%s", alloc.Exchange, alloc.Symbol)
			am.allocations[key] = &SymbolAllocationInfo{
				Exchange:   alloc.Exchange,
				Symbol:     alloc.Symbol,
				MaxAmount:  alloc.MaxAmountUSDT, // 初始值，后续会根据账户余额动态调整
				UsedAmount: 0,
			}
			logger.Info("📊 [资金分配] 初始化 %s:%s - 限额: %.2f USDT (百分比: %.1f%%)",
				alloc.Exchange, alloc.Symbol, alloc.MaxAmountUSDT, alloc.MaxPercentage)
		}
	}

	return am
}

// CheckAndReserve 检查并预留资金
func (am *AllocationManager) CheckAndReserve(exchange, symbol string, amount float64, accountBalance float64) error {
	if !am.cfg.PositionAllocation.Enabled {
		return nil // 未启用，直接通过
	}

	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.Lock()
	defer am.mu.Unlock()

	alloc, exists := am.allocations[key]
	if !exists {
		// 未配置限制，允许通过
		return nil
	}

	// 计算实际限制（取固定金额和百分比的较小值）
	configAlloc := am.getConfigAllocation(exchange, symbol)
	if configAlloc != nil && accountBalance > 0 {
		percentageLimit := accountBalance * (configAlloc.MaxPercentage / 100.0)
		if percentageLimit > 0 && percentageLimit < alloc.MaxAmount {
			alloc.MaxAmount = percentageLimit
		}
	}

	// 检查是否超出限制
	if alloc.UsedAmount+amount > alloc.MaxAmount {
		return fmt.Errorf("超出资金分配限制: %s:%s 已用 %.2f USDT, 限额 %.2f USDT, 本次需要 %.2f USDT",
			exchange, symbol, alloc.UsedAmount, alloc.MaxAmount, amount)
	}

	// 预留资金
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

// SetUsedAmount 直接设置已用资金（用于程序启动时恢复持仓）
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

// GetStatus 获取资金使用状态
func (am *AllocationManager) GetStatus(exchange, symbol string) *AllocationStatus {
	key := fmt.Sprintf("%s:%s", exchange, symbol)

	am.mu.RLock()
	defer am.mu.RUnlock()

	alloc, exists := am.allocations[key];
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

	return &AllocationStatus{
		Exchange:        alloc.Exchange,
		Symbol:          alloc.Symbol,
		MaxAmount:       alloc.MaxAmount,
		UsedAmount:      alloc.UsedAmount,
		AvailableAmount: availableAmount,
		UsagePercentage: usagePercentage,
	}
}

// GetAllStatuses 获取所有币种的资金使用状态
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

		statuses = append(statuses, &AllocationStatus{
			Exchange:        alloc.Exchange,
			Symbol:          alloc.Symbol,
			MaxAmount:       alloc.MaxAmount,
			UsedAmount:      alloc.UsedAmount,
			AvailableAmount: availableAmount,
			UsagePercentage: usagePercentage,
		})
	}

	return statuses
}

// getConfigAllocation 获取配置中的分配信息
func (am *AllocationManager) getConfigAllocation(exchange, symbol string) *config.SymbolAllocation {
	for _, alloc := range am.cfg.PositionAllocation.Allocations {
		if alloc.Exchange == exchange && alloc.Symbol == symbol {
			return &alloc
		}
	}
	return nil
}

