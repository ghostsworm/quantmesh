package saas

import (
	"context"
	"time"
	
	"quantmesh/logger"
)

// AutoScaler 自动扩缩容器
type AutoScaler struct {
	manager *InstanceManagerV2
	
	// 扩容阈值
	scaleUpCPUThreshold    float64
	scaleUpMemoryThreshold float64
	
	// 缩容阈值
	scaleDownCPUThreshold    float64
	scaleDownMemoryThreshold float64
	
	// 检查间隔
	checkInterval time.Duration
	
	// 冷却时间 (避免频繁扩缩容)
	cooldownPeriod time.Duration
	
	// 最后一次扩缩容时间
	lastScaleTime map[string]time.Time
}

// NewAutoScaler 创建自动扩缩容器
func NewAutoScaler(manager *InstanceManagerV2) *AutoScaler {
	return &AutoScaler{
		manager:                  manager,
		scaleUpCPUThreshold:      0.80, // CPU 超过 80% 扩容
		scaleUpMemoryThreshold:   0.80, // 内存超过 80% 扩容
		scaleDownCPUThreshold:    0.30, // CPU 低于 30% 缩容
		scaleDownMemoryThreshold: 0.30, // 内存低于 30% 缩容
		checkInterval:            2 * time.Minute,
		cooldownPeriod:           5 * time.Minute,
		lastScaleTime:            make(map[string]time.Time),
	}
}

// Start 启动自动扩缩容
func (s *AutoScaler) Start(ctx context.Context) {
	ticker := time.NewTicker(s.checkInterval)
	defer ticker.Stop()
	
	logger.Info("🤖 自动扩缩容已启动")
	
	for {
		select {
		case <-ctx.Done():
			logger.Info("自动扩缩容已停止")
			return
		case <-ticker.C:
			s.checkAllInstances()
		}
	}
}

// checkAllInstances 检查所有实例
func (s *AutoScaler) checkAllInstances() {
	instances := s.manager.ListInstances()
	
	for _, instance := range instances {
		// 只对企业版启用自动扩缩容
		if instance.Plan != "enterprise" {
			continue
		}
		
		// 检查冷却时间
		if lastTime, exists := s.lastScaleTime[instance.ID]; exists {
			if time.Since(lastTime) < s.cooldownPeriod {
				continue
			}
		}
		
		// 获取资源使用情况
		usage, err := s.manager.getResourceUsage(instance.ContainerID)
		if err != nil {
			logger.Error("❌ 获取实例 %s 资源使用失败: %v", instance.ID, err)
			continue
		}
		
		// 判断是否需要扩缩容
		if s.shouldScaleUp(usage) {
			s.manager.scaleUp(instance)
			s.lastScaleTime[instance.ID] = time.Now()
		} else if s.shouldScaleDown(usage) {
			s.manager.scaleDown(instance)
			s.lastScaleTime[instance.ID] = time.Now()
		}
	}
}

// shouldScaleUp 判断是否需要扩容
func (s *AutoScaler) shouldScaleUp(usage *ResourceUsage) bool {
	return usage.CPU > s.scaleUpCPUThreshold || usage.MemoryPct > s.scaleUpMemoryThreshold
}

// shouldScaleDown 判断是否需要缩容
func (s *AutoScaler) shouldScaleDown(usage *ResourceUsage) bool {
	return usage.CPU < s.scaleDownCPUThreshold && usage.MemoryPct < s.scaleDownMemoryThreshold
}

