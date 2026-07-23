package worker

import (
	"testing"
	"time"
)

func TestVMTaskRetryDelay(t *testing.T) {
	tests := []struct {
		attemptCount int
		want         time.Duration
		ok           bool
	}{
		{attemptCount: 0, ok: false},
		{attemptCount: 1, want: time.Minute, ok: true},
		{attemptCount: 2, want: 2 * time.Minute, ok: true},
		{attemptCount: 3, want: 4 * time.Minute, ok: true},
		{attemptCount: 4, want: 8 * time.Minute, ok: true},
		{attemptCount: 5, want: 16 * time.Minute, ok: true},
		{attemptCount: 6, ok: false},
	}
	for _, test := range tests {
		got, ok := vmTaskRetryDelay(test.attemptCount)
		if ok != test.ok || got != test.want {
			t.Errorf("vmTaskRetryDelay(%d) = (%s, %t), want (%s, %t)", test.attemptCount, got, ok, test.want, test.ok)
		}
	}
}

func TestVMTaskReady(t *testing.T) {
	now := time.Date(2026, time.July, 23, 8, 0, 0, 0, time.UTC)
	updatedAt := now.Add(-2 * time.Minute).Format(time.RFC3339Nano)

	if !vmTaskReady(vmTaskRow{Status: "pending"}, now) {
		t.Fatal("pending task should be ready immediately")
	}
	if !vmTaskReady(vmTaskRow{Status: "failed", AttemptCount: 1, UpdatedAt: updatedAt}, now) {
		t.Fatal("first retry should be ready after one minute")
	}
	if vmTaskReady(vmTaskRow{Status: "failed", AttemptCount: 3, UpdatedAt: updatedAt}, now) {
		t.Fatal("third retry should wait four minutes")
	}
	if vmTaskReady(vmTaskRow{Status: "failed", AttemptCount: 6, UpdatedAt: updatedAt}, now) {
		t.Fatal("task should not auto-retry after five retries")
	}
}
