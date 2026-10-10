// SPDX-License-Identifier: GPL-2.0-only
// Package cc3200cli implements the native CC3200 tool and integrated subcommand.
package cc3200cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"goteddycloud/internal/cc3200"
	"goteddycloud/internal/cc3200serial"
)

type options struct {
	port, image, output, loader, backup, reset, sop2, device string
	gateway, ports, yes, inactive, reboot                    bool
}
type operation struct {
	name                       string
	args                       []string
	size, offset, allocation   int
	id                         int
	signature                  string
	commit, noVerify, simulate bool
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	flags := flag.NewFlagSet("cc3200", flag.ContinueOnError)
	flags.SetOutput(stderr)
	var o options
	flags.StringVar(&o.port, "port", "", "explicit UART device (for example /dev/ttyUSB0 or COM3)")
	flags.StringVar(&o.port, "p", "", "UART device")
	flags.StringVar(&o.image, "image_file", "", "read an offline flash image")
	flags.StringVar(&o.image, "if", "", "offline flash image")
	flags.StringVar(&o.output, "output_file", "", "new offline output image")
	flags.StringVar(&o.output, "of", "", "new offline output image")
	flags.StringVar(&o.loader, "loader", "", "pinned rbtl3100s.dll for application ROM >= 4")
	flags.StringVar(&o.backup, "backup", "", "new verified full-flash backup file, required before device writes")
	flags.StringVar(&o.reset, "reset", "none", "reset modem line: none, dtr, rts; prefix ~ to invert")
	flags.StringVar(&o.sop2, "sop2", "none", "SOP2 modem line: none, dtr, rts; prefix ~ to invert")
	flags.StringVar(&o.device, "device", "cc3200", "offline layout: cc3200 or cc32xx")
	flags.StringVar(&o.device, "d", "cc3200", "offline layout")
	flags.BoolVar(&o.gateway, "gateway", false, "ESP32 UARTGWEX gateway, default GPIOs 20/21/10/9/8")
	flags.BoolVar(&o.ports, "ports", false, "list available serial devices and exit")
	flags.BoolVar(&o.yes, "yes", false, "authorize the explicitly selected write/erase/format operation")
	flags.BoolVar(&o.inactive, "inactive", false, "read inactive offline FAT")
	flags.BoolVar(&o.reboot, "reboot-to-app", false, "reset with SOP2 deasserted after a successful operation")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "Usage: goteddycloud cc3200 [options] <command> [command options] [arguments]\nCommands: version, storage_info, list_filesystem, read_flash, write_flash, read_file, write_file, erase_file, format_flash, read_all_files, write_all_files, reboot")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if o.ports {
		ports, err := cc3200serial.Ports()
		if err != nil {
			return err
		}
		for _, port := range ports {
			fmt.Fprintln(stdout, port)
		}
		return nil
	}
	if flags.NArg() == 0 {
		flags.Usage()
		return errors.New("choose a command")
	}
	op, err := parseOperation(flags.Args(), stderr)
	if err != nil {
		return err
	}
	if o.device != "cc3200" && o.device != "cc32xx" {
		return errors.New("device must be cc3200 or cc32xx")
	}
	offline := o.image != ""
	if offline && o.port != "" {
		return errors.New("choose image_file or port")
	}
	write := op.name == "write_file" || op.name == "write_flash" || op.name == "erase_file" || op.name == "format_flash" || op.name == "write_all_files" && !op.simulate
	if write && !o.yes {
		return errors.New("device/image mutation requires --yes")
	}
	if offline && write && o.output == "" {
		return errors.New("offline writes require a distinct --output_file")
	}
	if !offline && write && (o.backup == "" || o.backup == "-") {
		return errors.New("device writes require --backup <new-file> for a verified full backup")
	}
	if !offline && (o.device != "cc3200" || o.inactive || op.id >= 0) {
		return errors.New("cc32xx, inactive FAT and file-ID operations require an offline image")
	}
	if !offline && o.output != "" {
		return errors.New("output_file is only for offline images")
	}
	// Validate and stage all local inputs before touching a physical device.
	var input, signature []byte
	var bulk []inputFile
	if op.name == "write_file" || op.name == "write_flash" {
		input, err = readBounded(op.args[0], cc3200.MaxFlash)
		if err != nil {
			return err
		}
		if op.name == "write_flash" {
			if _, err = cc3200.Inspect(input); err != nil {
				return fmt.Errorf("invalid input flash image: %w", err)
			}
		} else if !cc3200.ValidName(op.args[1]) {
			return errors.New("invalid destination filename")
		}
	}
	if op.name == "write_all_files" {
		bulk, err = readDirectory(op.args[0])
		if err != nil {
			return err
		}
	}
	if op.signature != "" {
		signature, err = readBounded(op.signature, 256)
		if err != nil {
			return err
		}
		if len(signature) != 256 {
			return errors.New("signature must contain exactly 256 bytes")
		}
	}
	if op.name == "write_file" && (len(input) == 0 || max(len(input), op.allocation) > 65536*255) {
		return errors.New("invalid file allocation")
	}
	for _, file := range bulk {
		if max(len(file.data), op.allocation) > 65536*255 {
			return errors.New("invalid bulk file allocation")
		}
	}
	imageOptions := cc3200.ImageOptions{Inactive: o.inactive, CC32XX: o.device == "cc32xx"}
	var image []byte
	var client *cc3200.Client
	if offline {
		image, err = readBounded(o.image, cc3200.MaxFlash)
		if err != nil {
			return err
		}
		if _, err = cc3200.InspectImage(image, imageOptions); err != nil {
			return err
		}
	} else {
		if o.port == "" {
			return errors.New("specify --port or --image_file")
		}
		var loader []byte
		if o.loader != "" {
			loader, err = readBounded(o.loader, 128<<10)
			if err != nil {
				return err
			}
		}
		port, e := cc3200serial.Open(ctx, o.port, cc3200serial.Options{Gateway: o.gateway, Reset: o.reset, SOP2: o.sop2})
		if e != nil {
			return e
		}
		defer port.Close()
		client = cc3200.NewClient(port)
		if err = client.Prepare(ctx, loader); err != nil {
			return err
		}
		if write {
			before, e := client.ReadFlash(ctx, 0, 0, nil)
			if e != nil {
				return e
			}
			verify, e := client.ReadFlash(ctx, 0, 0, nil)
			if e != nil {
				return e
			}
			if !bytes.Equal(before, verify) {
				return errors.New("full backup verification failed; no writes attempted")
			}
			if err = writeOutput(o.backup, before, stdout); err != nil {
				return err
			}
			fmt.Fprintf(stderr, "Verified backup: %s (SHA256 %s)\n", o.backup, cc3200.Digest(before))
		}
	}
	readFlash := func() ([]byte, error) {
		if offline {
			return append([]byte(nil), image...), nil
		}
		return client.ReadFlash(ctx, 0, 0, nil)
	}
	readFile := func(name string) ([]byte, error) {
		if offline {
			return cc3200.ImageFile(image, imageOptions, name, op.id)
		}
		return client.ReadFile(ctx, name)
	}
	list := func() (cc3200.Manifest, error) {
		flash, e := readFlash()
		if e != nil {
			return cc3200.Manifest{}, e
		}
		return cc3200.InspectImage(flash, imageOptions)
	}
	writeFile := func(name string, data []byte) error {
		if offline {
			var e error
			image, e = cc3200.ReplaceImageFile(image, imageOptions, name, op.id, data)
			return e
		}

		return client.WriteFile(ctx, name, data, cc3200.WriteOptions{Allocation: op.allocation, Commit: op.commit, Signature: signature, Verify: !op.noVerify})
	}
	switch op.name {
	case "version":
		if offline {
			return errors.New("version requires a connected device")
		}
		value, e := client.Version(ctx)
		if e != nil {
			return e
		}
		err = json.NewEncoder(stdout).Encode(value)
	case "storage_info":
		if offline {
			err = json.NewEncoder(stdout).Encode(cc3200.StorageInfo{BlockSize: cc3200.BlockSize, Blocks: uint16(len(image) / cc3200.BlockSize)})
		} else {
			value, e := client.StorageInfo(ctx, cc3200.SerialFlash)
			if e != nil {
				return e
			}
			err = json.NewEncoder(stdout).Encode(value)
		}
	case "list_filesystem":
		value, e := list()
		if e != nil {
			return e
		}
		err = json.NewEncoder(stdout).Encode(value)
	case "read_flash":
		var data []byte
		if offline {
			size := op.size
			if op.offset < 0 || op.offset > len(image) {
				return errors.New("invalid flash offset")
			}
			if size == 0 {
				size = len(image) - op.offset
			}
			if size < 0 || size > len(image)-op.offset {
				return errors.New("invalid flash size")
			}
			data = image[op.offset : op.offset+size]
		} else {
			data, err = client.ReadFlash(ctx, op.offset, op.size, nil)
		}
		if err == nil {
			err = writeOutput(op.args[0], data, stdout)
		}
	case "read_file":
		data, e := readFile(op.args[0])
		if e != nil {
			return e
		}
		err = writeOutput(op.args[1], data, stdout)
	case "read_all_files":
		manifest, e := list()
		if e != nil {
			return e
		}
		root, e := createDirectory(op.args[0])
		if e != nil {
			return e
		}
		defer root.Close()
		for _, entry := range manifest.Entries {
			if err = ctx.Err(); err != nil {
				return err
			}
			if entry.Name == "" {
				return fmt.Errorf("file ID %d has no known name; use read_file --file-id", entry.ID)
			}
			data, e := readFile(entry.Name)
			if e != nil {
				return e
			}
			if !offline && !op.noVerify {
				again, e := readFile(entry.Name)
				if e != nil {
					return e
				}
				if !bytes.Equal(data, again) {
					return errors.New("file backup verification failed")
				}
			}
			if err = writeRootFile(root, strings.TrimPrefix(entry.Name, "/"), data); err != nil {
				return err
			}
		}
	case "write_file":
		err = writeFile(op.args[1], input)
	case "write_all_files":
		// Validate the complete local tree before the first device write.
		for _, file := range bulk {
			fmt.Fprintln(stderr, file.name)
			if !op.simulate {
				if err = writeFile(file.name, file.data); err != nil {
					return err
				}
			}
		}
	case "erase_file":
		if offline {
			return errors.New("offline deletion is unsupported; use the device filesystem API")
		}
		err = client.EraseFile(ctx, op.args[0])
	case "format_flash":
		if offline {
			return errors.New("offline formatting is unsupported; use the device filesystem API")
		}
		err = client.Format(ctx, op.size)
	case "write_flash":
		if offline {
			return errors.New("write_flash requires a connected device")
		}
		err = client.WriteFlash(ctx, input, !op.noVerify, nil)
	case "reboot":
		if offline {
			return errors.New("reboot requires a connected device")
		}
		err = client.Reboot(ctx)
	}
	if err != nil {
		return err
	}
	if offline && write {
		if err = writeOutput(o.output, image, stdout); err != nil {
			return err
		}
	}
	if !offline && o.reboot && op.name != "reboot" {
		return client.Reboot(ctx)
	}
	return nil
}
