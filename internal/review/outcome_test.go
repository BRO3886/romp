package review_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/review"
)

func TestParseOutcomeFixtures(t *testing.T) {
	line := 18
	parser := "internal/review/outcome.go"
	tests := []struct {
		name string
		file string
		want review.Outcome
	}{
		{
			name: "empty approval",
			file: "approve_empty.json",
			want: review.Outcome{Verdict: review.VerdictApprove, Findings: []review.Finding{}},
		},
		{
			name: "approval with lower severity findings",
			file: "approve_findings.json",
			want: review.Outcome{
				Verdict: review.VerdictApprove,
				Findings: []review.Finding{
					{Severity: review.SeverityNonBlocking, File: &parser, Line: &line, Description: "The error could include the rejected field name."},
					{Severity: review.SeverityNit, Description: "A helper name could be shorter."},
				},
			},
		},
		{
			name: "fix with blocking finding",
			file: "fix_blocking.json",
			want: review.Outcome{
				Verdict: review.VerdictFix,
				Findings: []review.Finding{
					{Severity: review.SeverityBlocking, File: &parser, Line: &line, Description: "Duplicate keys are accepted, so the parser is not fail-closed."},
					{Severity: review.SeverityNit, Description: "The table name could be more specific."},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, err := os.ReadFile(filepath.Join("testdata", tt.file))
			if err != nil {
				t.Fatalf("read fixture: %v", err)
			}
			got, err := review.ParseOutcome(string(output))
			if err != nil {
				t.Fatalf("ParseOutcome: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseOutcome = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestParseOutcomeRejectsInvalidDocuments(t *testing.T) {
	valid := `{"verdict":"approve","findings":[]}`
	tests := []struct {
		name   string
		output string
		want   string
	}{
		{name: "empty output", output: "", want: "empty"},
		{name: "whitespace only", output: " \n\t", want: "empty"},
		{name: "malformed JSON", output: `{"verdict":`, want: "JSON"},
		{name: "invalid UTF-8", output: "{\"verdict\":\"approve\",\"findings\":[{\"severity\":\"nit\",\"file\":null,\"line\":null,\"description\":\"" + string([]byte{0xff}) + "\"}]}", want: "UTF-8"},
		{name: "non-object", output: `[]`, want: "object"},
		{name: "null document", output: `null`, want: "object"},
		{name: "missing verdict", output: `{"findings":[]}`, want: "verdict"},
		{name: "missing findings", output: `{"verdict":"approve"}`, want: "findings"},
		{name: "unknown top-level field", output: `{"verdict":"approve","findings":[],"summary":"ok"}`, want: "summary"},
		{name: "duplicate top-level key", output: `{"verdict":"approve","verdict":"fix","findings":[]}`, want: "duplicate"},
		{name: "invalid verdict", output: `{"verdict":"approved","findings":[]}`, want: "verdict"},
		{name: "null findings", output: `{"verdict":"approve","findings":null}`, want: "array"},
		{name: "non-array findings", output: `{"verdict":"approve","findings":{}}`, want: "array"},
		{name: "missing finding field", output: `{"verdict":"approve","findings":[{"severity":"nit","file":null,"line":null}]}`, want: "description"},
		{name: "unknown finding field", output: `{"verdict":"approve","findings":[{"severity":"nit","file":null,"line":null,"description":"x","code":"N1"}]}`, want: "code"},
		{name: "duplicate finding key", output: `{"verdict":"approve","findings":[{"severity":"nit","severity":"non-blocking","file":null,"line":null,"description":"x"}]}`, want: "duplicate"},
		{name: "invalid severity", output: `{"verdict":"approve","findings":[{"severity":"warning","file":null,"line":null,"description":"x"}]}`, want: "severity"},
		{name: "empty file", output: findingWith(`""`, "null", `"x"`), want: "file"},
		{name: "absolute file", output: findingWith(`"/tmp/file.go"`, "1", `"x"`), want: "file"},
		{name: "parent file", output: findingWith(`"../file.go"`, "1", `"x"`), want: "file"},
		{name: "embedded parent component", output: findingWith(`"internal/../file.go"`, "1", `"x"`), want: "file"},
		{name: "unclean file", output: findingWith(`"internal//file.go"`, "1", `"x"`), want: "file"},
		{name: "Windows absolute file", output: findingWith(`"C:\\\\tmp\\\\file.go"`, "1", `"x"`), want: "file"},
		{name: "zero line", output: findingWith(`"file.go"`, "0", `"x"`), want: "line"},
		{name: "negative line", output: findingWith(`"file.go"`, "-1", `"x"`), want: "line"},
		{name: "fractional line", output: findingWith(`"file.go"`, "1.5", `"x"`), want: "line"},
		{name: "string line", output: findingWith(`"file.go"`, `"1"`, `"x"`), want: "line"},
		{name: "line without file", output: findingWith("null", "1", `"x"`), want: "file"},
		{name: "empty description", output: findingWith("null", "null", `""`), want: "description"},
		{name: "whitespace description", output: findingWith("null", "null", `"  \n"`), want: "description"},
		{name: "trailing prose", output: valid + " approved", want: "trailing"},
		{name: "trailing JSON value", output: valid + ` {}`, want: "trailing"},
		{name: "Markdown fence", output: "```json\n" + valid + "\n```", want: "JSON"},
		{name: "blocking approval", output: `{"verdict":"approve","findings":[{"severity":"blocking","file":"file.go","line":1,"description":"x"}]}`, want: "approve"},
		{name: "fix without findings", output: `{"verdict":"fix","findings":[]}`, want: "blocking"},
		{name: "fix with lower severity only", output: `{"verdict":"fix","findings":[{"severity":"non-blocking","file":null,"line":null,"description":"x"}]}`, want: "blocking"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := review.ParseOutcome(tt.output)
			if err == nil {
				t.Fatalf("ParseOutcome returned no error and outcome %+v", got)
			}
			if !reflect.DeepEqual(got, review.Outcome{}) {
				t.Errorf("ParseOutcome returned usable outcome on error: %+v", got)
			}
			if !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(tt.want)) {
				t.Errorf("ParseOutcome error = %q, want context containing %q", err, tt.want)
			}
		})
	}
}

func TestParseOutcomeAllowsSurroundingWhitespace(t *testing.T) {
	got, err := review.ParseOutcome(" \n\t{\"verdict\":\"approve\",\"findings\":[]}\r\n ")
	if err != nil {
		t.Fatalf("ParseOutcome: %v", err)
	}
	if got.Verdict != review.VerdictApprove || len(got.Findings) != 0 {
		t.Fatalf("ParseOutcome = %+v, want empty approval", got)
	}
}

func findingWith(file, line, description string) string {
	return `{"verdict":"approve","findings":[{"severity":"nit","file":` + file + `,"line":` + line + `,"description":` + description + `}]}`
}
