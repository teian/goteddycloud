# Performance measurements — 2026-10-09

Measured optimizations increase `/v1/time` loopback HTTP throughput from about
8,840 to 48,978 requests/sec (5.5×). These are local synthetic measurements,
not predictions for physical Toniebox playback or production deployments.

## Method

Linux amd64, AMD Ryzen 7 5800X (16 logical CPUs), Go
`go1.26.8-X:nodwarf5 linux/amd64`. Before and after use the same machine,
synthetic fixtures and benchmark harness. Go benchmarks use `GOMAXPROCS=4`,
five samples per case, 300 ms per sample and median results below. Fixture
creation and catalog publication are excluded from benchmark timing. Handler
benchmarks include request/response allocation but exclude sockets and TLS.

The separate HTTP measurement uses fresh isolated deployments, real loopback
HTTP connections, four concurrent clients and five rounds of 20,000 verified
requests per binary. Ten warmup requests per client per round are excluded.
Both server and probe explicitly use `GOMAXPROCS=4`. HTTP values below are the
medians of the five per-round metrics; the latency percentiles are not pooled.
Raw outputs and measured binary SHA-256 hashes are retained in
[benchmarks/2026-10-09](benchmarks/2026-10-09/).

## Results

| Benchmark | Before µs/op | After µs/op | Speedup | Bytes/op before → after | Allocs/op before → after |
| --- | ---: | ---: | ---: | ---: | ---: |
| Handlers/Time | 113.660 | 6.251 | 18.2× | 158933 → 24917 | 210 → 30 |
| Handlers/AuthStatus | 144.695 | 5.408 | 26.8× | 197992 → 7267 | 269 → 39 |
| Handlers/SettingsIndex | 801.936 | 62.914 | 12.7× | 383218 → 95820 | 2226 → 635 |
| Handlers/FileIndex | 175.898 | 16.142 | 10.9× | 237302 → 26531 | 320 → 48 |
| Handlers/TagInfo | 230.084 | 63.383 | 3.6× | 247429 → 55515 | 525 → 256 |
| Handlers/Freshness | 115.335 | 7.195 | 16.0× | 159727 → 25701 | 221 → 41 |
| Snapshot/Global | 22.698 | 1.884 | 12.0× | 38048 → 18520 | 46 → 4 |
| Snapshot/Overlay | 23.732 | 1.930 | 12.3× | 38048 → 18520 | 46 → 4 |
| Snapshot/UnknownOverlay | 22.781 | 2.045 | 11.1× | 38048 → 18520 | 46 → 4 |
| Snapshot/Parallel | 7.740 | 1.917 | 4.0× | 38048 → 18520 | 46 → 4 |
| CatalogLookup/100/ModelLast | 19.478 | 0.410 | 47.5× | 18123 → 520 | 307 → 7 |
| CatalogLookup/100/AudioLast | 55.539 | 0.270 | 205.5× | 47968 → 504 | 912 → 6 |
| CatalogLookup/100/AudioMissing | 54.001 | 0.007 | 7661.9× | 47200 → 0 | 900 → 0 |
| CatalogLookup/10000/ModelLast | 1939.729 | 0.444 | 4365.8× | 1760766 → 528 | 30009 → 8 |
| CatalogLookup/10000/AudioLast | 5451.856 | 0.289 | 18877.6× | 4720777 → 504 | 90012 → 6 |
| CatalogLookup/10000/AudioMissing | 5390.263 | 0.008 | 696236.5× | 4720007 → 0 | 90000 → 0 |

| Real HTTP metric | Before | After |
| --- | ---: | ---: |
| Requests/sec | 8,839.9 | 48,977.6 |
| Median latency (µs) | 460.2 | 73.3 |
| p95 latency (µs) | 661.7 | 139.6 |

## Changes and evidence

The original auth-status CPU profile attributed 71.7% of sampled CPU time
cumulatively to configuration snapshots. Snapshot map creation accounted for
94.1% of allocated bytes in that profile. Catalog profiles showed repeated JSON
parsing and allocation during linear scans. Profiles include fixture setup;
the benchmark table excludes setup.

- Cache immutable effective settings and invalidate them after successful
  updates, resets, runtime changes, overlay removal and reloads. Public
  `Snapshot` still returns an independently mutable map. New read-only `View`
  and single-setting accessors avoid copying entire maps on request hot paths.
  Unknown overlay queries do not create retained cache entries.
- Decode the embedded settings schema once, returning independent slices to
  callers. Parse settings-index filters once per request and build snapshots
  only in tag routes that need them.
- Build immutable model/audio indexes when publishing a catalog, decoding IDs
  and hashes once. Keep custom-first ordering, Unicode case-folding semantics,
  teddyBench ID adjustment, hash matching and independent returned entries.

The catalog late-hit and missing-hit cases deliberately expose the cost of a
linear scan. Their speedups depend heavily on catalog size and entry position.
The indexes and settings cache retain additional memory and shift work to
publication or the first settings read; startup time, retained heap size and
catalog-update throughput were not measured. No statistical-significance claim
is made from five samples. TLS, Wi-Fi, FFmpeg and physical-device playback are
outside these measurements.

## Reproduce

Run from the repository root. Preserve the pre-change binary and baseline
benchmark output before applying optimizations; the current source alone cannot
recreate the previous implementation.

```sh
GOMAXPROCS=4 go test -buildvcs=false ./internal/app ./internal/config ./internal/catalog \
  -run '^$' -bench . -benchmem -benchtime=300ms -count=5 > after.txt
go build -buildvcs=false -o bin/goteddycloud ./cmd/goteddycloud
go build -buildvcs=false -o .cache/performance/httpprobe ./cmd/httpprobe
python3 scripts/http-benchmark.py --binary bin/goteddycloud \
  --probe .cache/performance/httpprobe > http-after.json
python3 scripts/benchmark-report.py docs/benchmarks/2026-10-09/before.txt after.txt \
  --http-before docs/benchmarks/2026-10-09/http-before.json --http-after http-after.json
```

To profile an individual hot path:

```sh
GOMAXPROCS=4 go test -buildvcs=false ./internal/app -run '^$' \
  -bench '^BenchmarkHandlers/AuthStatus$' -benchtime=3s \
  -cpuprofile .cache/performance/handler.cpu -memprofile .cache/performance/handler.mem
go tool pprof -top .cache/performance/handler.cpu
go tool pprof -top -alloc_space .cache/performance/handler.mem
```

The preserved local baseline binary is `.cache/performance/goteddycloud-before`.
Profiles and verification logs are under `.cache/performance/`. The local
`bin/goteddycloud` includes these changes; existing versioned release archives
remain earlier snapshots.

## Correctness checks

The final source passes `go test -buildvcs=false -race ./...` and
`go vet -buildvcs=false ./...`. Regression tests compare indexed catalog results
with the original linear algorithm, including custom precedence, adjusted IDs,
malformed metadata and Unicode matching. Settings tests cover cache invalidation,
independent mutable snapshots, retained immutable views and bounded unknown-ID
queries.

Against the pinned C development reference, the API harness matches auth
lifecycle, CORS/cookies, JSON responses and exact individual/ZIP track bytes.
Security comparisons (10 responses), cloud forwarding/cache comparisons (24
checks) and five upstream custom-catalog API tests pass. Native startup with
verified TLS and restart identity, migration rollback and reference inventory
checks also pass. These checks do not establish interactive frontend or physical
CC3200 acceptance.
