package db

import (
	"context"
	"os"
	"testing"
)

func TestMigrateAddsVMTaskAttemptCount(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set, skipping PostgreSQL migration test")
	}

	database, err := Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	if err := Migrate(context.Background(), database); err != nil {
		t.Fatal(err)
	}

	rows, err := database.Query(`
		SELECT column_name FROM information_schema.columns
		WHERE table_name = 'vm_tasks' AND column_name = 'attempt_count'
	`)
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for rows.Next() {
		found = true
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
