package web

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// PasswordManager 密碼管理器
type PasswordManager struct {
	db          *sql.DB
	dbPath      string
	dataDir     string
	installFile string // .installed 標記文件路徑
}

// NewPasswordManager 創建密碼管理器
func NewPasswordManager(dataDir string) (*PasswordManager, error) {
	// 確保資料目錄存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("創建數據目錄失败: %v", err)
	}

	dbPath := filepath.Join(dataDir, "auth.db")
	installFile := filepath.Join(dataDir, ".installed")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开數據库失败: %v", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	pm := &PasswordManager{
		db:          db,
		dbPath:      dbPath,
		dataDir:     dataDir,
		installFile: installFile,
	}

	// 初始化數據库表
	if err := pm.initDatabase(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化數據库失败: %v", err)
	}

	return pm, nil
}

// initDatabase 初始化數據库表
func (pm *PasswordManager) initDatabase() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS users (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);
	`

	if _, err := pm.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("創建用戶表失败: %v", err)
	}

	// 創建索引
	indexSQL := "CREATE INDEX IF NOT EXISTS idx_users_username ON users(username);"
	if _, err := pm.db.Exec(indexSQL); err != nil {
		return fmt.Errorf("創建索引失败: %v", err)
	}

	return nil
}

// SetPassword 設置密碼（首次設置或修改）
func (pm *PasswordManager) SetPassword(username, password string) error {
	// 生成密碼哈希
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("生成密碼哈希失败: %v", err)
	}

	// 插入或更新用戶密碼
	_, err = pm.db.Exec(`
		INSERT INTO users (username, password_hash) 
		VALUES (?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = ?
	`, username, string(hash), string(hash))
	if err != nil {
		return fmt.Errorf("保存密碼失败: %v", err)
	}

	// 🔒 創建 .installed 標記文件，防止數據庫丟失後被重新初始化
	if err := pm.createInstalledMarker(); err != nil {
		// 僅記錄警告，不阻止密碼設置成功
		fmt.Printf("[WARN] 創建 .installed 標記文件失敗: %v\n", err)
	}

	return nil
}

// createInstalledMarker 創建 .installed 標記文件
// 此文件用於檢測系統是否已經完成過首次設置
// 即使 auth.db 被刪除，只要 .installed 存在，就會阻止重新設置密碼
func (pm *PasswordManager) createInstalledMarker() error {
	// 寫入安裝時間和基本信息
	content := fmt.Sprintf("installed_at=%s\nversion=1\n", time.Now().UTC().Format(time.RFC3339))
	return os.WriteFile(pm.installFile, []byte(content), 0644)
}

// IsInstalled 檢查系統是否已經安裝過（已完成首次設置）
// 返回 true 表示系統已安裝，返回 false 表示未安裝
func (pm *PasswordManager) IsInstalled() bool {
	_, err := os.Stat(pm.installFile)
	return err == nil
}

// IsSecurityCompromised 檢查是否存在安全隱患
// 如果 .installed 文件存在但數據庫中沒有密碼記錄，則可能存在數據丟失
// 這種情況應該警告用戶而不是允許重新設置密碼
func (pm *PasswordManager) IsSecurityCompromised() (bool, error) {
	// 檢查 .installed 標記文件是否存在
	if !pm.IsInstalled() {
		return false, nil // 未安裝過，不存在安全隱患
	}

	// 已安裝過，檢查數據庫中是否有用戶記錄
	var count int
	err := pm.db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
	if err != nil {
		return true, fmt.Errorf("查詢用戶數失敗: %v", err)
	}

	// 如果 .installed 存在但沒有用戶記錄，則數據可能已丟失
	if count == 0 {
		return true, nil
	}

	return false, nil
}

// VerifyPassword 驗证密碼
func (pm *PasswordManager) VerifyPassword(username, password string) (bool, error) {
	var passwordHash string
	err := pm.db.QueryRow("SELECT password_hash FROM users WHERE username = ?", username).Scan(&passwordHash)
	if err == sql.ErrNoRows {
		return false, nil // 用戶不存在
	}
	if err != nil {
		return false, fmt.Errorf("查詢用戶失败: %v", err)
	}

	// 驗证密碼
	err = bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(password))
	if err != nil {
		return false, nil // 密碼不匹配
	}

	return true, nil
}

// HasPassword 检查用戶是否已設置密碼
// 🔒 安全增強：如果 .installed 標記存在但數據庫中無記錄，仍返回 true
// 這可以防止數據庫被刪除後讓攻擊者繞過認證
func (pm *PasswordManager) HasPassword(username string) (bool, error) {
	var count int
	err := pm.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		// 如果數據庫查詢失敗，檢查 .installed 標記
		if pm.IsInstalled() {
			// 系統已安裝過，應該視為有密碼（即使查詢失敗）
			return true, fmt.Errorf("數據庫查詢失敗但系統已安裝: %v", err)
		}
		return false, fmt.Errorf("查詢用戶失败: %v", err)
	}

	// 🔒 安全檢查：如果 .installed 存在但數據庫中沒有用戶，仍視為已設置
	// 這防止了數據庫被刪除後繞過認證的攻擊
	if count == 0 && pm.IsInstalled() {
		return true, nil
	}

	return count > 0, nil
}

// Close 关闭數據库连接
func (pm *PasswordManager) Close() error {
	return pm.db.Close()
}
