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
	// BackupDirName 备份目錄名稱（位於 config.yaml 同級目錄）
	BackupDirName = "backups"
	// MaxBackups 最大备份數量（本地磁盘保留最近 10 個版本）
	MaxBackups = 10
)

// BackupInfo 备份信息
type BackupInfo struct {
	ID          string    `json:"id"`          // 备份ID（文件名）
	Timestamp   time.Time `json:"timestamp"`   // 备份時间
	FilePath    string    `json:"file_path"`   // 备份文件路径
	Size        int64     `json:"size"`        // 文件大小（字节）
	Description string    `json:"description"` // 描述信息（可選）
}

// BackupManager 配置备份管理器
type BackupManager struct {
	backupDir  string
	maxBackups int
}

// NewBackupManager 創建备份管理器，备份目錄為 config.yaml 同級的 backups/
func NewBackupManager(configPath string) *BackupManager {
	configDir := filepath.Dir(configPath)
	if configDir == "" || configDir == "." {
		configDir = "."
	}
	backupDir := filepath.Join(configDir, BackupDirName)
	return &BackupManager{
		backupDir:  backupDir,
		maxBackups: MaxBackups,
	}
}

// CreateBackup 創建配置备份
func (bm *BackupManager) CreateBackup(configPath string, description string) (*BackupInfo, error) {
	// 确保备份目錄存在（0776 確保運行用戶可寫入）
	if err := os.MkdirAll(bm.backupDir, 0776); err != nil {
		return nil, fmt.Errorf("創建备份目錄失败: %v", err)
	}

	// 读取當前配置文件
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

	// 獲取文件信息
	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("獲取备份文件信息失败: %v", err)
	}

	// 解析時间戳
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

	// 清理舊备份
	if err := bm.CleanOldBackups(); err != nil {
		// 清理失败不影响备份創建，只記錄錯误
		fmt.Printf("警告: 清理舊备份失败: %v\n", err)
	}

	return backupInfo, nil
}

// ListBackups 列出所有备份
func (bm *BackupManager) ListBackups() ([]*BackupInfo, error) {
	// 确保备份目錄存在
	if err := os.MkdirAll(bm.backupDir, 0776); err != nil {
		return nil, fmt.Errorf("創建备份目錄失败: %v", err)
	}

	// 读取备份目錄
	entries, err := os.ReadDir(bm.backupDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []*BackupInfo{}, nil
		}
		return nil, fmt.Errorf("读取备份目錄失败: %v", err)
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

		// 尝試解析時间戳
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

	// 按時间倒序排序（最新的在前）
	sort.Slice(backups, func(i, j int) bool {
		return backups[i].Timestamp.After(backups[j].Timestamp)
	})

	return backups, nil
}

// RestoreBackup 恢複指定备份
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

	// 驗证YAML格式
	var testConfig Config
	if err := yaml.Unmarshal(data, &testConfig); err != nil {
		return fmt.Errorf("备份文件格式無效: %v", err)
	}

	// 驗证配置
	if err := testConfig.Validate(); err != nil {
		return fmt.Errorf("备份配置驗证失败: %v", err)
	}

	// 写入目標文件
	if err := os.WriteFile(targetPath, data, 0644); err != nil {
		return fmt.Errorf("恢複配置文件失败: %v", err)
	}

	return nil
}

// DeleteBackup 刪除指定备份
func (bm *BackupManager) DeleteBackup(backupID string) error {
	backupPath := filepath.Join(bm.backupDir, backupID)

	// 检查备份文件是否存在
	if _, err := os.Stat(backupPath); err != nil {
		return fmt.Errorf("备份文件不存在: %v", err)
	}

	// 刪除文件
	if err := os.Remove(backupPath); err != nil {
		return fmt.Errorf("刪除备份文件失败: %v", err)
	}

	return nil
}

// CleanOldBackups 清理超出數量的舊备份
func (bm *BackupManager) CleanOldBackups() error {
	backups, err := bm.ListBackups()
	if err != nil {
		return err
	}

	// 如果备份數量不超過限制，不需要清理
	if len(backups) <= bm.maxBackups {
		return nil
	}

	// 刪除最舊的备份
	toDelete := backups[bm.maxBackups:]
	for _, backup := range toDelete {
		if err := bm.DeleteBackup(backup.ID); err != nil {
			// 刪除失败继续尝試刪除其他备份
			fmt.Printf("警告: 刪除舊备份失败 %s: %v\n", backup.ID, err)
		}
	}

	return nil
}

// GetBackup 獲取指定备份信息
func (bm *BackupManager) GetBackup(backupID string) (*BackupInfo, error) {
	backupPath := filepath.Join(bm.backupDir, backupID)

	fileInfo, err := os.Stat(backupPath)
	if err != nil {
		return nil, fmt.Errorf("备份文件不存在: %v", err)
	}

	timestamp, err := parseBackupTimestamp(backupID)
	if err != nil {
		return nil, fmt.Errorf("解析备份時间戳失败: %v", err)
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

// parseBackupTimestamp 解析备份文件中的時间戳
func parseBackupTimestamp(filename string) (time.Time, error) {
	// 格式: config.yaml.backup.20060102150405.yaml
	// 提取時间戳部分: 20060102150405
	if len(filename) < 34 {
		return time.Time{}, fmt.Errorf("备份文件名格式無效")
	}

	timestampStr := filename[19 : len(filename)-5]
	if len(timestampStr) != 14 {
		return time.Time{}, fmt.Errorf("時间戳长度無效")
	}

	return time.Parse("20060102150405", timestampStr)
}
