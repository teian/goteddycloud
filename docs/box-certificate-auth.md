# Box client certificate trust

The box listener accepts only exact client certificates installed by an
administrator. TLS requires the certificate and proof of its private key before
HTTP or RTNL is dispatched. Subject/issuer names, a familiar MAC, membership in a
manufacturer CA, and `core.allowNewBox=true` do not authorize an unknown certificate.

The server compares the SHA-256 fingerprint of the complete DER leaf certificate,
binds its common name to the box overlay, checks its validity dates and permits
client authentication/digital signatures. Missing/corrupt selections, expired or
not-yet-valid certificates, server-only EKUs and unknown critical extensions fail
closed. PEM and DER imports work. An explicitly imported self-signed or legacy
manufacturer certificate can be pinned without trusting its whole issuing CA.
Certificate-chain trust and revocation services are not substituted for this
local allowlist: removing/replacing a pin or disabling `toniebox.api_access` revokes
access locally.

## Import and migrate

Import the certificate the box actually presents before allowing it to connect.
For an existing overlay, select its `core.client_cert.file.crt` (or inline
`core.client_cert.data.crt`). For firmware using a replacement client identity,
select `core.client_cert_fake.file.crt`/`.data.crt` as well. These are independent,
explicitly trusted identities; a newly issued certificate with the same key or
common name still needs a new import.

Firmware certificate extraction already writes the conventional locations under
`core.certdir` (normally `certs/client`):

```text
certs/client/<lowercase-box-id>/client.der
certs/client/<lowercase-box-id>/client.fake.der   # optional replacement identity
```

Explicit overlay selections take priority. Without one, these per-box files take
priority over global client credentials. Global configured credentials can only
match the box named in that exact certificate; they do not authorize other IDs.
The original certificate's `b'001122334455'` common name maps to `001122334455`.
MAC spelling is case-insensitive when looking up an existing overlay; ambiguous
duplicate overlays fail closed. New MAC overlays use uppercase hexadecimal IDs.
Do not replace certificate files with material obtained from an untrusted client.

An unregistered box can create an overlay only if its certificate is already
imported and `core.allowNewBox=true`; the existing 16-overlay registration limit
still applies. Set `core.allowNewBox=false` to require existing overlays too.
Handshake verification itself never creates overlays. An overlay or previous
successful connection alone is insufficient evidence of trust.

Authentication is mandatory even in older configurations containing
`core.boxCertAuth=false`. That legacy setting remains readable for migration,
but cannot disable the certificate check. Deployments which previously relied
on automatic enrollment by issuer name must import their box certificates first.
Only public certificates are needed for inbound pinning. The corresponding
CA/client certificate/private key are separately needed for upstream cloud access.

## Changes while running

The isolated TLS worker asks the parent to validate each leaf using a bounded,
token-authenticated loopback request. Failure or timeout rejects the handshake.
It derives and forwards the fingerprint from the authenticated TLS connection,
replacing incoming internal identity headers. No configuration or certificate
private keys are exposed through this endpoint.

Trust and access are checked again before each HTTP request or RTNL frame. A
revoked certificate cannot continue using an established keep-alive connection;
RTNL closes on the next rejected frame. Session tickets remain disabled, so a new
connection cannot resume an old trust decision. A response already authorized
and streaming is not interrupted mid-response.

## Legacy TLS scope

The box listener remains TLS 1.2 with its existing AES-GCM/AES-CBC and RSA/ECDHE
compatibility suites. SHA-1 signature compatibility remains confined to the box
worker. RC4, 3DES and TLS 1.0/1.1 remain disabled. Pinning prevents unauthorized
client identities; it does not add forward secrecy to static RSA or modernize
CBC. Web HTTPS and upstream server-certificate verification remain separate.

The simulator profiles do not establish the minimum suites required by actual
Toniebox firmware. Physical CC3200/CC3235/ESP32/TB2 captures remain necessary before
removing additional compatibility suites. See [TLS compatibility](tls-compatibility.md).
