package saas

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"quantmesh/logger"
)

// Instance 用戶實例
type Instance struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	Plan        string    `json:"plan"`   // starter/professional/enterprise
	Status      string    `json:"status"` // running/stopped/error
	ContainerID string    `json:"container_id"`
	Port        int       `json:"port"`
	CPU         float64   `json:"cpu"`    // CPU核心數
	Memory      int64     `json:"memory"` // 記憶體MB
	CreatedAt   time.Time `json:"created_at"`
	LastActive  time.Time `json:"last_active"`
}

// Resources 资源配置
type Resources struct {
	CPU    float64
	Memory int64
	Disk   int64
}

// InstanceManager 實例管理器
type InstanceManager struct {
	instances   map[string]*Instance
	mu          sync.RWMutex
	db          *sql.DB
	portCounter int
	portMu      sync.Mutex
}

// NewInstanceManager 創建實例管理器
func NewInstanceManager(db *sql.DB) *InstanceManager {
	return &InstanceManager{
		instances:   make(map[string]*Instance),
		db:          db,
		portCounter: 8000,
	}
}

// CreateInstance 創建新實例
func (m *InstanceManager) CreateInstance(ctx context.Context, userID, plan string) (*Instance, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 1. 检查用戶是否已有實例
	if existing := m.getUserInstance(userID); existing != nil {
		return nil, fmt.Errorf("用戶已有运行中的實例: %s", existing.ID)
	}

	// 2. 生成實例ID
	instanceID := generateInstanceID(userID)

	// 3. 分配资源
	resources := m.allocateResources(plan)

	// 4. 創建數據目錄
	dataDir := fmt.Sprintf("/data/instances/%s", instanceID)
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("創建數據目錄失败: %v", err)
	}

	// 5. 生成配置文件
	configPath := fmt.Sprintf("%s/config.yaml", dataDir)
	if err := m.generateConfig(userID, plan, configPath); err != nil {
		return nil, fmt.Errorf("生成配置文件失败: %v", err)
	}

	// 6. 啟动Docker容器
	containerID, port, err := m.startContainer(instanceID, dataDir, resources)
	if err != nil {
		os.RemoveAll(dataDir)
		return nil, fmt.Errorf("啟动容器失败: %v", err)
	}

	// 7. 創建實例記錄
	instance := &Instance{
		ID:          instanceID,
		UserID:      userID,
		Plan:        plan,
		Status:      "running",
		ContainerID: containerID,
		Port:        port,
		CPU:         resources.CPU,
		Memory:      resources.Memory,
		CreatedAt:   time.Now(),
		LastActive:  time.Now(),
	}

	m.instances[instanceID] = instance

	// 8. 保存到數據库
	if err := m.saveToDatabase(instance); err != nil {
		m.stopContainer(containerID)
		os.RemoveAll(dataDir)
		delete(m.instances, instanceID)
		return nil, fmt.Errorf("保存到數據库失败: %v", err)
	}

	logger.Info("✅ 實例創建成功: %s (用戶: %s, 套餐: %s)", instanceID, userID, plan)
	return instance, nil
}

// allocateResources 根據套餐分配资源
func (m *InstanceManager) allocateResources(plan string) *Resources {
	switch plan {
	case "starter":
		return &Resources{
			CPU:    1.0,
			Memory: 1024,  // 1GB
			Disk:   10240, // 10GB
		}
	case "professional":
		return &Resources{
			CPU:    2.0,
			Memory: 2048,  // 2GB
			Disk:   51200, // 50GB
		}
	case "enterprise":
		return &Resources{
			CPU:    4.0,
			Memory: 8192,   // 8GB
			Disk:   204800, // 200GB
		}
	default:
		return &Resources{
			CPU:    1.0,
			Memory: 1024,
			Disk:   10240,
		}
	}
}

// startContainer 啟动Docker容器
func (m *InstanceManager) startContainer(instanceID, dataDir string, resources *Resources) (string, int, error) {
	port := m.allocatePort()

	args := []string{
		"run", "-d",
		"--name", instanceID,
		"--cpus", fmt.Sprintf("%.1f", resources.CPU),
		"--memory", fmt.Sprintf("%dm", resources.Memory),
		"-p", fmt.Sprintf("%d:8080", port),
		"-v", fmt.Sprintf("%s:/data", dataDir),
		"-e", fmt.Sprintf("INSTANCE_ID=%s", instanceID),
		"-e", "CONFIG_PATH=/data/config.yaml",
		"--restart", "unless-stopped",
		"quantmesh:latest",
	}

	cmd := exec.Command("docker", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", 0, fmt.Errorf("docker run 失败: %v, 输出: %s", err, output)
	}

	containerID := strings.TrimSpace(string(output))
	logger.Info("容器已啟动: %s (端口: %d)", containerID[:12], port)

	return containerID, port, nil
}

// allocatePort 分配端口
func (m *InstanceManager) allocatePort() int {
	m.portMu.Lock()
	defer m.portMu.Unlock()

	m.portCounter++
	return m.portCounter
}

// StopInstance 停止實例
func (m *InstanceManager) StopInstance(instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return fmt.Errorf("實例不存在: %s", instanceID)
	}

	if instance.Status == "stopped" {
		return fmt.Errorf("實例已停止")
	}

	// 停止容器
	if err := m.stopContainer(instance.ContainerID); err != nil {
		return err
	}

	instance.Status = "stopped"
	logger.Info("✅ 實例已停止: %s", instanceID)

	return m.updateDatabase(instance)
}

// stopContainer 停止Docker容器
func (m *InstanceManager) stopContainer(containerID string) error {
	cmd := exec.Command("docker", "stop", containerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker stop 失败: %v, 输出: %s", err, output)
	}
	return nil
}

// StartInstance 啟动已停止的實例
func (m *InstanceManager) StartInstance(instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return fmt.Errorf("實例不存在: %s", instanceID)
	}

	if instance.Status == "running" {
		return fmt.Errorf("實例已在运行")
	}

	// 啟动容器
	cmd := exec.Command("docker", "start", instance.ContainerID)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("docker start 失败: %v, 输出: %s", err, output)
	}

	instance.Status = "running"
	instance.LastActive = time.Now()
	logger.Info("✅ 實例已啟动: %s", instanceID)

	return m.updateDatabase(instance)
}

// RestartInstance 重啟實例
func (m *InstanceManager) RestartInstance(instanceID string) error {
	if err := m.StopInstance(instanceID); err != nil {
		return err
	}
	time.Sleep(2 * time.Second)
	return m.StartInstance(instanceID)
}

// DeleteInstance 刪除實例
func (m *InstanceManager) DeleteInstance(instanceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return fmt.Errorf("實例不存在: %s", instanceID)
	}

	// 1. 停止容器
	m.stopContainer(instance.ContainerID)

	// 2. 刪除容器
	cmd := exec.Command("docker", "rm", instance.ContainerID)
	cmd.Run()

	// 3. 备份數據
	dataDir := fmt.Sprintf("/data/instances/%s", instanceID)
	backupPath := fmt.Sprintf("/data/backups/%s-%d.tar.gz", instanceID, time.Now().Unix())
	if err := m.backupInstanceData(dataDir, backupPath); err != nil {
		logger.Warn("⚠️ 备份實例數據失败: %v", err)
	}

	// 4. 刪除數據目錄
	os.RemoveAll(dataDir)

	// 5. 從記憶體刪除
	delete(m.instances, instanceID)

	// 6. 從數據库刪除
	logger.Info("✅ 實例已刪除: %s", instanceID)
	return m.deleteFromDatabase(instanceID)
}

// GetInstance 獲取實例信息
func (m *InstanceManager) GetInstance(instanceID string) (*Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance, exists := m.instances[instanceID]
	if !exists {
		return nil, fmt.Errorf("實例不存在: %s", instanceID)
	}

	return instance, nil
}

// GetUserInstance 獲取用戶的實例
func (m *InstanceManager) GetUserInstance(userID string) (*Instance, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instance := m.getUserInstance(userID)
	if instance == nil {
		return nil, fmt.Errorf("用戶没有實例")
	}

	return instance, nil
}

// getUserInstance 内部方法：獲取用戶實例 (不加鎖)
func (m *InstanceManager) getUserInstance(userID string) *Instance {
	for _, instance := range m.instances {
		if instance.UserID == userID {
			return instance
		}
	}
	return nil
}

// ListInstances 列出所有實例
func (m *InstanceManager) ListInstances() []*Instance {
	m.mu.RLock()
	defer m.mu.RUnlock()

	instances := make([]*Instance, 0, len(m.instances))
	for _, instance := range m.instances {
		instances = append(instances, instance)
	}

	return instances
}

// MonitorInstances 監控所有實例
func (m *InstanceManager) MonitorInstances(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	logger.Info("🔍 實例監控已啟动")

	for {
		select {
		case <-ctx.Done():
			logger.Info("實例監控已停止")
			return
		case <-ticker.C:
			m.checkAllInstances()
		}
	}
}

// checkAllInstances 检查所有實例
func (m *InstanceManager) checkAllInstances() {
	m.mu.RLock()
	instances := make([]*Instance, 0, len(m.instances))
	for _, instance := range m.instances {
		instances = append(instances, instance)
	}
	m.mu.RUnlock()

	for _, instance := range instances {
		go m.checkInstanceHealth(instance)
	}
}

// checkInstanceHealth 检查實例健康状態
func (m *InstanceManager) checkInstanceHealth(instance *Instance) {
	// 1. 检查容器是否运行
	cmd := exec.Command("docker", "inspect", "-f", "{{.State.Running}}", instance.ContainerID)
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "true" {
		logger.Error("❌ 實例 %s 容器未运行，尝試重啟", instance.ID)
		m.RestartInstance(instance.ID)
		return
	}

	// 2. 检查HTTP健康端点
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://localhost:%d/health", instance.Port))
	if err != nil {
		logger.Warn("⚠️ 實例 %s 健康检查失败: %v", instance.ID, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		logger.Warn("⚠️ 實例 %s 健康检查返回非200状態碼: %d", instance.ID, resp.StatusCode)
		return
	}

	// 3. 更新最后活跃時间
	m.mu.Lock()
	instance.LastActive = time.Now()
	m.mu.Unlock()
}

// generateConfig 生成配置文件
func (m *InstanceManager) generateConfig(userID, plan, configPath string) error {
	// 根據套餐生成不同的配置
	config := fmt.Sprintf(`app:
  current_exchange: "binance"

exchanges:
  binance:
    api_key: ""
    secret_key: ""
    fee_rate: 0.0002

trading:
  symbol: "BTCUSDT"
  price_interval: 1.0
  order_quantity: 30.0
  buy_window_size: %d
  sell_window_size: %d

system:
  log_level: "INFO"
  cancel_on_exit: true

plugins:
  enabled: %t
`, m.getBuyWindowSize(plan), m.getSellWindowSize(plan), plan != "starter")

	return os.WriteFile(configPath, []byte(config), 0644)
}

// getBuyWindowSize 根據套餐獲取買單窗口大小
func (m *InstanceManager) getBuyWindowSize(plan string) int {
	switch plan {
	case "starter":
		return 50
	case "professional":
		return 100
	case "enterprise":
		return 200
	default:
		return 50
	}
}

// getSellWindowSize 根據套餐獲取賣單視窗大小
func (m *InstanceManager) getSellWindowSize(plan string) int {
	return m.getBuyWindowSize(plan)
}

// backupInstanceData 备份實例數據
func (m *InstanceManager) backupInstanceData(dataDir, backupPath string) error {
	// 确保备份目錄存在
	backupDir := "/data/backups"
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return err
	}

	// 使用tar压缩
	cmd := exec.Command("tar", "-czf", backupPath, "-C", dataDir, ".")
	return cmd.Run()
}

// saveToDatabase 保存實例到數據库
func (m *InstanceManager) saveToDatabase(instance *Instance) error {
	query := `
		INSERT INTO instances (id, user_id, plan, status, container_id, port, cpu, memory, created_at, last_active)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err := m.db.Exec(query,
		instance.ID,
		instance.UserID,
		instance.Plan,
		instance.Status,
		instance.ContainerID,
		instance.Port,
		instance.CPU,
		instance.Memory,
		instance.CreatedAt,
		instance.LastActive,
	)
	return err
}

// updateDatabase 更新數據库中的實例
func (m *InstanceManager) updateDatabase(instance *Instance) error {
	query := `
		UPDATE instances 
		SET status = ?, last_active = ?
		WHERE id = ?
	`
	_, err := m.db.Exec(query, instance.Status, instance.LastActive, instance.ID)
	return err
}

// deleteFromDatabase 從數據库刪除實例
func (m *InstanceManager) deleteFromDatabase(instanceID string) error {
	query := `DELETE FROM instances WHERE id = ?`
	_, err := m.db.Exec(query, instanceID)
	return err
}

// LoadFromDatabase 從數據库加載所有實例
func (m *InstanceManager) LoadFromDatabase() error {
	query := `
		SELECT id, user_id, plan, status, container_id, port, cpu, memory, created_at, last_active
		FROM instances
	`
	rows, err := m.db.Query(query)
	if err != nil {
		return err
	}
	defer rows.Close()

	m.mu.Lock()
	defer m.mu.Unlock()

	for rows.Next() {
		instance := &Instance{}
		err := rows.Scan(
			&instance.ID,
			&instance.UserID,
			&instance.Plan,
			&instance.Status,
			&instance.ContainerID,
			&instance.Port,
			&instance.CPU,
			&instance.Memory,
			&instance.CreatedAt,
			&instance.LastActive,
		)
		if err != nil {
			logger.Warn("⚠️ 加載實例失败: %v", err)
			continue
		}

		m.instances[instance.ID] = instance
	}

	logger.Info("✅ 從數據库加載了 %d 個實例", len(m.instances))
	return nil
}

// generateInstanceID 生成實例ID
func generateInstanceID(userID string) string {
	return fmt.Sprintf("qm-%s-%d", userID[:8], time.Now().Unix())
}
