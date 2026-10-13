// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestConsoleColorsAndBinarySeparation(t *testing.T) {
	image := imageFixture(t)
	for _, mode := range []string{"auto", "always", "never"} {
		var stdout, stderr bytes.Buffer
		if e := Run(context.Background(), []string{"--color=" + mode, "-if", image, "read_file", "/cert/ca.der", "-"}, &stdout, &stderr); e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(stdout.Bytes(), []byte{1, 2, 3, 4}) {
			t.Fatal("diagnostics contaminated binary stdout", stdout.Bytes())
		}
		if strings.Contains(stderr.String(), "\x1b[") != (mode == "always") || !strings.Contains(stderr.String(), "read_file complete") {
			t.Fatal(mode, stderr.String())
		}
	}
	var stdout, stderr bytes.Buffer
	if e := Run(context.Background(), []string{"--quiet", "-if", image, "read_file", "/cert/ca.der", "-"}, &stdout, &stderr); e != nil || stderr.Len() != 0 {
		t.Fatal(e, stderr.String())
	}
	for _, args := range [][]string{{"--color=always", "--help"}, {"--color=always", "write_file", "--help"}} {
		stderr.Reset()
		if e := Run(context.Background(), args, &stdout, &stderr); e != nil || !strings.Contains(stderr.String(), "\x1b[") {
			t.Fatal(e, stderr.String())
		}
	}
	t.Setenv("NO_COLOR", "1")
	if colorEnabled(&bytes.Buffer{}, "auto") {
		t.Fatal("color on redirected stream")
	}
	if newTerminal(&bytes.Buffer{}, "never", false).color {
		t.Fatal("color=never ignored")
	}
}
