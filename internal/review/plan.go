// Package review builds deterministic lens plans for changed files.
package review

import (
	"path/filepath"
	"strings"
)

// Lens describes one review emphasis in a plan.
type Lens struct {
	Name        string `json:"name"`
	Instruction string `json:"instruction"`
}

// Plan describes the review lenses and change categories for a file set.
type Plan struct {
	HasCode bool   `json:"has_code"`
	HasDocs bool   `json:"has_docs"`
	Lenses  []Lens `json:"lenses"`
}

var lensRegistry = []Lens{
	{Name: "correctness", Instruction: "Adversarial bug-hunt + verify the claim; default stance: find reasons NOT to merge."},
	{Name: "tests", Instruction: "Run the suite and print the exit code. No green run, no merge-safe verdict."},
	{Name: "security", Instruction: "Review security-sensitive behavior against the repository's actual enforcement mechanisms."},
	{Name: "quality", Instruction: "Strict maintainability / abstraction / spaghetti / 1k-line-file audit."},
	{Name: "pr-conventions", Instruction: "Project conventions + logical soundness via the repo's own reviewer."},
	{Name: "architecture", Instruction: "Deepening / structural opportunities. Auto-added on security-critical diffs."},
	{Name: "diagnose", Instruction: "Disciplined bug/perf diagnosis loop. Added when the diff is a bug/regression fix."},
	{Name: "go", Instruction: "Go backend conventions + architecture patterns."},
	{Name: "flutter", Instruction: "Flutter/Dart app conventions + architecture patterns."},
	{Name: "frontend", Instruction: "Design-engineering polish for UI components, animations, interaction states."},
	{Name: "docs", Instruction: "Detect AI-slop writing patterns in docs/markdown/copy."},
}

var codeExtensions = map[string]struct{}{
	".go": {}, ".dart": {}, ".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {},
	".py": {}, ".cs": {}, ".java": {}, ".kt": {}, ".rb": {}, ".rs": {},
	".c": {}, ".cc": {}, ".cpp": {}, ".h": {}, ".hpp": {}, ".swift": {},
	".scala": {}, ".php": {}, ".vue": {}, ".svelte": {}, ".css": {},
	".scss": {}, ".sql": {},
}

var docExtensions = map[string]struct{}{
	".md": {}, ".mdx": {}, ".txt": {}, ".rst": {},
}

var frontendExtensions = map[string]struct{}{
	".tsx": {}, ".jsx": {}, ".vue": {}, ".svelte": {}, ".css": {}, ".scss": {},
}

var securityPathTokens = []string{
	"crypto", "encrypt", "decrypt", "backup", "secret", "auth", "token",
	"hipaa", "pii", "password", "/keys", "keystore", "kms", "cipher",
}

// BuildPlan classifies changed files and returns lenses in registry order.
func BuildPlan(files []string, bugfix bool) Plan {
	extensions := make(map[string]struct{})
	names := make(map[string]struct{})
	lowered := make([]string, 0, len(files))

	for _, file := range files {
		if strings.TrimSpace(file) == "" {
			continue
		}
		lower := strings.ToLower(file)
		base := strings.ToLower(filepath.Base(file))
		extensions[fileExtension(base)] = struct{}{}
		names[base] = struct{}{}
		lowered = append(lowered, lower)
	}

	hasCode := intersects(extensions, codeExtensions)
	hasDocs := intersects(extensions, docExtensions)
	tags := make(map[string]struct{})
	if hasDocs {
		tags["docs"] = struct{}{}
	}
	if hasCode {
		for _, name := range []string{"correctness", "tests", "security", "quality", "pr-conventions"} {
			tags[name] = struct{}{}
		}
		if _, ok := extensions[".go"]; ok {
			tags["go"] = struct{}{}
		}
		_, hasPubspec := names["pubspec.yaml"]
		if _, hasDart := extensions[".dart"]; hasDart || hasPubspec {
			tags["flutter"] = struct{}{}
		}
		if intersects(extensions, frontendExtensions) {
			tags["frontend"] = struct{}{}
		}
		if containsSecurityToken(lowered) {
			tags["architecture"] = struct{}{}
		}
		if bugfix {
			tags["diagnose"] = struct{}{}
		}
	}

	plan := Plan{HasCode: hasCode, HasDocs: hasDocs, Lenses: make([]Lens, 0, len(tags))}
	for _, lens := range lensRegistry {
		if _, ok := tags[lens.Name]; ok {
			plan.Lenses = append(plan.Lenses, lens)
		}
	}
	return plan
}

func fileExtension(base string) string {
	dot := strings.LastIndexByte(base, '.')
	if dot == -1 {
		return ""
	}
	return base[dot:]
}

func intersects(left, right map[string]struct{}) bool {
	for value := range left {
		if _, ok := right[value]; ok {
			return true
		}
	}
	return false
}

func containsSecurityToken(paths []string) bool {
	for _, path := range paths {
		for _, token := range securityPathTokens {
			if strings.Contains(path, token) {
				return true
			}
		}
	}
	return false
}
