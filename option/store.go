package option

import (
	"sync"
	"time"
)

// Store 期权对冲状态存储（内存，按 bot 维度）
type Store struct {
	mu       sync.RWMutex
	byBot    map[string]*BotOptionState
	rollLogs map[string][]RollLogEntry
}

// BotOptionState 单个 Bot 的期权状态
type BotOptionState struct {
	BotID       string
	Positions   []OptionHedgePosition
	Coverage    *CoverageSnapshot
	SyncStatus  string // ok / degraded / failed
	LastSyncAt  *time.Time
	LastError   string
	UpdatedAt   time.Time
}

// RollLogEntry 展期执行日志
type RollLogEntry struct {
	ID         string
	BotID      string
	Action     string // roll_executed / roll_skipped
	FromInst   string
	ToInst     string
	ExecutedAt time.Time
	Details    string
}

// NewStore 创建存储
func NewStore() *Store {
	return &Store{
		byBot:    make(map[string]*BotOptionState),
		rollLogs: make(map[string][]RollLogEntry),
	}
}

// UpsertPositions 更新 Bot 的期权仓位
func (s *Store) UpsertPositions(botID string, positions []OptionHedgePosition, syncStatus string, errMsg string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	st, ok := s.byBot[botID]
	if !ok {
		st = &BotOptionState{BotID: botID}
		s.byBot[botID] = st
	}
	st.Positions = positions
	st.SyncStatus = syncStatus
	st.LastSyncAt = &now
	st.LastError = errMsg
	st.UpdatedAt = now
}

// SetCoverage 设置覆盖率快照
func (s *Store) SetCoverage(botID string, cov *CoverageSnapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st, ok := s.byBot[botID]
	if !ok {
		st = &BotOptionState{BotID: botID}
		s.byBot[botID] = st
	}
	st.Coverage = cov
	st.UpdatedAt = time.Now()
}

// GetState 获取 Bot 的期权状态
func (s *Store) GetState(botID string) *BotOptionState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.byBot[botID]
	if !ok {
		return nil
	}
	// 返回副本避免并发修改
	cpy := *st
	if st.Coverage != nil {
		covCpy := *st.Coverage
		cpy.Coverage = &covCpy
	}
	if len(st.Positions) > 0 {
		cpy.Positions = make([]OptionHedgePosition, len(st.Positions))
		copy(cpy.Positions, st.Positions)
	}
	return &cpy
}

// AppendRollLog 追加展期日志
func (s *Store) AppendRollLog(botID string, entry RollLogEntry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry.ExecutedAt = time.Now()
	if entry.ID == "" {
		entry.ID = time.Now().Format("20060102150405")
	}
	logs := s.rollLogs[botID]
	if len(logs) >= 100 {
		logs = logs[1:]
	}
	s.rollLogs[botID] = append(logs, entry)
}

// GetRollLogs 获取展期日志
func (s *Store) GetRollLogs(botID string, limit int) []RollLogEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	logs := s.rollLogs[botID]
	if limit <= 0 || limit > len(logs) {
		limit = len(logs)
	}
	if limit == 0 {
		return nil
	}
	start := len(logs) - limit
	if start < 0 {
		start = 0
	}
	out := make([]RollLogEntry, limit)
	copy(out, logs[start:])
	return out
}
