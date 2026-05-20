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

func TestIsPostgresStorageDSNString(t *testing.T) {
	tests := []struct {
		s    string
		want bool
	}{
		{"", false},
		{"./data/quantmesh.db", false},
		{"postgres://user:pass@localhost:5432/db?sslmode=disable", true},
		{"postgresql://user:pass@project.supabase.co:5432/postgres?sslmode=require", true},
		{"host=localhost user=quantmesh password=secret dbname=quantmesh sslmode=disable", true},
	}
	for _, tt := range tests {
		if got := IsPostgresStorageDSNString(tt.s); got != tt.want {
			t.Errorf("IsPostgresStorageDSNString(%q) = %v, want %v", tt.s, got, tt.want)
		}
	}
}

func TestResolvePostgresStorageDSN(t *testing.T) {
	dsn := "postgresql://u:p@h:5432/d?sslmode=require"
	cfg := func(path, dbDSN string) *config.Config {
		c := &config.Config{}
		c.Storage.Path = path
		c.Database.DSN = dbDSN
		return c
	}
	if got := ResolveSQLStorageDSN(cfg("./data/x.db", dsn), "postgres"); got != dsn {
		t.Fatalf("sqlite-like path should fall back to database.dsn: got %q", got)
	}
	if got := ResolveSQLStorageDSN(cfg("", dsn), "postgresql"); got != dsn {
		t.Fatalf("empty path should use database.dsn: got %q", got)
	}
	custom := "postgres://custom:secret@other:5432/db?sslmode=require"
	if got := ResolveSQLStorageDSN(cfg(custom, dsn), "postgres"); got != custom {
		t.Fatalf("explicit postgres DSN in path should win: got %q", got)
	}
	if got := ResolveSQLStorageDSN(cfg("./only.db", ""), "postgres"); got != "./only.db" {
		t.Fatalf("no database.dsn: should return path for surfacing driver error: got %q", got)
	}
}
