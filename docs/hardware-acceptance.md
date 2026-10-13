# Physical-device acceptance record

Status on 2026-10-09: the user identified a CC3200 Toniebox, but no box is
currently available. Firmware and isolated deployment addresses are unknown.
No physical handshake, playback, conversion, live, OTA or MQTT result is claimed.
The implementation remains a development backend; synthetic TLS/TAF tests do
not establish device support.

Use a separate deployment containing a copied snapshot and the box's existing
private certificate material. Keep private keys, tag passwords, audio recordings
and packet captures out of the repository. Run one backend against that copy at
a time, and retain the original snapshot for rollback. Synthetic fixture
certificates are only for automated tests.

Record these facts before testing:

- Chip/model, exact firmware version, box identifier and client-certificate issuer
  common name; redact identifiers in shared reports.
- Reference commit, Go build version/source digest, host architecture and FFmpeg
  version when conversion is tested.
- Isolated listener addresses and the box's DNS routing; record the effective
  cloud and local OTA policies.
- Snapshot identifier and original configuration/certificate hashes.

Run the same steps against C and Go, recording the observed result separately:

| Check | Required evidence | Current result |
| --- | --- | --- |
| Cold TLS connection | no-SNI TB1 certificate, negotiated version/suite, client identity | pending |
| Reconnect/resumption | reconnect after idle and backend restart, preserved server identity | pending |
| Claim/freshness | claim result and per-box speaker/headphone/slap/LED settings | pending |
| Local playback | the same known-good TAF plays from start to finish | pending |
| Cached playback | download under permitted cloud policy, then playback with cloud disabled | pending |
| Seek/chapters | resume/range requests, chapter transitions and end of content | pending |
| Converted audio | two-source conversion, chapter transition and skip duration | pending |
| Live stream | startup buffer, interruption, reconnect and stop scoped to this box | pending |
| OTA policy | local delivery disabled by default; selection without installing a new image | pending |
| MQTT settings | valid command persists overlay, applies on next freshness; invalid command has no effect | pending |
| Isolation | second box's state/settings/stream remain unchanged | pending |
| Rollback | stop Go, restore copied snapshot, resume the reference backend successfully | pending |

Actual firmware installation is a separate action. Selection/download policy can
be checked while keeping the device on its recorded firmware. Record timestamps,
statuses, sanitized event sequences and artifact hashes; a successful automated
replay must not be entered as a physical result. Tasks 3.8, 5.7 and 8.7 remain
pending until the required evidence exists.

## CC3200 ESP32-C3 flashing acceptance (pending)

Use an isolated, recoverable CC3200 and follow [the browser workflow](cc3200-flashing.md).
Record the ROM revision, gateway firmware build and browser/OS versions. Verify
USB configuration readback, SOP2/reset and NWP handover, byte-identical double
flash/file dumps, named-file fallback and unnamed/mirrored allocation backups.
Keep an independent original backup before installation. Verify certificate
import, preservation of `/sys/pre-img.bin`, and read-back equality of the CA and
preloader. Remove programming wiring, boot from the prepared SD card and confirm
original firmware and teddyCloud playback. Check reconnect/cancel behavior during
reads; do not deliberately interrupt destructive writes on the only available
box. The limited native CLI checks from 2026-10-13 are recorded in
[native tool validation](cc3200-tool.md); complete browser installation, SD boot
and playback acceptance remain pending.
