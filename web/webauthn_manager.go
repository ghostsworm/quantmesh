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

// NewWebAuthnManager 創建 WebAuthn 管理器。
// rpOrigins 是允許的瀏覽器 origin 列表（必須與請求時的 window.location.origin 嚴格匹配，
// 包括 scheme/host/port），可傳多個以支持 SSH 隧道、反向代理等場景。
func NewWebAuthnManager(log WebAuthnLogger, dataDir string, rpID string, rpOrigins []string) (*WebAuthnManager, error) {
	dbPath := filepath.Join(dataDir, "webauthn.db")

	// 確保資料目錄存在
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		return nil, fmt.Errorf("創建數據目錄失败: %v", err)
	}

	// 配置SQLite连接
	dsn := fmt.Sprintf("%s?_journal_mode=WAL&_synchronous=NORMAL&_cache_size=10000&_timeout=30000&_busy_timeout=30000", dbPath)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开數據库失败: %v", err)
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

	// 初始化數據库表
	if err := manager.initDatabase(); err != nil {
		db.Close()
		return nil, fmt.Errorf("初始化數據库失败: %v", err)
	}

	// 初始化 WebAuthn
	wconfig := &webauthn.Config{
		RPDisplayName: "QuantMesh",
		RPID:          rpID,
		RPOrigins:     rpOrigins,
	}
	wa, err := webauthn.New(wconfig)
	if err != nil {
		return nil, fmt.Errorf("初始化 WebAuthn 失败: %v", err)
	}
	manager.webauthn = wa

	return manager, nil
}

// initDatabase 初始化數據库表
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
		return fmt.Errorf("創建 WebAuthn 凭证表失败: %v", err)
	}

	// 創建索引
	indexes := []string{
		"CREATE INDEX IF NOT EXISTS idx_webauthn_user_id ON webauthn_credentials(user_id);",
		"CREATE INDEX IF NOT EXISTS idx_webauthn_username ON webauthn_credentials(username);",
		"CREATE INDEX IF NOT EXISTS idx_webauthn_credential_id ON webauthn_credentials(credential_id);",
	}

	for _, indexSQL := range indexes {
		if _, err := wm.db.Exec(indexSQL); err != nil {
			if wm.log != nil {
				wm.log.Warnf("創建索引失败: %v", err)
			}
		}
	}

	return nil
}

// WebAuthnUser WebAuthn 用戶接口實現
type WebAuthnUser struct {
	ID          []byte
	Name        string
	DisplayName string
	Credentials []webauthn.Credential
}

// WebAuthnID 返回用戶的 WebAuthn ID
func (u *WebAuthnUser) WebAuthnID() []byte {
	return u.ID
}

// WebAuthnName 返回用戶的 WebAuthn 名称
func (u *WebAuthnUser) WebAuthnName() string {
	return u.Name
}

// WebAuthnDisplayName 返回用戶的显示名称
func (u *WebAuthnUser) WebAuthnDisplayName() string {
	return u.DisplayName
}

// WebAuthnCredentials 返回用戶的所有凭证
func (u *WebAuthnUser) WebAuthnCredentials() []webauthn.Credential {
	return u.Credentials
}

// WebAuthnIcon 返回用戶的图標 URL（可選）
func (u *WebAuthnUser) WebAuthnIcon() string {
	return ""
}

// GetUser 獲取用戶（實現 webauthn.User 接口）
func (wm *WebAuthnManager) GetUser(username string) (*WebAuthnUser, error) {
	// 查詢用戶的所有凭证
	rows, err := wm.db.Query(`
		SELECT credential_id, public_key, counter, device_name, created_at, last_used_at
		FROM webauthn_credentials
		WHERE username = ? AND is_active = 1
	`, username)
	if err != nil {
		return nil, fmt.Errorf("查詢用戶凭证失败: %v", err)
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

		// 解碼 credential_id
		credentialIDBytes, err := base64.RawURLEncoding.DecodeString(credentialID)
		if err != nil {
			continue
		}

		credential, err := decodeStoredCredential(credentialIDBytes, []byte(publicKeyJSON), uint32(counter))
		if err != nil {
			if wm.log != nil {
				wm.log.Warnf("[WebAuthn] 跳過無法解析的凭证 - CredentialID: %s, Error: %v", credentialID, err)
			}
			continue
		}

		credentials = append(credentials, *credential)
	}

	// 創建用戶（使用用戶名作為 ID）
	userID := []byte(username)
	return &WebAuthnUser{
		ID:          userID,
		Name:        username,
		DisplayName: username,
		Credentials: credentials,
	}, nil
}

// decodeStoredCredential 解析當前版本和舊版本保存的 WebAuthn 凭证。
func decodeStoredCredential(credentialID []byte, raw []byte, counter uint32) (*webauthn.Credential, error) {
	var stored webauthn.Credential
	if err := json.Unmarshal(raw, &stored); err == nil && len(stored.PublicKey) > 0 {
		if len(stored.ID) == 0 {
			stored.ID = credentialID
		}
		if stored.Authenticator.SignCount < counter {
			stored.Authenticator.SignCount = counter
		}
		return &stored, nil
	}

	var publicKeyBase64 string
	if err := json.Unmarshal(raw, &publicKeyBase64); err == nil && publicKeyBase64 != "" {
		publicKey, err := base64.StdEncoding.DecodeString(publicKeyBase64)
		if err != nil {
			publicKey, err = base64.RawURLEncoding.DecodeString(publicKeyBase64)
		}
		if err != nil {
			return nil, fmt.Errorf("解析舊版公钥失败: %w", err)
		}

		return &webauthn.Credential{
			ID:        credentialID,
			PublicKey: publicKey,
			Authenticator: webauthn.Authenticator{
				SignCount: counter,
			},
		}, nil
	}

	var publicKeyBytes []byte
	if err := json.Unmarshal(raw, &publicKeyBytes); err == nil && len(publicKeyBytes) > 0 {
		return &webauthn.Credential{
			ID:        credentialID,
			PublicKey: publicKeyBytes,
			Authenticator: webauthn.Authenticator{
				SignCount: counter,
			},
		}, nil
	}

	return nil, fmt.Errorf("不支持的凭证存儲格式")
}

// SaveCredential 保存凭证
func (wm *WebAuthnManager) SaveCredential(userID, username string, credential *webauthn.Credential, deviceName string) error {
	credentialID := base64.RawURLEncoding.EncodeToString(credential.ID)

	if wm.log != nil {
		wm.log.Debugf("[WebAuthn] 开始保存凭证 - Username: %s, DeviceName: %s, CredentialID: %s",
			username, deviceName, credentialID)
	}

	credentialJSON, err := json.Marshal(credential)
	if err != nil {
		if wm.log != nil {
			wm.log.Errorf("[WebAuthn] 序列化凭证失败: %v", err)
		}
		return fmt.Errorf("序列化凭证失败: %v", err)
	}

	counter := credential.Authenticator.SignCount

	if wm.log != nil {
		wm.log.Debugf("[WebAuthn] 執行數據库插入 - CredentialID: %s, Counter: %d, Credential长度: %d",
			credentialID, counter, len(credentialJSON))
	}

	result, err := wm.db.Exec(`
		INSERT INTO webauthn_credentials (id, user_id, username, credential_id, public_key, counter, device_name)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, credentialID, userID, username, credentialID, string(credentialJSON), counter, deviceName)

	if err != nil {
		if wm.log != nil {
			wm.log.Errorf("[WebAuthn] 數據库插入失败: %v", err)
		}
		return err
	}

	rowsAffected, _ := result.RowsAffected()
	if wm.log != nil {
		wm.log.Infof("[WebAuthn] 凭证保存成功 - Username: %s, DeviceName: %s, CredentialID: %s, 影响行數: %d",
			username, deviceName, credentialID, rowsAffected)
	}

	return nil
}

// UpdateCredentialCounter 更新凭证计數器
func (wm *WebAuthnManager) UpdateCredentialCounter(credentialID string, counter uint32) error {
	updateSQL := `
	UPDATE webauthn_credentials
	SET counter = ?, last_used_at = ?
	WHERE credential_id = ?
	`

	_, err := wm.db.Exec(updateSQL, counter, time.Now(), credentialID)
	if err != nil {
		return fmt.Errorf("更新凭证计數器失败: %v", err)
	}

	return nil
}

// ListCredentials 列出用戶的所有凭证
func (wm *WebAuthnManager) ListCredentials(username string) ([]CredentialInfo, error) {
	if wm.log != nil {
		wm.log.Debugf("[WebAuthn] 查詢凭证列表 - Username: %s", username)
	}

	rows, err := wm.db.Query(`
		SELECT id, credential_id, device_name, created_at, last_used_at, is_active
		FROM webauthn_credentials
		WHERE username = ?
		ORDER BY created_at DESC
	`, username)
	if err != nil {
		if wm.log != nil {
			wm.log.Errorf("[WebAuthn] 查詢凭证失败: %v", err)
		}
		return nil, fmt.Errorf("查詢凭证失败: %v", err)
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
				wm.log.Warnf("[WebAuthn] 扫描凭证數據失败: %v", err)
			}
			continue
		}

		// 处理 device_name 可能為 NULL 的情况
		if deviceName.Valid {
			cred.DeviceName = deviceName.String
		} else {
			cred.DeviceName = "未命名設备"
		}

		// 处理 last_used_at 可能為 NULL 的情况
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
		wm.log.Infof("[WebAuthn] 查詢完成 - Username: %s, 找到 %d 条凭证記錄", username, count)
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

// DeleteCredential 刪除凭证
func (wm *WebAuthnManager) DeleteCredential(credentialID string) error {
	_, err := wm.db.Exec(`
		UPDATE webauthn_credentials
		SET is_active = 0
		WHERE credential_id = ?
	`, credentialID)
	if err != nil {
		return fmt.Errorf("刪除凭证失败: %v", err)
	}
	return nil
}

// HasCredentials 检查用戶是否已注册凭证
func (wm *WebAuthnManager) HasCredentials(username string) (bool, error) {
	var count int
	err := wm.db.QueryRow("SELECT COUNT(*) FROM webauthn_credentials WHERE username = ? AND is_active = 1", username).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("查詢凭证失败: %v", err)
	}
	return count > 0, nil
}

// Close 关闭數據库连接
func (wm *WebAuthnManager) Close() error {
	return wm.db.Close()
}
