// SPDX-License-Identifier: GPL-2.0-only
// Send a synthetic TLS 1.2 ClientHello and inspect the first server flight.
// This tests negotiation only, not a completed authenticated TLS handshake.
package main

import (
	"crypto/rand"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"time"
)

func u16(n int) []byte { return []byte{byte(n >> 8), byte(n)} }
func main() {
	addr := flag.String("address", "127.0.0.1:19444", "localhost TLS server")
	alg := flag.String("signature", "sha256", "sha256, sha1 or absent")
	flag.Parse()
	if err := probe(*addr, *alg); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func probe(addr, alg string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return fmt.Errorf("address must be numeric loopback")
	}
	extensions := []byte{0, 10, 0, 4, 0, 2, 0, 23, 0, 11, 0, 2, 1, 0, 0, 23, 0, 0} // P-256, uncompressed points, EMS
	switch alg {
	case "sha1":
		extensions = append(extensions, 0, 13, 0, 4, 0, 2, 2, 1)
	case "sha256":
		extensions = append(extensions, 0, 13, 0, 4, 0, 2, 4, 1)
	case "absent":
	default:
		return fmt.Errorf("unknown signature mode")
	}
	hello := []byte{3, 3}
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return err
	}
	hello = append(hello, random...)
	hello = append(hello, 0, 0, 4, 0xc0, 0x13, 0, 0xff, 1, 0) // no session; ECDHE_RSA_AES128_CBC_SHA + secure renegotiation SCSV; no compression
	hello = append(hello, u16(len(extensions))...)
	hello = append(hello, extensions...)
	handshake := append([]byte{1, byte(len(hello) >> 16), byte(len(hello) >> 8), byte(len(hello))}, hello...)
	record := append([]byte{22, 3, 1, byte(len(handshake) >> 8), byte(len(handshake))}, handshake...)
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write(record); err != nil {
		return err
	}
	var buffered []byte
	for records := 0; records < 16; records++ {
		var head [5]byte
		if _, err := io.ReadFull(conn, head[:]); err != nil {
			return err
		}
		n := binary.BigEndian.Uint16(head[3:])
		if n > 18432 {
			return fmt.Errorf("oversized TLS record")
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(conn, payload); err != nil {
			return err
		}
		if head[0] == 21 {
			if len(payload) != 2 {
				return fmt.Errorf("malformed TLS alert")
			}
			return fmt.Errorf("TLS alert level=%d description=%d", payload[0], payload[1])
		}
		if head[0] != 22 {
			return fmt.Errorf("unexpected record type %d", head[0])
		}
		buffered = append(buffered, payload...)
		for len(buffered) >= 4 {
			size := int(buffered[1])<<16 | int(buffered[2])<<8 | int(buffered[3])
			if size > 1<<20 {
				return fmt.Errorf("oversized handshake")
			}
			if len(buffered) < 4+size {
				break
			}
			kind := buffered[0]
			message := buffered[4 : 4+size]
			fmt.Printf("handshake type=%d bytes=%d\n", kind, size)
			if kind == 12 && len(message) >= 4 {
				pointLen := int(message[3])
				offset := 4 + pointLen
				if len(message) >= offset+2 {
					fmt.Printf("server signature algorithm=0x%02x%02x\n", message[offset], message[offset+1])
				}
			}
			buffered = buffered[4+size:]
			if kind == 14 {
				fmt.Println("ACCEPTED ClientHello (server flight completed; handshake not completed)")
				return nil
			}
		}
	}
	return fmt.Errorf("server flight record limit")
}
