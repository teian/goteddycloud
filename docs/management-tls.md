# Management HTTPS certificates

`-httpsecure` serves the Web UI and management API. It can use a manually supplied
PEM certificate/key or obtain and renew certificates automatically with ACME.
These options apply only to management HTTPS. The box listener retains
`core.server_cert` / `core.server_cert_tb2` and its separate TLS policy.

With no new options configured, management HTTPS continues using the existing
local `core.server_cert` pair, generated if missing according to the existing
`internal.autogen_certs` policy. Supplying manual material or enabling ACME
replaces this management default without replacing either box identity.

## Manual certificates

Set both paths, relative to the deployment base or absolute:

```sh
bin/goteddycloud -base_path /path/to/deployment \
  -config-set 'core.httpsecure.certificate_file=certs/web/fullchain.pem' \
  -config-set 'core.httpsecure.key_file=certs/web/privkey.pem'
bin/goteddycloud -base_path /path/to/deployment \
  -http off -httpsecure :8443 -box :443
```

Use PEM full-chain and private-key files from your CA. Paths use the selected
filesystem/S3 backend. Certificate and private key must match; incomplete,
invalid or mismatched pairs fail startup instead of generating a replacement.
Restart after rotating manual files. Use `-validate` to check the configuration
and existing manual material without generating certificates, writing a cache or
contacting an ACME provider. Existing deployment validation also requires the
normal box material and deployment directories.

## Automatic ACME / Let's Encrypt

The configuration defaults to disabled and to Let's Encrypt's production
ACME v2 directory. Start with its staging directory when evaluating:

```sh
bin/goteddycloud -base_path /path/to/deployment \
  -config-set 'core.httpsecure.acme.domains=teddy.example.net' \
  -config-set 'core.httpsecure.acme.email=admin@example.net' \
  -config-set 'core.httpsecure.acme.directory_url=https://acme-staging-v02.api.letsencrypt.org/directory' \
  -config-set 'core.httpsecure.acme.accept_terms=true' \
  -config-set 'core.httpsecure.acme.enabled=true' \
  -config-set 'core.host_url=https://teddy.example.net'
bin/goteddycloud -base_path /path/to/deployment \
  -http :80 -httpsecure :443 -box :10443
```

Substitute your public domain and contact email. Set `accept_terms=true` only
when accepting your configured provider's terms. Remove/reset both manual path
settings before enabling ACME; the modes are mutually exclusive. Multiple allowed
domains are separated by spaces. Use lowercase ASCII/Punycode DNS names, without
ports or wildcards. IP addresses and single-label names are rejected.

DNS must point to the server, and the CA must reach at least one challenge:

- **HTTP-01:** public TCP port 80 must reach the management HTTP listener. Only
  `/.well-known/acme-challenge/…` is intercepted; other HTTP requests continue to
  the management API/Web UI and its normal authentication policy.
- **TLS-ALPN-01:** public TCP port 443 must reach the management HTTPS listener
  with its ACME ALPN protocol preserved. It cannot be answered by the box listener.

The sample gives port 443 to management HTTPS and port 10443 to boxes; boxes need
the appropriate destination-port patch. Alternatively use separate IPs or expose
management HTTPS on another port with HTTP-01 available on public port 80. Local
listener ports can differ if forwarding maps public challenge ports appropriately.
DNS-01 and wildcard issuance are not implemented. If HTTP is disabled, only
TLS-ALPN-01 is available.

A certificate is requested on the first TLS handshake for an allowed domain,
which may take longer than subsequent handshakes. Startup readiness means the
listener is bound; it does not mean the CA has issued a certificate. Issuance
failure fails the handshake; no untrusted local certificate is substituted.
The ACME manager renews certificates automatically while the server is running.
Configuration changes require restart. Server shutdown cancels ACME network I/O.

Once staging works, switch to production and restart:

```sh
bin/goteddycloud -base_path /path/to/deployment \
  -config-set 'core.httpsecure.acme.directory_url=https://acme-v02.api.letsencrypt.org/directory'
```

Staging certificates are not browser-trusted. Account keys and certificate chains
are cached under `core.httpsecure.acme.cache_dir` (default `certs/acme`, relative
to the deployment base). The cache uses the selected filesystem or S3 backend,
atomic writes, opaque hashed filenames and private filesystem permissions.
Separate directory URLs get separate cache namespaces. Preserve this private
cache across restarts; it contains account and certificate private keys. The
existing storage single-writer deployment model applies to the ACME cache too.

| Setting | Default | Meaning |
| --- | --- | --- |
| `core.httpsecure.certificate_file` | empty | Manual PEM certificate chain |
| `core.httpsecure.key_file` | empty | Manual PEM private key |
| `core.httpsecure.acme.enabled` | false | Enable management ACME issuance/renewal |
| `core.httpsecure.acme.accept_terms` | false | Accept configured CA terms |
| `core.httpsecure.acme.domains` | empty | Explicit domain allowlist |
| `core.httpsecure.acme.email` | empty | Optional account contact email |
| `core.httpsecure.acme.directory_url` | Let's Encrypt production directory | ACME v2 HTTPS endpoint |
| `core.httpsecure.acme.cache_dir` | `certs/acme` | Persistent backend cache root |

## Reverse proxy deployments

If Nginx, Caddy or Traefik terminates browser TLS, let that proxy manage its public
certificate and use `-httpsecure off` with a private HTTP backend. Disabled
management HTTPS does not construct an ACME manager or contact the CA, even when
ACME remains enabled in configuration. See [proxy examples](reverse-proxy.md).

The implementation uses Go's
[autocert manager](https://pkg.go.dev/golang.org/x/crypto/acme/autocert).
Public validation requirements are described by
[Let's Encrypt's challenge documentation](https://letsencrypt.org/docs/challenge-types/).
Tests cover manual pairs, an HTTP-01 simulated issuer/CSR flow and cache reuse on
filesystem/S3, domain restrictions, independent management TLS, and read-only
configuration validation. No live Let's Encrypt issuance is claimed.
