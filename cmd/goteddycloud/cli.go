// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"flag"
)

type cliOptions struct {
	base              string
	httpAddress       string
	httpSecureAddress string
	boxAddress        string
	legacyListen      string
	hashFile          string
	encodeFile        string
	skipSeconds       uint64
	showVersion       bool
	generate          bool
	validate          bool
	updates           assignments
	extra             operations
	sources           []string
}

func parseCLI(args []string) (cliOptions, error) {
	flags := flag.NewFlagSet("goteddycloud", flag.ContinueOnError)
	var options cliOptions
	options.extra.register(flags)
	flags.StringVar(&options.base, "base_path", ".", "deployment base path")
	flags.StringVar(&options.base, "b", ".", "deployment base path")
	flags.StringVar(&options.httpAddress, "http", "", "Web UI / management API HTTP address (off disables)")
	flags.StringVar(&options.httpSecureAddress, "httpsecure", "", "Web UI / management API HTTPS address (off disables; optional behind a reverse proxy)")
	flags.StringVar(&options.boxAddress, "box", "", "Toniebox API HTTPS address (off disables)")
	flags.StringVar(&options.legacyListen, "listen", "", "HTTP-only address, retained for compatibility probes")
	flags.BoolVar(&options.validate, "validate", false, "read-only deployment validation; no listeners or generation")
	flags.BoolVar(&options.showVersion, "version", false, "print build version")
	flags.BoolVar(&options.generate, "generate-server-certs", false, "generate missing server certificates and exit")
	flags.BoolVar(&options.generate, "g", false, "generate missing server certificates and exit")
	flags.Var(&options.updates, "config-set", "persist key=value (repeatable)")
	flags.Var(&options.updates, "C", "persist key=value (repeatable)")
	flags.StringVar(&options.encodeFile, "encode", "", "encode 1–99 source files to a TAF and exit")
	flags.StringVar(&options.encodeFile, "e", "", "encode source files to a TAF and exit")
	flags.Uint64Var(&options.skipSeconds, "skip-seconds", 0, "skip seconds at the start of every audio source")
	flags.Uint64Var(&options.skipSeconds, "S", 0, "skip seconds at the start of every audio source")
	flags.StringVar(&options.hashFile, "hash", "", "print file SHA1 and exit")
	flags.StringVar(&options.hashFile, "H", "", "print file SHA1 and exit")
	if err := flags.Parse(interspersed(flags, args)); err != nil {
		return options, err
	}
	if options.extra.help {
		flags.PrintDefaults()
		return options, flag.ErrHelp
	}
	options.sources = flags.Args()
	return options, nil
}
