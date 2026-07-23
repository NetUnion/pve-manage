package db

import (
	"context"
	"path/filepath"
	"testing"
)

func TestMigrateAddsVMTaskAttemptCount(t *testing.T) {
	database, err := Open(filepath.Join(t.TempDir(), "migration.sqlite3"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`PRAGMA table_info(vm_tasks)`)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		if name == "attempt_count" {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("attempt_count column was not added")
	}

	if _, err := database.Exec(`
		INSERT INTO vm_tasks(vm_id, seq, kind, payload_json, status, created_at, updated_at)
		VALUES(1, 1, 'provision', '{}', 'pending', '2026-07-23T00:00:00Z', '2026-07-23T00:00:00Z')
	`); err != nil {
		t.Fatal(err)
	}
	var attemptCount int
	if err := database.QueryRow(`SELECT attempt_count FROM vm_tasks WHERE vm_id = 1`).Scan(&attemptCount); err != nil {
		t.Fatal(err)
	}
	if attemptCount != 0 {
		t.Fatalf("attempt_count = %d, want 0", attemptCount)
	}
}
