// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"goteddycloud/internal/compat"
)

func main() {
	reference := flag.String("reference", "http://127.0.0.1:18080", "reference origin")
	candidate := flag.String("candidate", "http://127.0.0.1:18081", "candidate origin")
	manifest := flag.String("cases", "testdata/compatibility.json", "case manifest")
	referenceRoot := flag.String("reference-root", "", "disposable reference deployment for file checks")
	candidateRoot := flag.String("candidate-root", "", "disposable candidate deployment for file checks")
	flag.Parse()
	data, err := os.ReadFile(*manifest)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	var cases []compat.Case
	if err = json.Unmarshal(data, &cases); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if len(cases) == 0 {
		fmt.Fprintln(os.Stderr, "empty case manifest")
		os.Exit(2)
	}
	client := &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	failed := false
	for _, c := range cases {
		if err := compat.Run(context.Background(), client, *reference, *candidate, *referenceRoot, *candidateRoot, c); err != nil {
			fmt.Printf("FAIL %s: %v\n", c.Name, err)
			failed = true
		} else {
			fmt.Printf("PASS %s\n", c.Name)
		}
	}
	if failed {
		os.Exit(1)
	}
}
