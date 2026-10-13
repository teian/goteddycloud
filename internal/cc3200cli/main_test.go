// SPDX-License-Identifier: GPL-2.0-only
//go:build !windows

package cc3200cli

import (
	"bufio"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestSignalHelperProcess(t *testing.T) {
	if os.Getenv("GOTEDDY_CC3200_SIGNAL_HELPER") != "1" {
		return
	}
	os.Exit(Main([]string{"--color=never", "write_file", "-", "/cert/ca.der"}, os.Stdout, os.Stderr))
}
func TestCtrlCInterruptsBlockedStdin(t *testing.T) {
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, exe, "-test.run=^TestSignalHelperProcess$")
	command.Env = append(os.Environ(), "GOTEDDY_CC3200_SIGNAL_HELPER=1")
	input, e := command.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	defer input.Close()
	stderr, e := command.StderrPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = command.Start(); e != nil {
		t.Fatal(e)
	}
	scanner := bufio.NewScanner(stderr)
	for scanner.Scan() {
		if strings.Contains(scanner.Text(), "Reading input from stdin") {
			break
		}
	}
	if ctx.Err() != nil {
		t.Fatal("helper did not reach blocked input", ctx.Err())
	}
	start := time.Now()
	if e = command.Process.Signal(os.Interrupt); e != nil {
		t.Fatal(e)
	}
	var messages strings.Builder
	for scanner.Scan() {
		messages.WriteString(scanner.Text())
		messages.WriteByte('\n')
	}
	e = command.Wait()
	var exit *exec.ExitError
	if !errors.As(e, &exit) || exit.ExitCode() != 130 {
		t.Fatal("expected interrupt exit 130", e, messages.String())
	}
	if time.Since(start) > 2*time.Second || !strings.Contains(messages.String(), "Interrupted") {
		t.Fatal("Ctrl+C did not exit promptly", time.Since(start), messages.String())
	}
}
