// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"fmt"
	"goteddycloud/internal/cc3200cli"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := cc3200cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintln(os.Stderr, "cc3200tool:", err)
		os.Exit(1)
	}
}
