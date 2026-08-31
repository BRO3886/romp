package review

import (
	"encoding/json"
	"reflect"
	"testing"
)

var routeInstructions = map[string]string{
	"correctness":    "Adversarial bug-hunt + verify the claim; default stance: find reasons NOT to merge.",
	"tests":          "Assess test coverage using the supplied verification transcript as evidence rather than claiming commands were run during review.",
	"security":       "Review security-sensitive behavior against the repository's actual enforcement mechanisms.",
	"quality":        "Strict maintainability / abstraction / spaghetti / 1k-line-file audit.",
	"pr-conventions": "Project conventions + logical soundness via the repo's own reviewer.",
	"architecture":   "Deepening / structural opportunities. Auto-added on security-critical diffs.",
	"diagnose":       "Disciplined bug/perf diagnosis loop. Added when the diff is a bug/regression fix.",
	"go":             "Go backend conventions + architecture patterns.",
	"flutter":        "Flutter/Dart app conventions + architecture patterns.",
	"frontend":       "Design-engineering polish for UI components, animations, interaction states.",
	"docs":           "Detect AI-slop writing patterns in docs/markdown/copy.",
}

func TestBuildPlan(t *testing.T) {
	tests := []struct {
		name     string
		files    []string
		bugfix   bool
		hasCode  bool
		hasDocs  bool
		lensKeys []string
	}{
		{
			name:     "pure Go code",
			files:    []string{"internal/api/handler.go", "go.mod"},
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "go"},
		},
		{
			name:     "docs only",
			files:    []string{"README.md", "docs/architecture.md"},
			hasDocs:  true,
			lensKeys: []string{"docs"},
		},
		{
			name:     "mixed code and docs preserves registry order",
			files:    []string{"README.md", "scripts/migrate.py"},
			hasCode:  true,
			hasDocs:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "docs"},
		},
		{
			name:     "security path elevates architecture",
			files:    []string{"backend/Backup/Encryptor.cs", "internal/auth/keys.go"},
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "architecture", "go"},
		},
		{
			name:     "Dart selects Flutter",
			files:    []string{"lib/main.dart", "pubspec.yaml"},
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "flutter"},
		},
		{
			name:     "pubspec selects Flutter alongside other code",
			files:    []string{"cmd/app/main.go", "pubspec.yaml"},
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "go", "flutter"},
		},
		{
			name:     "frontend extension selects frontend",
			files:    []string{"src/components/Button.tsx", "styles/app.css"},
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "frontend"},
		},
		{
			name:     "bugfix selects diagnose",
			files:    []string{"internal/queue/drain.go"},
			bugfix:   true,
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "diagnose", "go"},
		},
		{
			name:     "empty change list",
			files:    nil,
			lensKeys: []string{},
		},
		{
			name:     "case normalization and blank filtering",
			files:    []string{"", "  ", "INTERNAL/API/HANDLER.GO", "GUIDE.MDX"},
			hasCode:  true,
			hasDocs:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "go", "docs"},
		},
		{
			name:     "security tokens use substring matching",
			files:    []string{"internal/author/model.go"},
			hasCode:  true,
			lensKeys: []string{"correctness", "tests", "security", "quality", "pr-conventions", "architecture", "go"},
		},
		{
			name:     "pubspec alone is not code",
			files:    []string{"pubspec.yaml"},
			lensKeys: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildPlan(tt.files, tt.bugfix)
			want := Plan{
				HasCode: tt.hasCode,
				HasDocs: tt.hasDocs,
				Lenses:  routeLenses(tt.lensKeys),
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("BuildPlan(%q, %t) = %#v, want %#v", tt.files, tt.bugfix, got, want)
			}
		})
	}
}

func TestPlanJSON(t *testing.T) {
	plan := Plan{
		HasCode: true,
		HasDocs: false,
		Lenses: []Lens{
			{Name: "docs", Instruction: routeInstructions["docs"]},
		},
	}

	got, err := json.Marshal(plan)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"has_code":true,"has_docs":false,"lenses":[{"name":"docs","instruction":"Detect AI-slop writing patterns in docs/markdown/copy."}]}`
	if string(got) != want {
		t.Errorf("json.Marshal(Plan) = %s, want %s", got, want)
	}
}

func TestEmptyPlanJSONUsesArray(t *testing.T) {
	got, err := json.Marshal(BuildPlan(nil, false))
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	want := `{"has_code":false,"has_docs":false,"lenses":[]}`
	if string(got) != want {
		t.Errorf("json.Marshal(empty Plan) = %s, want %s", got, want)
	}
}

func routeLenses(keys []string) []Lens {
	lenses := make([]Lens, 0, len(keys))
	for _, key := range keys {
		lenses = append(lenses, Lens{Name: key, Instruction: routeInstructions[key]})
	}
	return lenses
}
