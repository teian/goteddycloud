// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"goteddycloud/internal/compat"
)

func main() {
	reference := flag.String("reference", "http://127.0.0.1:18080", "reference origin")
	candidate := flag.String("candidate", "http://127.0.0.1:18081", "candidate origin")
	candidateDevice := flag.String("candidate-device", "", "candidate box API origin for /v1, /v2 and /v3 requests")
	candidateCA := flag.String("candidate-ca", "", "PEM trust roots for the candidate box API")
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
	if *candidateCA != "" {
		pem, err := os.ReadFile(*candidateCA)
		roots := x509.NewCertPool()
		if err != nil || !roots.AppendCertsFromPEM(pem) {
			fmt.Fprintln(os.Stderr, "cannot load candidate CA:", err)
			os.Exit(2)
		}
		transport := http.DefaultTransport.(*http.Transport).Clone()
		transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots}
		client.Transport = transport
	}
	failed := false
	for _, c := range cases {
		origin := *candidate
		if *candidateDevice != "" && (strings.HasPrefix(c.Path, "/v1/") || strings.HasPrefix(c.Path, "/v2/") || strings.HasPrefix(c.Path, "/v3/")) {
			origin = *candidateDevice
		}
		if err := compat.Run(context.Background(), client, *reference, origin, *referenceRoot, *candidateRoot, c); err != nil {
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
