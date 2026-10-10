// SPDX-License-Identifier: GPL-2.0-only
package boxtransport

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"sync"
	"time"
)

type identityKey struct{}

func State(ctx context.Context) *tls.ConnectionState {
	v, _ := ctx.Value(identityKey{}).(*tls.ConnectionState)
	return v
}

type dispatched struct {
	address     net.Addr
	connections chan net.Conn
	done        chan struct{}
	once        sync.Once
}

func (l *dispatched) Accept() (net.Conn, error) {
	select {
	case c := <-l.connections:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}
func (l *dispatched) Close() error   { l.once.Do(func() { close(l.done) }); return nil }
func (l *dispatched) Addr() net.Addr { return l.address }

type tracked struct {
	net.Conn
	release func()
	once    sync.Once
}

func (c *tracked) Close() error { err := c.Conn.Close(); c.once.Do(c.release); return err }

type identified struct {
	*BufferedConn
	state *tls.ConnectionState
}

// Serve dispatches HTTP and bounded RTNL frames on one TLS listener. Cancellation
// closes all owned sockets, including idle binary clients and in-flight handshakes.
func Serve(ctx context.Context, raw net.Listener, config *tls.Config, handler http.Handler, onFrame func(context.Context, []byte) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	listener := &dispatched{address: raw.Addr(), connections: make(chan net.Conn), done: make(chan struct{})}
	server := &http.Server{BaseContext: func(net.Listener) context.Context { return ctx }, Handler: handler, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 0, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10,
		ConnContext: func(ctx context.Context, c net.Conn) context.Context {
			if identified, ok := c.(*identified); ok {
				return context.WithValue(ctx, identityKey{}, identified.state)
			}
			return ctx
		}}
	var mu sync.Mutex
	active := map[net.Conn]struct{}{}
	slots := make(chan struct{}, 32)
	var workers sync.WaitGroup
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener); cancel() }()
	go func() {
		<-ctx.Done()
		_ = raw.Close()
		_ = server.Close()
		mu.Lock()
		connections := make([]net.Conn, 0, len(active))
		for conn := range active {
			connections = append(connections, conn)
		}
		mu.Unlock()
		for _, conn := range connections {
			_ = conn.Close()
		}
	}()
	// Handshake and binary workers must exit before the listener owner returns.
	defer func() { cancel(); _ = raw.Close(); _ = server.Close(); workers.Wait() }()
	for {
		conn, err := raw.Accept()
		if err != nil {
			if ctx.Err() != nil {
				select {
				case serveErr := <-serveDone:
					if serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) && !errors.Is(serveErr, net.ErrClosed) {
						return serveErr
					}
				default:
				}
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
		mu.Lock()
		active[conn] = struct{}{}
		mu.Unlock()
		owned := &tracked{Conn: conn, release: func() { <-slots; mu.Lock(); delete(active, conn); mu.Unlock() }}
		workers.Add(1)
		go func() {
			defer workers.Done()
			socket := tls.Server(owned, config)
			transferred := false
			defer func() {
				if !transferred {
					_ = socket.Close()
				}
			}()
			_ = socket.SetDeadline(time.Now().Add(5 * time.Second))
			if err := socket.HandshakeContext(ctx); err != nil {
				return
			}
			state := socket.ConnectionState()
			reader := bufio.NewReaderSize(socket, 4096)
			protocol, err := Detect(reader)
			if err != nil {
				return
			}
			_ = socket.SetDeadline(time.Time{})
			if protocol == RTNL {
				frameCtx := context.WithValue(ctx, identityKey{}, &state)
				for {
					_ = socket.SetReadDeadline(time.Now().Add(time.Minute))
					frame, err := ReadFrame(reader)
					if errors.Is(err, io.EOF) {
						return
					}
					if err != nil {
						return
					}
					if onFrame != nil {
						if err := onFrame(frameCtx, frame); err != nil {
							return
						}
					}
				}
			}
			buffered := &identified{BufferedConn: &BufferedConn{Conn: socket, Reader: reader}, state: &state}
			select {
			case listener.connections <- buffered:
				transferred = true
			case <-ctx.Done():
			}
		}()
	}
}
