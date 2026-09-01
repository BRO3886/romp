package review

import "time"

const (
	SkipDisabled = "disabled"
	SkipDocsOnly = "docs-only"
	FixApproved  = "approved"
	FixBlocking  = "blocking"
)

// PassInstrumentation records one reviewer harness pass.
type PassInstrumentation struct {
	Verdict     Verdict `json:"verdict"`
	Blocking    int     `json:"blocking"`
	NonBlocking int     `json:"non_blocking"`
	Nit         int     `json:"nit"`
	DurationMS  int64   `json:"duration_ms"`
}

// Instrumentation records the review-gate facts for one job.
type Instrumentation struct {
	ReviewRan         bool                  `json:"review_ran"`
	SkipReason        string                `json:"skip_reason,omitempty"`
	BuilderDurationMS int64                 `json:"builder_duration_ms"`
	FixRoundFired     bool                  `json:"fix_round_fired"`
	FixRoundOutcome   string                `json:"fix_round_outcome,omitempty"`
	Passes            []PassInstrumentation `json:"passes,omitempty"`
}

// InstrumentPass converts a parsed review outcome into calibration counters.
func InstrumentPass(outcome Outcome, duration time.Duration) PassInstrumentation {
	pass := PassInstrumentation{Verdict: outcome.Verdict, DurationMS: duration.Milliseconds()}
	for _, finding := range outcome.Findings {
		switch finding.Severity {
		case SeverityBlocking:
			pass.Blocking++
		case SeverityNonBlocking:
			pass.NonBlocking++
		case SeverityNit:
			pass.Nit++
		}
	}
	return pass
}
