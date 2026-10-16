// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync/atomic"
	"syscall"

	"goteddycloud/internal/app"
	"goteddycloud/internal/certificates"
	"goteddycloud/internal/config"
	"goteddycloud/internal/diagnostics"
	serverruntime "goteddycloud/internal/runtime"
)

type serverOptions struct {
	httpAddress          string
	httpSecureAddress    string
	boxAddress           string
	legacyListen         string
	generateCertificates bool
	dockerTest           bool
}

func startServer(settings *config.Store, options serverOptions) error {
	configureLogging(os.Stderr, configuredLevel{settings}, version)
	var err error
	snap := settings.Snapshot("")
	address := func(override, key string) string {
		if override == "off" {
			return ""
		}
		if override != "" {
			return override
		}
		return net.JoinHostPort(snap.String("core.server.bind_ip"), strconv.FormatUint(snap.Uint(key), 10))
	}
	options.httpAddress = address(options.httpAddress, "core.server.http_port")
	options.httpSecureAddress = address(options.httpSecureAddress, "core.server.https_web_port")
	options.boxAddress = address(options.boxAddress, "core.server.https_api_port")
	if options.legacyListen != "" {
		options.httpAddress = options.legacyListen
		options.httpSecureAddress = ""
		options.boxAddress = ""
	}
	// Normal startup prepares configured roots once. Read-only validation returns
	// earlier and never reaches this creation path.
	for _, id := range append([]string{""}, settings.BoxIDs()...) {
		effective := settings.Snapshot(id)
		for _, key := range []string{"internal.datadirfull", "internal.contentdirfull", "internal.librarydirfull", "internal.wwwdirfull", "internal.firmwaredirfull", "internal.cachedirfull", "internal.pluginsdirfull"} {
			if err := settings.Storage().MkdirAll(effective.String(key), 0700); err != nil {
				return fmt.Errorf("prepare %s: %w", key, err)
			}
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var tb1, tb2 tls.Certificate
	if options.generateCertificates || options.boxAddress != "" {
		tb1, err = certificates.Ensure(snap, "core.server_cert")
		if err != nil {
			return err
		}
	}
	if options.generateCertificates || options.boxAddress != "" {
		tb2, err = certificates.Ensure(snap, "core.server_cert_tb2")
		if err != nil {
			return err
		}
	}
	if options.generateCertificates {
		fmt.Println("server certificate material ready")
		return nil
	}
	var webTLS *tls.Config
	var challenge func(http.Handler) http.Handler
	if options.httpSecureAddress != "" {
		web, err := certificates.ManagementTLS(ctx, snap)
		if err != nil {
			return err
		}
		webTLS, challenge = web.Config, web.HTTPHandler
	}
	application, err := app.New(settings)
	if err != nil {
		return err
	}
	defer application.Close()
	var restart atomic.Bool
	if err := application.StartIntegrations(ctx, version); err != nil {
		return err
	}
	application.Shutdown = func(again bool) { restart.Store(again); stop() }

	var management http.Handler = http.HandlerFunc(application.Management)
	var device http.Handler = http.HandlerFunc(application.Device)
	if snap.Bool("pcap.enabled") {
		captureFile, err := diagnostics.OpenWithStorage(snap.Base, snap.String("pcap.filename"), 64<<20, settings.Storage())
		if err != nil {
			return fmt.Errorf("packet diagnostics: %w", err)
		}
		defer captureFile.Close()
		capture, err := diagnostics.NewCapture(captureFile)
		if err != nil {
			return err
		}
		management = capture.Handler(management)
		device = capture.Handler(device)
	}
	var keylog *diagnostics.File
	if path := snap.String("core.sslkeylogfile"); path != "" {
		keylog, err = diagnostics.OpenWithStorage(snap.Base, path, 4<<20, settings.Storage())
		if err != nil {
			return fmt.Errorf("TLS diagnostics: %w", err)
		}
		defer keylog.Close()
	}
	var keylogWriter io.Writer
	if keylog != nil {
		keylogWriter = keylog
	}
	err = serverruntime.Run(ctx, serverruntime.Options{
		HTTP:                 options.httpAddress,
		Web:                  options.httpSecureAddress,
		WebTLS:               webTLS,
		HTTPChallenge:        challenge,
		Box:                  options.boxAddress,
		TB1:                  tb1,
		TB2:                  tb2,
		Management:           management,
		Device:               device,
		VerifyBoxCertificate: application.Identity.VerifyCertificate,
		KeyLogWriter:         keylogWriter,
		KeyLogBase:           snap.Base,
		KeyLogPath:           snap.String("core.sslkeylogfile"),
		Stderr:               os.Stderr,
		Ready: func(addresses map[string]string) {
			slog.Info("Server ready", "http", addresses["http"], "https", addresses["web"], "box_api", addresses["box"], "base_path", snap.Base)
			if options.dockerTest {
				stop()
			}
		},
	})
	if err == nil && restart.Load() {
		return errRestartRequested
	}
	return err
}
