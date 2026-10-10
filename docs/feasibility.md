# Milestone 1 feasibility gate

Baseline C commit: `78c3b38e36f665d3a5fffe907f73a02725176377`.
All evidence below is synthetic and local; CC3200 hardware is unavailable.

The independent Go backend, pinned oracle, exhaustive source inventory and
comparison runner are working. The reference checkout remains unchanged.

- TLS: modern complete handshakes, optional certificates, SNI identity selection,
  CBC/GCM and resumption pass. SHA1-only first-flight negotiation differs with
  strict Go defaults. The accepted design isolates `tlssha1=1` in a supervised
  box process; the parent stays strict. See [TLS evidence](tls-compatibility.md).
- RTNL: the reference uses four-byte big-endian lengths with the first two bytes
  zero. Go handles fragmentation and HTTP demultiplexing without consuming bytes;
  zero, truncated and over-limit frames fail without affecting another client.
- TAF: external FFmpeg Opus packets can be read and repackaged entirely in Go.
  A two-track Go TAF has verified protobuf framing, SHA1/length, Ogg CRC, chapter
  offsets and aligned 4096-byte blocks. The C library index accepts it as valid,
  and FFmpeg decodes it. Initial one-packet pages inflate size; efficient grouping
  belongs to task 5.1. This does not establish real playback or live-job support.
- Runtime: cgo-free Linux build and Go tests/vet pass. There is no C linkage.

The TLS design/specs explicitly select process isolation, resolving the Go policy
blocker. RTNL and TAF have demonstrated feasible primitives. Dependent work can
proceed with hardware acceptance, full legacy handshake, production audio grouping,
and live-stream cancellation tracked separately. No device support or replacement
parity is claimed by passing this gate.
