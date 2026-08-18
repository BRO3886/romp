package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/job"
)

func TestWriteHistorySingleRepoOmitsRepoColumn(t *testing.T) {
	var out bytes.Buffer
	if err := writeHistory(&out, []job.Outcome{
		{Repo: "a/b", Issue: 7, Outcome: "done", FinishedAt: "2026-08-18T12:00:00Z"},
	}, false); err != nil {
		t.Fatalf("writeHistory: %v", err)
	}
	got := out.String()
	if strings.Contains(got, "REPO") || strings.Contains(got, "a/b") {
		t.Errorf("single-repo table should omit REPO:\n%s", got)
	}
	if col := tableColumn(t, got, "ISSUE", 0); col != "7" {
		t.Errorf("ISSUE = %q, want 7\n%s", col, got)
	}
}

func TestWriteHistoryAllIncludesEveryRepo(t *testing.T) {
	var out bytes.Buffer
	if err := writeHistory(&out, []job.Outcome{
		{Repo: "a/b", Issue: 7, Outcome: "done", FinishedAt: "t1"},
		{Repo: "other/repo", Issue: 8, Outcome: "red", FinishedAt: "t2"},
	}, true); err != nil {
		t.Fatalf("writeHistory: %v", err)
	}
	got := out.String()
	if tableColumn(t, got, "REPO", 0) != "a/b" || tableColumn(t, got, "ISSUE", 0) != "7" {
		t.Errorf("row 0 = %q", got)
	}
	if tableColumn(t, got, "REPO", 1) != "other/repo" || tableColumn(t, got, "ISSUE", 1) != "8" {
		t.Errorf("row 1 = %q", got)
	}
}

func TestHistoryCmdHasAllFlag(t *testing.T) {
	if newHistoryCmd().Flags().Lookup("all") == nil {
		t.Fatal("history is missing --all")
	}
}

func tableColumn(t *testing.T, output, column string, row int) string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) < row+2 {
		t.Fatalf("need header + row %d, got %d lines:\n%s", row, len(lines), output)
	}
	headers := strings.Fields(lines[0])
	idx := -1
	for i, h := range headers {
		if h == column {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatalf("no column %q in %v", column, headers)
	}
	fields := strings.Fields(lines[row+1])
	if idx >= len(fields) {
		t.Fatalf("row %d has no field %d: %v", row, idx, fields)
	}
	return fields[idx]
}
