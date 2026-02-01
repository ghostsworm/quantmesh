package config

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// ConfigHistory 配置历史記錄
type ConfigHistory struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Version     int       `gorm:"uniqueIndex" json:"version"`        // 版本号（自增）
	Content     string    `gorm:"type:text;not null" json:"content"` // YAML 内容
	Description string    `gorm:"size:500" json:"description"`       // 变更描述
	CreatedAt   time.Time `gorm:"index" json:"created_at"`           // 备份時间
	CreatedBy   string    `gorm:"size:100" json:"created_by"`        // 操作者（可選）
}

// ConfigHistoryListItem 配置历史列表项（不含完整内容）
type ConfigHistoryListItem struct {
	ID          uint      `json:"id"`
	Version     int       `json:"version"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	CreatedBy   string    `json:"created_by"`
	Size        int       `json:"size"` // 内容大小（字节）
}

// HistoryDiffRequest 版本對比请求
type HistoryDiffRequest struct {
	SourceVersion int `json:"source_version"` // 源版本（0 表示當前版本）
	TargetVersion int `json:"target_version"` // 目標版本
}

// HistoryDiffResponse 版本對比响应
type HistoryDiffResponse struct {
	SourceVersion int       `json:"source_version"`
	TargetVersion int       `json:"target_version"`
	SourceContent string    `json:"source_content"` // 源版本 YAML
	TargetContent string    `json:"target_content"` // 目標版本 YAML
	SourceTime    time.Time `json:"source_time"`    // 源版本時间
	TargetTime    time.Time `json:"target_time"`    // 目標版本時间
}

// HistoryManager 配置历史管理器
type HistoryManager struct {
	db         *gorm.DB
	configPath string
}

// NewHistoryManager 創建配置历史管理器
func NewHistoryManager(dataDir string, configPath string) (*HistoryManager, error) {
	// 確保資料目錄存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("創建數據目錄失败: %v", err)
	}

	// 數據库文件路径
	dbPath := filepath.Join(dataDir, "config_history.db")

	// 打开數據库
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return nil, fmt.Errorf("打开历史數據库失败: %v", err)
	}

	// 自动迁移
	if err := db.AutoMigrate(&ConfigHistory{}); err != nil {
		return nil, fmt.Errorf("迁移历史表失败: %v", err)
	}

	return &HistoryManager{
		db:         db,
		configPath: configPath,
	}, nil
}

// SaveHistory 保存配置历史
func (hm *HistoryManager) SaveHistory(content string, description string, createdBy string) (*ConfigHistory, error) {
	// 獲取下一個版本号
	var maxVersion int
	hm.db.Model(&ConfigHistory{}).Select("COALESCE(MAX(version), 0)").Scan(&maxVersion)
	nextVersion := maxVersion + 1

	history := &ConfigHistory{
		Version:     nextVersion,
		Content:     content,
		Description: description,
		CreatedAt:   time.Now(),
		CreatedBy:   createdBy,
	}

	if err := hm.db.Create(history).Error; err != nil {
		return nil, fmt.Errorf("保存历史記錄失败: %v", err)
	}

	return history, nil
}

// ListHistory 獲取歷史版本列表
func (hm *HistoryManager) ListHistory(limit int, offset int) ([]*ConfigHistoryListItem, int64, error) {
	var total int64
	hm.db.Model(&ConfigHistory{}).Count(&total)

	var histories []*ConfigHistory
	query := hm.db.Order("version DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	if err := query.Find(&histories).Error; err != nil {
		return nil, 0, fmt.Errorf("獲取歷史列表失败: %v", err)
	}

	// 轉换為列表项
	items := make([]*ConfigHistoryListItem, len(histories))
	for i, h := range histories {
		items[i] = &ConfigHistoryListItem{
			ID:          h.ID,
			Version:     h.Version,
			Description: h.Description,
			CreatedAt:   h.CreatedAt,
			CreatedBy:   h.CreatedBy,
			Size:        len(h.Content),
		}
	}

	return items, total, nil
}

// GetHistory 獲取指定版本的历史記錄
func (hm *HistoryManager) GetHistory(version int) (*ConfigHistory, error) {
	var history ConfigHistory
	if err := hm.db.Where("version = ?", version).First(&history).Error; err != nil {
		return nil, fmt.Errorf("獲取歷史版本 %d 失败: %v", version, err)
	}
	return &history, nil
}

// GetLatestHistory 獲取最新的历史記錄
func (hm *HistoryManager) GetLatestHistory() (*ConfigHistory, error) {
	var history ConfigHistory
	if err := hm.db.Order("version DESC").First(&history).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, fmt.Errorf("獲取最新历史版本失败: %v", err)
	}
	return &history, nil
}

// RestoreHistory 恢複到指定版本
func (hm *HistoryManager) RestoreHistory(version int) error {
	// 獲取歷史版本
	history, err := hm.GetHistory(version)
	if err != nil {
		return err
	}

	// 驗证配置内容
	cfg, err := LoadConfigFromBytes([]byte(history.Content))
	if err != nil {
		return fmt.Errorf("历史版本配置無效: %v", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("历史版本配置驗证失败: %v", err)
	}

	// 写入配置文件
	if err := os.WriteFile(hm.configPath, []byte(history.Content), 0644); err != nil {
		return fmt.Errorf("写入配置文件失败: %v", err)
	}

	return nil
}

// DiffVersions 對比两個版本
func (hm *HistoryManager) DiffVersions(sourceVersion, targetVersion int) (*HistoryDiffResponse, error) {
	response := &HistoryDiffResponse{
		SourceVersion: sourceVersion,
		TargetVersion: targetVersion,
	}

	// 獲取源版本内容
	if sourceVersion == 0 {
		// 0 表示當前配置
		content, err := os.ReadFile(hm.configPath)
		if err != nil {
			return nil, fmt.Errorf("读取當前配置失败: %v", err)
		}
		response.SourceContent = string(content)
		response.SourceTime = time.Now()
	} else {
		history, err := hm.GetHistory(sourceVersion)
		if err != nil {
			return nil, err
		}
		response.SourceContent = history.Content
		response.SourceTime = history.CreatedAt
	}

	// 獲取目標版本内容
	if targetVersion == 0 {
		// 0 表示當前配置
		content, err := os.ReadFile(hm.configPath)
		if err != nil {
			return nil, fmt.Errorf("读取當前配置失败: %v", err)
		}
		response.TargetContent = string(content)
		response.TargetTime = time.Now()
	} else {
		history, err := hm.GetHistory(targetVersion)
		if err != nil {
			return nil, err
		}
		response.TargetContent = history.Content
		response.TargetTime = history.CreatedAt
	}

	return response, nil
}

// CleanupOldHistory 清理舊历史記錄（保留最新的 n 条）
func (hm *HistoryManager) CleanupOldHistory(keepCount int) error {
	var total int64
	hm.db.Model(&ConfigHistory{}).Count(&total)

	if int(total) <= keepCount {
		return nil
	}

	// 獲取需要保留的最小版本号
	var minVersionToKeep int
	hm.db.Model(&ConfigHistory{}).
		Order("version DESC").
		Limit(1).
		Offset(keepCount).
		Pluck("version", &minVersionToKeep)

	// 刪除舊版本
	if minVersionToKeep > 0 {
		if err := hm.db.Where("version < ?", minVersionToKeep).Delete(&ConfigHistory{}).Error; err != nil {
			return fmt.Errorf("清理舊历史記錄失败: %v", err)
		}
	}

	return nil
}

// Close 关闭數據库连接
func (hm *HistoryManager) Close() error {
	sqlDB, err := hm.db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}
