// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"context"
	"errors"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Main handles signals for either executable. Driver I/O and stdin can block in
// OS calls that do not honor context cancellation, so shutdown has a fixed bound.
func Main(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	result := make(chan error, 1)
	go func() { result <- Run(ctx, args, stdout, stderr) }()
	terminal := newTerminal(stderr, colorMode(args), false)
	select {
	case err := <-result:
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			terminal.notice("Interrupted")
			return 130
		}
		if err != nil {
			terminal.failure(err.Error())
			return 1
		}
		return 0
	case <-ctx.Done():
		// Restore the default handler immediately: a second Ctrl+C always exits.
		stop()
		terminal.notice("Interrupted; cancelling the current operation")
		timer := time.NewTimer(time.Second)
		defer timer.Stop()
		select {
		case <-result:
		case <-timer.C:
		}
		return 130
	}
}
