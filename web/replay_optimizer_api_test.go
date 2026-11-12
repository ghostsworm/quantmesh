package web

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest"
	"quantmesh/backtest/optimizer/replayfixture"
	"quantmesh/backtest/optimrun"
	"quantmesh/backtest/visionfeed"
	"quantmesh/exchange"
)

// The fake persists serialized bytes and reconstructs every read, like the SQL
// search_space column. Workers and HTTP reads never share mutable task pointers.
type replayAPIStore struct {
	mu                 sync.Mutex
	rows               map[string][]byte
	creates            int
	progressViolations int
}

func (s *replayAPIStore) put(task *backtest.OptimTask) error {
	body, err := json.Marshal(task)
	if err == nil {
		s.rows[task.ID] = body
	}
	return err
}

func (s *replayAPIStore) get(id string) (*backtest.OptimTask, error) {
	body, ok := s.rows[id]
	if !ok {
		return nil, nil
	}
	var task backtest.OptimTask
	if err := json.Unmarshal(body, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func (s *replayAPIStore) CreateOptimTask(task *backtest.OptimTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.creates++
	return s.put(task)
}

func (s *replayAPIStore) GetOptimTask(id string) (*backtest.OptimTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.get(id)
}

func (s *replayAPIStore) ListOptimTasks(limit, offset int) ([]*backtest.OptimTask, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.rows))
	for id := range s.rows {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]*backtest.OptimTask, 0)
	for i, id := range ids {
		if i < offset || len(out) >= limit {
			continue
		}
		task, err := s.get(id)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

func (s *replayAPIStore) UpdateOptimTaskProgress(id string, completed, progress int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.get(id)
	if err != nil || task == nil {
		return fmt.Errorf("progress task %s missing: %v", id, err)
	}
	if completed > task.TotalCombos || completed < task.CompletedCombos || progress > 100 {
		s.progressViolations++
		return fmt.Errorf("invalid task progress %d/%d", completed, task.TotalCombos)
	}
	task.CompletedCombos, task.Progress = completed, progress
	return s.put(task)
}

func (s *replayAPIStore) UpdateOptimTaskStatus(id, status string, startedAt, completedAt *time.Time, errMsg, resultPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, err := s.get(id)
	if err != nil || task == nil {
		return fmt.Errorf("status task %s missing: %v", id, err)
	}
	task.Status, task.Error, task.ResultPath = status, errMsg, resultPath
	if startedAt != nil {
		task.StartedAt = startedAt
	}
	if completedAt != nil {
		task.CompletedAt = completedAt
	}
	return s.put(task)
}

func (s *replayAPIStore) DeleteOptimTask(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.rows, id)
	return nil
}

// Ruby sends real HTTP requests to production handlers backed by the production
// manager and replay optimizer. No account, config file or public network is used.
func TestReplayOptimizerAPIWithRuby(t *testing.T) {
	ruby, err := exec.LookPath("ruby")
	if err != nil {
		t.Skip("Ruby is required for HTTP API acceptance tests")
	}
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	resultsDir := filepath.Join(dir, "results")
	if err := os.MkdirAll(resultsDir, 0700); err != nil {
		t.Fatal(err)
	}
	fixturePath := filepath.Join(dir, "candles.json")
	data, err := json.Marshal(replayfixture.Candles("sideways"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fixturePath, data, 0600); err != nil {
		t.Fatal(err)
	}
	store := &replayAPIStore{rows: make(map[string][]byte)}
	if err := store.put(&backtest.OptimTask{ID: "legacy", Strategy: "grid", Status: "completed", SearchSpace: backtest.OptimSearchSpace{Strategy: "grid"}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(resultsDir, "legacy.json"), []byte(`{"task_id":"legacy","strategy":"grid","all_results":[],"completed":1,"total_combos":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	var blocked atomic.Bool
	var loads, active, canceled atomic.Int64
	loader := func(ctx context.Context, symbol, interval string, start, end time.Time) ([]*exchange.Candle, error) {
		loads.Add(1)
		active.Add(1)
		defer active.Add(-1)
		if blocked.Load() {
			<-ctx.Done()
			canceled.Add(1)
			return nil, ctx.Err()
		}
		body, err := os.ReadFile(fixturePath)
		if err != nil {
			return nil, err
		}
		var candles []*exchange.Candle
		if err := json.Unmarshal(body, &candles); err != nil {
			return nil, err
		}
		return candles, nil
	}
	vision := func(ctx context.Context, _ visionfeed.LoadRequest) (visionfeed.LoadResult, error) {
		loads.Add(1)
		// Admission reads before a slot is taken and cannot be canceled, so it
		// must stay quick. Only the running task's read blocks; delete cancels it.
		if blocked.Load() && ctx.Done() != nil {
			active.Add(1)
			defer active.Add(-1)
			<-ctx.Done()
			canceled.Add(1)
			return visionfeed.LoadResult{}, ctx.Err()
		}
		return replayVisionFixture(), nil
	}
	makeManager := func() *optimrun.OptimTaskManager {
		return optimrun.NewOptimTaskManager(store, nil, optimrun.WithHistoricalDataLoader(loader), optimrun.WithReplayVisionLoader(vision), optimrun.WithResultsDir(resultsDir))
	}
	original := optimTaskManager
	optimTaskManager = makeManager()
	t.Cleanup(func() { optimTaskManager = original })
	router := gin.New()
	var apiMu sync.Mutex
	router.Use(func(c *gin.Context) {
		apiMu.Lock()
		defer apiMu.Unlock()
		c.Next()
	})
	router.POST("/api/backtest/optim/tasks", postOptimTasks)
	router.GET("/api/backtest/optim/tasks", getOptimTasks)
	router.GET("/api/backtest/optim/tasks/:id", getOptimTaskByID)
	router.GET("/api/backtest/optim/tasks/:id/result", getOptimTaskResult)
	router.DELETE("/api/backtest/optim/tasks/:id", deleteOptimTask)
	// Test-only controls are never registered by the application router.
	router.GET("/__test/state", func(c *gin.Context) {
		store.mu.Lock()
		defer store.mu.Unlock()
		c.JSON(http.StatusOK, gin.H{"creates": store.creates, "loads": loads.Load(), "active_loads": active.Load(), "canceled": canceled.Load(), "progress_violations": store.progressViolations})
	})
	router.POST("/__test/block", func(c *gin.Context) {
		blocked.Store(true)
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	router.POST("/__test/reload", func(c *gin.Context) {
		// Round-trip the entire fake store to emulate a process restart.
		store.mu.Lock()
		body, err := json.Marshal(store.rows)
		if err == nil {
			err = json.Unmarshal(body, &store.rows)
		}
		store.mu.Unlock()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		optimTaskManager = makeManager()
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	server := httptest.NewServer(router)
	defer server.Close()
	defer func() {
		apiMu.Lock()
		defer apiMu.Unlock()
		tasks, _ := store.ListOptimTasks(200, 0)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, task := range tasks {
			if err := optimTaskManager.DeleteTask(ctx, task.ID); err != nil {
				t.Errorf("cleanup task %s: %v", task.ID, err)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	script, err := filepath.Abs(filepath.Join("..", "scripts", "tests", "replay_optimizer_api_test.rb"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, ruby, script)
	cmd.Env = append(os.Environ(), "REPLAY_OPTIMIZER_API_URL="+server.URL)
	output, err := cmd.CombinedOutput()
	t.Log(string(output))
	if err != nil {
		t.Fatalf("Ruby HTTP acceptance failed: %v", err)
	}
}

// replayVisionFixture expands the synthetic hourly sample to the verified minute
// series a replay task reads. It carries no market file and no funding history.
func replayVisionFixture() visionfeed.LoadResult {
	hourly := replayfixture.Candles("sideways")
	minutes := make([]*exchange.Candle, 0, len(hourly)*60)
	for _, hour := range hourly {
		for step := int64(0); step < 60; step++ {
			candle := *hour
			candle.Timestamp = hour.Timestamp + step*time.Minute.Milliseconds()
			candle.Volume = hour.Volume / 60
			minutes = append(minutes, &candle)
		}
	}
	return visionfeed.LoadResult{
		Candles: minutes,
		Provenance: visionfeed.Provenance{
			Source: visionfeed.SourceBinanceVisionUM1m,
			Files:  []visionfeed.FileDigest{{Name: "BTCUSDT/1m/BTCUSDT-1m-2025-01-01.csv", SHA256: "replay-optimizer-api-fixture"}},
		},
	}
}
