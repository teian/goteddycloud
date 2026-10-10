// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"goteddycloud/internal/fixtures"
)

func main() {
	root := flag.String("fixtures", ".cache/oracle-runtime", "synthetic fixture directory")
	address := flag.String("listen", "127.0.0.1:18445", "localhost mock TLS address")
	flag.Parse()
	if _, err := os.Stat(filepath.Join(*root, fixtures.Marker)); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	host, _, err := net.SplitHostPort(*address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		fmt.Fprintln(os.Stderr, "mock must bind a numeric loopback address")
		os.Exit(2)
	}
	server := http.Server{Addr: *address, Handler: &fixtures.MockCloud{}, ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 32 << 10}
	if err := server.ListenAndServeTLS(filepath.Join(*root, "certs/server/teddy-cert.pem"), filepath.Join(*root, "certs/server/teddy-key.pem")); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
