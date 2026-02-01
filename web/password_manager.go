package web

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
	"golang.org/x/crypto/bcrypt"
)

// PasswordManager 密碼管理器
type PasswordManager struct {
	db     *sql.DB
	dbPath string
}

// NewPasswordManager 創建密碼管理器
func NewPasswordManager(dataDir string) (*PasswordManager, error) {
	// 確保資料目錄存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("創建數據目錄失败: %v", err)
	}

	dbPath := filepath.Join(dataDir, "auth.db")
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开數據库失败: %v", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)

	pm := &PasswordManager{
		db:     db,
		dbPath: dbPath,
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

	return nil
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
func (pm *PasswordManager) HasPassword(username string) (bool, error) {
	var count int
	err := pm.db.QueryRow("SELECT COUNT(*) FROM users WHERE username = ?", username).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("查詢用戶失败: %v", err)
	}
	return count > 0, nil
}

// Close 关闭數據库连接
func (pm *PasswordManager) Close() error {
	return pm.db.Close()
}
