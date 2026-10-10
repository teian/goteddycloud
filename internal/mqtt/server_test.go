// SPDX-License-Identifier: GPL-2.0-only
package mqtt

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
)

func TestEmbeddedTLSSettingsFreshnessAndShutdown(t *testing.T) {
	settings, _ := config.Open(t.TempDir())
	defer settings.Close()
	pair, err := certificates.Ensure(settings.Snapshot(""), "core.server_cert_tb2")
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{TLS: &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequestClientCert}, Known: func(id string) bool { return id == "001122334455" }, Fresh: func(string) []string { return []string{"0000000100000001"} }}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- server.Serve(ctx, listener) }()
	ca, err := x509.ParseCertificate(pair.Certificate[1])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	conn, err := tls.Dial("tcp", listener.Addr().String(), &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "localhost"})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(3 * time.Second))
	reader := bufio.NewReader(conn)
	data := append(str("MQTT"), 4, 2, 0, 30)
	data = append(data, str("synthetic-device")...)
	conn.Write(frame(0x10, data))
	p, err := readPacket(reader)
	if err != nil || p.header != 0x20 {
		t.Fatal(p, err)
	}
	data = []byte{0, 1}
	data = append(data, str("toniebox/001122334455/fresh-tonies")...)
	data = append(data, 0)
	conn.Write(frame(0x82, data))
	p, err = readPacket(reader)
	if err != nil || p.header != 0x90 {
		t.Fatal(p, err)
	}
	p, err = readPacket(reader)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err := parsePublish(p)
	if err != nil || message.Payload != `{"tonie":"0000000100000001"}` {
		t.Fatal(message, err)
	}
	conn.Write(publishPacket(Message{"toniebox/001122334455/settings/request", "{}", 0, false}, 0))
	p, err = readPacket(reader)
	if err != nil {
		t.Fatal(err)
	}
	message, _, err = parsePublish(p)
	var desired map[string]any
	if err != nil || message.Topic != "toniebox/001122334455/settings/desired" || json.Unmarshal([]byte(message.Payload), &desired) != nil || desired["max_volume"] != float64(100) {
		t.Fatal(message, err)
	}
	conn.Write(frame(0xc0, nil))
	p, err = readPacket(reader)
	if err != nil || p.header != 0xd0 {
		t.Fatal(p, err)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("server failed to reap connections")
	}
}
func TestClientTLSRequiresTrustUnlessExplicitlyInsecure(t *testing.T) {
	endpoint := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer endpoint.Close()
	settings, _ := config.Open(t.TempDir())
	defer settings.Close()
	snap := settings.Snapshot("")
	host, port, _ := net.SplitHostPort(endpoint.Listener.Addr().String())
	snap.Values["mqtt.hostname"] = host
	snap.Values["mqtt.port"] = port
	snap.Values["mqtt.tls_enabled"] = "true"
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if conn, err := dial(ctx, snap); err == nil {
		conn.Close()
		t.Fatal("untrusted broker accepted")
	}
	path := filepath.Join(snap.Base, "broker-ca.pem")
	os.WriteFile(path, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: endpoint.Certificate().Raw}), 0600)
	snap.Values["mqtt.tls_ca_file"] = path
	conn, err := dial(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
	snap.Values["mqtt.tls_ca_file"] = ""
	snap.Values["mqtt.tls_insecure"] = "true"
	conn, err = dial(ctx, snap)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}
