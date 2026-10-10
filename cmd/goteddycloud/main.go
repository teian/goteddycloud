// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"goteddycloud/internal/audio"
	"goteddycloud/internal/cc3200cli"
	"goteddycloud/internal/config"
	serverruntime "goteddycloud/internal/runtime"
	"goteddycloud/internal/storage"
)

var version = "development"

type assignments []string

func (a *assignments) String() string { return strings.Join(*a, ",") }
func (a *assignments) Set(value string) error {
	values := strings.Split(value, ",")
	for _, item := range values {
		if !strings.Contains(item, "=") {
			return fmt.Errorf("expected key=value")
		}
	}
	*a = append(*a, values...)
	return nil
}

var errRestartRequested = errors.New("restart requested")

func main() {
	for {
		err := run()
		if errors.Is(err, errRestartRequested) {
			continue
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "goteddycloud:", err)
			os.Exit(1)
		}
		return
	}
}
func run() error {
	if len(os.Args) > 1 && os.Args[1] == "cc3200" {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return cc3200cli.Run(ctx, os.Args[2:], os.Stdout, os.Stderr)
	}
	if len(os.Args) == 2 && os.Args[1] == "-box-worker" {
		return serverruntime.Worker(context.Background(), os.Stdin, os.Stdout)
	}
	if err := serverruntime.StrictParentTLS(); err != nil {
		return err
	}
	options, err := parseCLI(os.Args[1:])
	if errors.Is(err, flag.ErrHelp) {
		return nil
	}
	if err != nil {
		return err
	}
	if err := options.extra.validate(); err != nil {
		return err
	}
	if options.extra.active() && (options.encodeFile != "" || options.validate || options.generate || options.showVersion) {
		return fmt.Errorf("choose one operation")
	}
	if options.hashFile != "" && options.extra.active() && options.extra.urlTest == "" && options.extra.cloudTest == "" {
		return fmt.Errorf("hash cannot be combined with this operation")
	}
	if options.encodeFile == "" && len(options.sources) != 0 {
		return fmt.Errorf("unexpected positional argument")
	}
	if options.encodeFile != "" {
		if options.validate || options.generate || len(options.updates) > 0 || options.hashFile != "" || options.showVersion {
			return fmt.Errorf("encoding cannot be combined with another operation")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return audio.ConvertWithOptions(ctx, options.encodeFile, uint32(time.Now().Unix()-0x50000000), options.sources, audio.Options{SkipSeconds: options.skipSeconds})
	}
	if options.extra.encodeTest != "" {
		if len(options.updates) > 0 || options.hashFile != "" || options.skipSeconds != 0 {
			return fmt.Errorf("encode_test cannot be combined with other operations")
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return audio.EncodeTest(ctx, options.extra.encodeTest)
	}
	if options.skipSeconds != 0 {
		return fmt.Errorf("skip-seconds requires encode")
	}
	if options.showVersion {
		fmt.Println("goteddycloud", version)
		return nil
	}
	if options.hashFile != "" && options.extra.urlTest == "" && options.extra.cloudTest == "" {
		return printFileHash(options.hashFile)
	}
	backend, err := storage.FromEnvironment(context.Background(), options.base)
	if err != nil {
		return err
	}
	var settings *config.Store
	if options.validate {
		settings, err = config.OpenWithStorage(options.base, backend, true)
	} else {
		settings, err = config.OpenWithStorage(options.base, backend, false)
	}
	if err != nil {
		return err
	}
	defer settings.Close()
	if options.validate {
		if len(options.updates) > 0 || options.generate {
			return fmt.Errorf("validation cannot be combined with mutation flags")
		}
		return validateDeployment(settings)
	}
	for _, assignment := range options.updates {
		key, value, _ := strings.Cut(assignment, "=")
		if err := settings.Set("", key, value); err != nil {
			return err
		}
	}
	if len(options.updates) > 0 && !options.generate && !options.extra.active() {
		return nil
	}
	if options.extra.active() && !options.extra.dockerTest {
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		return options.extra.execute(ctx, settings, options.hashFile)
	}
	return startServer(settings, serverOptions{
		httpAddress: options.httpAddress, httpSecureAddress: options.httpSecureAddress, boxAddress: options.boxAddress,
		legacyListen: options.legacyListen, generateCertificates: options.generate, dockerTest: options.extra.dockerTest,
	})
}
