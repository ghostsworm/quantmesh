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

// SetInstanceManager 設置實例管理器
func SetInstanceManager(im *saas.InstanceManagerV2) {
	instanceManagerV2 = im
}

// createInstanceHandler 創建實例
// POST /api/saas/instances/create
func createInstanceHandler(c *gin.Context) {
	var req struct {
		Plan string `json:"plan" binding:"required"` // starter/professional/enterprise
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(400, gin.H{"error": "無效的请求参數"})
		return
	}

	// 驗证套餐
	validPlans := map[string]bool{
		"starter":      true,
		"professional": true,
		"enterprise":   true,
	}

	if !validPlans[req.Plan] {
		c.JSON(400, gin.H{"error": "無效的套餐類型"})
		return
	}

	// 從 session 或 JWT 中獲取用戶ID (这里简化处理)
	userID := c.GetString("user_id")
	if userID == "" {
		userID = "demo_user" // 演示用
	}

	// 創建實例
	instance, err := instanceManagerV2.CreateInstanceWithMonitoring(c.Request.Context(), userID, req.Plan)
	if err != nil {
		logger.Error("創建實例失败: %v", err)
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

// getInstanceHandler 獲取實例信息
// GET /api/saas/instances/:id
func getInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	// 驗证权限 (简化处理)
	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权访问"})
		return
	}

	c.JSON(200, gin.H{
		"instance": instance,
	})
}

// listInstancesHandler 列出所有實例
// GET /api/saas/instances
func listInstancesHandler(c *gin.Context) {
	instances := instanceManagerV2.ListInstances()

	// 如果有用戶ID,只返回該用戶的實例
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

// stopInstanceHandler 停止實例
// POST /api/saas/instances/:id/stop
func stopInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 驗证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权操作"})
		return
	}

	// 停止實例
	if err := instanceManagerV2.StopInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已停止"})
}

// startInstanceHandler 啟动實例
// POST /api/saas/instances/:id/start
func startInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 驗证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权操作"})
		return
	}

	// 啟动實例
	if err := instanceManagerV2.StartInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已啟动"})
}

// restartInstanceHandler 重啟實例
// POST /api/saas/instances/:id/restart
func restartInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 驗证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权操作"})
		return
	}

	// 重啟實例
	if err := instanceManagerV2.RestartInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已重啟"})
}

// deleteInstanceHandler 刪除實例
// DELETE /api/saas/instances/:id
func deleteInstanceHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 驗证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权操作"})
		return
	}

	// 刪除實例
	if err := instanceManagerV2.DeleteInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已刪除"})
}

// getInstanceLogsHandler 獲取實例日志
// GET /api/saas/instances/:id/logs
func getInstanceLogsHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 驗证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权访问"})
		return
	}

	// 獲取日志行數
	lines := 1000
	if linesStr := c.Query("lines"); linesStr != "" {
		if l, err := strconv.Atoi(linesStr); err == nil {
			lines = l
		}
	}

	// 獲取容器日志
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

// getInstanceMetricsHandler 獲取實例指標
// GET /api/saas/instances/:id/metrics
func getInstanceMetricsHandler(c *gin.Context) {
	instanceID := c.Param("id")

	// 驗证权限
	instance, err := instanceManagerV2.GetInstance(instanceID)
	if err != nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return
	}

	userID := c.GetString("user_id")
	if userID != "" && instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無权访问"})
		return
	}

	// 獲取指標
	metrics, err := instanceManagerV2.GetInstanceMetrics(instanceID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, metrics)
}

// getAllInstancesMetricsHandler 獲取所有實例指標
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

// getDockerLogs 獲取 Docker 容器日志
func getDockerLogs(containerID string, lines int) ([]string, error) {
	cmd := exec.Command("docker", "logs", "--tail", fmt.Sprintf("%d", lines), containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("獲取日志失败: %v", err)
	}

	logs := strings.Split(string(output), "\n")
	return logs, nil
}
