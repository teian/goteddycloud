// SPDX-License-Identifier: GPL-2.0-only
package main

import "testing"

func TestHTTPSManagementFlag(t *testing.T) {
	for _, address := range []string{"127.0.0.1:8443", "off"} {
		options, err := parseCLI([]string{"-http", "127.0.0.1:8080", "-httpsecure", address, "-box", ":10443"})
		if err != nil {
			t.Fatal(err)
		}
		if options.httpSecureAddress != address || options.httpAddress != "127.0.0.1:8080" || options.boxAddress != ":10443" {
			t.Fatalf("listener options mixed: %+v", options)
		}
	}
}
