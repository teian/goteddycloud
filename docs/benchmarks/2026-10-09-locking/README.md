# Locking review measurements

Go 1.26.8-X:nodwarf5, linux/amd64, Ryzen 7 5800X, 16 parallel workers, temporary files on tmpfs. These are synthetic mixed settings operations, not request latency or disk throughput. See ../../locking-review.md for workload and interpretation.

Reproduce from the repository root:

```sh
go test -buildvcs=false ./internal/config -run '^$' -bench '^BenchmarkSettingsMixed$' -benchtime=3s -count=3 -mutexprofile .cache/locking-review/settings-final.mutex -blockprofile .cache/locking-review/settings-final.block -o .cache/locking-review/config-final.test
go tool pprof -top .cache/locking-review/config-final.test .cache/locking-review/settings-final.mutex
go tool pprof -top .cache/locking-review/config-final.test .cache/locking-review/settings-final.block
```

Profile delay totals sum blocked time across workers. Rounded allocation counts do not imply the mixed workload allocates nothing. The earlier performance report remains a separate before/after experiment; these measurements establish no new speedup factor.
