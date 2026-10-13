# Upstream Boxine/Tonie cloud access

The global Boxine indicator reads `/api/cloud/status`. A successful probe returns
HTTP 200 with `{"enabled":true,"available":true}`. The local server and clock can
work while this upstream indicator is offline.

Enable `cloud.enabled` and import your box's original CA, client certificate and
matching private key. The global selection is `core.client_cert.file.ca`, `.crt`
and `.key`; defaults are `certs/client/ca.der`, `client.der` and `private.der` under
the deployment base directory. PEM and DER material are supported. The certificate
must be valid and authorized by Boxine, and the upstream must be reachable.

A box can use its own overlay identity for forwarding. The global indicator probes
with the global identity, so a working box-specific identity alone does not prove
that the global probe will succeed. The certificate used for inbound box pinning
and the original identity required by Boxine may differ when firmware uses a
replacement certificate. See [inbound box certificate trust](box-certificate-auth.md).
Web HTTPS/ACME certificates do not authenticate the server to Boxine.

## Server verification and the legacy endpoint

Normal upstream connections use Go's CA-chain and SAN hostname verification with
TLS 1.2 or newer. The default TB1 endpoint `prod.de.tbs.toys:443` currently presents
a server certificate with that exact Common Name and no SAN extension. Go rejects
such a certificate through its default hostname verifier.

Only this exact configured endpoint supports legacy Common Name matching. The
custom verifier first tries normal verification, then permits the exact Common
Name only if no SAN extension exists. It still verifies the complete original
chain against the configured CA, its signatures, validity and server-auth EKU.
Wildcard names, wrong names, conflicting SANs, unknown CAs, invalid signatures,
expired certificates and unknown critical extensions fail. Legacy fallback also
refuses CA chains with name constraints, preventing a Common Name from bypassing
SAN-based constraints. Custom endpoints and the TB2 endpoint keep normal hostname
verification. No insecure-verification option is added.

This compatibility is independent of the box worker's legacy SHA-1 signatures
and CBC/static-RSA suites. The real upstream verification on 2026-10-13 succeeded
with TLS 1.3 / `TLS_AES_256_GCM_SHA384`, HTTP 200 and a valid `/v1/time` response.
The application status probe also returned `enabled:true, available:true` using
the provided original credentials in an isolated configuration. Original key
files and deployment settings were unchanged. The network test uses private
credentials locally and is deliberately excluded from automated tests and release
artifacts; automated negative tests use generated identities only.
