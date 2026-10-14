// SPDX-License-Identifier: GPL-2.0-only
// Package cc3200cli implements the native CC3200 tool and integrated subcommand.
package cc3200cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"goteddycloud/internal/cc3200"
	"goteddycloud/internal/cc3200serial"
)

type options struct {
	port, image, output, loader, backup, reset, sop2, device, color string
	gateway, ports, yes, reboot, quiet, verbose                     bool
	eraseTimeout, resetWait                                         int
	gatewayTX, gatewayRX                                            int
}

const loaderDownloadURL = "https://github.com/toniebox-reverse-engineering/cc3200tool/tree/master/cc3200tool/dll"
const loaderSetupHelp = "Download rbtl3100s.dll from " + loaderDownloadURL + ".\n" +
	"Open rbtl3100s.dll and choose Download raw file; save the binary, not the GitHub web page.\n" +
	"For automatic loading, put rbtl3100s.dll beside cc3200tool/goteddycloud, in dll/ beside the executable, in the current working directory, or in its dll/ subdirectory.\n" +
	"Or pass --loader /path/to/rbtl3100s.dll before the command. Check the selected loader with dll_data_test."

type operation struct {
	name                                                                                          string
	args                                                                                          []string
	size, offset, allocation, id                                                                  int
	signature                                                                                     string
	commit, noVerify, simulate, inactive, noErase, ignoreMax, jsonOutput, extended, byID, allByID bool
	input, sign                                                                                   []byte
	bulk                                                                                          []inputFile
}

func (op operation) mutates() bool {
	return op.name == "write_file" || op.name == "write_flash" || op.name == "erase_file" || op.name == "format_flash" || op.name == "write_all_files" && !op.simulate
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer) error {
	return run(ctx, args, os.Stdin, stdout, stderr)
}

func run(ctx context.Context, args []string, stdin io.Reader, stdout, stderr io.Writer) error {
	terminal := newTerminal(stderr, colorMode(args), false)
	o, ops, handled, err := parseCLI(args, terminal.helpWriter())
	if handled || err != nil {
		return err
	}
	terminal.color = colorEnabled(stderr, o.color)
	terminal.quiet = o.quiet
	if err = ctx.Err(); err != nil {
		return err
	}
	if o.ports {
		ports, e := cc3200serial.Ports()
		if e != nil {
			return e
		}
		for _, port := range ports {
			fmt.Fprintln(stdout, port)
		}
		if len(ports) == 0 {
			terminal.status("No serial ports detected")
		}
		return nil
	}
	offline := o.image != ""
	if offline && o.port != "auto" {
		return errors.New("choose image_file or port")
	}
	if !offline && o.output != "" {
		return errors.New("output_file is only for offline images")
	}
	if o.backup == "-" {
		return errors.New("backup requires a file path, not stdout")
	}
	// Validate every command and local input before opening the port or mutating an image.
	write := false
	for i := range ops {
		op := &ops[i]
		if err = ctx.Err(); err != nil {
			return err
		}
		write = write || op.mutates()
		if !offline && o.device != "cc3200" {
			return errors.New("cc32xx requires an offline image")
		}
		if !offline && op.name == "write_file" && op.id >= 0 {
			return errors.New("writing by file ID requires an offline image")
		}
		if op.name == "erase_file" && !cc3200.ValidName(op.args[0]) {
			return errors.New("invalid device filename")
		}
		if op.name == "format_flash" && (op.size < 512<<10 || op.size > cc3200.MaxFlash || op.size&(op.size-1) != 0) {
			return errors.New("flash size must be 512KiB, 1, 2, 4, 8 or 16MiB")
		}
		if offline && (op.name == "format_flash" || op.name == "erase_file" || op.name == "write_flash" || op.name == "version" || op.name == "reboot") {
			return fmt.Errorf("%s requires a connected device", op.name)
		}
		if offline && op.mutates() && o.output == "" {
			return errors.New("offline writes require a distinct --output_file")
		}
		if op.name == "write_file" || op.name == "write_flash" {
			if op.args[0] == "-" {
				terminal.status("Reading input from stdin (Ctrl+C to cancel)")
			}
			op.input, err = readInput(stdin, op.args[0], cc3200.MaxFlash)
			if err != nil {
				return fmt.Errorf("%s input: %w", op.name, err)
			}
			if op.name == "write_flash" && (len(op.input) < 5*cc3200.BlockSize || len(op.input)%cc3200.BlockSize != 0) {
				return errors.New("invalid flash image size")
			}
			if op.name == "write_file" && (!cc3200.ValidName(op.args[1]) || len(op.input) == 0 || max(len(op.input), op.allocation) > 65536*255) {
				return errors.New("invalid device filename or file allocation")
			}
		}
		if op.name == "write_all_files" {
			op.bulk, err = readDirectory(op.args[0])
			if err != nil {
				return err
			}
		}
		if op.signature != "" {
			op.sign, err = readBounded(op.signature, 256)
			if err != nil {
				return err
			}
			if len(op.sign) != 256 {
				return errors.New("signature must contain exactly 256 bytes")
			}
		}
	}
	// Loader diagnostics work without opening a serial port.
	if len(ops) == 1 && ops[0].name == "dll_data_test" {
		loader, path, e := loadLoader(o.loader)
		if e != nil {
			return e
		}
		if o.loader == "" && cc3200.Digest(loader) != cc3200.LoaderSHA256 {
			return errors.New("loader SHA256 does not match pinned rbtl3100s.dll")
		}
		terminal.status(fmt.Sprintf("Loader verified: %s (%d bytes, SHA256 %s)", path, len(loader), cc3200.Digest(loader)))
		return nil
	}
	var image []byte
	var client *cc3200.Client
	if offline {
		terminal.status(fmt.Sprintf("Opening flash image %q", o.image))
		image, err = readBounded(o.image, cc3200.MaxFlash)
		if err != nil {
			return err
		}
		if _, err = cc3200.InspectImage(image, cc3200.ImageOptions{CC32XX: o.device == "cc32xx"}); err != nil {
			return err
		}
	} else {
		loader, path, e := loadLoader(o.loader)
		if e != nil && (o.loader != "" || !errors.Is(e, os.ErrNotExist)) {
			return e
		}
		if e != nil {
			// An already active NWP session can work without a loader, but
			// show setup instructions before any later ROM handover needs one.
			terminal.notice(e.Error())
		}
		if len(loader) > 0 {
			terminal.status(fmt.Sprintf("Using NWP loader %q (%d bytes)", path, len(loader)))
		}
		if o.port == "auto" {
			ports, e := cc3200serial.Ports()
			if e != nil {
				return fmt.Errorf("enumerating serial ports: %w", e)
			}
			o.port, e = selectPort(runtime.GOOS, ports)
			if e != nil {
				return e
			}
			terminal.status(fmt.Sprintf("Selected serial port %s", o.port))
		}
		terminal.status(fmt.Sprintf("Opening %s at 921600 baud, 8N1 (reset=%s, SOP2=%s)", o.port, o.reset, o.sop2))
		if o.gateway {
			terminal.status(fmt.Sprintf("Initializing ESP32 UARTGWEX gateway (GPIO TX%d RX%d reset10 control9 LED8)", o.gatewayTX, o.gatewayRX))
		} else if o.sop2 == "none" {
			terminal.status("SOP2 is manual: assert SOP2 before resetting into bootloader mode")
		}
		promptReader := bufio.NewReader(stdin)
		prompt := func(ctx context.Context, boot bool) error {
			state := "deasserted"
			if boot {
				state = "asserted"
			}
			terminal.notice(fmt.Sprintf("Reset the device with SOP2 %s and press Enter", state))
			done := make(chan error, 1)
			go func() { _, e := promptReader.ReadString('\n'); done <- e }()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case e := <-done:
				return e
			}
		}
		var debug func(string)
		if o.verbose {
			debug = terminal.debug
		}
		port, e := cc3200serial.Open(ctx, o.port, cc3200serial.Options{Gateway: o.gateway, Pins: [5]byte{byte(o.gatewayTX), byte(o.gatewayRX), 10, 9, 8}, Reset: o.reset, SOP2: o.sop2, ResetWait: time.Duration(o.resetWait) * time.Millisecond, ResetWaitSet: true, PromptReset: prompt, Debug: debug})
		if e != nil {
			return fmt.Errorf("opening %s: %w", o.port, e)
		}
		defer port.Close()
		clientOptions := cc3200.ClientOptions{Status: terminal.status, Debug: debug, EraseTimeout: time.Duration(o.eraseTimeout) * time.Second}
		if o.loader != "" {
			clientOptions.LoaderSHA256 = cc3200.Digest(loader)
			terminal.status(fmt.Sprintf("Explicit loader override: SHA256 %s", clientOptions.LoaderSHA256))
		}
		client = cc3200.NewClientWithOptions(port, clientOptions)
		if err = client.Prepare(ctx, loader); err != nil {
			return fmt.Errorf("preparing %s: %w", o.port, err)
		}
		terminal.success("Connected to target")
		if write && o.backup != "" {
			terminal.status("Reading full flash backup (pass 1/2)")
			before, e := client.ReadFlash(ctx, 0, 0, nil)
			if e != nil {
				return e
			}
			terminal.status("Verifying full flash backup (pass 2/2)")
			verify, e := client.ReadFlash(ctx, 0, 0, nil)
			if e != nil {
				return e
			}
			if !bytes.Equal(before, verify) {
				return errors.New("full backup verification failed; no writes attempted")
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			if err = writeOutput(o.backup, before, stdout); err != nil {
				return err
			}
			terminal.success(fmt.Sprintf("Verified backup: %s (SHA256 %s)", o.backup, cc3200.Digest(before)))
		}
	}
	for _, op := range ops {
		if err = ctx.Err(); err != nil {
			return err
		}
		terminal.status("Starting " + op.name)
		if err = execute(ctx, o, op, client, &image, stdout, terminal); err != nil {
			return fmt.Errorf("%s: %w", op.name, err)
		}
		terminal.success(op.name + " complete")
	}
	if offline && write {
		if err = ctx.Err(); err != nil {
			return err
		}
		if err = writeOutput(o.output, image, stdout); err != nil {
			return err
		}
		terminal.success("Saved updated image: " + o.output)
	}
	if !offline && o.reboot && ops[len(ops)-1].name != "reboot" {
		if err = client.Reboot(ctx); err != nil {
			return err
		}
	}
	terminal.success(fmt.Sprintf("All commands done in %s", time.Since(terminal.started).Round(time.Millisecond)))
	return nil
}

func execute(ctx context.Context, o options, op operation, client *cc3200.Client, image *[]byte, stdout io.Writer, t *terminal) error {
	offline := o.image != ""
	imageOptions := cc3200.ImageOptions{Inactive: op.inactive, CC32XX: o.device == "cc32xx"}
	readFlash := func() ([]byte, error) {
		if offline {
			return *image, nil
		}
		return client.ReadFlash(ctx, 0, 0, nil)
	}
	readFile := func(name string, id int) ([]byte, error) {
		if offline {
			return cc3200.ImageFile(*image, imageOptions, name, id)
		}
		if id >= 0 || op.inactive {
			flash, e := readFlash()
			if e != nil {
				return nil, e
			}
			return cc3200.ImageFile(flash, imageOptions, name, id)
		}
		return client.ReadFile(ctx, name)
	}
	writeFile := func(name string, data []byte) error {
		if offline {
			updated, e := cc3200.ReplaceImageFile(*image, imageOptions, name, op.id, data)
			if e == nil {
				*image = updated
			}
			return e
		}
		if op.id >= 0 {
			return errors.New("writing by file ID requires an offline image")
		}
		return client.WriteFile(ctx, name, data, cc3200.WriteOptions{Allocation: op.allocation, Commit: op.commit, Signature: op.sign, Verify: !op.noVerify})
	}
	switch op.name {
	case "version":
		v, e := client.Version(ctx)
		if e != nil {
			return e
		}
		return json.NewEncoder(stdout).Encode(v)
	case "storage_info":
		info := cc3200.StorageInfo{BlockSize: cc3200.BlockSize, Blocks: uint16(len(*image) / cc3200.BlockSize)}
		if !offline {
			var e error
			info, e = client.StorageInfo(ctx, cc3200.SerialFlash)
			if e != nil {
				return e
			}
		}
		return json.NewEncoder(stdout).Encode(info)
	case "list_filesystem":
		flash, e := readFlash()
		if e != nil {
			return e
		}
		return printFilesystem(flash, imageOptions, op.jsonOutput, op.extended, stdout, t)
	case "read_flash":
		read := func() ([]byte, error) {
			if !offline {
				return client.ReadFlashWithOptions(ctx, op.offset, op.size, op.ignoreMax, nil)
			}
			size := op.size
			if op.offset < 0 || op.offset > len(*image) {
				return nil, errors.New("invalid flash offset")
			}
			if size == 0 {
				size = len(*image) - op.offset
			}
			if size < 0 || size > len(*image)-op.offset {
				return nil, errors.New("invalid flash size")
			}
			return (*image)[op.offset : op.offset+size], nil
		}
		data, e := read()
		if e != nil {
			return e
		}
		if !offline && !op.noVerify {
			t.status("Verifying flash dump with a second read")
			again, e := read()
			if e != nil {
				return e
			}
			if !bytes.Equal(data, again) {
				return errors.New("flash dump verification failed; no output saved")
			}
			t.success("Flash dump verified")
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = writeOutput(op.args[0], data, stdout); e == nil {
			t.status(fmt.Sprintf("Saved %d bytes to %q", len(data), op.args[0]))
		}
		return e
	case "read_file":
		data, e := readFile(op.args[0], op.id)
		if e != nil {
			return e
		}
		if !offline && !op.noVerify {
			t.status("Verifying file with a second read")
			again, e := readFile(op.args[0], op.id)
			if e != nil {
				return e
			}
			if !bytes.Equal(data, again) {
				return errors.New("file read verification failed; no output saved")
			}
			t.success("File read verified")
		}
		if e = ctx.Err(); e != nil {
			return e
		}
		if e = writeOutput(op.args[1], data, stdout); e == nil {
			t.status(fmt.Sprintf("Saved %d bytes to %q", len(data), op.args[1]))
		}
		return e
	case "read_all_files":
		flash, e := readFlash()
		if e != nil {
			return e
		}
		manifest, e := cc3200.InspectImage(flash, imageOptions)
		if e != nil {
			return e
		}
		return extractFiles(ctx, manifest.Entries, op, offline, readFile, t)
	case "write_file":
		return writeFile(op.args[1], op.input)
	case "write_all_files":
		for _, file := range op.bulk {
			if e := ctx.Err(); e != nil {
				return e
			}
			if op.simulate {
				t.notice("Simulation: would write " + file.name)
			} else {
				if e := writeFile(file.name, file.data); e != nil {
					return e
				}
			}
		}
	case "erase_file":
		return client.EraseFile(ctx, op.args[0])
	case "format_flash":
		return client.Format(ctx, op.size)
	case "write_flash":
		return client.WriteFlashWithOptions(ctx, op.input, !op.noVerify, op.noErase, nil)
	case "reboot":
		return client.Reboot(ctx)
	case "dll_data_test":
		data, path, e := loadLoader(o.loader)
		if e != nil {
			return e
		}
		if o.loader == "" && cc3200.Digest(data) != cc3200.LoaderSHA256 {
			return errors.New("loader SHA256 mismatch")
		}
		t.success("Loader verified: " + path)
	}
	return nil
}

func readInput(stdin io.Reader, name string, limit int) ([]byte, error) {
	if name != "-" {
		return readBounded(name, limit)
	}
	data, e := io.ReadAll(io.LimitReader(stdin, int64(limit)+1))
	if e != nil {
		return nil, e
	}
	if len(data) > limit {
		return nil, errors.New("stdin exceeds input size limit")
	}
	return data, nil
}
func loadLoader(explicit string) ([]byte, string, error) {
	if explicit != "" {
		data, e := readBounded(explicit, 128<<10)
		if errors.Is(e, os.ErrNotExist) {
			return nil, explicit, fmt.Errorf("loader override %q: %w\n%s", explicit, e, loaderSetupHelp)
		}
		if e == nil && len(data) == 0 {
			e = errors.New("loader file is empty")
		}
		return data, explicit, e
	}
	if len(embeddedLoader) > 0 {
		if cc3200.Digest(embeddedLoader) != cc3200.LoaderSHA256 {
			return nil, "embedded rbtl3100s.dll", errors.New("embedded loader SHA256 does not match pinned rbtl3100s.dll")
		}
		return bytes.Clone(embeddedLoader), "embedded rbtl3100s.dll", nil
	}
	candidates := []string{"rbtl3100s.dll", filepath.Join("dll", "rbtl3100s.dll")}
	if exe, e := os.Executable(); e == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates, filepath.Join(dir, "rbtl3100s.dll"), filepath.Join(dir, "dll", "rbtl3100s.dll"))
	}
	for _, path := range candidates {
		data, e := readBounded(path, 128<<10)
		if e == nil {
			return data, path, nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return nil, path, e
		}
	}
	return nil, "", fmt.Errorf("local rbtl3100s.dll not found: %w\n%s", os.ErrNotExist, loaderSetupHelp)
}
func openExtractionDirectory(name string) (*os.Root, error) {
	if name == "-" {
		return nil, errors.New("stdout cannot be an extraction directory")
	}
	if e := os.MkdirAll(name, 0700); e != nil {
		return nil, e
	}
	return os.OpenRoot(name)
}
