package ai

import (
	"fmt"
	"quantmesh/config"
)

// ConfigService AI 配置服务
type ConfigService struct {
	configPath string
}

// NewConfigService 创建配置服务
func NewConfigService(configPath string) *ConfigService {
	return &ConfigService{
		configPath: configPath,
	}
}

// ApplyAIConfig 应用 AI 生成的配置
func (cs *ConfigService) ApplyAIConfig(aiConfig *GenerateConfigResponse, cfg *config.Config) error {
	// 1. 更新网格配置
	for _, gridCfg := range aiConfig.GridConfig {
		found := false
		for i, symCfg := range cfg.Trading.Symbols {
			if symCfg.Exchange == gridCfg.Exchange && symCfg.Symbol == gridCfg.Symbol {
				cfg.Trading.Symbols[i].PriceInterval = gridCfg.PriceInterval
				cfg.Trading.Symbols[i].OrderQuantity = gridCfg.OrderQuantity
				cfg.Trading.Symbols[i].BuyWindowSize = gridCfg.BuyWindowSize
				cfg.Trading.Symbols[i].SellWindowSize = gridCfg.SellWindowSize
				
				// 应用网格风控配置
				if gridCfg.GridRiskControl != nil {
					cfg.Trading.Symbols[i].GridRiskControl.Enabled = gridCfg.GridRiskControl.Enabled
					if gridCfg.GridRiskControl.Enabled {
						cfg.Trading.Symbols[i].GridRiskControl.MaxGridLayers = gridCfg.GridRiskControl.MaxGridLayers
						cfg.Trading.Symbols[i].GridRiskControl.StopLossRatio = gridCfg.GridRiskControl.StopLossRatio
						cfg.Trading.Symbols[i].GridRiskControl.TakeProfitTriggerRatio = gridCfg.GridRiskControl.TakeProfitTriggerRatio
						cfg.Trading.Symbols[i].GridRiskControl.TrailingTakeProfitRatio = gridCfg.GridRiskControl.TrailingTakeProfitRatio
						cfg.Trading.Symbols[i].GridRiskControl.TrendFilterEnabled = gridCfg.GridRiskControl.TrendFilterEnabled
					}
				}
				
				found = true
				break
			}
		}

		// 如果币种不存在，添加新配置
		if !found {
			newSymCfg := config.SymbolConfig{
				Exchange:       gridCfg.Exchange,
				Symbol:         gridCfg.Symbol,
				PriceInterval:  gridCfg.PriceInterval,
				OrderQuantity:  gridCfg.OrderQuantity,
				BuyWindowSize:  gridCfg.BuyWindowSize,
				SellWindowSize: gridCfg.SellWindowSize,
				// 使用默认值填充其他必要字段
				MinOrderValue:             20,
				ReconcileInterval:         60,
				OrderCleanupThreshold:     80,
				CleanupBatchSize:          20,
				MarginLockDurationSec:     20,
				PositionSafetyCheck:       50,
			}
			
			// 应用网格风控配置
			if gridCfg.GridRiskControl != nil {
				newSymCfg.GridRiskControl.Enabled = gridCfg.GridRiskControl.Enabled
				if gridCfg.GridRiskControl.Enabled {
					newSymCfg.GridRiskControl.MaxGridLayers = gridCfg.GridRiskControl.MaxGridLayers
					newSymCfg.GridRiskControl.StopLossRatio = gridCfg.GridRiskControl.StopLossRatio
					newSymCfg.GridRiskControl.TakeProfitTriggerRatio = gridCfg.GridRiskControl.TakeProfitTriggerRatio
					newSymCfg.GridRiskControl.TrailingTakeProfitRatio = gridCfg.GridRiskControl.TrailingTakeProfitRatio
					newSymCfg.GridRiskControl.TrendFilterEnabled = gridCfg.GridRiskControl.TrendFilterEnabled
				}
			}
			
			cfg.Trading.Symbols = append(cfg.Trading.Symbols, newSymCfg)
		}
	}

	// 2. 更新资金分配配置
	cfg.PositionAllocation.Enabled = true
	cfg.PositionAllocation.Allocations = []config.SymbolAllocation{}

	for _, allocCfg := range aiConfig.Allocation {
		cfg.PositionAllocation.Allocations = append(cfg.PositionAllocation.Allocations, config.SymbolAllocation{
			Exchange:      allocCfg.Exchange,
			Symbol:        allocCfg.Symbol,
			MaxAmountUSDT: allocCfg.MaxAmountUSDT,
			MaxPercentage: allocCfg.MaxPercentage,
		})
	}

	// 3. 保存配置
	if err := config.SaveConfig(cfg, cs.configPath); err != nil {
		return fmt.Errorf("保存配置失败: %w", err)
	}

	return nil
}

// ValidateAIConfig 验证 AI 配置
func (cs *ConfigService) ValidateAIConfig(aiConfig *GenerateConfigResponse, totalCapital float64) error {
	// 验证资金分配总和不超过可用资金
	var totalAllocated float64
	for _, alloc := range aiConfig.Allocation {
		totalAllocated += alloc.MaxAmountUSDT
	}

	if totalAllocated > totalCapital {
		return fmt.Errorf("资金分配总和 (%.2f USDT) 超过可用资金 (%.2f USDT)", totalAllocated, totalCapital)
	}

	// 验证网格参数合理性
	for _, grid := range aiConfig.GridConfig {
		if grid.PriceInterval <= 0 {
			return fmt.Errorf("%s 价格间隔必须大于0", grid.Symbol)
		}
		if grid.OrderQuantity <= 0 {
			return fmt.Errorf("%s 每单金额必须大于0", grid.Symbol)
		}
		if grid.BuyWindowSize <= 0 || grid.SellWindowSize <= 0 {
			return fmt.Errorf("%s 窗口大小必须大于0", grid.Symbol)
		}
		
		// 验证风控参数
		if grid.GridRiskControl != nil && grid.GridRiskControl.Enabled {
			if grid.GridRiskControl.StopLossRatio < 0 || grid.GridRiskControl.StopLossRatio > 1 {
				return fmt.Errorf("%s 止损比例必须在0-1之间", grid.Symbol)
			}
			if grid.GridRiskControl.TakeProfitTriggerRatio < 0 || grid.GridRiskControl.TakeProfitTriggerRatio > 1 {
				return fmt.Errorf("%s 盈利触发比例必须在0-1之间", grid.Symbol)
			}
			if grid.GridRiskControl.TrailingTakeProfitRatio < 0 || grid.GridRiskControl.TrailingTakeProfitRatio > 1 {
				return fmt.Errorf("%s 回撤止盈比例必须在0-1之间", grid.Symbol)
			}
			if grid.GridRiskControl.MaxGridLayers < 0 {
				return fmt.Errorf("%s 最大层数不能为负数", grid.Symbol)
			}
		}
	}

	return nil
}

