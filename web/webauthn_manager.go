package web

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-webauthn/webauthn/webauthn"
	_ "github.com/mattn/go-sqlite3"
)

// WebAuthnLogger WebAuthn 日志接口
type WebAuthnLogger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Errorf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

// WebAuthnManager WebAuthn 管理器
type WebAuthnManager struct {
	db       *sql.DB
	webauthn *webauthn.WebAuthn
	dbPath   string
	log      WebAuthnLogger
}

// NewWebAuthnManager 创建 WebAuthn 管理器
func NewWebAuthnManager(log WebAuthnLogger, dataDir string, rpID string, rpOrigin string) (*WebAuthnManager, error) {
	dbPath := filepath.Join(dataDir, "webauthn.db")

	// 确保数据目录存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("创建数据目录失败: %v", err)
	}

	// 配置SQLite连接
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %v", err)
	}

	// 配置连接池
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(5 * time.Minute)

	manager := &WebAuthnManager{
		db:     db,
		dbPath: dbPath,
		log:    log,
	}

	// 初始化数据库表
	if err := manager.initDatabase(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化数据库失败: %v", err)
	}

	// 初始化 WebAuthn
	wconfig := &webauthn.Config{
		RPDisplayName: "QuantMesh",
		RPID:          rpID,
		RPOrigins:     []string{rpOrigin},
	}
	wa, err := webauthn.New(wconfig)
	if err != nil {
		return nil, fmt.Errorf("初始化 WebAuthn 失败: %v", err)
	}
	manager.webauthn = wa

	return manager, nil
}

// initDatabase 初始化数据库表
func (wm *WebAuthnManager) initDatabase() error {
	createTableSQL := `
	CREATE TABLE IF NOT EXISTS webauthn_credentials (
		id TEXT PRIMARY KEY,
		user_id TEXT NOT NULL,
		username TEXT NOT NULL,
		credential_id TEXT NOT NULL UNIQUE,
		public_key TEXT NOT NULL,
		counter INTEGER DEFAULT 0,
		device_name TEXT,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_used_at DATETIME,
		is_active BOOLEAN DEFAULT 1
	);
	`

	if _, err := wm.db.Exec(createTableSQL); err != nil {
		return fmt.Errorf("创建 WebAuthn 凭证表失败: %v", err)
	}

	// 创建索引
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_webauthn_user_id ON webauthn_credentials(user_id);",
		"CREATE INDEX IF NOT EXISTS idx_webauthn_username ON webauthn_credentials(username);",
		"CREATE INDEX IF NOT EXISTS idx_webauthn_credential_id ON webauthn_credentials(credential_id);",
	}

	for _, indexSQL := range indexes {
		if _, err := wm.db.Exec(indexSQL); err != nil {
			if wm.log != nil {
				wm.log.Warnf("创建索引失败: %v", err)
			}
		}
	}

	return nil
}

// WebAuthnUser WebAuthn 用户接口实现
type WebAuthnUser struct {
	ID          []byte
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
}

// WebAuthnID 返回用户的 WebAuthn ID
func (u *WebAuthnUser) WebAuthnID() []byte {
	return u.ID
}

// WebAuthnName 返回用户的 WebAuthn 名称
func (u *WebAuthnUser) WebAuthnName() string {
	return u.Name
}

// WebAuthnDisplayName 返回用户的显示名称
func (u *WebAuthnUser) WebAuthnDisplayName() string {
	return u.DisplayName
}

// WebAuthnCredentials 返回用户的所有凭证
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// WebAuthnIcon 返回用户的图标 URL（可选）
func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// GetUser 获取用户（实现 webauthn.User 接口）
func (wm *WebAuthnManager) GetUser(username string) (*WebAuthnUser, error) {
	// 查询用户的所有凭证
	rows, err := wm.db.Query(`
		SELECT credential_id, public_key, counter, device_name, created_at, last_used_at
		FROM webauthn_credentials
		WHERE username = ? AND is_active = 1
	`, username)
	if err != nil {
		return nil, fmt.Errorf("查询用户凭证失败: %v", err)
	}
	defer rows.Close()

	var credentials []webauthn.Credential
	for rows.Next() {
		var credentialID, publicKeyJSON string
		var counter int64
		var deviceName sql.NullString
		var createdAt time.Time
		var lastUsedAt sql.NullTime

		if err := rows.Scan(&credentialID, &publicKeyJSON, &counter, &deviceName, &createdAt, &lastUsedAt); err != nil {
			continue
		}

		// 解码 credential_id
		credentialIDBytes, err := base64.RawURLEncoding.DecodeString(credentialID)
		if err != nil {
			continue
		}

		// 解析 public_key (JSON) 为 webauthn.Credential
		var credentialData map[string]interface{}
		if err := json.Unmarshal([]byte(publicKeyJSON), &credentialData); err != nil {
			continue
		}

		// 构造 webauthn.Credential
		credential := webauthn.Credential{
			ID:        credentialIDBytes,
			PublicKey: []byte(publicKeyJSON), // 存储 JSON 格式的公钥
		}
		credentials = append(credentials, credential)
	}

	// 创建用户（使用用户名作为 ID）
	userID := []byte(username)
	return &WebAuthnUser{
		ID:          userID,
		Name:        username,
		DisplayName: username,
		Credentials: credentials,
	}, nil
}

// SaveCredential 保存凭证
func (wm *WebAuthnManager) SaveCredential(userID, username string, credential *webauthn.Credential, deviceName string) error {
	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)

	if wm.log != nil {
		wm.log.Debugf("[WebAuthn] 开始保存凭证 - Username: %s, DeviceName: %s, CredentialID: %s",
			username, deviceName, credentialID)
	}

	// 序列化公钥
	publicKeyJSON, err := json.Marshal(credential.PublicKey)
	if err != nil {
		if wm.log != nil {
			wm.log.Errorf("[WebAuthn] 序列化公钥失败: %v", err)
		}
		return fmt.Errorf("序列化公钥失败: %v", err)
	}

	counter := credential.Authenticator.SignCount

	if wm.log != nil {
		wm.log.Debugf("[WebAuthn] 执行数据库插入 - CredentialID: %s, Counter: %d, PublicKey长度: %d",
			credentialID, counter, len(publicKeyJSON))
	}

	result, err := wm.db.Exec(`
		INSERT INTO webauthn_credentials (id, user_id, username, credential_id, public_key, counter, device_name)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, credentialID, userID, username, credentialID, string(publicKeyJSON), counter, deviceName)

	if err != nil {
		if wm.log != nil {
			wm.log.Errorf("[WebAuthn] 数据库插入失败: %v", err)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if wm.log != nil {
		wm.log.Infof("[WebAuthn] 凭证保存成功 - Username: %s, DeviceName: %s, CredentialID: %s, 影响行数: %d",
			username, deviceName, credentialID, rowsAffected)
	}

	return nil
}

// UpdateCredentialCounter 更新凭证计数器
func (wm *WebAuthnManager) UpdateCredentialCounter(credentialID string, counter uint32) error {
	updateSQL := `
	UPDATE webauthn_credentials
	SET counter = ?, last_used_at = ?
	WHERE credential_id = ?
	`

	_, err := wm.db.Exec(updateSQL, counter, time.Now(), credentialID)
	if err != nil {
		return fmt.Errorf("更新凭证计数器失败: %v", err)
	}

	return nil
}

// ListCredentials 列出用户的所有凭证
func (wm *WebAuthnManager) ListCredentials(username string) ([]CredentialInfo, error) {
	if wm.log != nil {
		wm.log.Debugf("[WebAuthn] 查询凭证列表 - Username: %s", username)
	}

	rows, err := wm.db.Query(`
		SELECT id, credential_id, device_name, created_at, last_used_at, is_active
		FROM webauthn_credentials
		WHERE username = ?
		ORDER BY created_at DESC
	`, username)
	if err != nil {
		if wm.log != nil {
			wm.log.Errorf("[WebAuthn] 查询凭证失败: %v", err)
		}
		return nil, fmt.Errorf("查询凭证失败: %v", err)
	}
	defer rows.Close()

	var credentials []CredentialInfo
	count := 0
	for rows.Next() {
		var cred CredentialInfo
		var deviceName sql.NullString
		var lastUsedAt sql.NullTime

		if err := rows.Scan(&cred.ID, &cred.CredentialID, &deviceName, &cred.CreatedAt, &lastUsedAt, &cred.IsActive); err != nil {
			if wm.log != nil {
				wm.log.Warnf("[WebAuthn] 扫描凭证数据失败: %v", err)
			}
			continue
		}

		// 处理 device_name 可能为 NULL 的情况
		if deviceName.Valid {
			cred.DeviceName = deviceName.String
		} else {
			cred.DeviceName = "未命名设备"
		}

		// 处理 last_used_at 可能为 NULL 的情况
		if lastUsedAt.Valid {
			cred.LastUsedAt = &lastUsedAt.Time
		}

		credentials = append(credentials, cred)
		count++

		if wm.log != nil {
			wm.log.Debugf("[WebAuthn] 找到凭证 - ID: %s, DeviceName: %s, CreatedAt: %v, IsActive: %v",
				cred.ID, cred.DeviceName, cred.CreatedAt, cred.IsActive)
		}
	}

	if wm.log != nil {
		wm.log.Infof("[WebAuthn] 查询完成 - Username: %s, 找到 %d 条凭证记录", username, count)
	}

	return credentials, nil
}

// CredentialInfo 凭证信息
type CredentialInfo struct {
	ID           string
	CredentialID string
	DeviceName   string
	CreatedAt    time.Time
	LastUsedAt   *time.Time
	IsActive     bool
}

// DeleteCredential 删除凭证
func (wm *WebAuthnManager) DeleteCredential(credentialID string) error {
	_, err := wm.db.Exec(`
		UPDATE webauthn_credentials
		SET is_active = 0
		WHERE credential_id = ?
	`, credentialID)
	if err != nil {
		return fmt.Errorf("删除凭证失败: %v", err)
	}
	return nil
}

// HasCredentials 检查用户是否已注册凭证
func (wm *WebAuthnManager) HasCredentials(username string) (bool, error) {
	var count int
	err := wm.db.QueryRow("SELECT COUNT(*) FROM webauthn_credentials WHERE username = ? AND is_active = 1", username).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("查询凭证失败: %v", err)
	}
	return count > 0, nil
}

// Close 关闭数据库连接
func (wm *WebAuthnManager) Close() error {
	return wm.db.Close()
}
