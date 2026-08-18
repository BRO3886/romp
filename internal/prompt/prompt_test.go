package prompt

import (
	"strings"
	"testing"
)

func TestRenderFillsPlaceholders(t *testing.T) {
	r := Renderer{Template: Default()}
	out, err := r.Render(Data{
		Issue:  "17",
		Title:  "Fix widget",
		Body:   "the widget is broken",
		Repo:   "owner/name",
		Branch: "romp-17",
		Base:   "main",
		URL:    "https://example.com/17",
		Verify: "go test ./... -count=1",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	for _, want := range []string{
		"owner/name",
		"romp-17",
		"#17",
		"Fix widget",
		"the widget is broken",
		"go test ./... -count=1",
		"PROVE IT",
		"CONSTRAINTS",
		"REPORT",
		"GATE",
		"self-reject",
		"Do not invent missing criteria",
		".romp/pull-request.md",
		".romp/blocked.md",
		"mermaid",
		"conventional commit",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
}

func TestDefaultPutsGateBeforeImplementation(t *testing.T) {
	text := Default()
	gate := strings.Index(text, "GATE")
	done := strings.Index(text, "DONE means")
	report := strings.Index(text, "REPORT:")
	if gate < 0 || done < 0 || report < 0 {
		t.Fatalf("template missing GATE/DONE/REPORT")
	}
	if !(gate < done && done < report) {
		t.Errorf("section order: GATE=%d DONE=%d REPORT=%d, want GATE then DONE then REPORT", gate, done, report)
	}
}

func TestRenderEmptyProtectedOmitsSection(t *testing.T) {
	r := Renderer{Template: Default()}
	out, err := r.Render(Data{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if strings.Contains(out, "Protected paths") {
		t.Errorf("protected section rendered when empty:\n%s", out)
	}
}

func TestRenderIgnoreAndBrief(t *testing.T) {
	r := Renderer{Template: Default()}
	out, err := r.Render(Data{
		Issue:  "9",
		Body:   "x",
		Ignore: "vendor/**, node_modules/**",
		Brief:  ".romp/DESIGN.md",
		Verify: "go test ./...",
		Repo:   "owner/name",
		Branch: "romp-9",
		Base:   "main",
		Title:  "t",
		URL:    "u",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, want := range []string{
		"Ignored paths (do not read): vendor/**, node_modules/**",
		"READ FIRST: read .romp/DESIGN.md",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered prompt missing %q", want)
		}
	}
}

func TestRenderEmptyIgnoreAndBriefOmitSections(t *testing.T) {
	r := Renderer{Template: Default()}
	out, err := r.Render(Data{})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	for _, unwanted := range []string{"Ignored paths", "READ FIRST"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("section %q rendered when empty:\n%s", unwanted, out)
		}
	}
}
