package main

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"testing"
)

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
