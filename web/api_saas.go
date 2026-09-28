package web

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

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

func getSaaSManager(c *gin.Context) (*saas.InstanceManagerV2, bool) {
	if instanceManagerV2 == nil || instanceManagerV2.InstanceManager == nil {
		c.JSON(503, gin.H{"error": "SaaS 實例服務未配置"})
		return nil, false
	}
	return instanceManagerV2, true
}

func getSaaSUserID(c *gin.Context) (string, bool) {
	userID := cryptoPaymentUserID(c)
	if userID == "" {
		c.JSON(401, gin.H{"error": "需要有效的用戶身份"})
		return "", false
	}
	return userID, true
}

func getOwnedSaaSInstance(c *gin.Context, manager *saas.InstanceManagerV2, userID, instanceID string) (*saas.Instance, bool) {
	instance, err := manager.GetInstance(instanceID)
	if err != nil || instance == nil {
		c.JSON(404, gin.H{"error": "實例不存在"})
		return nil, false
	}
	if instance.UserID != userID {
		c.JSON(403, gin.H{"error": "無權操作此實例"})
		return nil, false
	}
	return instance, true
}

func requireOwnedSaaSInstance(c *gin.Context) (*saas.InstanceManagerV2, *saas.Instance, bool) {
	manager, ok := getSaaSManager(c)
	if !ok {
		return nil, nil, false
	}
	userID, ok := getSaaSUserID(c)
	if !ok {
		return nil, nil, false
	}
	instance, ok := getOwnedSaaSInstance(c, manager, userID, c.Param("id"))
	if !ok {
		return nil, nil, false
	}
	return manager, instance, true
}

// createInstanceHandler 創建實例
// POST /api/saas/instances/create
func createInstanceHandler(c *gin.Context) {
	c.JSON(503, gin.H{"error": "SaaS 付費實例尚未接通已驗證的訂閱與支付履約，未建立實例"})
}

// getInstanceHandler 獲取實例信息
// GET /api/saas/instances/:id
func getInstanceHandler(c *gin.Context) {
	_, instance, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}

	c.JSON(200, gin.H{
		"instance": instance,
	})
}

// listInstancesHandler 列出所有實例
// GET /api/saas/instances
func listInstancesHandler(c *gin.Context) {
	manager, ok := getSaaSManager(c)
	if !ok {
		return
	}
	userID, ok := getSaaSUserID(c)
	if !ok {
		return
	}
	instances := manager.ListInstances()
	filtered := make([]*saas.Instance, 0, len(instances))
	for _, instance := range instances {
		if instance != nil && instance.UserID == userID {
			filtered = append(filtered, instance)
		}
	}
	instances = filtered

	c.JSON(200, gin.H{
		"instances": instances,
		"total":     len(instances),
	})
}

// stopInstanceHandler 停止實例
// POST /api/saas/instances/:id/stop
func stopInstanceHandler(c *gin.Context) {
	manager, _, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}
	instanceID := c.Param("id")

	// 停止實例
	if err := manager.StopInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已停止"})
}

// startInstanceHandler 啟动實例
// POST /api/saas/instances/:id/start
func startInstanceHandler(c *gin.Context) {
	manager, _, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}
	instanceID := c.Param("id")

	// 啟动實例
	if err := manager.StartInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已啟动"})
}

// restartInstanceHandler 重啟實例
// POST /api/saas/instances/:id/restart
func restartInstanceHandler(c *gin.Context) {
	manager, _, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}
	instanceID := c.Param("id")

	// 重啟實例
	if err := manager.RestartInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已重啟"})
}

// deleteInstanceHandler 刪除實例
// DELETE /api/saas/instances/:id
func deleteInstanceHandler(c *gin.Context) {
	manager, _, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}
	instanceID := c.Param("id")

	// 刪除實例
	if err := manager.DeleteInstance(instanceID); err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "實例已刪除"})
}

// getInstanceLogsHandler 獲取實例日志
// GET /api/saas/instances/:id/logs
func getInstanceLogsHandler(c *gin.Context) {
	_, instance, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}

	// 獲取日志行數
	lines := 1000
	if linesStr := c.Query("lines"); linesStr != "" {
		if l, err := strconv.Atoi(linesStr); err == nil {
			lines = l
		}
	}
	if lines < 1 {
		lines = 1
	}
	if lines > 1000 {
		lines = 1000
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
	manager, _, ok := requireOwnedSaaSInstance(c)
	if !ok {
		return
	}

	// 獲取指標
	metrics, err := manager.GetInstanceMetrics(c.Param("id"))
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, metrics)
}

// getAllInstancesMetricsHandler 獲取所有實例指標
// GET /api/saas/metrics
func getAllInstancesMetricsHandler(c *gin.Context) {
	manager, ok := getSaaSManager(c)
	if !ok {
		return
	}
	sessionValue, ok := c.Get("session")
	session, isSession := sessionValue.(*Session)
	if c.GetBool("local_dev_mode") || !ok || !isSession || session.Role != "admin" {
		c.JSON(403, gin.H{"error": "需要管理員權限"})
		return
	}
	metrics, err := manager.GetAllInstancesMetrics()
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
