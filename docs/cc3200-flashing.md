# CC3200 browser flashing

Implements the browser workflow requested in
[teddyCloud issue 437](https://github.com/toniebox-reverse-engineering/teddycloud/issues/437),
including its gateway preparation sub-issue. Open `/tools/cc3200` on the management
listener, or select the built-in **CC3200 flashing** plugin under Tonieboxes in a
compatible official frontend. The embedded tool also works without an installed
frontend. The browser uses the native Go protocol core compiled to WebAssembly.
The same core is available through [the integrated CLI](cc3200-tool.md). Existing
Python cc3200tool instructions remain available as **Legacy**.

## Preparation

Use a Web Serial browser over trusted HTTPS or localhost. The ESP32-C3 needs
native USB and the [UART gateway firmware](https://github.com/g3gg0/ESP32-UART-Gateway).
The page links the upstream browser installer; disconnect it before opening a
serial connection here. GPIO defaults follow issue 292: TX 20, RX 21, reset 10,
SOP2/control 9, LED 8, baud 921600. They are editable, and the tool reads back the
gateway configuration to confirm the requested pins and baud. GPIO18/19 are
reserved for native USB and cannot be selected.

Cross gateway TX/RX to box RX/TX, connect common ground, and wire reset and SOP2
to the selected gateway pins. Use 3.3 V logic and check the
[CC3200 pinout](https://toniebox-reverse-engineering.github.io/docs/wiki/cc3200/pinout/).
Stable box power is required throughout installation.

Download `rbtl3100s.dll` from the
[Toniebox project's cc3200tool DLL directory](https://github.com/toniebox-reverse-engineering/cc3200tool/tree/master/cc3200tool/dll).
Open that file, choose **Download raw file**, and save the binary with the exact
filename `rbtl3100s.dll`. Select the downloaded file in the browser tool's loader
field. Native CLI users can instead place it beside the executable or in its
`dll/` subdirectory for automatic loading, or pass `--loader /path/to/rbtl3100s.dll`
before the command; see [native loader setup](cc3200-tool.md).
The browser verifies SHA-256
`2c9a05c469389446e05871a88e84ca3994bf3d6a6785155135db21d33aa63cc9`
before sending it to network-processor SRAM. No TI binary is redistributed or
automatically downloaded by this application. CC3200 application ROM revisions
4 and later follow cc3200tool's network-processor handover, loader upload and
execution sequence. Earlier revisions are rejected. This workflow targets
CC3200 only; use the existing tools for CC3235, ESP32 and TB2.

## Backup and installation

1. Connect and prepare the gateway/CC3200. The browser handles reset, SOP2 and
   break signals automatically.
2. Read the complete serial flash twice and compare it byte for byte. Inspect
   the newest valid CC3200 FAT, then independently read every named file twice.
   Query important files directly even when their names are absent from the FAT.
3. Save the backup ZIP to the selected filesystem/S3 backend and download an
   independent copy. The archive contains `flash.bin`, `files/`, `manifest.json`
   and `allocations/<id>-<copy>.bin`. Both mirrored allocations and unnamed
   allocations remain recoverable by ID. Named files come from the filesystem
   API, rather than assuming which raw mirror is current. Missing required files
   block installation; a partial backup can still be saved/downloaded.
4. Enter the box MAC to import its CA, client certificate and private key. The
   server validates certificate/key matching and uses the existing certificate
   import conflict rules. Different installed identities are preserved by default.
5. Select `flash/sys/mcuimg.bin` from a
   [HackieboxNG release](https://github.com/toniebox-reverse-engineering/hackiebox_cfw_ng/releases).
   Enter the SHA-256 identifying the original bootloader from a trusted backup.
   Copy the release's `sd/` tree to microSD and the original bootloader to
   `revvox/boot/ng-ofw1.bin`, as described by the
   [official installation guide](https://toniebox-reverse-engineering.github.io/docs/custom-firmware/cc3200/hackieboxng-bl/install/).
6. Prepare and review the plan. The backend checks ARM stack/reset vectors,
   distinguishes the HackieboxNG preloader marker, checks the original against
   the supplied hash, and supplies the configured teddyCloud server CA in DER.
   These checks do not prove vendor provenance; the independent trusted original
   hash is required. A preserved `/sys/pre-img.bin` takes precedence on updates.
7. Install. Before writing, compare all three destinations and client identity
   against the backup to detect a changed box/session. Write and read back
   `/sys/pre-img.bin`, then `/cert/c2.der`, then `/sys/mcuimg.bin`. An identical
   existing original is retained without rewriting it. Any error stops later
   writes; the final preloader is not written after an earlier verification failure.
8. Disconnect, remove programming wiring and restart. Finish the SD `ngCfg.json`,
   `altCA.305` and URL patch configuration using the official guide.

Certificate/private-key bytes are never written to browser logs. Backups contain
private identities and belong in protected storage. They are addressed by the
SHA-256 of the complete archive at
`<firmwaredir>/cc3200/backups/<sha256>.zip`, use mode 0600 on filesystem storage,
and are read with integrity verification and `Cache-Control: no-store`.
The selected S3 backend controls its own bucket permissions and encryption.

## API and limits

All routes use the management authentication gate; none is available on the box
listener. Explicit cross-origin requests are rejected. HTTP methods match exactly.

| Route | Method | Input/result |
| --- | --- | --- |
| `/api/cc3200/inspect` | POST | Raw flash → FAT inventory |
| `/api/cc3200/backup` | POST | Multipart `flash`, optional named-files `files` ZIP → backup ID/manifest |
| `/api/cc3200/download?backup=<id>` | GET | Verified backup ZIP |
| `/api/cc3200/import?backup=<id>&mac=<12 hex>` | POST | Validate/import backed-up client identity |
| `/api/cc3200/plan?backup=<id>&originalSHA256=<hash>` | POST | Multipart `preloader` → three ordered artifacts and hashes |

Maximum serial flash and total named file data are each 16 MiB. CC3200 FAT uses
4 KiB blocks and at most 128 allocations. ZIP entries, filenames, allocations,
overlaps, CRCs and decompressed size are bounded/validated. Backups retain full
raw allocations in addition to logical files and therefore can approach 48 MiB.
Two server firmware/backup operations can be active simultaneously; serial
commands remain sequential. Timeouts and disconnects require reconnecting and
making a fresh backup. There is no automatic retry of a failed destructive write
and no claim of power-loss recovery. Existing backups are never auto-deleted.

The Go code supplies durable storage, validation, certificates and installation
plans. The browser owns USB access and UARTGWEX framing; the CC3200 ROM protocol and
file operations run in Go/WebAssembly. The Go server does not need access to a
host serial device. Protocol code is an
independent implementation based on the published gateway and cc3200tool formats;
no unlicensed gateway JavaScript or firmware is vendored.

## Verification and remaining acceptance

Run:

```sh
node --test scripts/cc3200-protocol.test.mjs
go test -buildvcs=false -race ./internal/cc3200 ./internal/app
GOMAXPROCS=2 go test -buildvcs=false ./internal/cc3200 -run '^$' \
  -fuzz FuzzInspect -fuzztime 3s
```

Set `TEDDYCLOUD_CC3200_NWP_LOADER` to a local copy of the pinned `rbtl3100s.dll`
when running the Node suite to include its ninth test: the exact official loader
bytes are uploaded to simulated SRAM and checked before simulated execution.
Without this optional file, the eight self-contained tests run and the loader
test explicitly skips. No target code is actually executed by the simulator.

Automated coverage includes FAT copy/metadata selection, anonymous/mirrored
preservation, malformed allocation and path rejection, filesystem/S3 backup
reload, certificate conflicts, original/preloader checks, authentication and
origin checks. Simulated serial tests cover fragmented headers, ACK/checksum
failure, timeout/disconnect, multi-chunk file verification, changed-device
preflight, preserving an existing original, and stopping before the final image
on an earlier verification failure. CI runs the Node suite and FAT parser fuzz
smoke alongside the existing Go checks.

Native ESP32-C3/CC3200 checks are recorded in [native tool validation](cc3200-tool.md).
The browser workflow has not been tested on hardware. Browser rendering could not
be visually checked because the computer-use environment exposes no browsers. HTTP asset,
plugin metadata and authentication checks are automated; USB permission prompts,
gateway firmware installation, native Web Serial operation, actual FAT layouts,
browser read/write verification and a successful post-install boot remain hardware
acceptance tasks. In particular, an image rejected by the structural checks must
be investigated rather than bypassing validation. This feature does not close
the project's existing physical-device acceptance gates.

### Use a different box connection port (issue #531)

In step 4 of `/tools/cc3200`, choose a port (default **10443**) and download
`altPort.json`. This generator works without a connected box. It is also available
as `GET /api/cc3200/altPort?port=10443` on the management listener, with the same
management authentication as the other tools. It returns a download directly;
there is no additional persistent storage.

For original firmware **3.1.0 BF2, 3.3.0 or 3.4.0**, copy the JSON to
`revvox/boot/patch/altPort.json` on the microSD. Add `altPort` to the patch list of
the selected original firmware in `revvox/boot/ngCfg.json`. HackieboxNG applies
these patterns at boot to the original image. Both API and RTNL use the chosen
port with TLS. CC3200 ports are **1–65535**. Disable the patch, or generate it for
443, to use the original port again. This does not change teddyCloud's listener.

Forward the chosen public TCP port to teddyCloud's box TLS listener (`-box`, or
`core.server.https_api_port`). Use TCP/TLS passthrough when retaining box certificate
authentication. The boxes send no SNI; route by destination port or IP address.
Keep the management web listener (normally 8443) separate.

ESP32 firmware **v5.231.0–v5.237.0** already has the corresponding firmware API
`GET /api/esp32/patchFirmware?filename=FILENAME&port=10443` and CLI
`--esp32-portpatch FILE --port 10443`. Its supported range is **1–32767**.
The patch keeps TLS enabled, repairs image checksums and SHA-256, and restores
original connection instructions when port 443 is selected. The source file is
preserved. No CC3235 port patch is provided by issue #531.

The CC3200 generator is checked against the published HackieboxNG v0.9.3d
port-10443 patch and exhaustively tested for Thumb MOVW immediate encoding.
ESP32 tests check multiple function copies, checksum validity, restoration and
unsupported-image preservation. Physical box and browser visual acceptance
remain pending; the issue author's device testing is not testing of this Go port.

References: [issue #531](https://github.com/toniebox-reverse-engineering/teddycloud/issues/531),
[HackieboxNG generator](https://github.com/toniebox-reverse-engineering/hackiebox_cfw_ng/blob/master/sd-bootloader-ng/tools/altport.py).
