package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/BRO3886/romp/internal/job"
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

func TestPrintJobsExposesSessionID(t *testing.T) {
	var out bytes.Buffer
	printJobs(&out, []job.Job{
		{Repo: "o/r", Issue: 7, Branch: "romp-7", ClaimedAt: time.Now().UTC().Format(time.RFC3339Nano), SessionID: "session-7"},
		{Repo: "o/r", Issue: 8, Branch: "romp-8", ClaimedAt: time.Now().UTC().Format(time.RFC3339Nano)},
	}, false)
	got := out.String()
	if tableColumn(t, got, "SESSION", 0) != "session-7" {
		t.Errorf("session row missing:\n%s", got)
	}
	if tableColumn(t, got, "SESSION", 1) != "-" {
		t.Errorf("empty session should be a dash:\n%s", got)
	}
	if !strings.Contains(got, "SESSION") {
		t.Errorf("header missing SESSION:\n%s", got)
	}
}
