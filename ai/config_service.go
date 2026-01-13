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
	// 1. 优先处理分级的 SymbolsConfig (资产优先重构)
	if len(aiConfig.SymbolsConfig) > 0 {
		for _, newSymCfg := range aiConfig.SymbolsConfig {
			// 确保交易所已设置
			if newSymCfg.Exchange == "" {
				newSymCfg.Exchange = cfg.App.CurrentExchange
			}

			found := false
			for i, oldSymCfg := range cfg.Trading.Symbols {
				// 注意：这里需要考虑 Exchange 为空的情况（默认使用 app.current_exchange）
				oldExchange := oldSymCfg.Exchange
				if oldExchange == "" {
					oldExchange = cfg.App.CurrentExchange
				}

				if oldExchange == newSymCfg.Exchange && oldSymCfg.Symbol == newSymCfg.Symbol {
					// 保留一些基础字段，防止 AI 覆盖掉（如订单清理等，除非 AI 显式指定）
					if newSymCfg.MinOrderValue == 0 {
						newSymCfg.MinOrderValue = oldSymCfg.MinOrderValue
					}
					if newSymCfg.ReconcileInterval == 0 {
						newSymCfg.ReconcileInterval = oldSymCfg.ReconcileInterval
					}
					if newSymCfg.OrderCleanupThreshold == 0 {
						newSymCfg.OrderCleanupThreshold = oldSymCfg.OrderCleanupThreshold
					}
					if newSymCfg.CleanupBatchSize == 0 {
						newSymCfg.CleanupBatchSize = oldSymCfg.CleanupBatchSize
					}
					if newSymCfg.MarginLockDurationSec == 0 {
						newSymCfg.MarginLockDurationSec = oldSymCfg.MarginLockDurationSec
					}
					if newSymCfg.PositionSafetyCheck == 0 {
						newSymCfg.PositionSafetyCheck = oldSymCfg.PositionSafetyCheck
					}

					cfg.Trading.Symbols[i] = newSymCfg
					found = true
					break
				}
			}

			// 如果币种不存在，添加新配置
			if !found {
				if newSymCfg.MinOrderValue == 0 {
					newSymCfg.MinOrderValue = 20
				}
				if newSymCfg.ReconcileInterval == 0 {
					newSymCfg.ReconcileInterval = 60
				}
				if newSymCfg.OrderCleanupThreshold == 0 {
					newSymCfg.OrderCleanupThreshold = 50
				}
				if newSymCfg.CleanupBatchSize == 0 {
					newSymCfg.CleanupBatchSize = 10
				}
				if newSymCfg.MarginLockDurationSec == 0 {
					newSymCfg.MarginLockDurationSec = 10
				}
				if newSymCfg.PositionSafetyCheck == 0 {
					newSymCfg.PositionSafetyCheck = 100
				}
				cfg.Trading.Symbols = append(cfg.Trading.Symbols, newSymCfg)
			}
		}
	} else {
		// 回退到原有的网格配置处理逻辑
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
					MinOrderValue:         20,
					ReconcileInterval:     60,
					OrderCleanupThreshold: 80,
					CleanupBatchSize:      20,
					MarginLockDurationSec: 20,
					PositionSafetyCheck:   50,
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
	}

	// 2. 更新资金分配配置 (PositionAllocation 也需要更新以确保兼容性)
	cfg.PositionAllocation.Enabled = true
	// 注意：如果 SymbolsConfig 已涵盖资金分配，这里也要同步
	if len(aiConfig.SymbolsConfig) > 0 {
		cfg.PositionAllocation.Allocations = []config.SymbolAllocation{}
		for _, sc := range aiConfig.SymbolsConfig {
			cfg.PositionAllocation.Allocations = append(cfg.PositionAllocation.Allocations, config.SymbolAllocation{
				Exchange:      sc.Exchange,
				Symbol:        sc.Symbol,
				MaxAmountUSDT: sc.TotalAllocatedCapital,
				MaxPercentage: 0, // 优先使用金额
			})
		}
	} else if len(aiConfig.Allocation) > 0 {
		cfg.PositionAllocation.Allocations = []config.SymbolAllocation{}
		for _, allocCfg := range aiConfig.Allocation {
			cfg.PositionAllocation.Allocations = append(cfg.PositionAllocation.Allocations, config.SymbolAllocation{
				Exchange:      allocCfg.Exchange,
				Symbol:        allocCfg.Symbol,
				MaxAmountUSDT: allocCfg.MaxAmountUSDT,
				MaxPercentage: allocCfg.MaxPercentage,
			})
		}
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

