package database

import "testing"

func TestModelTableNames(t *testing.T) {
	tests := []struct {
		name string
		got  string
		want string
	}{
		{name: "events", got: EventRecord{}.TableName(), want: "events"},
		{name: "async_tasks", got: AsyncTask{}.TableName(), want: "async_tasks"},
		{name: "position_plans", got: PositionPlan{}.TableName(), want: "position_plans"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Fatalf("TableName() = %q, want %q", tt.got, tt.want)
			}
		})
	}
}

func TestNewDatabaseRejectsUnsupportedType(t *testing.T) {
	db, err := NewDatabase(&Config{Type: "oracle"})
	if err == nil {
		t.Fatal("expected unsupported database type error")
	}
	if db != nil {
		t.Fatalf("expected nil database for unsupported type, got %#v", db)
	}
}
