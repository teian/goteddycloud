// SPDX-License-Identifier: GPL-2.0-only
package main

import (
	"flag"
	"strings"
)

// GNU getopt permits options after positional source files. Normalize using the
// registered flag definitions, preserving option values and positional order.
func interspersed(flags *flag.FlagSet, args []string) []string {
	options, positionals := []string{}, []string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if arg == "-" || !strings.HasPrefix(arg, "-") {
			positionals = append(positionals, arg)
			continue
		}
		name := strings.TrimLeft(arg, "-")
		name, _, hasValue := strings.Cut(name, "=")
		definition := flags.Lookup(name)
		if definition == nil && !strings.HasPrefix(arg, "--") && len(arg) > 2 {
			// getopt short options may be grouped; the first value option
			// consumes the complete remaining suffix, including '='.
			rest := arg[1:]
			group := []string{}
			valid, needsNext := true, false
			for len(rest) > 0 {
				short := flags.Lookup(rest[:1])
				if short == nil {
					valid = false
					break
				}
				group = append(group, "-"+rest[:1])
				rest = rest[1:]
				if boolean, ok := short.Value.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
					continue
				}
				if rest != "" {
					group = append(group, rest)
				} else {
					needsNext = true
				}
				break
			}
			if valid {
				options = append(options, group...)
				if needsNext && i+1 < len(args) {
					i++
					options = append(options, args[i])
				}
				continue
			}
		}
		options = append(options, arg)
		if definition != nil && !hasValue {
			if b, ok := definition.Value.(interface{ IsBoolFlag() bool }); ok && b.IsBoolFlag() {
				continue
			}
			if i+1 < len(args) {
				i++
				options = append(options, args[i])
			}
		}
	}
	return append(append(options, "--"), positionals...)
}
