// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestPythonCommandSyntax(t *testing.T) {
	cases := []struct {
		args  []string
		name  string
		check func(*testing.T, options, operation)
	}{
		{[]string{"-p", "COM12", "-g", "-rw", "0x20", "--erase_timeout", "0x78", "-v", "list_filesystem", "--json-output", "--extended", "--inactive"}, "list_filesystem", func(t *testing.T, o options, op operation) {
			if o.port != "COM12" || !o.gateway || o.resetWait != 32 || o.eraseTimeout != 120 || !o.verbose || !op.jsonOutput || !op.extended || !op.inactive {
				t.Fatal(o, op)
			}
		}},
		{[]string{"-if", "backup.bin", "-of", "updated.bin", "-d", "cc32xx", "write_file", "new.der", "/cert/ca.der", "--file-size", "0x20000", "--commit-flag", "--signature", "sig.bin", "--file-id", "0x1", "--no-verify"}, "write_file", func(t *testing.T, o options, op operation) {
			if o.image != "backup.bin" || o.output != "updated.bin" || o.device != "cc32xx" || op.allocation != 0x20000 || !op.commit || op.signature != "sig.bin" || op.id != 1 || !op.noVerify {
				t.Fatal(o, op)
			}
		}},
		{[]string{"read_file", "/cert/ca.der", "-", "--file-id", "0", "--no-verify", "--inactive"}, "read_file", func(t *testing.T, _ options, op operation) {
			if op.id != 0 || !op.noVerify || !op.inactive {
				t.Fatal(op)
			}
		}},
		{[]string{"read_flash", "dump.bin", "--offset", "0x1000", "--size", "-1", "--ignore-max-size", "True", "--no-verify", "True"}, "read_flash", func(t *testing.T, _ options, op operation) {
			if op.offset != 4096 || op.size != 0 || !op.ignoreMax || !op.noVerify {
				t.Fatal(op)
			}
		}},
		{[]string{"write_flash", "flash.bin", "--no-erase", "--no-verify"}, "write_flash", func(t *testing.T, _ options, op operation) {
			if !op.noErase || !op.noVerify {
				t.Fatal(op)
			}
		}},
		{[]string{"format_flash", "-s", "512"}, "format_flash", func(t *testing.T, _ options, op operation) {
			if op.size != 512<<10 {
				t.Fatal(op)
			}
		}},
		{[]string{"read_all_files", "extract/", "--all-by-file-id", "--by-file-id", "--inactive", "--no-verify"}, "read_all_files", func(t *testing.T, _ options, op operation) {
			if !op.byID || !op.allByID || !op.inactive || !op.noVerify {
				t.Fatal(op)
			}
		}},
		{[]string{"write_all_files", "input/", "--simulate", "--no-verify"}, "write_all_files", func(t *testing.T, _ options, op operation) {
			if !op.simulate || op.mutates() || !op.noVerify {
				t.Fatal(op)
			}
		}},
		{[]string{"--reset", "prompt", "--sop2", "~rts", "--reboot-to-app", "dll_data_test"}, "dll_data_test", func(t *testing.T, o options, _ operation) {
			if o.reset != "prompt" || o.sop2 != "~rts" || !o.reboot {
				t.Fatal(o)
			}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o, ops, handled, e := parseCLI(tc.args, &bytes.Buffer{})
			if e != nil || handled || len(ops) != 1 {
				t.Fatal(o, ops, handled, e)
			}
			if ops[0].name != tc.name {
				t.Fatal(ops)
			}
			tc.check(t, o, ops[0])
		})
	}
}
func TestCommandChainsRetainPerCommandOptions(t *testing.T) {
	args := strings.Fields("-p /dev/ttyUSB2 --reset dtr format_flash --size 1M write_file --file-size 0x20000 --signature signature.bin servicepack.bin /sys/servicepack.ucf write_file --commit-flag app.bin /sys/mcuimg.bin read_flash write_file --size 4096 list_filesystem --json-output")
	o, ops, _, e := parseCLI(args, &bytes.Buffer{})
	if e != nil {
		t.Fatal(e)
	}
	if o.port != "/dev/ttyUSB2" || len(ops) != 5 {
		t.Fatal(o, ops)
	}
	if ops[1].allocation != 0x20000 || ops[1].signature != "signature.bin" || ops[2].allocation != 0 || ops[2].signature != "" || !ops[2].commit {
		t.Fatal(ops)
	}
	if ops[3].args[0] != "write_file" || ops[3].size != 4096 || !ops[4].jsonOutput {
		t.Fatal(ops)
	}
}

func TestGatewayGPIOOverrides(t *testing.T) {
	defaults, _, _, err := parseCLI([]string{"--gateway", "version"}, &bytes.Buffer{})
	if err != nil || defaults.gatewayTX != 20 || defaults.gatewayRX != 21 {
		t.Fatal("gateway defaults changed", defaults, err)
	}
	opts, ops, _, err := parseCLI(strings.Fields("--gateway --gateway-tx 21 --gateway-rx 20 version list_filesystem"), &bytes.Buffer{})
	if err != nil || !opts.gateway || opts.gatewayTX != 21 || opts.gatewayRX != 20 || len(ops) != 2 {
		t.Fatal(opts, ops, err)
	}
	for _, pins := range []string{"-1 20", "256 20", "18 20", "20 19", "20 20", "10 20", "20 9", "8 20"} {
		values := strings.Fields(pins)
		if _, _, _, err := parseCLI([]string{"--gateway", "--gateway-tx", values[0], "--gateway-rx", values[1], "version"}, &bytes.Buffer{}); err == nil {
			t.Fatal("accepted invalid gateway GPIOs", pins)
		}
	}
}
func TestHelpAndInvalidFlagsDoNotExecute(t *testing.T) {
	for _, args := range [][]string{nil, {"--help"}, {"read_file", "-h"}, {"write_file", "--help"}, {"--color=always", "list_filesystem", "--help"}} {
		var help bytes.Buffer
		_, ops, handled, e := parseCLI(args, &help)
		if e != nil || !handled || len(ops) != 0 || !strings.Contains(help.String(), "Usage:") {
			t.Fatal(args, help.String(), handled, e)
		}
	}
	for _, args := range [][]string{{"write_file", "a"}, {"read_flash", "dump", "--signature", "sig"}, {"unknown_command"}, {"--reset-wait", "-1", "version"}, {"--erase_timeout", "0", "version"}, {"read_file", "a", "b", "--file-id", "128"}, {"--color", "invalid", "version"}} {
		if _, _, _, e := parseCLI(args, &bytes.Buffer{}); e == nil {
			t.Fatal("accepted", args)
		}
	}
	var out bytes.Buffer
	o, ops, handled, e := parseCLI([]string{"-ports"}, &out)
	if e != nil || handled || !o.ports || len(ops) != 0 {
		t.Fatal(o, ops, handled, e)
	}
}
