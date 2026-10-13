// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"goteddycloud/internal/cc3200cli"
	"os"
)

func main() { os.Exit(cc3200cli.Main(os.Args[1:], os.Stdout, os.Stderr)) }
