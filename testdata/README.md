# Fixture provenance

`compatibility.json` contains authored localhost-only requests, no captures or
credentials. Time comparison allows exactly two seconds to account for sequential
requests. No fields or file effects are silently ignored.

`go run ./cmd/fixturegen` generates a new disposable `.cache/oracle-runtime/` with
synthetic RSA identities, two box overlays, test UID metadata and one-second stereo
440/660 Hz tones. Keys have 24-hour lifetimes and mode 0600; they are never committed.
Do not supply real deployment paths to fixture tools. Existing output is refused.

Generate the reference multi-track TAF after `scripts/reference.sh build`:

```sh
.cache/teddycloud-reference/bin/teddycloud \
  --base_path "$PWD/.cache/oracle-runtime/" \
  --encode "$PWD/.cache/oracle-runtime/data/library/synthetic.taf" \
  "$PWD/.cache/oracle-runtime/tone1.wav" "$PWD/.cache/oracle-runtime/tone2.wav"
```

No upstream sandbox audio, firmware image, or manufacturer certificates are copied.
Plugin discovery is checked by `scripts/plugins-differential.py`: the candidate
must preserve the reference list and add exactly its built-in CC3200 tool. Other
static cases retain exact comparisons; user management is not a plugin.
Firmware uploads use the verified management TLS listener: the pinned C raw HTTP
socket buffer can parse binary body bytes as headers when they arrive together.
The content comparison does not validate that upstream raw HTTP buffering bug.
The MQTT replay registers the box before enabling the client and waits for the
discovery burst and QoS handshakes to finish before sending commands. This avoids
known registration/TX lock stalls in the pinned C client. It asserts the two
updated Go management-port display names explicitly; other discovery fields and
state values remain exact comparisons.

Future captures must be replaced with synthetic payloads or reviewed for client
certificates, private keys, authorization, box IDs/IPs, tag identifiers and audio
before inclusion. Record the baseline commit and generator/recording command.

The comparison runner supports exact bytes, JSON object ordering normalization,
protobuf wire-field ordering normalization (nested/packed bytes remain exact),
explicit epoch tolerance and bounded SSE event sequences. Schema-aware protobuf
comparisons are a later domain-binding check. Cases can select response headers
and relative files to compare SHA256 after the request; symlink/root escapes fail.
All other normalization requires a documented new case policy.
