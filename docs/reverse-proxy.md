# Web UI and management API behind a reverse proxy

The Web UI and its management API share listeners. The box API has its own TLS
listener and certificate policy:

| Traffic | CLI address | Persistent port setting | Default |
| --- | --- | --- | --- |
| Web UI and management `/api/…`, HTTP | `-http` | `core.server.http_port` | 80 |
| Web UI and management `/api/…`, HTTPS | `-httpsecure` | `core.server.https_web_port` | 8443 |
| Box `/v1/…`, `/v2/…`, `/v3/…` and binary RTNL, TLS | `-box` | `core.server.https_api_port` | 443 |

Management listeners reject box API routes. The box listener does not serve the
Web UI or management API. `core.server.https_api_port` retains its upstream ID for
configuration compatibility; its UI label explicitly says **box API**. Listener
ports are independent of a box's patched destination port and a proxy's public
ports. Set `core.host_url` to the public Web UI URL when generating links.

For example, let a proxy own public HTTPS port 443 and terminate browser TLS.
Use loopback HTTP for its management backend and a separate box TLS port:

```sh
bin/goteddycloud -base_path /path/to/deployment \
  -config-set 'core.server.trusted_proxies=127.0.0.1/32 ::1/128'
bin/goteddycloud -base_path /path/to/deployment \
  -config-set 'core.host_url=https://teddy.example.net'
bin/goteddycloud -base_path /path/to/deployment \
  -http 127.0.0.1:8080 -httpsecure off -box :10443
```

`-httpsecure off` disables the optional management HTTPS listener when the proxy
terminates TLS. If omitted, it still uses `core.server.https_web_port` (8443 by
default). The previous `-web` flag has been renamed to `-httpsecure`; update
existing startup commands. The persistent setting ID is unchanged.

Point the box at port 10443 using the appropriate firmware/SD patch. Alternatively
keep the box listener on 443 on a different IP, or forward a distinct public TCP
port to its internal TLS port. Preserve box TLS end to end; the supervised box
listener handles device certificates and legacy TLS requirements. Browser TLS
termination and forwarded headers apply only to the management listener.

## Nginx

Use this server configuration (substitute hostname and certificate paths):

```nginx
server {
    listen 80;
    server_name teddy.example.net;
    return 308 https://teddy.example.net$request_uri;
}

server {
    listen 443 ssl;
    server_name teddy.example.net;
    ssl_certificate /etc/nginx/teddy/fullchain.pem;
    ssl_certificate_key /etc/nginx/teddy/privkey.pem;
    client_max_body_size 128m;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_http_version 1.1;
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_buffering off;
        proxy_read_timeout 300s;
    }
}
```

Host preservation includes nonstandard public ports. Disabling response buffering
allows SSE events to arrive promptly. See the official
[Nginx proxy module documentation](https://nginx.org/en/docs/http/ngx_http_proxy_module.html).

## Caddy

Use this `Caddyfile` on the same host as teddyCloud:

```caddyfile
teddy.example.net {
    reverse_proxy 127.0.0.1:8080 {
        header_up X-Forwarded-Proto https
        header_up -X-Forwarded-Host
        flush_interval -1
    }
}
```

Caddy manages the public certificate and HTTP-to-HTTPS redirect automatically
when DNS points to this host and the required public ports are reachable. For a
certificate you manage yourself, add `tls /path/fullchain.pem /path/privkey.pem`
inside the site block. Caddy preserves the original Host header for this HTTP
backend. Removing `X-Forwarded-Host` makes teddyCloud use that original Host,
including its port; the HTTPS site explicitly sets the forwarded scheme.
`flush_interval -1` flushes responses immediately for streaming/SSE.

Validate before reloading:

```sh
caddy validate --config /etc/caddy/Caddyfile --adapter caddyfile
```

References: [Caddy reverse proxy](https://caddyserver.com/docs/caddyfile/directives/reverse_proxy),
[automatic HTTPS](https://caddyserver.com/docs/automatic-https).

## Traefik

This file-provider example runs Traefik on the same host as teddyCloud and uses
an existing public certificate. Put the static configuration in
`/etc/traefik/traefik.yml`:

```yaml
entryPoints:
  web:
    address: ":80"
    http:
      redirections:
        entryPoint:
          to: websecure
          scheme: https
          permanent: true
  websecure:
    address: ":443"
    forwardedHeaders:
      insecure: false

providers:
  file:
    filename: /etc/traefik/dynamic.yml
    watch: true
```

Put the routes, service and certificate in `/etc/traefik/dynamic.yml`:

```yaml
http:
  routers:
    teddycloud:
      rule: "Host(`teddy.example.net`)"
      entryPoints: [websecure]
      service: teddycloud
      middlewares: [teddycloud-origin]
      tls: {}

  middlewares:
    teddycloud-origin:
      headers:
        customRequestHeaders:
          X-Forwarded-Proto: "https"
          X-Forwarded-Host: ""

  services:
    teddycloud:
      loadBalancer:
        passHostHeader: true
        servers:
          - url: "http://127.0.0.1:8080"
        responseForwarding:
          flushInterval: "-1ms"

tls:
  certificates:
    - certFile: /etc/traefik/teddy/fullchain.pem
      keyFile: /etc/traefik/teddy/privkey.pem
```

The HTTPS-only router overwrites the forwarded scheme and clears any supplied
forwarded host. The preserved Host includes any public port.
Immediate response flushing supports streaming/SSE. Substitute certificate paths
and start with `traefik --configFile=/etc/traefik/traefik.yml`. Traefik must be
able to read both configuration files and the certificate/key. If it already has
a certificate resolver, use `tls: {certResolver: YOUR_RESOLVER}` on the router
instead of the example's file certificate configuration.

References: [Traefik entry points](https://doc.traefik.io/traefik/reference/install-configuration/entrypoints/),
[header middleware](https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/headers/),
[HTTP services and streaming](https://doc.traefik.io/traefik/reference/routing-configuration/http/load-balancing/service/).

For direct management HTTPS without a TLS-terminating proxy, see
[manual certificates and automatic ACME](management-tls.md).

## Shared deployment requirements

Choose one proxy; all three examples use public HTTPS port 443, management backend
`127.0.0.1:8080`, and the separate box TLS listener on 10443 from the startup
example above. Serve the complete application at the origin root; subpath hosting
is not supported. These are configuration templates checked against official
proxy documentation, not live-tested proxy deployments.

For containers or a proxy on another host, replace the loopback backend address
with a reachable private address and trust the proxy's actual backend source CIDR
in teddyCloud. A container's `127.0.0.1` refers to that container itself. Keep the
management backend inaccessible to public clients. Update the public URL and
proxy listener together when using a different public port.

`core.server.trusted_proxies` is a space-separated list of **direct peer CIDRs**;
its empty default trusts no proxy. Trust only your proxy's actual backend source
addresses and keep the HTTP backend private. The trusted proxy must overwrite
`X-Forwarded-Proto` (`http` or `https`) and `X-Forwarded-Host` (public host with
optional port). If the latter is omitted, the preserved Host header is used.
Comma-separated chains, duplicate headers and malformed hosts are rejected.
Untrusted forwarded headers are ignored. Standard `Forwarded` and
`X-Forwarded-For` are not used: login rate limiting continues to use the direct
peer address, so users behind one proxy share its address limit.

Trusted HTTPS metadata enables Secure session cookies and correct same-origin
checks across TLS termination. It does not create a device identity. Enable the
optional management login before publishing the application; see
[login setup](develop-port.md).

Automated coverage includes a real HTTPS-to-HTTP reverse proxy with login cookie
and same-origin checks, untrusted/invalid header cases, independent listener tests
and management/box route separation. No physical box or browser visual acceptance
is implied.
