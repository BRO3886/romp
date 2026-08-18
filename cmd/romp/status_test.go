package main

import (
	"testing"
	"time"
)

func TestElapsed(t *testing.T) {
	now := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	if got := elapsed("2026-08-18T11:55:00Z", now); got != "5m0s" {
		t.Errorf("elapsed = %q, want 5m0s", got)
	}
	if got := elapsed("2026-08-18T11:59:30Z", now); got != "30s" {
		t.Errorf("elapsed = %q, want 30s", got)
	}
	if got := elapsed("not-a-time", now); got != "?" {
		t.Errorf("elapsed unparseable = %q, want ?", got)
	}
}
