# License and asset inventory

Project code: GPL-2.0-only; the verbatim upstream GPLv2 text is in `LICENSE`.
`NOTICE` credits the teddyCloud community. Baseline and component revisions are
recorded in `reference-inventory.json`. This is an inventory of checked local
source notices; it does not relicense third-party code or assets.

| Component | Observed notice | Location in pinned oracle | Use in Go project |
| --- | --- | --- | --- |
| teddyCloud | GNU GPL version 2 | LICENSE | Behavioral reference, attributed schema/logic derivation |
| Cyclone Common/Crypto/SSL/TCP | GPL-2.0-or-later in source headers | cyclone/*/LICENSE and source headers | Oracle only; not linked or bundled |
| cJSON | MIT, Dave Gamble and contributors | cJSON/LICENSE | Oracle only |
| libogg | BSD-style three-clause, Xiph.Org | ogg/COPYING | Oracle only |
| libopus | BSD-style three-clause plus patent/IPR notices | opus/COPYING, LICENSE_PLEASE_READ.txt | Oracle only; retain notices if ever redistributed |
| protobuf-c | BSD-2-Clause, Dave Benson and authors | src/proto/protobuf-c.c header | Oracle only; generated Go bindings use their own runtime |
| FatFs | ChaN permissive one-condition notice | fat/LICENSE.txt, fat/source/ff.c | Oracle only; no C filesystem code copied |
| emsdk | Not populated or reviewed | .gitmodules pinned revision | Excluded WASM toolchain; not distributed |
| official frontend | No top-level LICENSE or package.json license field at pinned revision | teddycloud_web@636a8d65043f6c6e322f76bcb11e958725159e96 | Downloaded and built in Docker; upstream distribution license remains unresolved |
| frontend fonts | SIL OFL 1.1 noted, per-family notices and licenses embedded in font binaries | src/assets/fonts/teddystudio/LICENSES.md | Docker assets; notice retained in image |
| frontend opening guide | CC BY-NC-SA 3.0 referenced; Tobias Isakeit/iFixit attribution | frontend README Attribution and OpenBoxGuidePage.tsx | Docker assets; original attribution retained |
| frontend logo/icons | Flaticon links, license terms not included in upstream README | upstream README Attribution | Docker assets; original attribution retained, no replacement rights assumed |
| frontend flags | country-flag-icons v1.5.18 attribution | frontend README Attribution | Docker assets; original attribution retained |
| npm dependencies | Per-package license metadata and available LICENSE/LICENCE/COPYING/NOTICE files | lockfile-pinned installed dependencies | Docker build records metadata and copies notices; collection is not a legal compatibility audit |
| FFmpeg | External executable, not vendored; build-dependent license | installed executable's `-version`/`-L` | Optional native runtime process; Debian package and dependency notices retained in ffmpeg image |
| Go standard library | Go distribution LICENSE | Go toolchain | Linked according to Go license; include notice in releases |

Docker builds fetch the revision recorded in `frontend/upstream-revision`, apply
the project's overlay, and include compiled frontend assets. Native binary
archives still omit those assets. The image preserves the upstream README,
font notices, source revision, package manifests/lockfile and installed npm
package notices under `/usr/share/doc/goteddycloud/frontend` using
`scripts/frontend-notices.py`. This records provenance and available notices;
it does not resolve the missing upstream top-level license or establish
redistribution permission. Do not infer frontend licensing from the backend's
GPL text. Manufacturer audio, firmware and client keys remain excluded.

Any added Go module must be pinned in go.mod/go.sum and added to this inventory
before release packaging. Frontend redistribution terms and a full transitive
asset license audit remain unresolved, independently of the implemented Docker
packaging.

Added Go module: `google.golang.org/protobuf v1.36.12`, observed BSD-3-Clause
notice, Copyright (c) 2018 The Go Authors. Module LICENSE must accompany packaged
binaries; Go toolchain notices are supplied by the distribution's source package.
The comparison runner currently uses its wire decoder; it does not link protobuf-c.

Go Opus packet padding was cross-checked against libopus's repacketizer framing.
`third_party/libopus-COPYING` retains the Xiph.Org/Jean-Marc Valin attribution and
BSD-style notice. No C encoder is linked. Generated bindings use protoc 36.2 and
protoc-gen-go 1.36.12, pinned by scripts/generate-proto.sh with archive SHA256.

Storage tests use `github.com/johannesboyne/gofakes3` at
`v0.0.0-20260208201424-4c385a1f6a73` (MIT). Its notice is in
`third_party/gofakes3-LICENSE`. Its in-memory backend also links
`github.com/ryszard/goskiplist` (Apache-2.0), whose notice is retained in
`third_party/goskiplist-LICENSE`. These are test-only dependencies and are not linked into the application. Test executables are not release artifacts. The S3 REST client and
Signature V4 implementation use the Go standard library. AWS SDK v2 is not linked
because its Apache-2.0 license conflicts with this project's GPL-2.0-only license.
The signing regression vectors come from the AWS S3 Signature V4 documentation.

Management ACME support links `golang.org/x/crypto v0.58.0` (`acme/autocert`),
`golang.org/x/net v0.60.0` and `golang.org/x/text v0.43.0` (IDNA handling).
Their Go Authors BSD-3-Clause notices are retained as `third_party/x-crypto-LICENSE`,
`third_party/x-net-LICENSE` and `third_party/x-text-LICENSE`, and copied into release
packages. Versions are pinned in go.mod/go.sum. The tool-only x/tools dependency
is updated to v0.51.0 by module version selection; it is not linked into the server.

The native CC3200 tool uses `go.bug.st/serial v1.8.0` and
`golang.org/x/sys v0.49.0` (BSD-3-Clause), with notices retained in
`third_party/go-serial-LICENSE` and `third_party/x-sys-LICENSE`. These are linked
into the native CLI and the integrated server subcommand. Port enumeration uses
the basic serial API and does not require the macOS cgo-only USB metadata API.
The generated `cc3200-wasm-exec.js` comes from Go 1.26.8; its Go Authors notice
is preserved and `third_party/Go-LICENSE` accompanies release/container packages.

The protocol port is based on cc3200tool commit
`c2332898a1ededc0bdd01ec279b23366d213f81e`, GPL-2.0-or-later, used under version 2.
Allterco Robotics and Team RevvoX attribution is retained in NOTICE. Manufacturer
loader/firmware binaries remain user-supplied and are excluded from standard
release/container builds and Go/WASM browser assets. A local native build can
embed the supplied stock loader using `scripts/build-cc3200-with-loader.py`.
The binary blob is Git-ignored; its redistribution license is unconfirmed.
The GPL license of the upstream Python tool is not treated as proof of rights
to redistribute the TI blob. The BSD license in TI's embedded-programming
example package covers that package, which does not contain `rbtl3100s.dll`.
The separate UniFlash v3 license includes restrictions on object-code distribution;
see [TI's UniFlash licensing information](https://software-dl.ti.com/ccs/esd/uniflash/docs/uniflash.html).

Loader license recheck (2026-10-13): the complete upstream Git history identifies
[commit e03bb3ad3d4df3d1a8ec0ec94de72a340a7c2a09](https://github.com/toniebox-reverse-engineering/cc3200tool/commit/e03bb3ad3d4df3d1a8ec0ec94de72a340a7c2a09)
as the update from TI UniFlash **3.4.1.00012**. Its `rbtl3100s.dll` is byte-identical
to the current pinned 49,588-byte blob, SHA-256
`2c9a05c469389446e05871a88e84ca3994bf3d6a6785155135db21d33aa63cc9`.
This establishes TI provenance, not a GPL grant for the blob.

TI's linked [UniFlash v3 license archive](https://software-dl.ti.com/ccs/esd/uniflash/docs/license/UniFlashLicensing.zip)
was downloaded again and matches the reviewed `287163_UniFlash.DOC`, SHA-256
`72eded755f1010954d6a481950a10b0b060886f43cc98b42a1eda013f55abd6a`.
Section 1(a)(ii) limits object-code redistribution to target applications
executing on TI devices; section 1(f)(iv) restricts other third-party distribution;
section 1(f)(vii) restricts combinations that subject TI materials to open-source
license obligations. No loader-specific redistribution exception was found in
the reviewed upstream history or TI license materials. Whether a host binary
carrying a separate target-only blob qualifies under the target-application grant
is not resolved by those sources. Therefore public release embedding remains
uncleared; changing goTeddyCloud's license to GPLv2 does not resolve that question.
The optional local embedding build and explicit `--loader` override remain
available. A loader-specific TI grant or applicable permissive license is needed
before enabling the blob in standard release builds.

Native command parsing uses `github.com/alecthomas/kong v1.16.1` (MIT); its
notice is retained in `third_party/kong-COPYING`. Terminal capability detection
uses `golang.org/x/term v0.47.0` (BSD-3-Clause), with its notice in
`third_party/x-term-LICENSE`. Both notices accompany release/container packages.
These modules are linked into the native and integrated CC3200 commands, not
the browser WebAssembly module.
