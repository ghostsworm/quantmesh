package storage

import (
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestParseBotStateUpdatedAtAcceptsMySQLAndSQLiteValues(t *testing.T) {
	want := time.Date(2026, 10, 10, 6, 47, 3, 0, time.UTC)
	cases := []struct {
		name  string
		value any
	}{
		{name: "driver time", value: want},
		{name: "mysql datetime bytes", value: []byte("2026-10-10 06:47:03")},
		{name: "mysql datetime string", value: "2026-10-10 06:47:03"},
		{name: "sqlite rfc3339 string", value: want.Format(time.RFC3339Nano)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseBotStateUpdatedAt(tc.value)
			if !ok || !got.Equal(want) {
				t.Fatalf("parseBotStateUpdatedAt(%v) = %v, %t; want %v, true", tc.value, got, ok, want)
			}
		})
	}
}

func TestSQLiteBotStateTimestampRoundTrip(t *testing.T) {
	store, err := NewSQLStorage(t.TempDir() + "/bot-state.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close SQLite storage: %v", err)
		}
	})
	want := time.Date(2026, 10, 10, 6, 47, 3, 0, time.FixedZone("UTC+8", 8*60*60))
	if err := store.SetBotState(&BotState{BotID: "bot-state-time-test", Enabled: false, UpdatedAt: want}); err != nil {
		t.Fatal(err)
	}
	got, err := store.GetBotState("bot-state-time-test")
	if err != nil || got == nil || !got.UpdatedAt.Equal(want) {
		t.Fatalf("SQLite timestamp round trip: got=%+v err=%v, want=%s", got, err, want)
	}
}

func TestMySQLBotStateTimestampRoundTrip(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := migrateBotStatesTableMySQL(db); err != nil {
		t.Fatal(err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	botID := fmt.Sprintf("bot-state-time-%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := db.Exec(`DELETE FROM bot_states WHERE bot_id = ?`, botID); err != nil {
			t.Errorf("remove MySQL bot state fixture: %v", err)
		}
	})
	want := time.Date(2026, 10, 10, 6, 47, 3, 0, time.UTC)
	if err := store.SetBotState(&BotState{BotID: botID, Enabled: false, UpdatedAt: want}); err != nil {
		t.Fatalf("persist MySQL bot state timestamp: %v", err)
	}
	got, err := store.GetBotState(botID)
	if err != nil || got == nil || !got.UpdatedAt.Equal(want) {
		t.Fatalf("MySQL timestamp round trip: got=%+v err=%v, want=%s", got, err, want)
	}
}
