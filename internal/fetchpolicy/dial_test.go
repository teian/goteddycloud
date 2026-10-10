// SPDX-License-Identifier: GPL-2.0-only
package fetchpolicy

import (
	"context"
	"net"
	"net/netip"
	"testing"
)

func TestPublicAddressPolicy(t *testing.T) {
	for _, s := range []string{"0.1.2.3", "10.0.0.1", "100.64.0.1", "127.0.0.1", "169.254.169.254", "172.16.0.1", "192.168.1.1", "192.0.2.1", "198.18.0.1", "198.51.100.1", "203.0.113.1", "224.1.1.1", "255.255.255.255", "::1", "::ffff:127.0.0.1", "fc00::1", "fe80::1", "2001:db8::1"} {
		if Public(netip.MustParseAddr(s)) {
			t.Fatal("allowed", s)
		}
	}
	for _, s := range []string{"1.1.1.1", "8.8.8.8", "2606:4700:4700::1111"} {
		if !Public(netip.MustParseAddr(s)) {
			t.Fatal("blocked", s)
		}
	}
}
func TestPrivateConnectionOptIn(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if c, err := Dial(nil)(context.Background(), "tcp", l.Addr().String()); err == nil {
		c.Close()
		t.Fatal("private address accepted")
	}
	c, err := Dial(func() bool { return true })(context.Background(), "tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}
