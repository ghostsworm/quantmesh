package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/database"
)

type fakeTaskDB struct {
	tasks         map[string]*database.AsyncTask
	saved         *database.AsyncTask
	updated       *database.AsyncTask
	cleanupCutoff time.Time
	cleanupCount  int64
	saveErr       error
	getErr        error
	updateErr     error
	listErr       error
	cleanupErr    error
}

func newFakeTaskDB() *fakeTaskDB {
	return &fakeTaskDB{tasks: make(map[string]*database.AsyncTask), cleanupCount: 3}
}

func (f *fakeTaskDB) SaveAsyncTask(ctx context.Context, task *database.AsyncTask) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	copied := *task
	f.saved = &copied
	f.tasks[task.ID] = &copied
	return nil
}

func (f *fakeTaskDB) UpdateAsyncTask(ctx context.Context, task *database.AsyncTask) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	copied := *task
	f.updated = &copied
	f.tasks[task.ID] = &copied
	return nil
}

func (f *fakeTaskDB) GetAsyncTask(ctx context.Context, id string) (*database.AsyncTask, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	task, ok := f.tasks[id]
	if !ok {
		return nil, errors.New("task not found")
	}
	copied := *task
	return &copied, nil
}

func (f *fakeTaskDB) GetPendingAsyncTasks(ctx context.Context, limit int) ([]*database.AsyncTask, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	tasks := make([]*database.AsyncTask, 0, limit)
	for _, task := range f.tasks {
		if task.Status == "pending" {
			copied := *task
			tasks = append(tasks, &copied)
			if len(tasks) == limit {
				break
			}
		}
	}
	return tasks, nil
}

func (f *fakeTaskDB) GetAsyncTasks(ctx context.Context, filter *database.AsyncTaskFilter) ([]*database.AsyncTask, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	tasks := make([]*database.AsyncTask, 0)
	for _, task := range f.tasks {
		if filter != nil && filter.Status != "" && task.Status != filter.Status {
			continue
		}
		copied := *task
		tasks = append(tasks, &copied)
		if filter != nil && filter.Limit > 0 && len(tasks) == filter.Limit {
			break
		}
	}
	return tasks, nil
}

func (f *fakeTaskDB) CleanupExpiredAsyncTasks(ctx context.Context, cutoff time.Time) (int64, error) {
	f.cleanupCutoff = cutoff
	if f.cleanupErr != nil {
		return 0, f.cleanupErr
	}
	return f.cleanupCount, nil
}

func (f *fakeTaskDB) SaveTrade(context.Context, *database.Trade) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetTrades(context.Context, *database.TradeFilter) ([]*database.Trade, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) BatchSaveTrades(context.Context, []*database.Trade) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) SaveOrder(context.Context, *database.Order) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetOrders(context.Context, *database.OrderFilter) ([]*database.Order, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) SaveStatistics(context.Context, *database.Statistics) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetStatistics(context.Context, *database.StatFilter) ([]*database.Statistics, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) SaveReconciliation(context.Context, *database.Reconciliation) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetReconciliations(context.Context, *database.ReconciliationFilter) ([]*database.Reconciliation, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) SaveRiskCheck(context.Context, *database.RiskCheck) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetRiskChecks(context.Context, *database.RiskCheckFilter) ([]*database.RiskCheck, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) SaveEvent(context.Context, *database.EventRecord) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetEvents(context.Context, *database.EventFilter) ([]*database.EventRecord, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) GetEventByID(context.Context, int64) (*database.EventRecord, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) GetEventStats(context.Context) (*database.EventStats, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) CleanupOldEvents(context.Context, string, int, int) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetAsyncTaskStats(context.Context, *time.Time, *time.Time) (*database.AsyncTaskStats, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) SavePositionPlan(context.Context, *database.PositionPlan) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) UpdatePositionPlan(context.Context, *database.PositionPlan) error {
	return errors.New("not implemented")
}
func (f *fakeTaskDB) GetPositionPlan(context.Context, int64) (*database.PositionPlan, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) GetPositionPlans(context.Context, *database.PositionPlanFilter) ([]*database.PositionPlan, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) BeginTx(context.Context) (database.Tx, error) {
	return nil, errors.New("not implemented")
}
func (f *fakeTaskDB) Ping(context.Context) error { return nil }
func (f *fakeTaskDB) Close() error               { return nil }

func TestTaskServiceCreateTaskRedactsSecretsAndStoresModel(t *testing.T) {
	db := newFakeTaskDB()
	service := NewTaskService(db)

	taskID, err := service.CreateTask(context.Background(), "generate_content", map[string]interface{}{
		"prompt":         "hello",
		"api_key":        "secret-key",
		"gemini_api_key": "legacy-key",
		"model":          "gpt-test",
	}, 30, 2)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if taskID == "" || db.saved == nil {
		t.Fatalf("task was not saved")
	}
	if db.saved.Status != "pending" || db.saved.TaskType != "generate_content" || db.saved.MaxRetries != 2 || db.saved.TimeoutSeconds != 30 {
		t.Fatalf("unexpected saved task: %#v", db.saved)
	}
	if db.saved.Model == nil || *db.saved.Model != "gpt-test" {
		t.Fatalf("model was not stored: %#v", db.saved.Model)
	}
	if !strings.Contains(db.saved.RequestData, RedactedAPIKey) || strings.Contains(db.saved.RequestData, "secret-key") {
		t.Fatalf("request data was not redacted: %s", db.saved.RequestData)
	}
	if got := ResolveTaskAPIKey(taskID); got != "secret-key" {
		t.Fatalf("ResolveTaskAPIKey() = %q", got)
	}
	if got := ResolveTaskGeminiAPIKey(taskID); got != "legacy-key" {
		t.Fatalf("ResolveTaskGeminiAPIKey() = %q", got)
	}
}

func TestTaskServiceUpdateTaskStatusLifecycle(t *testing.T) {
	db := newFakeTaskDB()
	taskID := "task-1"
	db.tasks[taskID] = &database.AsyncTask{ID: taskID, Status: "pending"}
	taskSecrets.Store(secretKey(taskID, "api_key"), "secret")
	service := NewTaskService(db)

	if err := service.UpdateTaskStatus(context.Background(), taskID, "running", nil, nil); err != nil {
		t.Fatalf("running update failed: %v", err)
	}
	if db.updated.StartedAt == nil || db.updated.CompletedAt != nil {
		t.Fatalf("unexpected running timestamps: %#v", db.updated)
	}

	errMessage := "failed"
	result := map[string]interface{}{
		"ai_input":            "in",
		"ai_output":           "out",
		"input_tokens":        float64(11),
		"output_tokens":       float64(7),
		"processing_time_ms":  float64(123),
		"used_api_key":        "key-name",
		"unrelated_stat_kind": 42,
	}
	if err := service.UpdateTaskStatus(context.Background(), taskID, "completed", result, &errMessage); err != nil {
		t.Fatalf("completed update failed: %v", err)
	}
	if db.updated.CompletedAt == nil || db.updated.Result == "" {
		t.Fatalf("completion fields missing: %#v", db.updated)
	}
	if db.updated.AIInput == nil || *db.updated.AIInput != "in" || db.updated.InputTokens != 11 || db.updated.OutputTokens != 7 {
		t.Fatalf("token stats not extracted: %#v", db.updated)
	}
	if db.updated.ErrorMessage == nil || *db.updated.ErrorMessage != "failed" {
		t.Fatalf("error message missing: %#v", db.updated.ErrorMessage)
	}
	if got := ResolveTaskAPIKey(taskID); got != "" {
		t.Fatalf("task secret should be forgotten, got %q", got)
	}
}

func TestTaskServicePendingStaleRetryCleanupAndErrors(t *testing.T) {
	db := newFakeTaskDB()
	now := time.Now()
	oldStarted := now.Add(-2 * time.Minute)
	freshStarted := now
	db.tasks["pending"] = &database.AsyncTask{ID: "pending", Status: "pending"}
	db.tasks["stale"] = &database.AsyncTask{ID: "stale", Status: "running", StartedAt: &oldStarted, TimeoutSeconds: 30}
	db.tasks["fresh"] = &database.AsyncTask{ID: "fresh", Status: "running", StartedAt: &freshStarted, TimeoutSeconds: 300}
	db.tasks["nostart"] = &database.AsyncTask{ID: "nostart", Status: "running", TimeoutSeconds: 30}
	service := NewTaskService(db)

	pending, err := service.GetPendingTasks(context.Background(), 10)
	if err != nil || len(pending) != 1 || pending[0].ID != "pending" {
		t.Fatalf("GetPendingTasks() = %#v, %v", pending, err)
	}
	stale, err := service.GetStaleRunningTasks(context.Background(), 10)
	if err != nil || len(stale) != 1 || stale[0].ID != "stale" {
		t.Fatalf("GetStaleRunningTasks() = %#v, %v", stale, err)
	}

	db.tasks["pending"].RetryCount = 1
	db.tasks["pending"].Status = "failed"
	db.tasks["pending"].StartedAt = &oldStarted
	db.tasks["pending"].CompletedAt = &now
	msg := "boom"
	db.tasks["pending"].ErrorMessage = &msg
	if err := service.RetryTask(context.Background(), "pending"); err != nil {
		t.Fatalf("RetryTask failed: %v", err)
	}
	retried := db.tasks["pending"]
	if retried.RetryCount != 2 || retried.Status != "pending" || retried.StartedAt != nil || retried.CompletedAt != nil || retried.ErrorMessage != nil {
		t.Fatalf("unexpected retried task: %#v", retried)
	}

	cleaned, err := service.CleanupExpiredTasks(context.Background())
	if err != nil || cleaned != 3 || db.cleanupCutoff.IsZero() {
		t.Fatalf("CleanupExpiredTasks() = %d, %v cutoff=%v", cleaned, err, db.cleanupCutoff)
	}

	db.getErr = errors.New("db down")
	if _, err := service.GetTask(context.Background(), "missing"); err == nil {
		t.Fatal("expected GetTask error")
	}
	if err := service.UpdateTaskStatus(context.Background(), "missing", "failed", nil, nil); err == nil {
		t.Fatal("expected UpdateTaskStatus error")
	}
}
