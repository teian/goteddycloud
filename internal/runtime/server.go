// SPDX-License-Identifier: GPL-2.0-only
package runtime

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"sync"
	"time"
)

type Options struct {
	KeyLogBase, KeyLogPath string
	KeyLogWriter           io.Writer
	HTTP, Web, Box         string // Empty disables the listener; :0 requests an ephemeral port.
	TB1, TB2               tls.Certificate
	WebTLS                 *tls.Config
	HTTPChallenge          func(http.Handler) http.Handler
	Management, Device     http.Handler
	WorkerCommand          []string
	Ready                  func(map[string]string)
	Stderr                 io.Writer
}
type child struct {
	process *exec.Cmd
	input   io.WriteCloser
	done    chan error
	once    sync.Once
}

func (c *child) stop() {
	c.once.Do(func() {
		_ = c.input.Close()
		select {
		case <-c.done:
		case <-time.After(3 * time.Second):
			_ = c.process.Process.Kill()
			<-c.done
		}
	})
}
func startChild(ctx context.Context, o Options, target, token string) (*child, string, error) {
	command := o.WorkerCommand
	if len(command) == 0 {
		path, err := os.Executable()
		if err != nil {
			return nil, "", err
		}
		command = []string{path, "-box-worker"}
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Env = LegacyEnvironment(os.Environ(), true)
	cmd.Stderr = o.Stderr
	input, err := cmd.StdinPipe()
	if err != nil {
		return nil, "", err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		input.Close()
		return nil, "", err
	}
	c := &child{process: cmd, input: input, done: make(chan error, 1)}
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, "", err
	}
	go func() { c.done <- cmd.Wait() }()
	fail := func(err error) (*child, string, error) { c.stop(); return nil, "", err }
	tb1Certificate, tb1Key, err := certificatePEM(o.TB1)
	if err != nil {
		return fail(err)
	}
	tb2Certificate, tb2Key, err := certificatePEM(o.TB2)
	if err != nil {
		return fail(err)
	}
	if err := json.NewEncoder(input).Encode(workerInput{
		Address:     o.Box,
		Target:      target,
		Token:       token,
		KeyLogBase:  o.KeyLogBase,
		KeyLogPath:  o.KeyLogPath,
		KeyLogRelay: o.KeyLogWriter != nil,
		TB1Cert:     tb1Certificate,
		TB1Key:      tb1Key,
		TB2Cert:     tb2Certificate,
		TB2Key:      tb2Key,
	}); err != nil {
		return fail(err)
	}
	ready := make(chan struct {
		address string
		err     error
	}, 1)
	go func() {
		var state map[string]string
		err := json.NewDecoder(bufio.NewReader(io.LimitReader(output, 4096))).Decode(&state)
		ready <- struct {
			address string
			err     error
		}{state["box"], err}
	}()
	select {
	case r := <-ready:
		if r.err != nil || r.address == "" {
			return fail(errors.New("box worker failed before readiness"))
		}
		return c, r.address, nil
	case <-ctx.Done():
		return fail(ctx.Err())
	case <-time.After(10 * time.Second):
		return fail(errors.New("box worker readiness timeout"))
	}
}

// Run binds all parent listeners before starting the child. Any startup failure
// closes all acquired sockets, and every exit reaps the box child process.
func Run(ctx context.Context, o Options) (runErr error) {
	if o.Management == nil || o.Device == nil {
		return errors.New("both request handlers are required")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var listeners []net.Listener
	var servers []*http.Server
	var workers sync.WaitGroup
	defer func() {
		cancel()
		shutdown, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer stop()
		runErr = errors.Join(runErr, shutdownServers(shutdown, servers))
		for _, l := range listeners {
			_ = l.Close()
		}
		workers.Wait()
	}()
	bind := func(address string) (net.Listener, error) {
		l, err := net.Listen("tcp", address)
		if err == nil {
			listeners = append(listeners, l)
		}
		return l, err
	}
	addresses := map[string]string{}
	type endpoint struct {
		l       net.Listener
		handler http.Handler
	}
	var endpoints []endpoint
	for _, v := range []struct{ name, address string }{{"http", o.HTTP}, {"web", o.Web}} {
		if v.address == "" {
			continue
		}
		l, err := bind(v.address)
		if err != nil {
			return fmt.Errorf("%s bind: %w", v.name, err)
		}
		addresses[v.name] = l.Addr().String()
		handler := o.Management
		if v.name == "web" {
			webTLS := o.WebTLS
			if webTLS == nil {
				if len(o.TB1.Certificate) == 0 {
					return errors.New("web certificate is required")
				}
				webTLS = &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{o.TB1}}
			}
			webTLS = webTLS.Clone()
			webTLS.KeyLogWriter = o.KeyLogWriter
			l = tls.NewListener(l, webTLS)
		} else if o.HTTPChallenge != nil {
			handler = o.HTTPChallenge(handler)
		}
		endpoints = append(endpoints, endpoint{l, handler})
	}
	var private net.Listener
	var token string
	if o.Box != "" {
		var err error
		private, err = bind("127.0.0.1:0")
		if err != nil {
			return err
		}
		secret := make([]byte, 32)
		if _, err := rand.Read(secret); err != nil {
			return err
		}
		token = hex.EncodeToString(secret)
		endpoints = append(endpoints, endpoint{private, privateHandler(token, diagnosticHandler(o.KeyLogWriter, o.Device))})
	}
	failures := make(chan error, len(endpoints))
	for _, e := range endpoints {
		server := &http.Server{Handler: e.handler, BaseContext: func(net.Listener) context.Context { return ctx }, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 32 << 10}
		servers = append(servers, server)
		workers.Add(1)
		go func() {
			defer workers.Done()
			err := server.Serve(e.l)
			if !errors.Is(err, http.ErrServerClosed) {
				failures <- err
			}
		}()
	}
	var c *child
	if private != nil {
		var address string
		var err error
		c, address, err = startChild(ctx, o, "http://"+private.Addr().String(), token)
		if err != nil {
			return err
		}
		defer c.stop()
		addresses["box"] = address
	}
	if len(addresses) == 0 {
		return errors.New("no public listener enabled")
	}
	if o.Ready != nil {
		o.Ready(addresses)
	}
	var childDone <-chan error
	if c != nil {
		childDone = c.done
	}
	select {
	case <-ctx.Done():
		return nil
	case err := <-failures:
		return err
	case err := <-childDone:
		// Put the terminal status back for the single shutdown owner to consume.
		c.done <- err
		return fmt.Errorf("box worker exited unexpectedly: %v", err)
	}
}

// shutdownServers drains all listeners concurrently under one shutdown budget.
// Context-ignoring handlers cannot be forcibly stopped by Go; after the budget
// expires, close their connections and report the failed graceful shutdown.
func shutdownServers(ctx context.Context, servers []*http.Server) error {
	results := make(chan error, len(servers))
	for _, server := range servers {
		go func() {
			err := server.Shutdown(ctx)
			if err != nil {
				err = errors.Join(err, server.Close())
			}
			results <- err
		}()
	}
	var result error
	for range servers {
		result = errors.Join(result, <-results)
	}
	return result
}

func diagnosticHandler(writer io.Writer, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/_keylog" {
			next.ServeHTTP(w, r)
			return
		}
		if writer == nil || r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		data, err := io.ReadAll(io.LimitReader(r.Body, 4097))
		if err != nil || len(data) > 4096 {
			http.Error(w, "key log bounds", http.StatusBadRequest)
			return
		}
		if _, err = writer.Write(data); err != nil {
			http.Error(w, "key log unavailable", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}
