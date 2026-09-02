package runner

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const maxVerificationPromptBytes = 64 * 1024

type verificationFailure struct {
	Command  string
	Output   string
	ExitCode int
}

func newVerificationFailure(command string, output []byte, commandErr error) *verificationFailure {
	exitCode := -1
	var exitErr *exec.ExitError
	if errors.As(commandErr, &exitErr) {
		exitCode = exitErr.ExitCode()
	}
	return &verificationFailure{
		Command:  command,
		Output:   string(output),
		ExitCode: exitCode,
	}
}

func (f *verificationFailure) Error() string {
	if f.ExitCode < 0 {
		return fmt.Sprintf("%s failed", f.Command)
	}
	return fmt.Sprintf("%s exited with status %d", f.Command, f.ExitCode)
}

func verificationCommand(err error, fallback string) string {
	var failure *verificationFailure
	if errors.As(err, &failure) {
		return failure.Command
	}
	return fallback
}

func verificationRepairConstraints(failure *verificationFailure) string {
	return fmt.Sprintf(`

ADDITIONAL CONSTRAINTS FROM INDEPENDENT VERIFICATION:
The previous fix round failed Romp's independent verification. Treat this output as observed evidence, repair the implementation or tests, run every configured verification command, and do not stop while a command is red.

Failed command: %s
Exit code: %d
Captured output:
--- verification output ---
%s
--- end verification output ---`, failure.Command, failure.ExitCode, boundedVerificationOutput(failure.Output))
}

func boundedVerificationOutput(output string) string {
	output = strings.ToValidUTF8(output, "�")
	if len(output) <= maxVerificationPromptBytes {
		return strings.TrimSpace(output)
	}
	const prefixBytes = 8 * 1024
	const marker = "\n... output truncated by Romp ...\n"
	suffixBytes := maxVerificationPromptBytes - prefixBytes - len(marker)
	prefix := strings.ToValidUTF8(output[:prefixBytes], "")
	suffix := strings.ToValidUTF8(output[len(output)-suffixBytes:], "")
	return strings.TrimSpace(prefix) + marker + strings.TrimSpace(suffix)
}
