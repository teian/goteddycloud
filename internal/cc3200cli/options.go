// SPDX-License-Identifier: GPL-2.0-only
package cc3200cli

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/alecthomas/kong"
	"goteddycloud/internal/cc3200"
)

type globals struct {
	Port         string `short:"p" default:"auto" help:"Serial port; auto detects USB/COM ports (Linux fallback /dev/ttyUSB0)."`
	Image        string `name:"image_file" aliases:"if" help:"Read an offline flash image."`
	Output       string `name:"output_file" aliases:"of" help:"Write a distinct offline image."`
	Reset        string `default:"none" help:"Reset: dtr, rts, none or prompt; prefix ~ to invert."`
	SOP2         string `name:"sop2" default:"none" help:"SOP2: dtr, rts or none; prefix ~ to invert."`
	EraseTimeout int    `name:"erase_timeout" aliases:"erase-timeout" default:"120" help:"Erase timeout in seconds."`
	ResetWait    int    `name:"reset-wait" aliases:"rw" default:"1000" help:"Wait after reset in milliseconds."`
	Reboot       bool   `name:"reboot-to-app" help:"Reboot to the application after all commands."`
	Device       string `short:"d" default:"cc3200" enum:"cc3200,cc32xx" help:"Flash layout (cc32xx supports offline images)."`
	Gateway      bool   `short:"g" help:"Use an ESP32 UARTGWEX gateway."`
	GatewayTX    int    `name:"gateway-tx" default:"20" help:"ESP32-C3 gateway TX GPIO (connect to target RX)."`
	GatewayRX    int    `name:"gateway-rx" default:"21" help:"ESP32-C3 gateway RX GPIO (connect to target TX)."`
	Verbose      bool   `short:"v" help:"Show protocol opcodes, lengths and timeouts."`
	Quiet        bool   `short:"q" help:"Suppress status and progress messages."`
	Color        string `default:"auto" enum:"auto,always,never" help:"Terminal color policy (auto honors NO_COLOR and TERM=dumb)."`
	Loader       string `help:"Override the NWP loader with a local binary; defaults to the embedded loader when built in, then local rbtl3100s.dll discovery."`
	Backup       string `help:"Save a double-read verified full backup before device writes."`
	Yes          bool   `help:"Accepted for compatibility with earlier native releases."`
	Ports        bool   `help:"List serial ports and exit."`
	Inactive     bool   `help:"Select the inactive FAT (also accepted per command)."`
}

type fileIDFlags struct {
	ID int `name:"file-id" default:"-1" help:"Read/write by FAT file ID (0..127)."`
}
type verifyFlags struct {
	NoVerify bool `name:"no-verify" help:"Skip read-back verification."`
}
type readFileCommand struct {
	Name   string `arg:"" name:"cc_filename" help:"Device filename."`
	Output string `arg:"" name:"local_file" help:"Local output path, or - for stdout."`
	fileIDFlags
	verifyFlags
}
type writeFileCommand struct {
	Input      string `arg:"" name:"local_file" help:"Local input path, or - for stdin."`
	Name       string `arg:"" name:"cc_filename" help:"Device filename."`
	Signature  string `help:"Local file containing a 256-byte signature."`
	Allocation string `name:"file-size" default:"0" help:"Allocation bytes (decimal, hex, K/M)."`
	Commit     bool   `name:"commit-flag" aliases:"commit" help:"Enable fail-safe mirrored allocation."`
	fileIDFlags
	verifyFlags
}
type readFlashCommand struct {
	Output    string `arg:"" name:"dump_file" help:"Local output path, or - for stdout."`
	Offset    string `default:"0" help:"Starting byte offset."`
	Size      string `default:"-1" help:"Byte count; -1 reads the remaining flash."`
	IgnoreMax bool   `name:"ignore-max-size" help:"Allow reads past reported capacity, bounded to 16MiB."`
	verifyFlags
}
type writeFlashCommand struct {
	Input   string `arg:"" name:"gang_image_file" help:"Full flash image, or - for stdin."`
	NoErase bool   `name:"no-erase" help:"Skip erase (for already blank flash)."`
	verifyFlags
}
type listCommand struct {
	JSON     bool `name:"json-output" help:"Emit Python-compatible filesystem JSON on stdout."`
	Extended bool `help:"Include file size and header magic."`
}
type readAllCommand struct {
	Directory string `arg:"" name:"local_dir" help:"Destination directory."`
	ByID      bool   `name:"by-file-id" help:"Extract unnamed files by numeric ID."`
	AllByID   bool   `name:"all-by-file-id" help:"Extract every file under its numeric ID."`
	verifyFlags
}
type writeAllCommand struct {
	Directory string `arg:"" name:"local_dir" help:"Source directory."`
	Simulate  bool   `help:"List planned writes without writing."`
	verifyFlags
}
type commandLine struct {
	globals
	Format struct {
		Size string `short:"s" default:"1M" help:"Flash size: 512 (KiB), 1M, 2M, 4M, 8M or 16M."`
	} `cmd:"" name:"format_flash" help:"Format serial flash."`
	Erase struct {
		Name string `arg:"" name:"filename"`
	} `cmd:"" name:"erase_file" help:"Erase a device file."`
	WriteFile  writeFileCommand  `cmd:"" name:"write_file" help:"Upload a device file and verify it."`
	ReadFile   readFileCommand   `cmd:"" name:"read_file" help:"Read a device file and verify a second read."`
	WriteFlash writeFlashCommand `cmd:"" name:"write_flash" help:"Write a full flash image."`
	ReadFlash  readFlashCommand  `cmd:"" name:"read_flash" help:"Read and double-check a flash dump."`
	List       listCommand       `cmd:"" name:"list_filesystem" help:"List filesystem allocations and statistics."`
	ReadAll    readAllCommand    `cmd:"" name:"read_all_files" help:"Extract files into a directory."`
	WriteAll   writeAllCommand   `cmd:"" name:"write_all_files" help:"Upload files from a directory."`
	DLLTest    struct{}          `cmd:"" name:"dll_data_test" help:"Locate and validate the embedded or explicitly selected NWP loader."`
	Version    struct{}          `cmd:"" name:"version" help:"Query device ROM/NWP/MAC/PHY versions."`
	Storage    struct{}          `cmd:"" name:"storage_info" help:"Query serial flash geometry."`
	Reboot     struct{}          `cmd:"" name:"reboot" help:"Reboot into application mode."`
}

func newParser(model *commandLine, output io.Writer, exited *bool) (*kong.Kong, error) {
	return kong.New(model, kong.Name("cc3200tool"), kong.Description("Serial flash utility for CC3200. Python-compatible commands may be chained in one connection.\nExample: cc3200tool -p /dev/ttyUSB0 --reset dtr list_filesystem --extended read_file /cert/ca.der ca.der\n\nNWP loader setup:\n"+loaderSetupHelp), kong.Writers(output, output), kong.Exit(func(int) { *exited = true }), kong.ConfigureHelp(kong.HelpOptions{Compact: true}))
}

// splitCommands only splits at completed positional arguments. Unlike upstream's
// token-only splitter, files and flag values named after commands remain usable.
func splitCommands(args []string, parser *kong.Kong) ([][]string, error) {
	flags := map[string]*kong.Flag{}
	counts := map[string]int{}
	var visit func(*kong.Node)
	visit = func(n *kong.Node) {
		for _, f := range n.Flags {
			flags["--"+f.Name] = f
			flags["-"+f.Name] = f // earlier Go flag syntax and Python multi-letter aliases
			for _, alias := range f.Aliases {
				flags["--"+alias] = f
				flags["-"+alias] = f
			}
			if f.Short != 0 {
				flags["-"+string(f.Short)] = f
			}
		}
		for _, c := range n.Children {
			counts[c.Name] = len(c.Positional)
			visit(c)
		}
	}
	visit(parser.Model.Node)
	var sections [][]string
	var prefix, current []string
	command := ""
	positions := 0
	positionalOnly := false
	for i := 0; i < len(args); i++ {
		token := args[i]
		if !positionalOnly && token == "--" {
			current = append(current, token)
			positionalOnly = true
			continue
		}
		if !positionalOnly && strings.HasPrefix(token, "-") && token != "-" {
			name, value, equals := strings.Cut(token, "=")
			if f, ok := flags[name]; ok {
				token = "--" + f.Name
				if equals {
					token += "=" + value
				}
				current = append(current, token)
				if !equals && !f.IsBool() {
					if i+1 >= len(args) {
						return nil, fmt.Errorf("%s requires a value", name)
					}
					i++
					current[len(current)-1] += "=" + args[i]
				} else if !equals && f.IsBool() && i+1 < len(args) && (args[i+1] == "True" || args[i+1] == "False" || args[i+1] == "true" || args[i+1] == "false") {
					i++
					current[len(current)-1] += "=" + strings.ToLower(args[i])
				}
				continue
			}
		}
		if _, ok := counts[token]; !positionalOnly && ok && (command == "" || positions >= counts[command]) {
			if command == "" {
				prefix = append([]string(nil), current...)
			} else {
				sections = append(sections, current)
			}
			current = append(append([]string(nil), prefix...), token)
			command = token
			positions = 0
		} else {
			current = append(current, token)
			if command != "" {
				positions++
			}
		}
	}
	if len(current) > 0 {
		sections = append(sections, current)
	}
	if len(sections) == 0 {
		sections = [][]string{{"--help"}}
	}
	return sections, nil
}

func parseCLI(args []string, stderr io.Writer) (options, []operation, bool, error) {
	var model commandLine
	exited := false
	parser, err := newParser(&model, stderr, &exited)
	if err != nil {
		return options{}, nil, false, err
	}
	sections, err := splitCommands(args, parser)
	if err != nil {
		return options{}, nil, false, err
	}
	var opts options
	var operations []operation
	for index, section := range sections {
		hasCommand := false
		for _, token := range section {
			for _, node := range parser.Model.Children {
				if token == node.Name {
					hasCommand = true
				}
			}
		}
		if !hasCommand {
			for _, token := range section {
				if token == "--ports" {
					section = append(append([]string(nil), section...), "storage_info")
					break
				}
			}
		}
		model = commandLine{}
		exited = false
		parser, err = newParser(&model, stderr, &exited)
		if err != nil {
			return opts, nil, false, err
		}
		ctx, e := parser.Parse(section)
		if exited {
			return opts, nil, true, nil
		}
		if e != nil {
			return opts, nil, false, e
		}
		g := model.globals
		candidate := options{port: g.Port, image: g.Image, output: g.Output, loader: g.Loader, backup: g.Backup, reset: g.Reset, sop2: g.SOP2, device: g.Device, gateway: g.Gateway, ports: g.Ports, yes: g.Yes, reboot: g.Reboot, quiet: g.Quiet, verbose: g.Verbose, color: g.Color, eraseTimeout: g.EraseTimeout, resetWait: g.ResetWait}
		candidate.gatewayTX, candidate.gatewayRX = g.GatewayTX, g.GatewayRX
		for _, pin := range []int{g.GatewayTX, g.GatewayRX} {
			if pin < 0 || pin > 21 || (pin > 10 && pin < 20) {
				return opts, nil, false, errors.New("gateway TX/RX GPIO must be 0..10, 20 or 21 (USB and flash pins are reserved)")
			}
			if pin == 8 || pin == 9 || pin == 10 {
				return opts, nil, false, errors.New("gateway TX/RX GPIO must differ from LED8, control9 and reset10")
			}
		}
		if g.GatewayTX == g.GatewayRX {
			return opts, nil, false, errors.New("gateway TX and RX must use different GPIOs")
		}
		if index == 0 {
			opts = candidate
		} else if candidate != opts {
			return opts, nil, false, errors.New("connection options must precede the first command in a chain")
		}
		if g.Ports {
			return opts, nil, false, nil
		}
		op := operation{name: strings.Fields(ctx.Command())[0], id: -1}
		var size, offset, allocation string
		switch op.name {
		case "format_flash":
			size = model.Format.Size
			if size == "512" {
				size = "512K"
			}
		case "erase_file":
			op.args = []string{model.Erase.Name}
		case "write_file":
			c := model.WriteFile
			op.args = []string{c.Input, c.Name}
			op.id = c.ID
			op.signature = c.Signature
			op.commit = c.Commit
			op.noVerify = c.NoVerify
			allocation = c.Allocation
		case "read_file":
			c := model.ReadFile
			op.args = []string{c.Name, c.Output}
			op.id = c.ID
			op.noVerify = c.NoVerify
		case "write_flash":
			c := model.WriteFlash
			op.args = []string{c.Input}
			op.noErase = c.NoErase
			op.noVerify = c.NoVerify
		case "read_flash":
			c := model.ReadFlash
			op.args = []string{c.Output}
			size = c.Size
			if size == "-1" {
				size = "0"
			}
			offset = c.Offset
			op.ignoreMax = c.IgnoreMax
			op.noVerify = c.NoVerify
		case "list_filesystem":
			op.jsonOutput = model.List.JSON
			op.extended = model.List.Extended
		case "read_all_files":
			c := model.ReadAll
			op.args = []string{c.Directory}
			op.byID = c.ByID
			op.allByID = c.AllByID
			op.noVerify = c.NoVerify
		case "write_all_files":
			c := model.WriteAll
			op.args = []string{c.Directory}
			op.simulate = c.Simulate
			op.noVerify = c.NoVerify
		}
		for _, v := range []struct {
			value  string
			target *int
		}{{size, &op.size}, {offset, &op.offset}, {allocation, &op.allocation}} {
			if v.value != "" {
				*v.target, err = parseSize(v.value)
				if err != nil {
					return opts, nil, false, fmt.Errorf("%s: %w", op.name, err)
				}
			}
		}
		if op.id < -1 || op.id > 127 {
			return opts, nil, false, errors.New("file ID must be 0..127")
		}
		op.inactive = g.Inactive
		operations = append(operations, op)
	}
	if opts.eraseTimeout <= 0 || opts.eraseTimeout > 86400 || opts.resetWait < 0 || opts.resetWait > 3600000 {
		return opts, nil, false, errors.New("erase_timeout must be 1..86400 seconds and reset-wait 0..3600000 milliseconds")
	}
	return opts, operations, false, nil
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
