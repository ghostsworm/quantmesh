package storage

import (
	"context"
	"testing"
)

func TestMySQLConfigStorage_InitializeConfigs_NilDB(t *testing.T) {
	// 模拟 MySQL 连接失败后 db 为 nil 的情况，确保不会 panic
	s := &MySQLConfigStorage{db: nil}
	ctx := context.Background()
	entries := []*ConfigEntry{
		{Key: "test", Scope: ScopeGlobal, ScopeID: "", Type: TypeString, Value: "v"},
	}
	err := s.InitializeConfigs(ctx, entries)
	if err == nil {
		t.Fatal("expected error when db is nil, got nil")
	}
	if err != nil && err.Error() == "" {
		t.Fatal("expected non-empty error message")
	}
}
