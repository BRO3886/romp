package codename

import (
	"regexp"
	"testing"
)

var nameRe = regexp.MustCompile(`^[a-z]+_[a-z]+$`)

func TestForDeterministic(t *testing.T) {
	a := For("owner/repo", 12)
	b := For("owner/repo", 12)
	if a != b {
		t.Errorf("For is not deterministic: %q != %q", a, b)
	}
}

func TestForShape(t *testing.T) {
	for _, tc := range []struct {
		repo  string
		issue int
	}{
		{"owner/repo", 1},
		{"a/b", 99},
		{"x/y", 1000000},
	} {
		if got := For(tc.repo, tc.issue); !nameRe.MatchString(got) {
			t.Errorf("For(%q, %d) = %q, want adjective_name shape", tc.repo, tc.issue, got)
		}
	}
}

func TestForVaries(t *testing.T) {
	a := For("owner/repo", 1)
	b := For("owner/repo", 2)
	if a == b {
		t.Errorf("For(%q, 1) == For(%q, 2) == %q", "owner/repo", "owner/repo", a)
	}
}
