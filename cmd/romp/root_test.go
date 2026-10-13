package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/BRO3886/romp/internal/config"
)

func TestRunCmdHelpListsOpenCode(t *testing.T) {
	cmd := newRemoteRunCmd()
	if !strings.Contains(cmd.UsageString(), "claude, codex, or opencode") {
		t.Errorf("run help does not list OpenCode:\n%s", cmd.UsageString())
	}
}

func TestWarnOpenCodeVariant(t *testing.T) {
	var out bytes.Buffer
	warnOpenCodeVariant(&out, &config.Config{Harness: config.Harness{
		Default: "opencode",
		Model:   "openai/gpt-5",
		Effort:  "high",
	}, HarnessEffortSource: "/repo/romp.toml"})
	if got := out.String(); !strings.Contains(got, `OpenCode variant "high" may not be supported by openai/gpt-5`) {
		t.Fatalf("warning = %q", got)
	}

	out.Reset()
	warnOpenCodeVariant(&out, &config.Config{Harness: config.Harness{
		Default: "codex", Effort: "high",
	}, HarnessEffortSource: "/repo/romp.toml"})
	if out.Len() != 0 {
		t.Fatalf("non-OpenCode warning = %q, want empty", out.String())
	}

	out.Reset()
	warnOpenCodeVariant(&out, &config.Config{Harness: config.Harness{
		Default: "opencode", Effort: "high",
	}})
	if out.Len() != 0 {
		t.Fatalf("default warning = %q, want empty", out.String())
	}
}
