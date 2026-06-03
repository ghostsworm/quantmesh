package database

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestGormDatabaseSQLiteCRUDAndFilters(t *testing.T) {
	ctx := context.Background()
	db, err := NewGormDatabase(&DBConfig{
		Type:            "sqlite",
		DSN:             ":memory:",
		MaxOpenConns:    1,
		MaxIdleConns:    1,
		ConnMaxLifetime: time.Minute,
		LogLevel:        "silent",
	})
	if err != nil {
		t.Fatalf("NewGormDatabase sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if db.DB() == nil {
		t.Fatal("expected underlying gorm DB")
	}
	if err := db.Ping(ctx); err != nil {
		t.Fatalf("Ping returned error: %v", err)
	}
	if db.getDateExpr("created_at") != "DATE(created_at)" {
		t.Fatalf("sqlite date expr = %s", db.getDateExpr("created_at"))
	}
	if (&GormDatabase{dbType: "mysql"}).getDateExpr("created_at") != "DATE(CONVERT_TZ(created_at, '+00:00', '+00:00'))" {
		t.Fatal("mysql date expr mismatch")
	}
	if (&GormDatabase{dbType: "postgres"}).getDateExpr("created_at") != "DATE(created_at AT TIME ZONE 'UTC')" {
		t.Fatal("postgres date expr mismatch")
	}

	now := time.Now().Add(-time.Hour)
	if err := db.SaveTrade(ctx, &Trade{BotID: "bot-1", Exchange: "binance", Symbol: "BTCUSDT", OrderID: 1, Side: "BUY", Price: 100, Quantity: 2, Amount: 200, CreatedAt: now}); err != nil {
		t.Fatalf("SaveTrade: %v", err)
	}
	if err := db.BatchSaveTrades(ctx, []*Trade{{BotID: "bot-2", Exchange: "okx", Symbol: "ETHUSDT", OrderID: 2, CreatedAt: now}}); err != nil {
		t.Fatalf("BatchSaveTrades: %v", err)
	}
	if err := db.BatchSaveTrades(ctx, nil); err != nil {
		t.Fatalf("empty BatchSaveTrades: %v", err)
	}
	trades, err := db.GetTrades(ctx, &TradeFilter{BotID: "bot-1", Exchange: "binance", Symbol: "BTCUSDT", StartTime: &now, Limit: 10})
	if err != nil || len(trades) != 1 {
		t.Fatalf("GetTrades = %#v err=%v", trades, err)
	}

	if err := db.SaveOrder(ctx, &Order{BotID: "bot-1", Exchange: "binance", Symbol: "BTCUSDT", OrderID: 11, Status: "FILLED", CreatedAt: now}); err != nil {
		t.Fatalf("SaveOrder: %v", err)
	}
	orders, err := db.GetOrders(ctx, &OrderFilter{BotID: "bot-1", Exchange: "binance", Symbol: "BTCUSDT", Status: "FILLED", Limit: 5})
	if err != nil || len(orders) != 1 {
		t.Fatalf("GetOrders = %#v err=%v", orders, err)
	}

	statDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if err := db.SaveStatistics(ctx, &Statistics{Exchange: "binance", Symbol: "BTCUSDT", Date: statDate, TotalPnL: 12}); err != nil {
		t.Fatalf("SaveStatistics: %v", err)
	}
	stats, err := db.GetStatistics(ctx, &StatFilter{Exchange: "binance", Symbol: "BTCUSDT", StartDate: &statDate, EndDate: &statDate, Limit: 5})
	if err != nil || len(stats) != 1 {
		t.Fatalf("GetStatistics = %#v err=%v", stats, err)
	}

	resolved := false
	if err := db.SaveReconciliation(ctx, &Reconciliation{Exchange: "binance", Symbol: "BTCUSDT", Type: "order_diff", Resolved: resolved, CreatedAt: now}); err != nil {
		t.Fatalf("SaveReconciliation: %v", err)
	}
	recons, err := db.GetReconciliations(ctx, &ReconciliationFilter{Exchange: "binance", Symbol: "BTCUSDT", Type: "order_diff", Resolved: &resolved, StartTime: &now, Limit: 5})
	if err != nil || len(recons) != 1 {
		t.Fatalf("GetReconciliations = %#v err=%v", recons, err)
	}

	healthy := true
	if err := db.SaveRiskCheck(ctx, &RiskCheck{Exchange: "binance", Symbol: "BTCUSDT", IsHealthy: healthy, Reason: "ok", CreatedAt: now}); err != nil {
		t.Fatalf("SaveRiskCheck: %v", err)
	}
	checks, err := db.GetRiskChecks(ctx, &RiskCheckFilter{Exchange: "binance", Symbol: "BTCUSDT", IsHealthy: &healthy, StartTime: &now, Limit: 5})
	if err != nil || len(checks) != 1 {
		t.Fatalf("GetRiskChecks = %#v err=%v", checks, err)
	}
}

func TestGormDatabaseEventsTasksPlansAndTransactions(t *testing.T) {
	ctx := context.Background()
	db, err := NewGormDatabase(&DBConfig{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	if err != nil {
		t.Fatalf("NewGormDatabase sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	old := time.Now().AddDate(0, 0, -10)
	recent := time.Now().Add(-time.Hour)
	for _, ev := range []*EventRecord{
		{Type: "risk", Severity: "critical", Source: "risk", Exchange: "binance", Symbol: "BTCUSDT", Title: "old", CreatedAt: old},
		{Type: "api", Severity: "warning", Source: "api", Exchange: "okx", Symbol: "ETHUSDT", Title: "new", CreatedAt: recent},
		{Type: "api", Severity: "info", Source: "api", Exchange: "okx", Symbol: "ETHUSDT", Title: "new2", CreatedAt: recent},
	} {
		if err := db.SaveEvent(ctx, ev); err != nil {
			t.Fatalf("SaveEvent: %v", err)
		}
	}
	events, err := db.GetEvents(ctx, &EventFilter{Type: "api", Severity: "warning", Source: "api", Exchange: "okx", Symbol: "ETHUSDT", StartTime: &recent, Limit: 10})
	if err != nil || len(events) != 1 {
		t.Fatalf("GetEvents = %#v err=%v", events, err)
	}
	byID, err := db.GetEventByID(ctx, events[0].ID)
	if err != nil || byID.Title != "new" {
		t.Fatalf("GetEventByID = %#v err=%v", byID, err)
	}
	eventStats, err := db.GetEventStats(ctx)
	if err != nil || eventStats.TotalCount != 3 || eventStats.CountByType["api"] != 2 {
		t.Fatalf("GetEventStats = %#v err=%v", eventStats, err)
	}
	if err := db.CleanupOldEvents(ctx, "critical", 1, 1); err != nil {
		t.Fatalf("CleanupOldEvents by date: %v", err)
	}

	model := "gpt-test"
	errMsg := "failed"
	completed := time.Now().Add(-2 * time.Hour)
	expires := time.Now().Add(-time.Hour)
	tasks := []*AsyncTask{
		{ID: "task-1", TaskType: "generate", Status: "pending", RequestData: "{}", InputTokens: 10, OutputTokens: 20, CreatedAt: recent},
		{ID: "task-2", TaskType: "generate", Status: "completed", RequestData: "{}", Model: &model, ErrorMessage: &errMsg, InputTokens: 30, OutputTokens: 40, CreatedAt: recent, CompletedAt: &completed, ExpiresAt: &expires},
	}
	for _, task := range tasks {
		if err := db.SaveAsyncTask(ctx, task); err != nil {
			t.Fatalf("SaveAsyncTask: %v", err)
		}
	}
	task, err := db.GetAsyncTask(ctx, "task-1")
	if err != nil || task.Status != "pending" {
		t.Fatalf("GetAsyncTask = %#v err=%v", task, err)
	}
	task.Status = "running"
	if err := db.UpdateAsyncTask(ctx, task); err != nil {
		t.Fatalf("UpdateAsyncTask: %v", err)
	}
	pending, err := db.GetPendingAsyncTasks(ctx, 10)
	if err != nil || len(pending) != 0 {
		t.Fatalf("GetPendingAsyncTasks = %#v err=%v", pending, err)
	}
	filteredTasks, err := db.GetAsyncTasks(ctx, &AsyncTaskFilter{Status: "running", TaskType: "generate", StartTime: &recent, Limit: 10})
	if err != nil || len(filteredTasks) != 1 {
		t.Fatalf("GetAsyncTasks = %#v err=%v", filteredTasks, err)
	}
	tokenStats, err := db.GetAsyncTaskStats(ctx, nil, nil)
	if err != nil || tokenStats.TotalTasks != 2 || tokenStats.TotalTokens != 100 {
		t.Fatalf("GetAsyncTaskStats = %#v err=%v", tokenStats, err)
	}
	cleaned, err := db.CleanupExpiredAsyncTasks(ctx, time.Now().Add(-time.Hour))
	if err != nil || cleaned == 0 {
		t.Fatalf("CleanupExpiredAsyncTasks = %d err=%v", cleaned, err)
	}

	plan := &PositionPlan{Exchange: "binance", Symbol: "BTCUSDT", Status: "pending", Direction: "reduce", TargetAmountUSDT: 100, CreatedAt: recent}
	if err := db.SavePositionPlan(ctx, plan); err != nil {
		t.Fatalf("SavePositionPlan: %v", err)
	}
	plan.Status = "in_progress"
	if err := db.UpdatePositionPlan(ctx, plan); err != nil {
		t.Fatalf("UpdatePositionPlan: %v", err)
	}
	gotPlan, err := db.GetPositionPlan(ctx, plan.ID)
	if err != nil || gotPlan.Status != "in_progress" {
		t.Fatalf("GetPositionPlan = %#v err=%v", gotPlan, err)
	}
	plans, err := db.GetPositionPlans(ctx, &PositionPlanFilter{Exchange: "binance", Symbol: "BTCUSDT", Status: "in_progress", Limit: 10})
	if err != nil || len(plans) != 1 {
		t.Fatalf("GetPositionPlans = %#v err=%v", plans, err)
	}

	tx, err := db.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx: %v", err)
	}
	if err := tx.SaveTrade(ctx, &Trade{Exchange: "tx", Symbol: "BTCUSDT", CreatedAt: recent}); err != nil {
		t.Fatalf("tx SaveTrade: %v", err)
	}
	if _, err := tx.GetTrades(ctx, &TradeFilter{}); err == nil || !strings.Contains(err.Error(), "not implemented") {
		t.Fatalf("tx GetTrades err = %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("tx Commit: %v", err)
	}

	tx, err = db.BeginTx(ctx)
	if err != nil {
		t.Fatalf("BeginTx rollback: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("tx Rollback: %v", err)
	}
}

func TestNewGormDatabaseRejectsUnsupportedType(t *testing.T) {
	if _, err := NewGormDatabase(&DBConfig{Type: "oracle"}); err == nil || !strings.Contains(err.Error(), "unsupported database type") {
		t.Fatalf("unexpected unsupported type err: %v", err)
	}
}
