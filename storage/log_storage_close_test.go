package storage

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// 回归：Close 必须先等后台协程把队列里剩余日志刷入数据库，再关闭数据库。
// 旧实现持有 mu 睡 100ms 后直接关库，最后一批日志以「sql: database is closed」丢失。
func TestLogStorageClose_FlushesPendingLogsBeforeClosingDB(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs.db")
	ls, err := NewLogStorage(path)
	if err != nil {
		t.Fatalf("NewLogStorage: %v", err)
	}

	const n = 150
	for i := 0; i < n; i++ {
		ls.WriteLog("INFO", fmt.Sprintf("msg-%d", i))
	}

	start := time.Now()
	if err := ls.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if elapsed := time.Since(start); elapsed > logFlushOnCloseTimeout+time.Second {
		t.Fatalf("Close took %v", elapsed)
	}

	// 关闭后再写日志不能 panic（send on closed channel），也不再投递
	ls.WriteLog("INFO", "after-close")
	if err := ls.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}

	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM logs`).Scan(&count); err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != n {
		t.Fatalf("logs in db = %d, want %d (pending logs must be flushed on Close)", count, n)
	}
}

func TestLogStorageClose_ConcurrentWritesDoNotPanic(t *testing.T) {
	ls, err := NewLogStorage(filepath.Join(t.TempDir(), "logs.db"))
	if err != nil {
		t.Fatalf("NewLogStorage: %v", err)
	}
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				ls.WriteLog("INFO", fmt.Sprintf("c-%d", i))
			}
		}
	}()
	time.Sleep(20 * time.Millisecond)
	if err := ls.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	close(stop)
	<-done
}
