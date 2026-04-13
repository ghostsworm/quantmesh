// Package geminiusage 記錄進程內 Gemini API 調用的時間與 token 用量（內存環形緩衝，供 Web 展示）。
package geminiusage

import (
	"sync"
	"time"
)

const maxEntries = 300

// Entry 單次調用記錄
type Entry struct {
	At           time.Time `json:"at"`
	Model        string    `json:"model"`
	Source       string    `json:"source"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	DurationMs   int64     `json:"duration_ms"`
}

var (
	mu        sync.RWMutex
	entries   []Entry
	persistFn func(Entry) // 可選：寫入主庫（由 main 注入）
)

// SetPersist 註冊持久化回調；每條 Record 會在內存寫入後調用一次（勿阻塞過久）。
func SetPersist(fn func(Entry)) {
	mu.Lock()
	defer mu.Unlock()
	persistFn = fn
}

// Record 追加一條記錄（線程安全）；若已註冊 persistFn 則異步寫庫。
func Record(e Entry) {
	mu.Lock()
	entries = append(entries, e)
	if len(entries) > maxEntries {
		entries = entries[len(entries)-maxEntries:]
	}
	fn := persistFn
	mu.Unlock()
	if fn != nil {
		go func(ev Entry) {
			defer func() { recover() }()
			fn(ev)
		}(e)
	}
}

// Snapshot 返回當前記錄副本，**最新在前**
func Snapshot() []Entry {
	mu.RLock()
	defer mu.RUnlock()
	n := len(entries)
	if n == 0 {
		return nil
	}
	out := make([]Entry, n)
	for i := 0; i < n; i++ {
		out[i] = entries[n-1-i]
	}
	return out
}

// Summary 當前緩衝區內聚合
type Summary struct {
	CallCount          int   `json:"call_count"`
	TotalInputTokens   int64 `json:"total_input_tokens"`
	TotalOutputTokens  int64 `json:"total_output_tokens"`
}

// Aggregate 對 Snapshot 結果做聚合（避免重複遍歷）
func Aggregate(snap []Entry) Summary {
	var sum Summary
	sum.CallCount = len(snap)
	for _, e := range snap {
		sum.TotalInputTokens += e.InputTokens
		sum.TotalOutputTokens += e.OutputTokens
	}
	return sum
}
