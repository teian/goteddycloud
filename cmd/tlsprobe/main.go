// SPDX-License-Identifier: GPL-2.0-only
// Diagnostic prototype: performs TLS and RTNL transport checks, no box commands.
package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"goteddycloud/internal/boxtransport"
	"goteddycloud/internal/config"
	"goteddycloud/internal/devicehttp"
	"goteddycloud/internal/identity"
)

// The HTTP server accepts dispatched HTTP sockets from the same TLS acceptor.
type connListener struct {
	address     net.Addr
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *connListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *connListener) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *connListener) Addr() net.Addr { return l.address }

// ownedConn releases the accept slot when the socket actually closes, including
// connections handed off to net/http.
type ownedConn struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *ownedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

func main() {
	root := flag.String("fixtures", ".cache/oracle-runtime", "synthetic fixture deployment")
	addr := flag.String("listen", "127.0.0.1:19444", "localhost diagnostic address")
	flag.Parse()
	if err := run(*root, *addr); err != nil {
		slog.Error("TLS prototype", "error", err)
		os.Exit(1)
	}
}
func run(root, addr string) error {
	if _, err := os.Stat(filepath.Join(root, ".synthetic-fixture")); err != nil {
		return err
	}
	load := func(dir string) (tls.Certificate, error) {
		return tls.LoadX509KeyPair(filepath.Join(root, dir, "teddy-cert.pem"), filepath.Join(root, dir, "teddy-key.pem"))
	}
	tb1, err := load("certs/server")
	if err != nil {
		return err
	}
	tb2, err := load("certs/server_tb2")
	if err != nil {
		return err
	}
	settings, err := config.OpenReadOnly(root)
	if err != nil {
		return err
	}
	defer settings.Close()
	policy := &identity.Policy{Store: settings}
	config, err := boxtransport.TLSConfig(tb1, tb2, policy.VerifyCertificate)
	if err != nil {
		return err
	}
	raw, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	defer raw.Close()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	httpListener := &connListener{address: raw.Addr(), connections: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{Handler: devicehttp.Handler(time.Now), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 32 << 10}
	go func() { _ = server.Serve(httpListener) }()
	go func() { <-ctx.Done(); _ = raw.Close(); _ = server.Close() }()
	defer server.Close()
	// Diagnostic prototype keeps at most 16 accepted sockets active.
	slots := make(chan struct{}, 16)
	var workers sync.WaitGroup
	defer workers.Wait()
	fmt.Println("TLS/RTNL prototype listening:", raw.Addr())
	for {
		conn, err := raw.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		select {
		case slots <- struct{}{}:
		default:
			_ = conn.Close()
			continue
		}
		workers.Add(1)
		go func() {
			defer workers.Done()
			socket := tls.Server(&ownedConn{Conn: conn, release: func() { <-slots }}, config)
			transferred := false
			defer func() {
				if !transferred {
					_ = socket.Close()
				}
			}()
			_ = socket.SetDeadline(time.Now().Add(5 * time.Second))
			if err := socket.HandshakeContext(ctx); err != nil {
				slog.Info("handshake rejected", "error", err)
				return
			}
			state := socket.ConnectionState()
			slog.Info("handshake accepted", "sni", state.ServerName, "suite", tls.CipherSuiteName(state.CipherSuite), "resumed", state.DidResume, "client_certificate", len(state.PeerCertificates) > 0)
			reader := bufio.NewReaderSize(socket, 4096)
			protocol, err := boxtransport.Detect(reader)
			if err != nil {
				return
			}
			_ = socket.SetDeadline(time.Time{})
			if protocol == boxtransport.RTNL {
				for {
					_ = socket.SetReadDeadline(time.Now().Add(5 * time.Second))
					frame, err := boxtransport.ReadFrame(reader)
					if err != nil {
						return
					}
					slog.Info("RTNL frame accepted", "bytes", len(frame))
				}
			}
			buffered := &boxtransport.BufferedConn{Conn: socket, Reader: reader}
			select {
			case httpListener.connections <- buffered:
				transferred = true
			case <-ctx.Done():
			}
		}()
	}
}
