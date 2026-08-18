package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/codename"
)

func TestJobLogPathUsesRepoNotCodename(t *testing.T) {
	got := jobLogPath("BRO3886", "romp", "jolly_kabuto")
	if !strings.HasSuffix(got, filepath.Join("BRO3886-romp", "logs", "jolly_kabuto.log")) {
		t.Errorf("jobLogPath = %q, want .../BRO3886-romp/logs/jolly_kabuto.log", got)
	}
	if strings.Contains(got, "BRO3886-jolly_kabuto") {
		t.Errorf("jobLogPath used the codename as the repo dir: %q", got)
	}
}

func TestLogCodenameAcceptsIssueNumber(t *testing.T) {
	got := logCodename("BRO3886", "romp", "8")
	want := codename.For("BRO3886/romp", 8)
	if got != want {
		t.Errorf("logCodename(8) = %q, want %q", got, want)
	}
	if logCodename("BRO3886", "romp", "jolly_kabuto") != "jolly_kabuto" {
		t.Errorf("logCodename passed a name through = %q", logCodename("BRO3886", "romp", "jolly_kabuto"))
	}
}
