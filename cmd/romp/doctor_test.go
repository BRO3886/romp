package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestGitVersionAtLeast(t *testing.T) {
	tests := []struct {
		version string
		minMaj  int
		minMin  int
		want    bool
	}{
		{"git version 2.50.1", 2, 35, true},
		{"git version 2.50.1 (Apple Git-155)", 2, 35, true},
		{"git version 2.35.0", 2, 35, true},
		{"git version 2.34.9", 2, 35, false},
		{"git version 2.35", 2, 35, true},
		{"git version 3.0.0", 2, 35, true},
		{"git version 1.9.0", 2, 35, false},
		{"git version 2.4", 2, 35, false},
		{"garbage", 2, 35, false},
		{"", 2, 35, false},
	}
	for _, tt := range tests {
		if got := gitVersionAtLeast(tt.version, tt.minMaj, tt.minMin); got != tt.want {
			t.Errorf("gitVersionAtLeast(%q, %d, %d) = %v, want %v", tt.version, tt.minMaj, tt.minMin, got, tt.want)
		}
	}
}

func TestRunDoctorAllPass(t *testing.T) {
	checks := []doctorCheck{
		{name: "a", run: func(context.Context) (string, error) { return "one", nil }},
		{name: "b", run: func(context.Context) (string, error) { return "two", nil }},
	}
	var out bytes.Buffer
	if err := runDoctor(&out, context.Background(), checks); err != nil {
		t.Fatalf("runDoctor = %v, want nil", err)
	}
	for _, want := range []string{"a", "one", "ok", "b", "two"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q:\n%s", want, out.String())
		}
	}
}

func TestRunDoctorFailure(t *testing.T) {
	checks := []doctorCheck{
		{name: "ok", run: func(context.Context) (string, error) { return "fine", nil }},
		{name: "bad", run: func(context.Context) (string, error) { return "thing", fmt.Errorf("broken") }},
	}
	var out bytes.Buffer
	err := runDoctor(&out, context.Background(), checks)
	if err == nil {
		t.Fatal("runDoctor = nil error, want failure")
	}
	if err.Error() != "1 of 2 checks failed" {
		t.Errorf("err = %q, want 1 of 2 checks failed", err)
	}
	got := out.String()
	if !strings.Contains(got, "bad") || !strings.Contains(got, "broken") || !strings.Contains(got, "FAIL") {
		t.Errorf("output missing failing row:\n%s", got)
	}
	if !strings.Contains(got, "ok") || !strings.Contains(got, "fine") {
		t.Errorf("output missing passing row:\n%s", got)
	}
}

func TestSummarizeHarnesses(t *testing.T) {
	claudeOK := harnessProbe{name: "claude", detail: "2.1.0"}
	codexOK := harnessProbe{name: "codex", detail: "0.146.0"}
	claudeBad := harnessProbe{name: "claude", err: fmt.Errorf("claude CLI not found on PATH")}
	codexBad := harnessProbe{name: "codex", err: fmt.Errorf("codex CLI not found on PATH")}

	tests := []struct {
		name    string
		probes  []harnessProbe
		want    string
		wantErr string
	}{
		{
			name:   "both healthy",
			probes: []harnessProbe{claudeOK, codexOK},
			want:   "claude 2.1.0; codex 0.146.0",
		},
		{
			name:   "only claude",
			probes: []harnessProbe{claudeOK, codexBad},
			want:   "claude 2.1.0 (codex: codex CLI not found on PATH)",
		},
		{
			name:   "only codex",
			probes: []harnessProbe{claudeBad, codexOK},
			want:   "codex 0.146.0 (claude: claude CLI not found on PATH)",
		},
		{
			name:    "neither",
			probes:  []harnessProbe{claudeBad, codexBad},
			wantErr: "need claude or codex: claude: claude CLI not found on PATH; codex: codex CLI not found on PATH",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := summarizeHarnesses(tt.probes)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("summarizeHarnesses = %q, nil error, want %q", got, tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Errorf("err = %q, want %q", err.Error(), tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("summarizeHarnesses = %v", err)
			}
			if got != tt.want {
				t.Errorf("summarizeHarnesses = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDoctorCmdWritesToStdout(t *testing.T) {
	cmd := newDoctorCmd(
		doctorCheck{name: "x", run: func(context.Context) (string, error) { return "y", nil }},
	)
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(out.String(), "x") || !strings.Contains(out.String(), "y") || !strings.Contains(out.String(), "ok") {
		t.Errorf("output = %q, want x row", out.String())
	}
}
