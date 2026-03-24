package storage

import (
	"testing"

	"quantmesh/config"
)

func TestIsMySQLStorageDSNString(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"  ", false},
		{"./data/quantmesh.db", false},
		{"/var/lib/quantmesh.db", false},
		{"user:pass@tcp(127.0.0.1:3306)/qt?parseTime=true", true},
		{"mysql://user:pass@localhost:3306/db", true},
		{"u:p@unix(/tmp/mysql.sock)/db", true},
	}
	for _, tt := range tests {
		if got := IsMySQLStorageDSNString(tt.s); got != tt.want {
			t.Errorf("IsMySQLStorageDSNString(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestResolveMySQLStorageDSN(t *testing.T) {
	dsn := "u:p@tcp(h:3306)/d"
	cfg := func(path, dbDSN string) *config.Config {
		c := &config.Config{}
		c.Storage.Path = path
		c.Database.DSN = dbDSN
		return c
	}
	if got := resolveMySQLStorageDSN(cfg("./data/x.db", dsn)); got != dsn {
		t.Fatalf("sqlite-like path should fall back to database.dsn: got %q", got)
	}
	if got := resolveMySQLStorageDSN(cfg("", dsn)); got != dsn {
		t.Fatalf("empty path should use database.dsn: got %q", got)
	}
	custom := "custom:c@tcp(other:3306)/db"
	if got := resolveMySQLStorageDSN(cfg(custom, dsn)); got != custom {
		t.Fatalf("explicit mysql DSN in path should win: got %q", got)
	}
	if got := resolveMySQLStorageDSN(cfg("./only.db", "")); got != "./only.db" {
		t.Fatalf("no database.dsn: should return path for surfacing driver error: got %q", got)
	}
}
