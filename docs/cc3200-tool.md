# Native CC3200 tool

The Go port of [cc3200tool](https://github.com/toniebox-reverse-engineering/cc3200tool)
is integrated as `goteddycloud cc3200`. It can also be built as a standalone
`cc3200tool` executable. Neither path starts the server or requires Python/pip.
The existing browser flashing workflow uses the same Go protocol core, compiled
to WebAssembly, with Web Serial handling USB in the browser.

Reference: upstream commit `c2332898a1ededc0bdd01ec279b23366d213f81e`.
The native CLI uses Kong and follows the Python tool's command names, option
names, aliases, and command chaining. Global connection options go before the
first command in a chain; command options can precede or follow their arguments.
Every command has context-sensitive `--help` / `-h`. Earlier native spellings,
such as `-port` and `--commit`, remain accepted.

```sh
bin/cc3200tool --help
bin/cc3200tool read_file --help
bin/cc3200tool --ports
bin/cc3200tool --reset dtr list_filesystem --extended
bin/cc3200tool -p /dev/ttyUSB0 --reset dtr \
  list_filesystem --json-output read_file /cert/ca.der ca.der
```

Status messages on stderr show port opening, bootloader synchronization attempts,
ROM/NWP versions, loader handover, storage geometry, transfer byte counts and
percentages, verification, and completion. `-v` / `--verbose` also reports
protocol opcodes, lengths and timeouts, without printing payloads or credentials.
The ACK reader skips startup bytes like the Python tool, with one deadline for
each acknowledgement. Verbose output reports skipped-byte counts; failures show
how many bytes arrived without a complete `00 CC` ACK.
Verbose synchronization output distinguishes sending the 200ms UART break from
waiting for its ACK. Native reads and break calls have driver-independent
deadlines; if a driver stalls, the connection closes and must be reopened.
Use `--quiet` to suppress progress. JSON and binary results remain on stdout.

Help and diagnostic levels use color on capable terminals. Redirected output,
`NO_COLOR`, and `TERM=dumb` disable automatic color. `--color=always` explicitly
forces ANSI color and `--color=never` disables it. Text labels also identify
message levels without color.

Ctrl+C cancels the operation, reports the interruption, and exits with status 130.
Shutdown waits at most one second for cleanup if a serial driver or stdin read
is blocked; a second Ctrl+C uses the operating system's default signal handler.

## Build

```sh
scripts/build-cc3200-wasm.sh
go build -buildvcs=false -o bin/goteddycloud ./cmd/goteddycloud
go build -buildvcs=false -o bin/cc3200tool ./cmd/cc3200tool
```

The checked-in/generated embedded module permits normal `go build` directly.
Regenerate it after changing the protocol core or Go toolchain. The generation
script also copies the matching Go runtime support script. CI and Docker builds
regenerate the module. Normal release archives include the integrated command.

For a self-contained local build, embed your stock loader into both native binaries:

```sh
python3 scripts/build-cc3200-with-loader.py /path/to/rbtl3100s.dll
bin/cc3200tool dll_data_test
bin/cc3200tool -p /dev/ttyACM0 --gateway --gateway-tx 21 --gateway-rx 20 version
```

The helper verifies the stock loader SHA-256 and builds `bin/cc3200tool` and
`bin/goteddycloud` with the `cc3200_embedded_loader` build tag. No loader file or
`--loader` argument is needed when running these binaries. The local embedded
blob is excluded from Git, Docker contexts and standard release snapshots.
The exact blob was traced to TI UniFlash 3.4.1.00012. Its redistribution license
has not been cleared; see the [loader license review](licenses.md). This build
option does not change the project's GPL-2.0-only license or include the blob in
standard releases. `--loader /path/to/another-loader.bin` still overrides the
embedded copy at runtime.

## Native UART and ESP32 gateway

```sh
bin/goteddycloud cc3200 --ports
bin/goteddycloud cc3200 --port /dev/ttyUSB0 --reset dtr --sop2 '~rts' \
  --loader /path/to/rbtl3100s.dll list_filesystem
bin/goteddycloud cc3200 --port /dev/ttyACM0 --gateway \
  --loader /path/to/rbtl3100s.dll read_flash backup.bin
```

Without `--port`, the tool selects the single detected USB serial adapter.
Linux considers `/dev/ttyUSB*` and `/dev/ttyACM*` and falls back to
`/dev/ttyUSB0` when none is detected. Windows selects an enumerated `COM<x>` port.
macOS uses call-out devices such as `/dev/cu.usbserial-*`, `/dev/cu.usbmodem*`,
and `/dev/cu.SLAB_USBtoUART`. Several candidates produce a list and require
`--port`; Windows/macOS with no adapter report how to list/select a port.

UART is 921600 baud, 8N1. Without configured modem lines, enter SOP2/bootloader
mode and reset the device manually before invoking the command. `--reset` and
`--sop2` accept `none`, `dtr`, `rts`, and their inverted `~` forms; use distinct
lines. `--reset prompt` asks you to reset the device and press Enter.
`-rw` / `--reset-wait` sets the post-reset delay in milliseconds (default 1000).
`--erase_timeout` sets the erase timeout in seconds (default 120). The gateway
uses GPIO defaults TX20, RX21, reset10, control9, LED8 and
verifies the configuration returned by UARTGWEX. Browser GPIOs remain editable.
Use `--gateway-tx 21 --gateway-rx 20` for a gateway wired with TX on GPIO21
and RX on GPIO20. These flags select the ESP32 pins; cross gateway TX to target
RX and gateway RX to target TX. `--verbose` also shows gateway configuration,
control commands and firmware logs without dumping target data.
Native gateway uploads use 64-byte DATA fragments with a 5 ms pause between
fragments to avoid overrunning the gateway receive path during loader or file
uploads. If a failed upload leaves the gateway unable to answer configuration
queries, reconnect the ESP32 USB cable before retrying. Failed writes are never
automatically replayed.
Direct UART does not require an ESP32 gateway. The caller must have serial-device
permissions; a remote server cannot access the user's USB device through this CLI.

Application ROM >=4 uses the stock upstream `rbtl3100s.dll`, SHA256
`2c9a05c469389446e05871a88e84ca3994bf3d6a6785155135db21d33aa63cc9`.

Download **`rbtl3100s.dll`** from the
[Toniebox project's cc3200tool DLL directory](https://github.com/toniebox-reverse-engineering/cc3200tool/tree/master/cc3200tool/dll).
Open that file and choose **Download raw file**. Save the actual binary with the
exact filename `rbtl3100s.dll`, not the GitHub HTML page or one of the other DLLs.

For automatic loading in a standard build, put the file in any of these locations
(searched in this order):

1. `rbtl3100s.dll` in the directory you run the command from.
2. `dll/rbtl3100s.dll` in that working directory.
3. `rbtl3100s.dll` beside the `cc3200tool` or `goteddycloud` executable.
4. `dll/rbtl3100s.dll` beside the executable, for example `bin/dll/rbtl3100s.dll`
   when the tool is `bin/cc3200tool`.

Check discovery without connecting to a box:

```sh
bin/cc3200tool dll_data_test
# Or, when using the integrated command:
bin/goteddycloud cc3200 dll_data_test
```

To keep the loader somewhere else, pass its path before the command:

```sh
bin/cc3200tool -p /dev/ttyACM0 --gateway --gateway-tx 21 --gateway-rx 20 \
  --loader /path/to/rbtl3100s.dll version storage_info
```

Already active NWP sessions do not need a loader. Application ROM <4 is unsupported,
as in the upstream tool. The loader is supplied locally, never downloaded by the
application. With no `--loader`, a native tool built with an embedded loader uses
that copy first. Other builds search the current directory, the executable
directory, and their `dll/` subdirectories for `rbtl3100s.dll`.
`--loader /path/to/another-loader.bin` explicitly overrides either default, accepts
a nonempty binary up to 128 KiB, and reports its SHA-256. A missing or invalid
explicit file is an error; it never silently falls back. This allows future
compatible loaders without changing the embedded stock image. The override runs
on the target, so its protocol and hardware compatibility remain the caller's
responsibility. Default loader selection remains pinned to the stock SHA-256.
`dll_data_test` reports the selected loader and its SHA-256 without opening a device.
The browser likewise requires the selected loader file.

## Commands

| Command | Arguments | Behavior |
| --- | --- | --- |
| `version` | none | Query ROM/NWP/MAC/PHY versions |
| `storage_info` | none | Query serial flash geometry |
| `list_filesystem` | none | Human-readable allocation/statistics table on stderr; `--json-output` emits Python-compatible JSON; `--extended` adds file sizes/header magic |
| `read_flash` | `output` | Full flash, or `--offset` / `--size` byte range |
| `write_flash` | `input` | Full same-size image; erase, write, verify; `--no-erase` skips erase for blank chips |
| `read_file` | `device-path output` | Read named device file |
| `write_file` | `input device-path` | Replace/create and verify; supports `--file-size`, `--commit-flag` (alias `--commit`), `--signature` |
| `erase_file` | `device-path` | Delete named file |
| `format_flash` | none | Format with `--size`, default 1M |
| `read_all_files` | `directory` | Extract files; `--by-file-id` includes unknown names, `--all-by-file-id` uses IDs for every output; double-read on devices |
| `write_all_files` | `directory` | Validate local tree and upload files; `--simulate` lists without writing |
| `reboot` | none | Reset with SOP2 deasserted |
| `dll_data_test` | none | Locate and validate the selected NWP loader |

Sizes accept decimal/hex integers and K/M suffixes, up to 16MiB. `format_flash -s 512` means 512KiB, matching Python. `read_flash --size -1` reads the remaining
flash. `--ignore-max-size True` permits reads beyond reported capacity within
the absolute 16MiB bound. Both Python-style boolean values and standalone
`--no-verify` flags are accepted. `-` writes file/flash reads to stdout and reads
file/flash write inputs from stdin. Binary output is separated from diagnostic
stderr. Existing output files are not overwritten; extraction directories may be reused
for new files. Backups and
extracted credentials use private permissions (0600 files / 0700 directories).
Bulk uploads reject symlinks, unsafe names, oversized trees and nonregular files.
Unknown FAT filenames can be extracted with `--file-id`, `--by-file-id`, or
`--all-by-file-id`; live ID/inactive-FAT reads use the raw flash image.

As in Python, invoking a write/erase/format command selects that mutation;
`--yes` is accepted for compatibility with earlier native releases but is not
required. Optional `--backup <new-file>` reads full flash twice before the first
mutation in the chain, checks equality, saves it, and reports SHA256. All local
inputs for every command in a chain are staged before opening the device.
Read-back verification is enabled by default; `--no-verify` is explicit
(for example when uploading a signed service pack whose API readback is denied).
When requested, the full raw backup remains verified even with `--no-verify`.
`read_file` and `read_flash` also double-read device data before saving output,
unless `--no-verify` is selected. `read_all_files` skips `/sys/servicepack.ucf`,
matching the upstream extraction blacklist.

Named extraction saves `/cert/ca.der`, `/cert/private.der`, `/cert/client.der`,
`/sys/mcuimg.bin`, `/sys/mcuimg1.bin`, `/sys/mcuimg2.bin`, `/sys/mcuimg3.bin`
and `/sys/mcubootinfo.bin` first. Live named extraction also queries these paths
when their names are absent from the FAT. Files reported as nonexistent by the
device API (such as stale `/sys/pref.net` entries) are reported and skipped so
later files are still extracted. An unavailable priority file listed in the FAT
causes an error after the remaining files have been saved. Transport errors and
read verification failures stop extraction immediately. Existing output files
are still never overwritten; use an empty destination for a fresh extraction.

```sh
bin/goteddycloud cc3200 --port /dev/ttyUSB0 --loader rbtl3100s.dll \
  --yes --backup before-write.bin write_file new-ca.der /cert/ca.der
```

An interrupted serial operation invalidates the connection; reconnect before
retrying. Full image writes require the device's exact flash size and publish the
first eight filesystem bytes last. The browser retains its stricter existing
installation-plan checks, double backups and identity/read-back verification.

## Offline images

```sh
bin/goteddycloud cc3200 -if backup.bin list_filesystem
bin/goteddycloud cc3200 -if backup.bin read_file /cert/ca.der ca.der
bin/goteddycloud cc3200 -if backup.bin read_file --file-id 0 unused-name file-0.bin
bin/goteddycloud cc3200 -if backup.bin read_all_files extracted
bin/goteddycloud cc3200 -if backup.bin -of updated.bin --yes \
  write_file ca.der /cert/ca.der
```

Offline reads support the active/inactive FAT (`--inactive`) and the upstream
partial CC3230/CC3235 layout (`--device cc32xx`). Inactive FAT metadata follows its
own selected copy. Existing-file replacement never expands the previous content
and refuses mirrored files, whose commit/signature semantics need the device API.
Offline deletion, formatting, allocation changes, reads beyond 16MiB and live
CC32xx flashing are unsupported. Inputs remain unchanged; a distinct
new output is required. CLI syntax follows the pinned Python version;
intentional behavior differences
include optional local loader embedding and explicit overrides, private
output permissions, refusing to overwrite output files, full-size image writes,
bounded reads, and rejecting unsupported offline allocation changes. Command
failures use status 1; interruptions use 130.

## Validation

Go tests cover protocol framing, checksum failures, fail-closed sessions, cancellation,
concurrent-operation rejection, command chains, Python aliases/JSON, platform
port selection, terminal color/binary separation, Ctrl+C during blocked stdin,
file and flash round trips, last-write ordering,
loader handover, bounded FAT reads and private offline extraction. Node tests run
the actual Go/WASM module against a fragmented simulated gateway, including readback
corruption and disconnects.

Native hardware checks on 2026-10-13 used an ESP32-C3 gateway with TX21/RX20,
reset10/control9 and a CC3200 application ROM reporting `[0 4 1 2]` with 4 MiB
serial flash. Three consecutive paced loader uploads and geometry queries passed.
A complete flash dump was saved after two identical reads; 22 available files
were individually double-read and verified. `/sys/pref.net` was listed in the FAT
but unavailable through the device API, and `/sys/servicepack.ucf` was skipped by
the extraction blacklist; their raw allocations remain in the full dump.
Writing the saved 20,956-byte original to `/sys/pre-img.bin` passed readback
verification. The user also reported a verified 15,791-byte HackieboxNG preloader
write to `/sys/mcuimg.bin`. The embedded stock loader was then tested without a
`--loader` argument. No post-install boot/playback or complete browser installation
is claimed; the broader physical acceptance gates remain pending.

On 2026-10-14, the native command
`bin/cc3200tool -g --gateway-tx=21 --gateway-rx=20 read_all_files ExtractedFromBox/`
completed on this gateway/CC3200 setup. All eight priority paths above were
extracted and verified by two matching device API reads, with mode 0600 outputs.
Extraction continued after the device reported `/sys/pref.net` as nonexistent
and saved all 22 available files; `/sys/servicepack.ucf` remained blacklisted.
A separate `read_flash backup.bin` then saved the complete 4 MiB flash after
two matching reads.
