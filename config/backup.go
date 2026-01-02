package config

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// BackupDir 备份目录
	BackupDir = "./config_backups"
	// MaxBackups 最大备份数量
	MaxBackups = 50
)

// BackupInfo 备份信息
type BackupInfo struct {
	ID          string    `json:"id"`          // 备份ID（文件名）
	Timestamp   time.Time `json:"timestamp"`   // 备份时间
	FilePath    string    `json:"file_path"`   // 备份文件路径
	Size        int64     `json:"size"`        // 文件大小（字节）
	Description string    `json:"description"` // 描述信息（可选）
}

// BackupManager 配置备份管理器
type BackupManager struct {
	backupDir  string
	maxBackups int
}

// NewBackupManager 创建备份管理器
func NewBackupManager() *BackupManager {
	return &BackupManager{
		backupDir:  BackupDir,
		maxBackups: MaxBackups,
	}
}

// CreateBackup 创建配置备份
func (bm *BackupManager) CreateBackup(configPath string, description string) (*BackupInfo, error) {
	// 确保备份目录存在
	if err := os.MkdirAll(bm.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %v", err)
	}

	// 读取当前配置文件
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %v", err)
	}

	// 生成备份文件名
	timestamp := time.Now().Format("20060102150405")
	backupFileName := fmt.Sprintf("config.yaml.backup.%s.yaml", timestamp)
	backupPath := filepath.Join(bm.backupDir, backupFileName)

	// 写入备份文件
	if err := os.WriteFile(backupPath, data, 0644); err != nil {
		return nil, fmt.Errorf("写入备份文件失败: %v", err)
	}

	// 获取文件信息
	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("获取备份文件信息失败: %v", err)
	}

	// 解析时间戳
	backupTime, err := time.Parse("20060102150405", timestamp)
	if err != nil {
		backupTime = time.Now()
	}

	backupInfo := &BackupInfo{
		ID:          backupFileName,
		Timestamp:   backupTime,
		FilePath:    backupPath,
		Size:        fileInfo.Size(),
		Description: description,
	}

	// 清理旧备份
	if err := bm.CleanOldBackups(); err != nil {
		// 清理失败不影响备份创建，只记录错误
		fmt.Printf("警告: 清理旧备份失败: %v\n", err)
	}

	return backupInfo, nil
}

// ListBackups 列出所有备份
func (bm *BackupManager) ListBackups() ([]*BackupInfo, error) {
	// 确保备份目录存在
	if err := os.MkdirAll(bm.backupDir, 0755); err != nil {
		return nil, fmt.Errorf("创建备份目录失败: %v", err)
	}

	// 读取备份目录
	entries, err := os.ReadDir(bm.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*BackupInfo{}, nil
		}
		return nil, fmt.Errorf("读取备份目录失败: %v", err)
	}

	var backups []*BackupInfo
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		// 只处理备份文件
		name := entry.Name()
		if !isBackupFile(name) {
			continue
		}

		filePath := filepath.Join(bm.backupDir, name)
		fileInfo, err := entry.Info()
		if err != nil {
			continue
		}

		// 尝试解析时间戳
		timestamp, err := parseBackupTimestamp(name)
		if err != nil {
			continue
		}

		backupInfo := &BackupInfo{
			ID:        name,
			Timestamp: timestamp,
			FilePath:  filePath,
			Size:      fileInfo.Size(),
		}

		backups = append(backups, backupInfo)
	}

	// 按时间倒序排序（最新的在前）
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

// RestoreBackup 恢复指定备份
func (bm *BackupManager) RestoreBackup(backupID string, targetPath string) error {
	backupPath := filepath.Join(bm.backupDir, backupID)

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("备份文件不存在: %v", err)
	}

	// 读取备份文件
	data, err := os.ReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("读取备份文件失败: %v", err)
	}

	// 验证YAML格式
	var testConfig Config
	if err := yaml.Unmarshal(data, &testConfig); err != nil {
		return fmt.Errorf("备份文件格式无效: %v", err)
	}

	// 验证配置
	if err := testConfig.Validate(); err != nil {
		return fmt.Errorf("备份配置验证失败: %v", err)
	}

	// 写入目标文件
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return fmt.Errorf("恢复配置文件失败: %v", err)
	}

	return nil
}

// DeleteBackup 删除指定备份
func (bm *BackupManager) DeleteBackup(backupID string) error {
	backupPath := filepath.Join(bm.backupDir, backupID)

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("备份文件不存在: %v", err)
	}

	// 删除文件
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("删除备份文件失败: %v", err)
	}

	return nil
}

// CleanOldBackups 清理超出数量的旧备份
func (bm *BackupManager) CleanOldBackups() error {
	backups, err := bm.ListBackups()
	if err != nil {
		return err
	}

	// 如果备份数量不超过限制，不需要清理
	if len(backups) <= bm.maxBackups {
		return nil
	}

	// 删除最旧的备份
	toDelete := backups[bm.maxBackups:]
	for _, backup := range toDelete {
		if err := bm.DeleteBackup(backup.ID); err != nil {
			// 删除失败继续尝试删除其他备份
			fmt.Printf("警告: 删除旧备份失败 %s: %v\n", backup.ID, err)
		}
	}

	return nil
}

// GetBackup 获取指定备份信息
func (bm *BackupManager) GetBackup(backupID string) (*BackupInfo, error) {
	backupPath := filepath.Join(bm.backupDir, backupID)

	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("备份文件不存在: %v", err)
	}

	timestamp, err := parseBackupTimestamp(backupID)
	if err != nil {
		return nil, fmt.Errorf("解析备份时间戳失败: %v", err)
	}

	return &BackupInfo{
		ID:        backupID,
		Timestamp: timestamp,
		FilePath:  backupPath,
		Size:      fileInfo.Size(),
	}, nil
}

// isBackupFile 判断是否是备份文件
func isBackupFile(filename string) bool {
	// 格式: config.yaml.backup.20060102150405.yaml
	return len(filename) > 30 &&
		filename[:19] == "config.yaml.backup." &&
		filename[len(filename)-5:] == ".yaml"
}

// parseBackupTimestamp 解析备份文件中的时间戳
func parseBackupTimestamp(filename string) (time.Time, error) {
	// 格式: config.yaml.backup.20060102150405.yaml
	// 提取时间戳部分: 20060102150405
	if len(filename) < 34 {
		return time.Time{}, fmt.Errorf("备份文件名格式无效")
	}

	timestampStr := filename[19 : len(filename)-5]
	if len(timestampStr) != 14 {
		return time.Time{}, fmt.Errorf("时间戳长度无效")
	}

	return time.Parse("20060102150405", timestampStr)
}
