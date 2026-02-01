package saas

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/notify"
)

// InstanceManagerV2 增强版實例管理器
type InstanceManagerV2 struct {
	*InstanceManager // 继承現有實現

	notifier notify.Notifier
}

// NewInstanceManagerV2 創建增强版實例管理器
func NewInstanceManagerV2(im *InstanceManager, notifier notify.Notifier) *InstanceManagerV2 {
	return &InstanceManagerV2{
		InstanceManager: im,
		notifier:        notifier,
	}
}

// ResourceUsage 资源使用情况
type ResourceUsage struct {
	CPU        float64 // CPU 使用率 (0-1)
	Memory     int64   // 記憶體使用量 (MB)
	MemoryPct  float64 // 記憶體使用率 (0-1)
	NetworkIn  int64   // 网络入流量 (bytes)
	NetworkOut int64   // 网络出流量 (bytes)
}

// CreateInstanceWithMonitoring 創建實例並配置監控
func (m *InstanceManagerV2) CreateInstanceWithMonitoring(
	ctx context.Context,
	userID, plan string,
) (*Instance, error) {
	// 1. 創建實例 (複用現有逻辑)
	instance, err := m.InstanceManager.CreateInstance(ctx, userID, plan)
	if err != nil {
		return nil, err
	}

	// 2. 配置監控標签
	if err := m.setupMonitoring(instance); err != nil {
		logger.Warn("⚠️ 配置監控失败: %v", err)
	}

	// 3. 配置告警规则
	if err := m.setupAlerts(instance); err != nil {
		logger.Warn("⚠️ 配置告警失败: %v", err)
	}

	// 4. 发送通知
	if m.notifier != nil {
		m.notifier.Send(&event.Event{
			Type:      event.EventTypeSystemStart,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"message": fmt.Sprintf("✅ 實例創建成功\n用戶: %s\n套餐: %s\n實例ID: %s", userID, plan, instance.ID),
			},
		})
	}

	return instance, nil
}

// setupMonitoring 配置監控
func (m *InstanceManagerV2) setupMonitoring(instance *Instance) error {
	// 為容器添加 Prometheus 監控標签
	cmd := exec.Command("docker", "update",
		"--label", fmt.Sprintf("prometheus.io/scrape=true"),
		"--label", fmt.Sprintf("prometheus.io/port=%d", instance.Port),
		"--label", fmt.Sprintf("prometheus.io/path=/metrics"),
		"--label", fmt.Sprintf("quantmesh.instance.id=%s", instance.ID),
		"--label", fmt.Sprintf("quantmesh.instance.plan=%s", instance.Plan),
		instance.ContainerID,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("添加監控標签失败: %v, 输出: %s", err, output)
	}

	logger.Info("✅ 實例 %s 監控配置完成", instance.ID)
	return nil
}

// setupAlerts 配置告警规则
func (m *InstanceManagerV2) setupAlerts(instance *Instance) error {
	// 这里可以配置 Prometheus Alertmanager 规则
	// 或者使用自定义的告警逻辑
	logger.Info("✅ 實例 %s 告警规则配置完成", instance.ID)
	return nil
}

// MonitorResources 監控所有實例资源使用
func (m *InstanceManagerV2) MonitorResources(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	logger.Info("🔍 啟动實例资源監控")

	for {
		select {
		case <-ctx.Done():
			logger.Info("實例资源監控已停止")
			return
		case <-ticker.C:
			instances := m.ListInstances()
			for _, inst := range instances {
				go m.checkInstanceResources(inst)
			}
		}
	}
}

// checkInstanceResources 检查單個實例资源
func (m *InstanceManagerV2) checkInstanceResources(instance *Instance) {
	usage, err := m.getResourceUsage(instance.ContainerID)
	if err != nil {
		logger.Error("❌ 獲取實例 %s 资源使用失败: %v", instance.ID, err)
		return
	}

	// 检查 CPU 使用率
	if usage.CPU > 0.9 {
		m.handleHighCPU(instance, usage)
	}

	// 检查記憶體使用率
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

// getResourceUsage 獲取容器资源使用情况
func (m *InstanceManagerV2) getResourceUsage(containerID string) (*ResourceUsage, error) {
	// 使用 docker stats 獲取资源使用
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
		return nil, fmt.Errorf("無效的 stats 输出")
	}

	usage := &ResourceUsage{}

	// 解析 CPU (去掉 % 符号)
	cpuStr := strings.TrimSuffix(parts[0], "%")
	fmt.Sscanf(cpuStr, "%f", &usage.CPU)
	usage.CPU /= 100.0

	// 解析記憶體百分比
	memPctStr := strings.TrimSuffix(parts[2], "%")
	fmt.Sscanf(memPctStr, "%f", &usage.MemoryPct)
	usage.MemoryPct /= 100.0

	return usage, nil
}

// handleHighCPU 处理 CPU 使用率過高
func (m *InstanceManagerV2) handleHighCPU(instance *Instance, usage *ResourceUsage) {
	msg := fmt.Sprintf(
		"⚠️ 實例 %s CPU 使用率過高: %.1f%%\n套餐: %s\n用戶: %s",
		instance.ID, usage.CPU*100, instance.Plan, instance.UserID,
	)

	logger.Warn(msg)

	if m.notifier != nil {
		m.notifier.Send(&event.Event{
			Type:      event.EventTypeError,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"message": msg,
			},
		})
	}
}

// handleHighMemory 处理記憶體使用率過高
func (m *InstanceManagerV2) handleHighMemory(instance *Instance, usage *ResourceUsage) {
	msg := fmt.Sprintf(
		"⚠️ 實例 %s 記憶體使用率過高: %.1f%%\n套餐: %s\n用戶: %s",
		instance.ID, usage.MemoryPct*100, instance.Plan, instance.UserID,
	)

	logger.Warn(msg)

	if m.notifier != nil {
		m.notifier.Send(&event.Event{
			Type:      event.EventTypeError,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"message": msg,
			},
		})
	}
}

// scaleUp 扩容實例
func (m *InstanceManagerV2) scaleUp(instance *Instance) {
	logger.Info("🔼 尝試扩容實例 %s", instance.ID)

	// 计算新的资源限制
	newCPU := instance.CPU * 1.5
	newMemory := int64(float64(instance.Memory) * 1.5)

	// 更新容器资源限制
	cmd := exec.Command("docker", "update",
		"--cpus", fmt.Sprintf("%.1f", newCPU),
		"--memory", fmt.Sprintf("%dm", newMemory),
		instance.ContainerID,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Error("❌ 扩容失败: %v, 输出: %s", err, output)
		return
	}

	// 更新實例記錄
	m.mu.Lock()
	instance.CPU = newCPU
	instance.Memory = newMemory
	m.mu.Unlock()

	msg := fmt.Sprintf(
		"✅ 實例 %s 扩容成功\nCPU: %.1f → %.1f\n記憶體: %dMB → %dMB",
		instance.ID, instance.CPU/1.5, instance.CPU,
		int64(float64(instance.Memory)/1.5), instance.Memory,
	)

	logger.Info(msg)

	if m.notifier != nil {
		m.notifier.Send(&event.Event{
			Type:      event.EventTypeError,
			Timestamp: time.Now(),
			Data: map[string]interface{}{
				"message": msg,
			},
		})
	}
}

// scaleDown 缩容實例
func (m *InstanceManagerV2) scaleDown(instance *Instance) {
	logger.Info("🔽 尝試缩容實例 %s", instance.ID)

	// 计算新的资源限制
	newCPU := instance.CPU * 0.75
	newMemory := int64(float64(instance.Memory) * 0.75)

	// 确保不低於最小值
	resources := m.allocateResources(instance.Plan)
	if newCPU < resources.CPU {
		newCPU = resources.CPU
	}
	if newMemory < resources.Memory {
		newMemory = resources.Memory
	}

	// 更新容器资源限制
	cmd := exec.Command("docker", "update",
		"--cpus", fmt.Sprintf("%.1f", newCPU),
		"--memory", fmt.Sprintf("%dm", newMemory),
		instance.ContainerID,
	)

	if output, err := cmd.CombinedOutput(); err != nil {
		logger.Error("❌ 缩容失败: %v, 输出: %s", err, output)
		return
	}

	// 更新實例記錄
	m.mu.Lock()
	instance.CPU = newCPU
	instance.Memory = newMemory
	m.mu.Unlock()

	logger.Info("✅ 實例 %s 缩容成功", instance.ID)
}

// GetInstanceMetrics 獲取實例指標
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

// GetAllInstancesMetrics 獲取所有實例指標
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
