package review_test

import (
	"reflect"
	"testing"

	"github.com/BRO3886/romp/internal/review"
)

func TestMergeOutcomesCombinesLensFindingsAndPromotesSeverity(t *testing.T) {
	file := "internal/relay.go"
	line := 42
	outcomes := []review.Outcome{
		{
			Verdict: review.VerdictApprove,
			Findings: []review.Finding{
				{Severity: review.SeverityNonBlocking, File: &file, Line: &line, Description: "The shutdown path can leak a connection."},
			},
		},
		{
			Verdict: review.VerdictFix,
			Findings: []review.Finding{
				{Severity: review.SeverityBlocking, File: &file, Line: &line, Description: "  The shutdown path can leak a connection.  "},
				{Severity: review.SeverityBlocking, Description: "The required race check is missing."},
			},
		},
	}

	got := review.MergeOutcomes(outcomes)
	want := review.Outcome{
		Verdict: review.VerdictFix,
		Findings: []review.Finding{
			{Severity: review.SeverityBlocking, File: &file, Line: &line, Description: "The shutdown path can leak a connection."},
			{Severity: review.SeverityBlocking, Description: "The required race check is missing."},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeOutcomes = %#v, want %#v", got, want)
	}
}

func TestMergeOutcomesApprovesEmptyLensSet(t *testing.T) {
	got := review.MergeOutcomes(nil)
	want := review.Outcome{Verdict: review.VerdictApprove, Findings: []review.Finding{}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("MergeOutcomes(nil) = %#v, want %#v", got, want)
	}
}
