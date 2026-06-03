package lock

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestNopLockAlwaysSucceeds(t *testing.T) {
	l := NewNopLock()
	ctx := context.Background()

	if err := l.Lock(ctx, "bot:1", time.Second); err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	ok, err := l.TryLock(ctx, "bot:1", time.Second)
	if err != nil {
		t.Fatalf("TryLock() error = %v", err)
	}
	if !ok {
		t.Fatal("TryLock() = false, want true")
	}
	if err := l.Extend(ctx, "bot:1", time.Second); err != nil {
		t.Fatalf("Extend() error = %v", err)
	}
	if err := l.Unlock(ctx, "bot:1"); err != nil {
		t.Fatalf("Unlock() error = %v", err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

func TestNewDistributedLockDisabledReturnsNop(t *testing.T) {
	l, err := NewDistributedLock(&Config{Enabled: false})
	if err != nil {
		t.Fatalf("NewDistributedLock disabled error = %v", err)
	}
	if _, ok := l.(*NopLock); !ok {
		t.Fatalf("disabled lock type = %T, want *NopLock", l)
	}
}

func TestNewDistributedLockUnsupportedTypes(t *testing.T) {
	tests := []struct {
		name string
		typ  string
		want string
	}{
		{name: "etcd", typ: "etcd", want: "etcd lock not implemented yet"},
		{name: "database", typ: "database", want: "database lock not implemented yet"},
		{name: "unknown", typ: "zookeeper", want: "unsupported lock type: zookeeper"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewDistributedLock(&Config{Enabled: true, Type: tt.typ})
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, err) || err.Error() != tt.want {
				t.Fatalf("error = %q, want %q", err.Error(), tt.want)
			}
		})
	}
}
