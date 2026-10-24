# Continuous integration

The **GoTeddyCloud checks** workflow validates this Go implementation directly.
It does not build, start or compare against the original C teddyCloud. The former
`oracle` job was retired: deliberate API/SSE extensions are covered by Go and
WebUI contract tests rather than byte equality with an older implementation.
Historical C-reference scripts remain optional manual tools.

Four independent jobs report results even if another area fails:

| Job | Checks |
| --- | --- |
| Server — unit tests and static checks | Go APIs, storage, protocols and codecs; CC3200 gateway protocol; workflow lint, vet, pinned Staticcheck and dependency lock files |
| Server — race detection | All Go packages under the race detector |
| Web UI — build, unit tests and browser checks | Pinned WebUI production build, components, translations, login/roles/tools, assignment, real TLS RTNL, snapshots, backend mapping and console responsiveness |
| Native releases — cross-build, startup and migration | Linux/Windows/macOS archives and checksums; native startup, persistence, TLS, restart, shutdown and migration rollback |

CI does not run fuzzing campaigns. Fuzz targets remain available for manual
local use; their existing seed cases run as ordinary regression tests with
`go test ./...`.

Jobs have separate time budgets. New pushes cancel outdated push/PR check runs;
release callers retain their checks. The runner OS is pinned to Ubuntu 24.04.
Go, Node, Actionlint, Staticcheck, browser tooling and
the WebUI revision are pinned. Playwright uses the tracked lock file in
`scripts/browser/`; no pre-existing developer checkout or browser installation
is required. Each browser test generates its own disposable Go deployment.

Package setup reuses available FFmpeg tools, omits optional packages and uses
the official signed Ubuntu repositories over HTTPS. Network/lock waits have
bounded timeouts and retries, and the install step has its own five-minute
budget. The same APT configuration applies to Playwright's system dependencies.
The setup script refuses to run on a developer/deployment host.

Check output is streamed and saved through `scripts/ci-run.sh`. The wrapper
preserves a failing command's exit code; its own tests cover this behavior.
Logs are uploaded even after failure, and browser diagnostics include screenshots.
Native archives keep the existing `backend-builds` artifact name.

The RTNL performance check always enforces bounded text/DOM rendering, backend
mapping use, one mapping fetch across navigation, complete exports and raw-data
fallback/retry. The default route latency budget is 1,000 ms at 4× CPU slowdown;
CI allows 2,000 ms for shared-runner scheduling jitter. Measured timings remain
in the browser log. This does not relax the structural or functional assertions.

For local browser checks after building the server and WebUI:

```sh
npm ci --prefix scripts/browser --ignore-scripts --no-audit --no-fund
scripts/browser/node_modules/.bin/playwright install chromium
export PLAYWRIGHT_MODULE=scripts/browser/node_modules/playwright/index.mjs
node scripts/webui-smoke.mjs
node scripts/webui-assignment-smoke.mjs
node scripts/webui-live-smoke.mjs
node scripts/webui-rtnl-performance-smoke.mjs
```

Run `node --test scripts/ci-run.test.mjs` to check log/exit-code handling. The
workflow file contains the exact build, test and packaging commands.
