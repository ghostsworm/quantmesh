package web

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"quantmesh/logger"
	"quantmesh/saas"
)

// SaaS API 处理器

var (
	instanceManagerV2 *saas.InstanceManagerV2
)

// SetInstanceManager 设置实例管理器
func SetInstanceManager(im *saas.InstanceManagerV2) {
	instanceManagerV2 = im
}

// createInstanceHandler 创建实例
// POST /api/saas/instances/create
func createInstanceHandler(c *gin.Context) {
	var req struct {
		Plan string `json:"plan" binding:"required"` // starter/professional/enterprise
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "无效的请求参数"})
		return
	}

	// 验证套餐
	validPlans := map[string]bool{
		"starter":      true,
		"professional": true,
		"enterprise":   true,
	}

	if !validPlans[req.Plan] {
		c.JSON(400, gin.H{"error": "无效的套餐类型"})
		return
	}

	// 从 session 或 JWT 中获取用户ID (这里简化处理)
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "demo_user" // 演示用
	}

	// 创建实例
	instance, err := instanceManagerV2.CreateInstanceWithMonitoring(c.Request.Context(), userID, req.Plan)
	if err != nil {
		logger.Error("创建实例失败: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"instance_id": instance.ID,
		"status":      instance.Status,
		"plan":        instance.Plan,
		"url":         fmt.Sprintf("https://%s.quantmesh.cloud", instance.ID),
		"port":        instance.Port,
		"created_at":  instance.CreatedAt,
	})
}

// getInstanceHandler 获取实例信息
// GET /api/saas/instances/:id
func getInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	// 验证权限 (简化处理)
	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权访问"})
		return
	}

	c.JSON(200, gin.H{
		"instance": instance,
	})
}

// listInstancesHandler 列出所有实例
// GET /api/saas/instances
func listInstancesHandler(c *gin.Context) {
	instances := instanceManagerV2.ListInstances()

	// 如果有用户ID,只返回该用户的实例
	userID := c.GetString("user_id")
	if userID != "" {
		filtered := []*saas.Instance{}
		for _, inst := range instances {
			if inst.UserID == userID {
				filtered = append(filtered, inst)
			}
		}
		instances = filtered
	}

	c.JSON(200, gin.H{
		"instances": instances,
		"total":     len(instances),
	})
}

// stopInstanceHandler 停止实例
// POST /api/saas/instances/:id/stop
func stopInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 验证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权操作"})
		return
	}

	// 停止实例
	if err := instanceManagerV2.StopInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "实例已停止"})
}

// startInstanceHandler 启动实例
// POST /api/saas/instances/:id/start
func startInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 验证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权操作"})
		return
	}

	// 启动实例
	if err := instanceManagerV2.StartInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "实例已启动"})
}

// restartInstanceHandler 重启实例
// POST /api/saas/instances/:id/restart
func restartInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 验证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权操作"})
		return
	}

	// 重启实例
	if err := instanceManagerV2.RestartInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "实例已重启"})
}

// deleteInstanceHandler 删除实例
// DELETE /api/saas/instances/:id
func deleteInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 验证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权操作"})
		return
	}

	// 删除实例
	if err := instanceManagerV2.DeleteInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "实例已删除"})
}

// getInstanceLogsHandler 获取实例日志
// GET /api/saas/instances/:id/logs
func getInstanceLogsHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 验证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权访问"})
		return
	}

	// 获取日志行数
	lines := 1000
	if linesStr := c.Query("lines"); linesStr != "" {
		if l, err := strconv.Atoi(linesStr); err == nil {
			lines = l
		}
	}

	// 获取容器日志
	logs, err := getDockerLogs(instance.ContainerID, lines)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"logs":  logs,
		"lines": len(logs),
	})
}

// getInstanceMetricsHandler 获取实例指标
// GET /api/saas/instances/:id/metrics
func getInstanceMetricsHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 验证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "实例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "无权访问"})
		return
	}

	// 获取指标
	metrics, err := instanceManagerV2.GetInstanceMetrics(instanceID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, metrics)
}

// getAllInstancesMetricsHandler 获取所有实例指标
// GET /api/saas/metrics
func getAllInstancesMetricsHandler(c *gin.Context) {
	metrics, err := instanceManagerV2.GetAllInstancesMetrics()
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{
		"metrics": metrics,
		"total":   len(metrics),
	})
}

// getDockerLogs 获取 Docker 容器日志
func getDockerLogs(containerID string, lines int) ([]string, error) {
	cmd := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", lines), containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("获取日志失败: %v", err)
	}

	logs := strings.Split(string(output), "\n")
	return logs, nil
}
