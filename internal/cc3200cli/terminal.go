// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/term"
)

type terminal struct {
	out          io.Writer
	color, quiet bool
	started      time.Time
	mu           sync.Mutex
}

func newTerminal(out io.Writer, mode string, quiet bool) *terminal {
	return &terminal{out: out, color: colorEnabled(out, mode), quiet: quiet, started: time.Now()}
}
func colorMode(args []string) string {
	mode := "auto"
	for i, arg := range args {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "--color=") || strings.HasPrefix(arg, "-color=") {
			_, mode, _ = strings.Cut(arg, "=")
		}
		if (arg == "--color" || arg == "-color") && i+1 < len(args) {
			mode = args[i+1]
		}
	}
	return mode
}
func colorEnabled(out io.Writer, mode string) bool {
	if mode == "never" {
		return false
	}
	if mode == "always" {
		return true
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	return ok && term.IsTerminal(int(file.Fd())) && enableANSI(file)
}
func (t *terminal) paint(code, text string) string {
	if t.color {
		return "\x1b[" + code + "m" + text + "\x1b[0m"
	}
	return text
}
func (t *terminal) message(level, code, text string, always bool) {
	if t.quiet && !always {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	fmt.Fprintf(t.out, "%s %s %s\n", t.paint("2", fmt.Sprintf("[%6.1fs]", time.Since(t.started).Seconds())), t.paint(code, level), text)
}
func (t *terminal) status(text string) {
	if strings.Contains(text, "failed") {
		t.message("WARN", "33", text, false)
		return
	}
	if strings.Contains(text, "verified") || strings.Contains(text, "synchronized") || strings.Contains(text, "ready") {
		t.success(text)
		return
	}
	t.message("INFO", "36", text, false)
}
func (t *terminal) success(text string) { t.message("OK", "32", text, false) }
func (t *terminal) notice(text string)  { t.message("NOTE", "33", text, true) }
func (t *terminal) debug(text string)   { t.message("DEBUG", "2", text, false) }
func (t *terminal) failure(text string) { t.message("ERROR", "31", text, true) }

var helpTokens = regexp.MustCompile(`--[a-z][a-z0-9_-]*|<[a-z_]+>`)

type helpOutput struct{ terminal *terminal }

func (h helpOutput) Write(data []byte) (int, error) {
	text := string(data)
	if h.terminal.color {
		text = helpTokens.ReplaceAllStringFunc(text, func(s string) string { return h.terminal.paint("36", s) })
		lines := strings.Split(text, "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "Usage:") || line == "Flags:" || line == "Commands:" {
				lines[i] = h.terminal.paint("1", line)
			}
		}
		text = strings.Join(lines, "\n")
	}
	_, err := io.WriteString(h.terminal.out, text)
	if err != nil {
		return 0, err
	}
	return len(data), nil
}
func (t *terminal) helpWriter() io.Writer { return helpOutput{t} }
