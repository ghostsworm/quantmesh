package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// LoadDotEnvIfPresent 從 dir/.env 讀取 KEY=VALUE，僅在 os.Getenv(k) 為空時 Setenv（不覆蓋已有環境變量）
func LoadDotEnvIfPresent(dir string) error {
	path := filepath.Join(dir, ".env")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		idx := strings.IndexByte(line, '=')
		if idx <= 0 {
			continue
		}
		k := strings.TrimSpace(line[:idx])
		v := strings.TrimSpace(line[idx+1:])
		if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
			v = v[1 : len(v)-1]
		}
		if k != "" && os.Getenv(k) == "" {
			_ = os.Setenv(k, v)
		}
	}
	return nil
}

// EnsureEnvFileIfMissing 若 dir 下不存在 .env，則寫入不含密鑰的模板（路徑類變量，便於下次啟動）
func EnsureEnvFileIfMissing(dir string, cfg *Config) error {
	if cfg == nil {
		return nil
	}
	path := filepath.Join(dir, ".env")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# QuantMesh — 由程式自動生成。請勿提交本檔到版本庫；API 密鑰以數據庫 app_config 為準。\n")
	b.WriteString("# Auto-generated .env (no secrets). Safe to keep local only.\n\n")

	sqlitePath := cfg.Storage.Path
	if cfg.Storage.Type == "sqlite" && sqlitePath != "" {
		if abs, err := filepath.Abs(sqlitePath); err == nil {
			sqlitePath = abs
		}
		b.WriteString(fmt.Sprintf("QUANTMESH_SQLITE_PATH=%s\n", sqlitePath))
	}
	b.WriteString("# 主庫 DSN（覆蓋 config.yaml 中 database.dsn；敏感信息建議只放 .env）\n")
	b.WriteString("# QUANTMESH_DATABASE_DSN=\n")
	b.WriteString("# 可選：mysql | sqlite | postgres（未設時僅在 database.type 為空時根據 DSN 推斷）\n")
	b.WriteString("# QUANTMESH_DATABASE_TYPE=\n")
	botsDir := os.Getenv("QUANTMESH_BOTS_DIR")
	if botsDir == "" {
		botsDir = "./bots"
	}
	b.WriteString(fmt.Sprintf("QUANTMESH_BOTS_DIR=%s\n", botsDir))
	b.WriteString("\n# 禁用自動從數據庫覆蓋配置: QUANTMESH_USE_APP_CONFIG=0\n")
	b.WriteString("# 禁用啟動時自動遷移: QUANTMESH_SKIP_AUTO_MIGRATE=1\n")
	return os.WriteFile(path, []byte(b.String()), 0600)
}

// ApplyStoragePathFromEnv 若配置中未設 SQLite 路徑，使用 QUANTMESH_SQLITE_PATH
func ApplyStoragePathFromEnv(cfg *Config) {
	if cfg == nil {
		return
	}
	if p := strings.TrimSpace(os.Getenv("QUANTMESH_SQLITE_PATH")); p != "" {
		if cfg.Storage.Path == "" {
			cfg.Storage.Type = "sqlite"
			cfg.Storage.Path = p
		}
	}
}

// ApplyDatabaseDSNFromEnv 使用環境變量覆蓋主庫連接（十二要素：密碼不進 YAML）。
// QUANTMESH_DATABASE_DSN：非空時寫入 cfg.Database.DSN（覆蓋 config.yaml）。
// QUANTMESH_DATABASE_TYPE：可選，非空時寫入 cfg.Database.Type（mysql/sqlite/postgres）。
// 若未設 TYPE 且 cfg.Database.Type 為空，則根據 DSN 簡單推斷（含 @tcp( / @unix( 視為 mysql 等）。
// 當 Database 與 Storage 均為 sqlite 時，同步 Storage.Path = Database.DSN（與 Validate 內邏輯一致）。
func ApplyDatabaseDSNFromEnv(cfg *Config) {
	if cfg == nil {
		return
	}
	dsn := strings.TrimSpace(os.Getenv("QUANTMESH_DATABASE_DSN"))
	if dsn == "" {
		return
	}
	cfg.Database.DSN = dsn

	if t := strings.TrimSpace(os.Getenv("QUANTMESH_DATABASE_TYPE")); t != "" {
		cfg.Database.Type = strings.ToLower(t)
		syncStoragePathWithDatabaseDSN(cfg)
		return
	}

	if cfg.Database.Type != "" {
		syncStoragePathWithDatabaseDSN(cfg)
		return
	}

	switch {
	case strings.Contains(dsn, "@tcp(") || strings.Contains(dsn, "@unix("):
		cfg.Database.Type = "mysql"
	case strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://"):
		cfg.Database.Type = "postgres"
	default:
		cfg.Database.Type = "sqlite"
	}
	syncStoragePathWithDatabaseDSN(cfg)
}

func syncStoragePathWithDatabaseDSN(cfg *Config) {
	if cfg == nil {
		return
	}
	if cfg.Database.Type == "sqlite" && strings.TrimSpace(cfg.Database.DSN) != "" &&
		cfg.Storage.Enabled && cfg.Storage.Type == "sqlite" {
		cfg.Storage.Path = strings.TrimSpace(cfg.Database.DSN)
	}
}

// RenameConfigYAMLToBackup 將 config.yaml 歸檔為 config.yaml.migrated.<UTC>.bak
func RenameConfigYAMLToBackup(configPath string) (string, error) {
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)
	ts := time.Now().UTC().Format("20060102T150405Z")
	backupPath := filepath.Join(dir, fmt.Sprintf("%s.migrated.%s.bak", base, ts))
	return backupPath, os.Rename(configPath, backupPath)
}
