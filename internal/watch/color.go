package watch

import (
	"hash/fnv"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"

	"github.com/mattn/go-isatty"
)

const (
	ansiReset   = "\x1b[0m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiMagenta = "\x1b[35m"
	ansiRed     = "\x1b[31m"
	ansiBlue    = "\x1b[34m"
	ansiYellow  = "\x1b[33m"
	ansiGreen   = "\x1b[1;32m"
	ansiBoldRed = "\x1b[1;31m"
	ansiBlocked = "\x1b[1;33m"
)

var (
	jobIdentity  = regexp.MustCompile(`^(?:\d{2}:){2}\d{2}  (\[[^\]]+\])`)
	warningToken = regexp.MustCompile(`(\] )warning:`)
	prURL        = regexp.MustCompile(`https://github\.com/[^[:space:]]+/pull/[0-9]+`)
	outcomeToken = regexp.MustCompile(`#\d+: (?:done|blocked|timeout|red|no-changes|error)\b`)
)

type colorizer struct {
	enabled bool
	name    string
}

type colorWriter struct {
	mu sync.Mutex
	w  io.Writer
	c  colorizer
}

func newColorizer(enabled bool, name string) colorizer {
	return colorizer{enabled: enabled, name: name}
}

func colorEnabled(interactive bool) bool {
	_, disabled := os.LookupEnv("NO_COLOR")
	return interactive && !disabled
}

func NewColorWriter(w io.Writer, name string) io.Writer {
	return &colorWriter{
		w: w,
		c: newColorizer(colorEnabled(isatty.IsTerminal(os.Stderr.Fd()) || isatty.IsCygwinTerminal(os.Stderr.Fd())), name),
	}
}

func (w *colorWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, err := io.WriteString(w.w, w.c.colorize(string(p))); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c colorizer) colorize(line string) string {
	if !c.enabled {
		return line
	}
	line = warningToken.ReplaceAllString(line, "${1}"+ansiYellow+"warning"+ansiReset+":")
	line = jobIdentity.ReplaceAllStringFunc(line, func(match string) string {
		identityStart := strings.LastIndexByte(match, ' ') + 1
		return match[:identityStart] + c.nameColor() + match[identityStart:] + ansiReset
	})
	if len(line) >= len("12:34:56") {
		line = ansiDim + line[:len("12:34:56")] + ansiReset + line[len("12:34:56"):]
	}
	line = prURL.ReplaceAllStringFunc(line, func(token string) string {
		return ansiYellow + token + ansiReset
	})
	line = outcomeToken.ReplaceAllStringFunc(line, func(match string) string {
		tokenStart := strings.LastIndexByte(match, ' ') + 1
		token := match[tokenStart:]
		color := ansiBoldRed
		switch token {
		case "done":
			color = ansiGreen
		case "blocked":
			color = ansiBlocked
		}
		return match[:tokenStart] + color + token + ansiReset
	})
	return line
}

func (c colorizer) nameColor() string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(c.name))
	colors := []string{ansiCyan, ansiMagenta, ansiRed, ansiBlue}
	return colors[h.Sum32()%uint32(len(colors))]
}
