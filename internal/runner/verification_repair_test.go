package runner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"unicode/utf8"

	"github.com/BRO3886/romp/internal/harness"
	"github.com/BRO3886/romp/internal/progress"
	"github.com/BRO3886/romp/internal/review"
)

func TestVerifyWithResultsLogsFailureOutput(t *testing.T) {
	var logs strings.Builder
	var events []progress.Event
	r := &Runner{
		Verify:   []string{`printf 'FAIL: got invalid_frame, want invalid_state\n' >&2; exit 7`},
		Stderr:   &logs,
		Progress: func(event progress.Event) { events = append(events, event) },
	}

	_, err := r.verifyWithResults(context.Background(), t.TempDir(), 15, progress.PhaseReverifying)
	var failure *verificationFailure
	if !errors.As(err, &failure) {
		t.Fatalf("verifyWithResults() error = %v, want verificationFailure", err)
	}
	if failure.Command != r.Verify[0] || failure.ExitCode != 7 || !strings.Contains(failure.Output, "invalid_frame") {
		t.Errorf("failure = %+v", failure)
	}
	if !strings.Contains(logs.String(), "verify failed") || !strings.Contains(logs.String(), "got invalid_frame, want invalid_state") {
		t.Errorf("logs missing failure evidence:\n%s", logs.String())
	}
	if len(events) != 2 || !strings.Contains(events[1].Detail, "failed") {
		t.Errorf("progress events = %+v, want start and failure", events)
	}
}

func TestRunRetriesRedReverificationWithinFixBudget(t *testing.T) {
	verify := sequentialVerifyCommand(t, false)
	fix := `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The readiness invariant is lost."}]}`
	approve := `{"verdict":"approve","findings":[]}`
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc fix: a thing"}
	c := &fakeGH{}
	builder := &sequenceHarness{results: []harness.Result{{}, {}, {}}}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: fix}, {Output: approve}}}
	r := newTestRunner(t, g, c, []string{verify})
	r.Harness = builder
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true
	r.MaxFixRounds = 2
	var logs strings.Builder
	r.Stderr = &logs
	var events []progress.Event
	r.Progress = func(event progress.Event) { events = append(events, event) }
	instrumentation := &fakeReviewStore{}
	r.ReviewInstrumentation = instrumentation

	if _, err := r.Run(context.Background(), 7); err != nil {
		t.Fatalf("Run() error: %v", err)
	}
	if len(builder.requests) != 3 {
		t.Fatalf("builder calls = %d, want initial build, review fix, and verification repair", len(builder.requests))
	}
	if !strings.Contains(builder.requests[2].Prompt, "INDEPENDENT VERIFICATION") || !strings.Contains(builder.requests[2].Prompt, "got invalid_frame, want invalid_state") {
		t.Errorf("verification repair prompt missing captured failure:\n%s", builder.requests[2].Prompt)
	}
	if !strings.Contains(builder.requests[2].Prompt, "The readiness invariant is lost.") {
		t.Errorf("verification repair prompt dropped the blocking review constraint:\n%s", builder.requests[2].Prompt)
	}
	wantReviewCalls := 2 * len(review.BuildPlan(g.files, false).Lenses)
	if len(reviewer.requests) != wantReviewCalls {
		t.Errorf("reviewer calls = %d, want %d", len(reviewer.requests), wantReviewCalls)
	}
	if got, want := g.commits, []string{"fix: a thing", "fix: address review findings for #7", "fix: repair verification failure for #7"}; !slices.Equal(got, want) {
		t.Errorf("commits = %v, want %v", got, want)
	}
	if len(g.pushed) != 2 {
		t.Errorf("pushes = %v, want initial green push and repaired green push", g.pushed)
	}
	if !strings.Contains(logs.String(), "verify failed") || !strings.Contains(logs.String(), "got invalid_frame, want invalid_state") {
		t.Errorf("logs missing re-verification failure:\n%s", logs.String())
	}
	if !hasProgressDetail(events, "retrying builder (2/2)") {
		t.Errorf("progress events missing visible repair transition: %+v", events)
	}
	if instrumentation.metrics.ReverificationFailures != 1 {
		t.Errorf("ReverificationFailures = %d, want 1", instrumentation.metrics.ReverificationFailures)
	}
}

func TestRunReportsRedAfterVerificationRepairBudgetIsExhausted(t *testing.T) {
	verify := sequentialVerifyCommand(t, true)
	fix := `{"verdict":"fix","findings":[{"severity":"blocking","file":"internal/a.go","line":7,"description":"The readiness invariant is lost."}]}`
	g := &fakeGit{changed: true, onAdd: writePR, files: []string{"internal/a.go"}, diff: "diff --git a/a b/a", log: "abc fix: a thing"}
	builder := &sequenceHarness{results: []harness.Result{{}, {}}}
	reviewer := &sequenceHarness{results: []harness.Result{{Output: fix}}}
	r := newTestRunner(t, g, &fakeGH{}, []string{verify})
	r.Harness = builder
	r.ReviewHarness = reviewer
	r.ReviewEnabled = true
	r.MaxFixRounds = 1
	r.Codename = "test_job"
	var logs strings.Builder
	r.Stderr = &logs

	_, err := r.Run(context.Background(), 7)
	if !errors.Is(err, ErrRed) {
		t.Fatalf("Run() error = %v, want ErrRed", err)
	}
	if !strings.Contains(err.Error(), "failed after fix round 1/1") || !strings.Contains(err.Error(), "full output: romp logs 7") {
		t.Errorf("terminal error lacks bounded failure guidance: %v", err)
	}
	if len(builder.requests) != 2 {
		t.Errorf("builder calls = %d, want no retry after exhausted round", len(builder.requests))
	}
	if !strings.Contains(logs.String(), "got invalid_frame, want invalid_state") {
		t.Errorf("logs missing terminal verification output:\n%s", logs.String())
	}
}

func TestVerificationOutputHintDistinguishesWatchFromRun(t *testing.T) {
	r := &Runner{}
	if got := r.verificationOutputHint(7); got != "full output written above" {
		t.Errorf("run output hint = %q", got)
	}
	r.Codename = "test_job"
	if got := r.verificationOutputHint(7); got != "full output: romp logs 7" {
		t.Errorf("watch output hint = %q", got)
	}
}

func TestBoundedVerificationOutputFitsPromptBudget(t *testing.T) {
	got := boundedVerificationOutput(strings.Repeat("é", maxVerificationPromptBytes))
	if len(got) > maxVerificationPromptBytes {
		t.Fatalf("bounded output bytes = %d, want at most %d", len(got), maxVerificationPromptBytes)
	}
	if !utf8.ValidString(got) {
		t.Fatal("bounded output split a UTF-8 sequence")
	}
	if !strings.Contains(got, "output truncated by Romp") {
		t.Fatal("bounded output omitted truncation marker")
	}
}

func sequentialVerifyCommand(t *testing.T, failAfterFirst bool) string {
	t.Helper()
	dir := t.TempDir()
	counter := filepath.Join(dir, "count")
	script := filepath.Join(dir, "verify.sh")
	condition := `[ "$count" -eq 2 ]`
	if failAfterFirst {
		condition = `[ "$count" -ge 2 ]`
	}
	body := fmt.Sprintf(`count=0
if [ -f %q ]; then count=$(cat %q); fi
count=$((count + 1))
printf '%%s' "$count" > %q
if %s; then
  printf 'FAIL: got invalid_frame, want invalid_state\n' >&2
  exit 1
fi
`, counter, counter, counter, condition)
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write verification script: %v", err)
	}
	return fmt.Sprintf("sh %q", script)
}

func hasProgressDetail(events []progress.Event, detail string) bool {
	for _, event := range events {
		if strings.Contains(event.Detail, detail) {
			return true
		}
	}
	return false
}
