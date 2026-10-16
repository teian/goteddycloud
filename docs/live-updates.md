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
