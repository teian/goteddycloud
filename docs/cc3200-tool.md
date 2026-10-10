# Native CC3200 tool

The Go port of [cc3200tool](https://github.com/toniebox-reverse-engineering/cc3200tool)
is integrated as `goteddycloud cc3200`. It can also be built as a standalone
`cc3200tool` executable. Neither path starts the server or requires Python/pip.
The existing browser flashing workflow uses the same Go protocol core, compiled
to WebAssembly, with Web Serial handling USB in the browser.

Reference: upstream commit `c2332898a1ededc0bdd01ec279b23366d213f81e`.
This is a native port of the CC3200 operations, not a Python CLI argument clone.
Use one operation per invocation, with global options before the command and
command options before its positional arguments. `--help` lists global options.

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

## Native UART and ESP32 gateway

```sh
bin/goteddycloud cc3200 --ports
bin/goteddycloud cc3200 --port /dev/ttyUSB0 --reset dtr --sop2 '~rts' \
  --loader /path/to/rbtl3100s.dll list_filesystem
bin/goteddycloud cc3200 --port /dev/ttyACM0 --gateway \
  --loader /path/to/rbtl3100s.dll read_flash backup.bin
```

UART is 921600 baud, 8N1. Without configured modem lines, enter SOP2/bootloader
mode and reset the device manually before invoking the command. `--reset` and
`--sop2` accept `none`, `dtr`, `rts`, and their inverted `~` forms; use distinct
lines. The gateway uses GPIO defaults TX20, RX21, reset10, control9, LED8 and
verifies the configuration returned by UARTGWEX. Browser GPIOs remain editable.
Direct UART does not require an ESP32 gateway. The caller must have serial-device
permissions; a remote server cannot access the user's USB device through this CLI.

Application ROM >=4 needs the exact upstream `rbtl3100s.dll`, SHA256
`2c9a05c469389446e05871a88e84ca3994bf3d6a6785155135db21d33aa63cc9`.
Already active NWP sessions do not need a loader. Application ROM <4 is unsupported,
as in the upstream tool. The loader is supplied locally, never downloaded by the
application. The browser likewise requires the selected loader file.

## Commands

| Command | Arguments | Behavior |
| --- | --- | --- |
| `version` | none | Query ROM/NWP/MAC/PHY versions |
| `storage_info` | none | Query serial flash geometry |
| `list_filesystem` | none | JSON FAT/filename/allocation inventory |
| `read_flash` | `output` | Full flash, or `--offset` / `--size` byte range |
| `write_flash` | `input` | Full same-size image; erase, write, verify |
| `read_file` | `device-path output` | Read named device file |
| `write_file` | `input device-path` | Replace/create and verify; supports `--file-size`, `--commit`, `--signature` |
| `erase_file` | `device-path` | Delete named file |
| `format_flash` | none | Format with `--size`, default 1M |
| `read_all_files` | `new-directory` | Extract all named files; double-read on devices |
| `write_all_files` | `directory` | Validate local tree and upload files; `--simulate` lists without writing |
| `reboot` | none | Reset with SOP2 deasserted |

Sizes accept decimal/hex integers and K/M suffixes, up to 16MiB. `-` writes file
or flash read results to stdout. Binary output is separated from diagnostic
stderr. Existing output files/directories are not overwritten. Backups and
extracted credentials use private permissions (0600 files / 0700 directories).
Bulk uploads reject symlinks, unsafe names, oversized trees and nonregular files.
Unknown FAT filenames require explicit offline file-ID extraction.

Every device mutation requires `--yes --backup <new-file>` before the command.
Before the mutation, the tool reads full flash twice, checks equality, saves it,
and reports SHA256. Bulk local inputs and signatures are validated before opening
the port. Read-back verification is enabled by default; `--no-verify` is explicit
(for example when uploading a signed service pack whose API readback is denied).
The full raw backup remains verified even with `--no-verify`.

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
Offline deletion, formatting, allocation changes, unsafe bounds overrides and live
CC32xx flashing are deliberately unsupported. Inputs remain unchanged; a distinct
new output is required. This port does not promise all historical Python flags.

## Validation

Go tests cover protocol framing, checksum failures, fail-closed sessions, cancellation,
concurrent-operation rejection, file and flash round trips, last-write ordering,
loader handover, bounded FAT reads and private offline extraction. Node tests run
the actual Go/WASM module against a fragmented simulated gateway, including readback
corruption and disconnects. Physical-device acceptance is still pending because
no CC3200 box is currently available.
