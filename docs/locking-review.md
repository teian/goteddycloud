# Locking review

Reviewed 2026-10-09. Scope: handwritten Go code in `internal/` and `cmd/`, including mutexes, atomic state, once guards, resource-token channels, admission queues, and shutdown ownership. The fixes below have been applied and verified. The original findings are retained as the rationale; their source line numbers refer to the pre-fix audit.

## Applied fixes

| Finding | Implemented behavior |
| --- | --- |
| 1. Image cache | Cancellable per-entry fetch/flush coordination; duplicate misses share completed fetches. Metadata locks are released before upstream I/O and response streaming. Statistics remain available during downloads; index initialization probes outside the global lock. |
| 2. RTNL | Logging, reverse-cache bookkeeping and freshness have separate mutexes. |
| 3. Settings | Immutable effective state is atomically published. Durable mutations serialize separately from transient state; readers and runtime updates progress during persistence. Failed writes retain active durable values, and commits merge the latest runtime state. |
| 4. Content and assignment | Context-aware Open/Inspect/Update and listened operations; cancellable writer-preferring content mutation barrier. File selection/open remains coordinated with migration, but full validation leaves the UID lock. Assignment work uses a cancellable gate and a versioned reservation, so replacing a pending assignment never waits for its file copy or gets consumed by an earlier attempt. |
| 5. Playlists/live | Live, finite playlist and file-management operations coordinate by the same destination path. Recheck completed output after acquisition. Encoding publishes and releases its destination independently of a slow response; publication workers are joined during cleanup. |
| 6. Firmware | Confined shared-root locks serialize upload suffix allocation, patched destinations and overlapping certificate files. Transactions return results and release coordination before HTTP response writes. Certificate defaults and box materials participate in one ordered acquisition. |
| 7. Identity | Known boxes use the published overlay index without waiting for registration. New registration still locks and rechecks policy/capacity. |
| 8. Spool | A short state mutex covers committed size/notifications. Positional I/O has separate header/lifetime coordination; independent finalized readers release it between reads. FinalCopy does not hold a lock across destination writes. Close is idempotent; continuation handles close before replacement. |
| 9. Redundant locks | Embedded MQTT frame writes rely on documented connection-loop ownership; PCAP append relies on the existing File mutex, retaining per-flow ordering. |
| 10. Resource details | Separate/nested/aliased root handles share canonical path tokens, reclaimed after the last holder/waiter. Windows case aliases coalesce. Root creation happens outside the content registry mutex. Destination acquisition rechecks cancellation. |

MQTT Restore additionally copies its bounded telemetry maps under a short lock and builds discovery outside it. Box-state callback reentrancy constraints are documented. Event hub, catalog transactions, authentication, admission limits and lifecycle guards retain the coordination identified as necessary in the audit; optional actor/atomic rewrites have not been introduced without evidence of a benefit.

Permanent regression tests live in `internal/app/locking_test.go`, `internal/config/concurrency_test.go`, `internal/content/concurrency_test.go`, `internal/audio/locking_test.go`, `internal/storage/root_test.go`, `internal/identity/policy_test.go`, `internal/mqtt/integration_concurrency_test.go` and `internal/syncutil/gate_test.go`.

Full race tests, vet, pinned Staticcheck and module-tidy checks pass. The targeted concurrency suite and image single-fetch test each pass ten repeated race-enabled runs. C comparisons pass for content, tags, image cache, firmware/certificates, audio, RTNL, live, MQTT and develop APIs. Native TLS/settings/restart/shutdown and migration rollback pass. Non-native targets are cross-build checks only. Detailed logs are under `.cache/locking-review/`; permanent measurement outputs are under `docs/benchmarks/2026-10-09-locking/`.

The mixed settings workload uses 16 workers, effective-view reads, one transient update per 128 operations (excluding durable updates), and one durable update per 1,024 operations. Three 3-second runs measured 148.5, 146.8 and 147.2 ns/op (median 147.2), with 162 B/op amortized over the mix. The reported 0 allocs/op is rounded; writers do allocate. Files used `/tmp` on tmpfs. Lock profiles attribute settings contention to Update/SetRuntime, with no reader lock waits. This is an aggregate synthetic coordination measurement, not an HTTP latency measurement, disk-throughput result or a measured before/after speedup.

## Evidence and limits

The complete lock-site inventory is saved in `.cache/lock-audit-inventory.txt`. Source inspection establishes the critical sections and coordination gaps below. It does not establish how frequently each lock contends in a real deployment.

A temporary test, retained as `.cache/lock-audit-image_test.go`, blocked the HTTP response writer while serving an existing cached image. Both cache statistics and a fetch with an already-cancelled context remained blocked until the response was released. Three runs with `-race` confirmed this behavior without a detected race. Output: `.cache/lock-audit-reproduction.txt`. The temporary test was removed from `internal/app/` after execution; it deliberately asserts current undesirable behavior and is not a permanent regression test.

No lock-order deadlock was established during this review. This is not proof of deadlock freedom. Physical-device testing and deployment contention profiling were not performed.

## Original findings, in implementation order

### 1. Image entry locks span HTTP streaming and upstream downloads

`internal/app/image_cache.go:72,170`: `fetchImage` holds `entry.mu` during upstream HTTP and disk publication. `cacheAPI` holds the same mutex through `serveRangeFile` and error/redirect responses. A slow client blocks all operations on that entry, including statistics and flush. Mutex acquisition ignores request cancellation. `registerImage` also performs disk `Stat` while holding the global image-index mutex.

Keep single-fetch coordination per entry, but use a completion channel that followers can wait on with their contexts. Keep metadata critical sections short. Open the cached file and capture response metadata under coordination, then release before streaming. Coordinate flush with active fetch publication, so a fetch cannot unexpectedly undo a completed flush. Move initial disk probing out of the global map lock with a recheck before insertion.

### 2. RTNL disk logging blocks unrelated server state

`internal/app/rtnl.go:67`: `Server.mu` covers root opening, append, CSV header creation, and file writes. The same mutex protects freshness used by HTTP and MQTT and reverse-cache bookkeeping (`server.go`, `v3.go`, `integrations.go`, `reverse.go`). Slow storage therefore stalls unrelated operations across boxes.

Separate freshness, reverse bookkeeping, and log serialization. Preserve serialization for header creation and complete log records; a dedicated log mutex is enough initially. A bounded logging worker is an optional larger change, requiring an explicit overflow and shutdown-drain policy. An unbounded queue would merely move the failure to memory growth.

### 3. Configuration readers wait for durable writes

`internal/config/store.go:337,437`; `reload.go:15,52`: a single RWMutex covers disk reads, parsing, marshaling, atomic replacement, and file sync, as well as settings reads. Cached snapshots still acquire this mutex. Unknown-overlay cache misses also build snapshots while holding its exclusive side.

This is the strongest candidate for atomic immutable snapshots, following the catalog design. Serialize mutations with a separate writer mutex and publish one complete immutable state only after successful persistence. Readers load that state without a mutex; public mutable snapshots still clone maps. Construct effective views for unknown IDs without mutating published maps or retaining arbitrary IDs. Coordinate runtime updates with durable writes to prevent lost updates; simply unlocking before persistence is unsafe. Retain existing validation and rollback behavior.

### 4. Request-facing waits are not consistently cancellable

`internal/content/store.go:119,135,190`, `inspect.go:25`, `rename.go:18`, `assign.go:18`; `internal/app/listened.go:17`, `tags.go:39,291`: background resource waits and ordinary mutation/assignment mutexes can keep cancelled requests alive. A pending catalog-linked rename can block mutations across all UIDs. Assignment serialization covers inspection and potentially large validation/copy/persistence work.

Propagate contexts through content Open/Inspect/Update/listened operations and use cancellable gates for request-facing mutation waits. Preserve the exclusive barrier needed by linked renames. For assignment, reserve the pending assignment under a short lock, perform work outside it, and consume only that reservation; preserve the reference's behavior on failed eligible attempts and concurrent replacement of the pending assignment.

Keep metadata selection and opening the selected content file coordinated with migration. Once the file is open, full TAF hashing can generally leave the UID critical section, subject to the supported platforms' file-replacement behavior. Do not remove that selection/open protection.

### 5. Shared playlist outputs have different coordination keys

`internal/app/live.go:35` locks `live:<box>:<target>`. Playlist target names remain shared across boxes (`internal/content/stream.go`). `internal/content/playlist.go:137` instead locks `playlist:<target>`. Thus two boxes, or live and finite playlist generation, do not exclude one another when publishing the same output. Atomic replacement prevents partially published files, but does not prevent competing completed outputs from replacing each other.

Use a common destination identity for operations on the same output, with an explicit policy for sharing a generation versus serializing it. Recheck existing output after acquiring coordination. Avoid serializing all client playback behind a destination lock: coordinate generation/publication, then stream independently. This source-confirmed gap still needs a competing-generation regression test.

### 6. Firmware locks cover responses and unrelated destinations

`internal/app/firmware.go:96,316,374`: deferred `firmwareMu.Unlock` extends through HTTP responses. Upload suffix selection, certificate publication, extraction, and settings persistence also share this mutex. A blocked response can stall unrelated firmware or certificate work.

Unlock before writing responses. Use directory coordination for suffix allocation and per-identity coordination for certificate bundles. Keep filename selection and publication serialized; keep certificate bundle operations coordinated. Each handler opens a fresh storage Root, so replacing this mutex with a lock on that fresh Root would not coordinate different requests.

### 7. Identity resolution serializes known devices behind registration

`internal/identity/policy.go:39`: every accepted identity takes one mutex, including already registered boxes. An unknown box holds it while configuration is persisted.

Add a known-box read fast path. Lock and recheck only for registration, retaining the 16-box limit and allow-new-box policy. Config publication improvements are needed as well, because otherwise its own disk-held mutex still blocks these reads. Registration is a transaction and should remain serialized.

### 8. Audio spool state mutex covers file I/O

`internal/audio/stream.go:117,168,202,218`: reads, writes, validation, and FinalCopy's arbitrary destination writer hold the same state mutex. This is local to one stream, but a slow read/copy can block other readers and state access.

Prefer a short state mutex over a fully lock-free spool. Give the encoder sole ownership of its write position, publish committed ranges, and read those ranges using positional I/O outside the state mutex. Account for header rewrites during finalization; a size atomic alone is insufficient. After completion, use independent section readers instead of the shared seek position. Coordinate file lifetime with readers and Close before reducing locking. `FinalCopy` currently has no production call sites, so its optimization has lower immediate value.

### 9. Two mutex layers appear redundant

`internal/mqtt/client.go:381`: embedded-server frame writes all run in the connection's owning goroutine (`server.go`); its reader goroutine only reads and queues packets. `lockedWriter` can therefore lose its mutex if this ownership contract is documented and retained.

`internal/diagnostics/pcap.go:127`: Capture's mutex wraps a complete call to `diagnostics.File.Write`, which already serializes the entire record and cap accounting. The outer Capture mutex can be removed. Keep the per-flow mutex for sequence numbers and packet ordering and the File mutex for append/cap/close coordination.

These are small simplifications, not expected major speedups.

### 10. Lock identity and cancellation details

`internal/storage/root.go`: resource locks are scoped to a Root instance, not to the filesystem directory. `internal/app/tags.go:125` locks a freshly opened root per request, so that lock does not serialize concurrent requests. Content roots are cached by their configured path string; aliases to the same directory can also create independent registries. Define and document the intended coordination domain, use shared root instances, and normalize supported aliases where necessary. These locks provide no cross-process exclusion.

`internal/audio/lock.go`: destination acquisition can succeed when cancellation and the token are simultaneously ready. Add a post-acquisition context check and release on cancellation, as `storage.Root.LockContext` already does.

## Review of the remaining synchronization areas

| Area | Assessment and recommendation |
| --- | --- |
| Catalog (`catalog.go`, `custom.go`, `rename.go`) | Atomic immutable lookup snapshots are already appropriate. Keep serialized writers and the journaled linked-rename transaction. Disk-backed ReadCustom could use a published snapshot only if its external-file visibility contract is retained. |
| Box state (`boxstate/state.go`) | Short global state/map mutex plus per-box ingestion ordering is appropriate. Optional per-box atomic snapshots need profiling to justify added complexity. ContentInfo executes under ingestion serialization; document that it must not recursively ingest the same box. |
| Event hub (`events/hub.go`) | Nonblocking bounded delivery avoids network waits. Keep send/unsubscribe/close coordination. Publishing outside the lock after copying channels risks sending to closed channels. Payload cloning under the lock is a possible allocation hotspot; immutable payload redesign needs measurements and must preserve receiver isolation. |
| Box commands (`boxcommands/service.go`) | Short map changes and cancellation outside the mutex are appropriate. Keep the mutex. |
| Web authentication (`auth.go`, `auth_http.go`) | Separate durable mutation and in-memory locks are appropriate. HTTP body/response and durable user-file I/O avoid the state mutex. Login/user mutation still performs hashing/random generation under it; current operations are small, but a future expensive password hash must be moved outside with state revalidation. |
| MQTT integration | Applied: copy bounded replay state under the mutex, generate discovery outside it. |
| MQTT connection registry (`mqtt/server.go`) | Short bounded map mutex, connection close outside lock, and worker ownership are appropriate. No clear benefit from sync.Map. |
| Storage resource registry (`storage/root.go`) | Short bookkeeping mutex and sorted/deduplicated cancellable tokens are appropriate. Sorting orders one acquisition call; nested acquisitions still need an explicit order. |
| Content root registry | Applied: open outside the registry mutex, double-check insertion and close duplicate handles; closed stores reject new roots. Cached reads avoid filesystem probing. |
| Cache migration/download (`content/cache.go`) | Separate download/migration transactions and cancellable UID publication locks are justified. Do not replace read-modify-publish locks with atomics. Include them in any common-destination lock design. |
| Files, content, OTA and audio APIs | Existing cancellable per-resource locks preserve transactional operations; retain them and review key compatibility with live/playlist/cache writers. Long work is acceptable only within the affected resource's coordination domain. |
| Reverse cache (`app/reverse.go`) | Per-prefix cancellable lock is appropriate for shared captures. Split its global bookkeeping from RTNL logging. |
| Diagnostics File | Keep serialized append, size-cap decision, and close. Optional asynchronous recording needs bounded buffering and explicit loss semantics. |
| Runtime and TLS transport | Once guards, bounded connection ownership, WaitGroups and completion channels have distinct lifecycle purposes; retain. TLS transport closes active sockets outside its registry mutex. |
| Admission channels | Audio encoder/conversion, playlists, firmware, cloud, image, export, reverse and connection limits intentionally bound expensive work. Retain bounds; moving work to goroutines is not a lock-free improvement. |
| Counters/restart flag | Existing scalar atomics are appropriate; related state must not be split into unrelated atomics without a consistency design. |
| Schema initialization | sync.OnceValue is appropriate for immutable schema decoding. |
| Fixture and probe tools | Mock request-list mutex and probe WaitGroups/once guards are appropriate; no production performance priority. |

## Validation coverage and future guardrails

The regression suite covers slow image responses and cancellation; fetch/flush publication; settings readers/runtime updates during a held durable-writer lock and failed-write rollback; cancelled mutation waiters; concurrent assignment replacement; shared-target playlist generations; and telemetry recording during Restore. Retain per-box event ordering, QoS acknowledgement ordering, durable catalog recovery and migration pointer/open consistency tests.

Use race detection on these workloads and measure mutex/block profiles under mixed reads and writes before claiming throughput improvements. Replacing every mutex with channels or atomics would add complexity without addressing the dominant I/O-held critical sections.
