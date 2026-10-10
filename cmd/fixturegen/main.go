// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"goteddycloud/internal/fixtures"
)

func main() {
	output := flag.String("output", ".cache/oracle-runtime", "new disposable fixture directory (must not exist)")
	flag.Parse()
	if err := os.MkdirAll(filepath.Dir(*output), 0700); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := fixtures.Generate(*output); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Synthetic fixture generated:", *output)
}
