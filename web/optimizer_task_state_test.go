package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/backtest/optimizer"
)

func registerOptimizerStateTest(t *testing.T) (string, context.Context) {
	t.Helper()
	id := t.Name()
	ctx, cancel := context.WithCancel(context.Background())
	optimizerTasksMu.Lock()
	optimizerTasks[id] = &optimizerTask{ID: id, Status: "running", cancel: cancel}
	optimizerTasksMu.Unlock()
	t.Cleanup(func() { cancel(); optimizerTasksMu.Lock(); delete(optimizerTasks, id); optimizerTasksMu.Unlock() })
	return id, ctx
}

func stopOptimizerForTest(id string) int {
	router := gin.New()
	router.POST("/stop/:id", postOptimizerStop)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/stop/"+id, nil))
	return w.Code
}

func TestOptimizerAcceptedStopWinsOverLateSuccessfulResult(t *testing.T) {
	id, ctx := registerOptimizerStateTest(t)
	if stopOptimizerForTest(id) != http.StatusOK {
		t.Fatal("stop request failed")
	}
	task, _ := snapshotOptimizerTask(id)
	if task.Status != "stopping" || ctx.Err() != context.Canceled {
		t.Fatalf("stop acknowledged as terminated too soon: %s", task.Status)
	}
	if finishOptimizerTask(ctx, id, &optimizer.OptimResult{}, nil) {
		t.Fatal("late success overwrote accepted stop")
	}
	task, _ = snapshotOptimizerTask(id)
	if task.Status != "stopped" || task.Result != nil {
		t.Fatalf("incorrect terminal result: %+v", task)
	}
	if beginOptimizerPhase(context.Background(), id, "running") {
		t.Fatal("terminal task restarted")
	}
}

func TestOptimizerCompletedTaskRemainsCompletedAfterStop(t *testing.T) {
	id, ctx := registerOptimizerStateTest(t)
	result := &optimizer.OptimResult{BestScore: 1}
	if !finishOptimizerTask(ctx, id, result, nil) {
		t.Fatal("completion failed")
	}
	if stopOptimizerForTest(id) != http.StatusOK {
		t.Fatal("stop request failed")
	}
	task, _ := snapshotOptimizerTask(id)
	if task.Status != "completed" || task.Result != result || ctx.Err() != nil {
		t.Fatal("late stop rewrote completed task")
	}
}

func TestOptimizerCanceledBeforeFetchDoesNotEnterLoading(t *testing.T) {
	id, ctx := registerOptimizerStateTest(t)
	stopOptimizerForTest(id)
	runOptimizerTaskWithDataFetch(ctx, id, "binance", "BTCUSDT", "1h", time.Time{}, time.Time{}, optimizer.OptimSearchSpace{}, optimizer.OptimConfig{}, 1000)
	task, _ := snapshotOptimizerTask(id)
	if task.Status != "stopped" {
		t.Fatalf("state=%s", task.Status)
	}
}

func TestOptimizerStatusReadsAreSnapshotsDuringUpdates(t *testing.T) {
	id, ctx := registerOptimizerStateTest(t)
	router := gin.New()
	router.GET("/status/:id", getOptimizerStatus)
	router.GET("/result/:id", getOptimizerResult)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			beginOptimizerPhase(ctx, id, "running")
		}
	}()
	for i := 0; i < 100; i++ {
		for _, path := range []string{"/status/", "/result/"} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, path+id, nil))
			if w.Code != http.StatusOK {
				t.Errorf("read status=%d", w.Code)
			}
		}
	}
	wg.Wait()
}
