# Live UI updates

The management endpoint `GET /api/sse` streams named Server-Sent Events using
normal management authentication and area access. Each frame contains JSON in
`data`: `{"type":"event-name","data":...}`. Proxies should pass the response
through without buffering; the server sets `X-Accel-Buffering: no` and
`Cache-Control: no-cache, no-transform`.

| Event | Payload | Purpose |
| --- | --- | --- |
| `keep-alive` | Empty string | Sent on connection and every 15 seconds. |
| `tag-state` | Array of `{box, ruid, time}` | Last presented tag for each accessible box, sent on connection. |
| `tag-present` | `{box, ruid, time}` | A box presented a tag; `time` is Unix seconds. |
| `tag-updated` | `{box, ruid}` | Stored metadata or content may have changed. |

RUIDs are lowercase 16-character hexadecimal strings. Invalid and sentinel IDs
are excluded from presentation and snapshot events. Unknown-tag metadata and
last-tag settings are committed before presentation is published, so the UI can
immediately fetch the new card without waiting for an audio download. Both known
and unknown RTNL tag functions are supported on CC3200 and ESP32 boxes; content
and claim requests also announce tags when RTNL is unavailable.

The frontend uses a shared fetch stream to preserve bearer headers, cookies and
runtime reverse-proxy prefixes. It highlights a presented tag immediately,
coalesces inventory refreshes, records notifications, and refreshes metadata and
box cards. RTNL subscribes to the same connection. Transient disconnects reconnect
with backoff; the initial snapshot repairs state without replaying notifications.
Access denial stops retries, and backend access revocation closes existing streams.

Frontend source is maintained in the experimental frontend fork, pinned by
`frontend/upstream-revision`; see [frontend build instructions](../frontend/README.md).
Run `node scripts/webui-live-smoke.mjs` after building the frontend to verify real
TLS RTNL discovery, unknown cards, blue borders, tag switching, metadata updates,
notifications, reconnect snapshots and shared RTNL streaming using generated
identities and a disposable deployment.


## Cloud download diagnostics

Custom-tag autodetection retains the original password/UID behavior and
blocks cloud access on the next device request even after a manual enable.
The legacy `nocloud_manual` field from an earlier experimental image is ignored.
Browser downloads reuse saved credentials and do not perform RFID autodetection.
Fresh nonempty credentials received from a device refresh stored credentials;
empty claim passwords cannot overwrite a useful password or a cloud override.
`cloudAuthState` in tag info distinguishes missing, empty and present passwords
without returning their values. A present password is not a guarantee that the
external cloud accepts it.

The cloud icon tooltip explains whether access is blocked for the tag. Default
server logs distinguish a locally blocked download (`reason=tag_cloud_blocked`)
from an upstream HTTP rejection (`Tonies cloud response`, with `status`,
`auth_state` and an eight-character `tag_prefix`), missing
client identity, network/TLS failures and interrupted transfers. Credentials,
query parameters, complete tag paths and response bodies are not logged.
For Docker deployments, run `docker logs --since 2m --follow <container>` and
present the affected tag. A cloud setting permits an upstream request; it does
not replace the tag authorization or guarantee that Tonies will accept it.

The global cloud status probe is advisory for downloads: it may lack the selected
box's client identity or return stale availability. A permitted download remains
clickable and reports the actual response using that box's saved tag credentials.
`Browser audio download requested` confirms receipt of an explicit download click;
an already cached file can be served without a new upstream content request.
Upstream audio errors carry `X-TeddyCloud-Error-Origin: tonies-cloud` so a cloud 401
is displayed as a download failure without opening the web login prompt. A local
management 401 still requires login.

`node scripts/webui-assignment-smoke.mjs` verifies the selected-box library and
catalog assignment against a generated deployment. `FRONTEND_DIST` can point to
a local frontend build instead of the default cached stage.
