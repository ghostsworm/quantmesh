package config

import (
	"os"
	"testing"
)

func TestApplyDatabaseDSNFromEnv(t *testing.T) {
	t.Cleanup(func() {
		_ = os.Unsetenv("QUANTMESH_DATABASE_DSN")
		_ = os.Unsetenv("QUANTMESH_DATABASE_TYPE")
	})

	cfg := &Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = "./data/quantmesh.db"
	cfg.Database.Type = "sqlite"
	cfg.Database.DSN = "./data/quantmesh.db"

	t.Run("empty env no change to dsn semantics", func(t *testing.T) {
		_ = os.Unsetenv("QUANTMESH_DATABASE_DSN")
		c := *cfg
		ApplyDatabaseDSNFromEnv(&c)
		if c.Database.DSN != cfg.Database.DSN {
			t.Fatalf("unexpected DSN change: %q", c.Database.DSN)
		}
	})

	t.Run("mysql dsn from env overrides", func(t *testing.T) {
		dsn := "user:pass@tcp(127.0.0.1:3306)/qm?parseTime=true&charset=utf8mb4"
		t.Setenv("QUANTMESH_DATABASE_DSN", dsn)
		t.Setenv("QUANTMESH_DATABASE_TYPE", "mysql")
		c := *cfg
		ApplyDatabaseDSNFromEnv(&c)
		if c.Database.DSN != dsn || c.Database.Type != "mysql" {
			t.Fatalf("got DSN=%q type=%q", c.Database.DSN, c.Database.Type)
		}
	})

	t.Run("infer mysql when type empty", func(t *testing.T) {
		dsn := "user:pass@tcp(10.0.0.1:3306)/db"
		t.Setenv("QUANTMESH_DATABASE_DSN", dsn)
		_ = os.Unsetenv("QUANTMESH_DATABASE_TYPE")
		c := &Config{}
		c.Database.Type = ""
		ApplyDatabaseDSNFromEnv(c)
		if c.Database.Type != "mysql" || c.Database.DSN != dsn {
			t.Fatalf("got DSN=%q type=%q", c.Database.DSN, c.Database.Type)
		}
	})

	t.Run("sqlite path syncs storage when both sqlite", func(t *testing.T) {
		path := "/tmp/qm-test.db"
		t.Setenv("QUANTMESH_DATABASE_DSN", path)
		_ = os.Unsetenv("QUANTMESH_DATABASE_TYPE")
		c := &Config{}
		c.Storage.Enabled = true
		c.Storage.Type = "sqlite"
		c.Storage.Path = "./old.db"
		c.Database.Type = "sqlite"
		c.Database.DSN = ""
		ApplyDatabaseDSNFromEnv(c)
		if c.Storage.Path != path {
			t.Fatalf("storage path not synced: %q", c.Storage.Path)
		}
	})
}
