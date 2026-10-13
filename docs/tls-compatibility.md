Current policy: box client certificates are mandatory and pinned; session tickets are disabled. Earlier optional-certificate feasibility results below are historical.

Current develop policy: box/embedded MQTT session tickets are disabled; web TLS remains separate. See [development port](develop-port.md). The following baseline feasibility record is historical.

# TLS feasibility: legacy signature negotiation

Baseline: `78c3b38e36f665d3a5fffe907f73a02725176377`.
Local toolchain: `go1.26.8-X:nodwarf5 linux/amd64`.
All probes use generated 2048-bit RSA test identities, loopback endpoints and
TLS 1.2; no physical box or manufacturer identity is involved.

## Observed results

| Check | Pinned C | Go default prototype | Go diagnostic with `GODEBUG=tlssha1=1` |
| --- | --- | --- | --- |
| ECDHE-RSA/AES128-CBC, RSA/SHA256 ClientHello | server flight accepted, signature 0x0401 | server flight accepted, signature 0x0401 | not needed |
| Same cipher, RSA/SHA1-only ClientHello | server flight accepted, signature 0x0201 | fatal handshake_failure alert 40; no shared certificate signature algorithm | server flight accepted, signature 0x0201 |

These raw ClientHello tests inspect the complete first server flight, including
ServerKeyExchange and ServerHelloDone. They **do not complete** an authenticated
TLS handshake, nor establish a hardware support claim. SHA1-only clients are a
reproducible difference from the reference's accepted negotiation behavior.

Full synthetic Go handshake tests separately pass for no-SNI TB1 selection,
SNI TB2 selection, optional presented/absent client certificates, RSA key-exchange
CBC, ECDHE GCM/CBC and TLS 1.2 session resumption. Root verification is enabled in
these tests with the synthetic fixture roots; no InsecureSkipVerify is used.
Fragmented RTNL frames, multiple frames, zero/oversize/truncated frames and byte
preservation during HTTP/binary detection also pass automated tests.

## Reproduce

Build/generate fixtures and launch the pinned C oracle as described in README.
In separate terminals:

```sh
go run -buildvcs=false ./cmd/tlsprobe
GODEBUG=tlssha1=1 go run -buildvcs=false ./cmd/tlsprobe -listen 127.0.0.1:19445
```

Then run each signature mode `sha256`, `sha1` (or `absent`) against each endpoint:

```sh
go run -buildvcs=false ./cmd/clienthelloprobe -address 127.0.0.1:18444 -signature sha1
go run -buildvcs=false ./cmd/clienthelloprobe -address 127.0.0.1:19444 -signature sha1
go run -buildvcs=false ./cmd/clienthelloprobe -address 127.0.0.1:19445 -signature sha1
```

Expected exit statuses for SHA1: 0, 1, 0. The failed default Go prototype logs
`tls: peer doesn't support any of the certificate's signature algorithms`.
The installed OpenSSL CLI rejected the requested SHA1-only sigalgs option before
sending a ClientHello, so it was not usable as evidence; the authored probe sends
an explicit signature_algorithms extension and positive SHA256 control.

## Selected policy

`tlssha1=1` is process-wide, so the box TLS listener runs in a supervised Go
child of the same executable. Only the child receives `GODEBUG=tlssha1=1`.
The parent explicitly selects `tlssha1=0` for web/upstream TLS, preserving other
GODEBUG settings. The child receives certificates over its private stdin pipe
and forwards requests to a token-protected loopback endpoint; it strips forged
internal identity headers. Closing the parent pipe cancels the child, with a
bounded kill-and-reap fallback. No C crypto stack is linked.

`internal/runtime` integration tests exercise all three listeners, header spoofing,
parent/child environment separation, shutdown during an incomplete handshake,
and cleanup after parent or child bind failures. `internal/certificates` tests
verify RSA TB1 and ECDSA TB2 generation, trusted server chains, restart preservation,
disabled generation, and refusal to overwrite invalid existing material.

The current box identity policy requires an exact locally imported client
certificate and private-key proof during TLS. It checks the pin again on each
HTTP request and RTNL frame; issuer names alone are never trusted. This is an
intentional security deviation from the pinned C oracle's name-based policy.
See [box certificate trust and migration](box-certificate-auth.md).
Web requests never acquire a box identity through forwarded headers.

Complete SHA1-only handshake and physical-device interoperability remain separate
validation gaps. The target identified by the user is CC3200; no box is currently
available and no firmware version has been supplied.

## Actual TLS/RTNL differential replay

`scripts/rtnl-differential.py` sends the same synthetic volume frame using
one-byte TLS application writes to both C and Go box listeners, with synthetic
client certificates and server verification enabled. Four decoded SSE events
match exactly: keep-alive, rtnl-raw-log2, VolumeLevel and VolumedB. The reference
uses its HTTP line reader for initial binary detection, so optional field9 ends
with LF in this fixture. Without any LF, the reference waits rather than dispatching
the first frame; Go dispatches complete length-framed messages immediately.
This is synthetic transport evidence, not CC3200 hardware verification.

Development cipher limitation: the pinned C oracle accepts RSA AES256-CBC-SHA256 (`0x003d`), which Go crypto/tls does not implement. AES128-CBC-SHA256 is enabled on the isolated box listener. The unsupported AES256 case is retained as a strict expected failure by `scripts/develop_pytest_policy.py`; all other simulator assertions remain mandatory. Normal simulated CC3200 negotiation succeeds. This is a compatibility deviation, not physical-device acceptance.

## TI CC3200 SDK cipher coverage (2026-10-09)

Scope: the ten distinct TLS suites in the TI CC3200 SDK v1.3.0 release notes,
section 7.2, pages 10–11:
https://software-dl.ti.com/ecs/CC3200SDK/1_3_0/exports/CC3200-SDK_Release_Note.pdf
This is SDK capability, not a captured Toniebox firmware requirement.

| TI TLS suite | Box listener at TLS 1.2 |
| --- | --- |
| TLS_RSA_WITH_RC4_128_SHA | Intentionally disabled; forced handshake rejected |
| TLS_RSA_WITH_RC4_128_MD5 | Excluded: RC4 is prohibited; also absent from Go crypto/tls |
| TLS_RSA_WITH_AES_256_CBC_SHA | Enabled; forced handshake passes |
| TLS_DHE_RSA_WITH_AES_256_CBC_SHA | Not implemented by Go crypto/tls |
| TLS_ECDHE_RSA_WITH_RC4_128_SHA | Intentionally disabled; forced handshake rejected |
| TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA | Enabled; forced handshake passes |
| TLS_RSA_WITH_AES_128_CBC_SHA256 | Enabled; forced handshake passes |
| TLS_RSA_WITH_AES_256_CBC_SHA256 | Not implemented by Go crypto/tls |
| TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA256 | Enabled; forced handshake passes |
| TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA256 | Enabled; forced handshake passes with ECDSA certificate |

Five of the ten SDK suites are enabled and verified. All RC4 suites are
intentionally excluded at the user's request, including any future RC4-MD5
support. Tests force RSA, ECDHE-RSA and ECDHE-ECDSA RC4 handshakes and require
rejection. ECDHE-RSA/ECDHE-ECDSA AES128-CBC-SHA256 remain enabled. Web and
upstream TLS configurations are unchanged. ECDSA negotiation requires an ECDSA
server identity; normal generated box certificates are RSA. The test creates
both key types and verifies certificate trust, selected suite and application
bytes. Evidence: `TestTISDKCipherHandshakes`, race check in
`.cache/ti-cipher-tests.log`.

The listener remains TLS 1.2 only. This does not add SSLv3 or TLS 1.0/1.1 from
the SDK's historical protocol list. Full SDK suite coverage would require a
separate TLS engine or a maintained Go TLS fork for DHE-RSA AES256-CBC-SHA and RSA AES256-CBC-SHA256;
putting unsupported numeric IDs into CipherSuites cannot enable them. No such
engine or fork has been added. Physical-device acceptance remains pending.
The versioned 20261009-develop archives predate this cipher expansion.
