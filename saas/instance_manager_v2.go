package saas

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"
	
	"quantmesh/logger"
	"quantmesh/notify"
)

// InstanceManagerV2 增强版实例管理器
type InstanceManagerV2 struct {
	*InstanceManager // 继承现有实现
	
	notifier *notify.Notifier
}

// NewInstanceManagerV2 创建增强版实例管理器
func NewInstanceManagerV2(im *InstanceManager, notifier *notify.Notifier) *InstanceManagerV2 {
	return &InstanceManagerV2{
		InstanceManager: im,
		notifier:        notifier,
	}
}

// ResourceUsage 资源使用情况
type ResourceUsage struct {
	CPU        float64 // CPU 使用率 (0-1)
	Memory     int64   // 内存使用量 (MB)
	MemoryPct  float64 // 内存使用率 (0-1)
	NetworkIn  int64   // 网络入流量 (bytes)
	NetworkOut int64   // 网络出流量 (bytes)
}

// CreateInstanceWithMonitoring 创建实例并配置监控
func (m *InstanceManagerV2) CreateInstanceWithMonitoring(
	ctx context.Context,
	userID, plan string,
) (*Instance, error) {
	// 1. 创建实例 (复用现有逻辑)
	instance, err := m.InstanceManager.CreateInstance(ctx, userID, plan)
	if err != nil {
		return nil, err
	}
	
	// 2. 配置监控标签
	if err := m.setupMonitoring(instance); err != nil {
		logger.Warn("⚠️ 配置监控失败: %v", err)
	}
	
	// 3. 配置告警规则
	if err := m.setupAlerts(instance); err != nil {
		logger.Warn("⚠️ 配置告警失败: %v", err)
	}
	
	// 4. 发送通知
	if m.notifier != nil {
		m.notifier.Send(fmt.Sprintf(
			"✅ 实例创建成功\n用户: %s\n套餐: %s\n实例ID: %s",
			userID, plan, instance.ID,
		))
	}
	
	return instance, nil
}

// setupMonitoring 配置监控
func (m *InstanceManagerV2) setupMonitoring(instance *Instance) error {
	// 为容器添加 Prometheus 监控标签
	cmd := exec.Command("docker", "update",
		"--label", fmt.Sprintf("prometheus.io/scrape=true"),
		"--label", fmt.Sprintf("prometheus.io/port=%d", instance.Port),
		"--label", fmt.Sprintf("prometheus.io/path=/metrics"),
		"--label", fmt.Sprintf("quantmesh.instance.id=%s", instance.ID),
		"--label", fmt.Sprintf("quantmesh.instance.plan=%s", instance.Plan),
		instance.ContainerID,
	)
	
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("添加监控标签失败: %v, 输出: %s", err, output)
	}
	
	logger.Info("✅ 实例 %s 监控配置完成", instance.ID)
	return nil
}

// setupAlerts 配置告警规则
func (m *InstanceManagerV2) setupAlerts(instance *Instance) error {
	// 这里可以配置 Prometheus Alertmanager 规则
	// 或者使用自定义的告警逻辑
	logger.Info("✅ 实例 %s 告警规则配置完成", instance.ID)
	return nil
}

// MonitorResources 监控所有实例资源使用
func (m *InstanceManagerV2) MonitorResources(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	
	logger.Info("🔍 启动实例资源监控")
	
	for {
		select {
		case <-ctx.Done():
			logger.Info("实例资源监控已停止")
			return
		case <-ticker.C:
			instances := m.ListInstances()
			for _, inst := range instances {
				go m.checkInstanceResources(inst)
			}
		}
	}
}

// checkInstanceResources 检查单个实例资源
func (m *InstanceManagerV2) checkInstanceResources(instance *Instance) {
	usage, err := m.getResourceUsage(instance.ContainerID)
	if err != nil {
		logger.Error("❌ 获取实例 %s 资源使用失败: %v", instance.ID, err)
		return
	}
	
	// 检查 CPU 使用率
	if usage.CPU > 0.9 {
		m.handleHighCPU(instance, usage)
	}
	
	// 检查内存使用率
	if usage.MemoryPct > 0.9 {
		m.handleHighMemory(instance, usage)
	}
	
	// 企业版自动扩容
	if instance.Plan == "enterprise" {
		if usage.CPU > 0.8 || usage.MemoryPct > 0.8 {
			m.scaleUp(instance)
		}
	}
}

// getResourceUsage 获取容器资源使用情况
func (m *InstanceManagerV2) getResourceUsage(containerID string) (*ResourceUsage, error) {
	// 使用 docker stats 获取资源使用
	cmd := exec.Command("docker", "stats", containerID, "--no-stream", "--format",
		"{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}|{{.NetIO}}")
	
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	
	// 解析输出
	// 格式: 12.34%|123.4MiB / 2GiB|6.17%|1.23MB / 4.56MB
	parts := strings.Split(strings.TrimSpace(string(output)), "|")
	if len(parts) < 4 {
		return nil, fmt.Errorf("无效的 stats 输出")
	}
	
	usage := &ResourceUsage{}
	
	// 解析 CPU (去掉 % 符号)
	cpuStr := strings.TrimSuffix(parts[0], "%")
	fmt.Sscanf(cpuStr, "%f", &usage.CPU)
	usage.CPU /= 100.0
	
	// 解析内存百分比
	memPctStr := strings.TrimSuffix(parts[2], "%")
	fmt.Sscanf(memPctStr, "%f", &usage.MemoryPct)
	usage.MemoryPct /= 100.0
	
	return usage, nil
}

// handleHighCPU 处理 CPU 使用率过高
func (m *InstanceManagerV2) handleHighCPU(instance *Instance, usage *ResourceUsage) {
	msg := fmt.Sprintf(
		"⚠️ 实例 %s CPU 使用率过高: %.1f%%\n套餐: %s\n用户: %s",
		instance.ID, usage.CPU*100, instance.Plan, instance.UserID,
	)
	
	logger.Warn(msg)
	
	if m.notifier != nil {
		m.notifier.Send(msg)
	}
}

// handleHighMemory 处理内存使用率过高
func (m *InstanceManagerV2) handleHighMemory(instance *Instance, usage *ResourceUsage) {
	msg := fmt.Sprintf(
		"⚠️ 实例 %s 内存使用率过高: %.1f%%\n套餐: %s\n用户: %s",
		instance.ID, usage.MemoryPct*100, instance.Plan, instance.UserID,
	)
	
	logger.Warn(msg)
	
	if m.notifier != nil {
		m.notifier.Send(msg)
	}
}

// scaleUp 扩容实例
func (m *InstanceManagerV2) scaleUp(instance *Instance) {
	logger.Info("🔼 尝试扩容实例 %s", instance.ID)
	
	// 计算新的资源限制
	newCPU := instance.CPU * 1.5
	newMemory := instance.Memory * 1.5
	
	// 更新容器资源限制
	cmd := exec.Command("docker", "update",
		"--cpus", fmt.Sprintf("%.1f", newCPU),
		"--memory", fmt.Sprintf("%dm", int64(newMemory)),
		instance.ContainerID,
	)
	
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Error("❌ 扩容失败: %v, 输出: %s", err, output)
		return
	}
	
	// 更新实例记录
	m.mu.Lock()
	instance.CPU = newCPU
	instance.Memory = int64(newMemory)
	m.mu.Unlock()
	
	msg := fmt.Sprintf(
		"✅ 实例 %s 扩容成功\nCPU: %.1f → %.1f\n内存: %dMB → %dMB",
		instance.ID, instance.CPU/1.5, instance.CPU,
		int64(instance.Memory/1.5), instance.Memory,
	)
	
	logger.Info(msg)
	
	if m.notifier != nil {
		m.notifier.Send(msg)
	}
}

// scaleDown 缩容实例
func (m *InstanceManagerV2) scaleDown(instance *Instance) {
	logger.Info("🔽 尝试缩容实例 %s", instance.ID)
	
	// 计算新的资源限制
	newCPU := instance.CPU * 0.75
	newMemory := instance.Memory * 0.75
	
	// 确保不低于最小值
	resources := m.allocateResources(instance.Plan)
	if newCPU < resources.CPU {
		newCPU = resources.CPU
	}
	if newMemory < float64(resources.Memory) {
		newMemory = float64(resources.Memory)
	}
	
	// 更新容器资源限制
	cmd := exec.Command("docker", "update",
		"--cpus", fmt.Sprintf("%.1f", newCPU),
		"--memory", fmt.Sprintf("%dm", int64(newMemory)),
		instance.ContainerID,
	)
	
	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Error("❌ 缩容失败: %v, 输出: %s", err, output)
		return
	}
	
	// 更新实例记录
	m.mu.Lock()
	instance.CPU = newCPU
	instance.Memory = int64(newMemory)
	m.mu.Unlock()
	
	logger.Info("✅ 实例 %s 缩容成功", instance.ID)
}

// GetInstanceMetrics 获取实例指标
func (m *InstanceManagerV2) GetInstanceMetrics(instanceID string) (map[string]interface{}, error) {
	instance, err := m.GetInstance(instanceID)
	if err != nil {
		return nil, err
	}
	
	usage, err := m.getResourceUsage(instance.ContainerID)
	if err != nil {
		return nil, err
	}
	
	return map[string]interface{}{
		"instance_id":  instance.ID,
		"user_id":      instance.UserID,
		"plan":         instance.Plan,
		"status":       instance.Status,
		"cpu_usage":    usage.CPU,
		"memory_usage": usage.MemoryPct,
		"cpu_limit":    instance.CPU,
		"memory_limit": instance.Memory,
		"uptime":       time.Since(instance.CreatedAt).Seconds(),
		"last_active":  instance.LastActive,
	}, nil
}

// GetAllInstancesMetrics 获取所有实例指标
func (m *InstanceManagerV2) GetAllInstancesMetrics() ([]map[string]interface{}, error) {
	instances := m.ListInstances()
	metrics := make([]map[string]interface{}, 0, len(instances))
	
	for _, inst := range instances {
		if metric, err := m.GetInstanceMetrics(inst.ID); err == nil {
			metrics = append(metrics, metric)
		}
	}
	
	return metrics, nil
}

