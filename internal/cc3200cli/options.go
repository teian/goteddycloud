// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"errors"
	"flag"
	"fmt"
	"goteddycloud/internal/cc3200"
	"io"
	"strconv"
	"strings"
)

func parseOperation(args []string, stderr io.Writer) (operation, error) {
	op := operation{name: args[0], id: -1}
	flags := flag.NewFlagSet(op.name, flag.ContinueOnError)
	flags.SetOutput(stderr)
	var size, offset, allocation string
	flags.StringVar(&size, "size", "0", "flash byte count (supports K/M suffix)")
	flags.StringVar(&size, "s", "0", "flash byte count")
	flags.StringVar(&offset, "offset", "0", "flash read offset")
	flags.StringVar(&allocation, "file-size", "0", "file allocation bytes")
	flags.IntVar(&op.id, "file-id", -1, "offline FAT file ID")
	flags.StringVar(&op.signature, "signature", "", "256-byte signature")
	flags.BoolVar(&op.commit, "commit", false, "mirrored file allocation")
	flags.BoolVar(&op.noVerify, "no-verify", false, "skip read-back verification")
	flags.BoolVar(&op.simulate, "simulate", false, "list bulk writes without mutating")
	if err := flags.Parse(args[1:]); err != nil {
		return op, err
	}
	op.args = flags.Args()
	var err error
	if op.size, err = parseSize(size); err != nil {
		return op, err
	}
	if op.offset, err = parseSize(offset); err != nil {
		return op, err
	}
	if op.allocation, err = parseSize(allocation); err != nil {
		return op, err
	}
	count := 0
	switch op.name {
	case "version", "storage_info", "list_filesystem", "format_flash", "reboot":
	case "read_flash", "write_flash", "erase_file", "read_all_files", "write_all_files":
		count = 1
	case "read_file", "write_file":
		count = 2
	default:
		return op, fmt.Errorf("unknown command %q", op.name)
	}
	if len(op.args) != count {
		return op, fmt.Errorf("%s requires %d arguments", op.name, count)
	}
	if op.id < -1 || op.id > 127 {
		return op, errors.New("file ID must be 0..127")
	}
	if op.name == "format_flash" && op.size == 0 {
		op.size = 1 << 20
	}
	return op, nil
}
func parseSize(value string) (int, error) {
	multiplier := uint64(1)
	if len(value) > 0 {
		switch strings.ToUpper(value[len(value)-1:]) {
		case "K":
			multiplier = 1024
			value = value[:len(value)-1]
		case "M":
			multiplier = 1 << 20
			value = value[:len(value)-1]
		}
	}
	n, err := strconv.ParseUint(value, 0, 32)
	if err != nil || n > uint64(cc3200.MaxFlash)/multiplier {
		return 0, errors.New("invalid size (maximum 16MiB)")
	}
	return int(n * multiplier), nil
}
