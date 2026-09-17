package cfgmgr

import (
	"os"
	"strings"
	"testing"

	"quantmesh/config"
)

func TestRedactDSNPassword(t *testing.T) {
	tests := []struct {
		name string
		dsn  string
		want string
	}{
		{"empty", "", ""},
		{"sqlite path", "./data/quantmesh.db", "./data/quantmesh.db"},
		{"mysql no password", "root@tcp(localhost:3306)/quantmesh?parseTime=True", "root@tcp(localhost:3306)/quantmesh?parseTime=True"},
		{"mysql with password", "quantmesh:s3cr3t@tcp(db:3306)/quantmesh", "quantmesh:" + dsnPasswordPlaceholder + "@tcp(db:3306)/quantmesh"},
		{"mysql password contains at", "u:p@ss@tcp(db:3306)/q", "u:" + dsnPasswordPlaceholder + "@tcp(db:3306)/q"},
		{"url style", "postgres://u:pw@host:5432/db", "postgres://u:" + dsnPasswordPlaceholder + "@host:5432/db"},
		{"postgres keywords", "host=h user=u password=pw dbname=d", "host=h user=u password=" + dsnPasswordPlaceholder + " dbname=d"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactDSNPassword(tt.dsn); got != tt.want {
				t.Fatalf("redactDSNPassword(%q) = %q, want %q", tt.dsn, got, tt.want)
			}
		})
	}
}

// 回归测试：生成的简化版配置文件不得包含任何交易所凭据、Web API Key 或 DSN 密码
func TestGenerateMinimalConfigNeverWritesSecrets(t *testing.T) {
	const (
		apiKey     = "LEAK_API_KEY_0123456789"
		secretKey  = "LEAK_SECRET_KEY_0123456789"
		passphrase = "LEAK_PASSPHRASE_0123"
		webKey     = "LEAK_WEB_API_KEY_0123"
		dbPassword = "LEAK_DB_PASSWORD"
	)

	dir := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	if err := os.MkdirAll("docs/config/examples", 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cfg := &config.Config{}
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: apiKey, SecretKey: secretKey, Passphrase: passphrase, Testnet: true},
	}
	cfg.Web.APIKey = webKey
	cfg.Database.Type = "mysql"
	cfg.Database.DSN = "quantmesh:" + dbPassword + "@tcp(localhost:3306)/quantmesh"

	cm := &ConfigManager{cfg: cfg}
	if err := cm.generateMinimalConfig(); err != nil {
		t.Fatalf("generateMinimalConfig: %v", err)
	}

	data, err := os.ReadFile("docs/config/examples/config.minimal.yaml")
	if err != nil {
		t.Fatalf("read generated file: %v", err)
	}
	content := string(data)
	for _, secret := range []string{apiKey, secretKey, passphrase, webKey, dbPassword} {
		if strings.Contains(content, secret) {
			t.Fatalf("generated minimal config leaked secret %q", secret)
		}
	}
	if !strings.Contains(content, "binance") {
		t.Fatalf("generated minimal config should keep exchange names")
	}
}
