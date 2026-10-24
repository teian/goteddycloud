# Integrated Web UI

The integrated frontend lives in the
[teian/teddycloud_web fork](https://github.com/teian/teddycloud_web/tree/experimental/live-tag-sse),
on `experimental/live-tag-sse`. `upstream-revision` pins the exact commit used by
local and Docker builds. The earlier `.cache/goteddycloud-web` work, including
users/areas, shared catalogs and UI fixes, has been committed to that branch.
It uses the existing React router, Ant Design components, theme, login/session
handling and runtime reverse-proxy prefix.

- **Settings → Users and areas**, `/web/settings/users`: initial administrator
  setup, login protection, account roles, password reset/change, shared areas,
  independent membership roles and global box assignments. Ordinary users see
  their accessible areas and own-password controls; area administrators also
  manage memberships. `/web/settings/webauth` redirects to this page.
- **Tonies → Shared catalogs**, `/web/tonies/shared`: stored audio browsing,
  received downloads, selected/full outgoing grants, editing and revocation.
  Grant controls follow backend authorization. Each recipient's grant can be
  replaced with the current selection; full grants include future files.
- **Area selector** in the header (navigation drawer on mobile): changes the
  session's active area and reloads the application. Old `area`/`overlay` query
  parameters and saved device selection are cleared. Other parameters survive.

English and German labels are included; other languages fall back to English.
The authenticated app shares an SSE stream for tag discovery and metadata
updates. Presenting a tag immediately marks its card with a blue border; unknown
tags are added without reloading the page or waiting for an audio download.
Tag notifications appear in the existing notification UI. Lists retain their
filters during background refreshes, and box cards update their last played tag.
The stream uses the normal bearer/cookie handling and reverse-proxy prefix,
reconnects after transient failures, and restores last-tag state on reconnection.
Area access and revocation are enforced by the backend.

The recording includes server `audio-sync` events for claims, freshness decisions
and local transfers. Decisions show the selected source, actual file and box
audio IDs, missing/invalid files, pending local content and final response marks.
Empty/rejected freshness requests and the running server version are included.
A response mark is distinct from a completed download. Remote URLs and credentials
are omitted. Diagnostic frames are bounded for large box libraries.
The console renders at most 100 entries / 64 KiB at once without syntax
highlighting. Older/newer/latest controls navigate the recording. Incoming
updates are batched every 100 ms; snapshots still export the complete buffer.
`node scripts/webui-rtnl-performance-smoke.mjs` checks a full recording in Chromium
with 4× CPU slowdown, history navigation and complete diagnostic exports.

**Settings → RTNL log** offers decoded, raw and combined views, a box filter,
pause/resume and JSON/TXT snapshot downloads. Recording continues across routes
within the current authenticated page session, retaining at most 2,000 entries
and 2 MiB; signing out or switching users clears it. JSON snapshots retain the
complete raw event alongside decoded fields, receive times and box IDs. Unknown
functions remain explicitly unknown; inferred formats are labelled. Snapshots
are downloaded locally and are not uploaded to another service.

Audio assignment uses the selected Toniebox's library, including the custom model
editor and previous/next episode controls. The file chooser also offers a
`tonies.json` tab: choosing a model sets its metadata and picks matching library
audio when available, checking every catalog audio revision. Saving applies the
assignment; metadata-only choices retain the existing audio source. Failed or
empty cloud downloads produce an error instead of a success notification.


Normal accounts entering `/web/settings` go to the accessible users/areas page.
Backend authorization remains authoritative for all old and new pages.
User and area management is a built-in application route, not a plugin.
It is absent from plugin discovery and the community plugin list. Legacy
`/tools/users` and user-plugin page links redirect to `/web/settings/users`.
The standalone `/tools/shares` tool remains available.

## Build and install locally

With the pinned reference checkout already available:

```sh
scripts/frontend.sh build
scripts/frontend.sh test
scripts/frontend.sh install-fixture .cache/local-test-20261010
```

The build fetches the pinned fork into `.cache/teddycloud-web-fork`, creates an
isolated copy in `.cache/goteddycloud-web` and installs lockfile dependencies.
The generated stage is marked before reuse. Edit the frontend fork at
`~/code/teddycloud_web`; cached assets and dependencies are never committed.
To build an already checked-out pinned revision locally, use
`FRONTEND_SOURCE=../teddycloud_web scripts/frontend.sh build`. Unexpected
revisions and tracked source edits are rejected.

`install-fixture` accepts only a marked disposable generated deployment. For an
existing deployment, install the **complete** `.cache/goteddycloud-web/dist/` asset
build into its configured `www/web/` directory and refresh the browser. These
assets are independent of the Go binary. Native release archives still require
separately installed frontend assets.

Docker builds fetch the revision in `upstream-revision`, stage the same frontend,
install lockfile-pinned dependencies using Node.js 24 and build the UI. Both the
`runtime` and `ffmpeg` targets include the result at
`/usr/share/goteddycloud/www/web`. Node.js and npm dependencies stay in the build
stage. `GOTEDDYCLOUD_BUNDLED_WWW=/usr/share/goteddycloud/www` enables the server's
read-only fallback, independently of its data storage backend. A custom
deployment with `www/web/index.html` overrides the whole bundled build; missing
custom assets return 404 rather than mixing builds. An empty `/data` host mount
therefore works without copying assets, and image updates replace the bundled UI.
Upstream attribution, font notices and installed npm package notices are retained
under `/usr/share/doc/goteddycloud/frontend`. See the
[license inventory](../docs/licenses.md) for remaining upstream licensing gaps.

## Browser verification

The optional Playwright smoke test creates and removes its own deployment and
server. It does not alter the running manual-test deployment:

```sh
npm install --prefix .cache/browser-tools --no-audit --no-fund playwright
node .cache/browser-tools/node_modules/playwright/cli.js install chromium
node scripts/webui-smoke.mjs
node scripts/webui-live-smoke.mjs
```

`PLAYWRIGHT_MODULE` can specify an existing Playwright `index.mjs`, and
`BROWSER_EXECUTABLE_PATH` an installed Chromium executable. The test covers
account setup/login, independent shared-area roles, read-only partial/full grants,
download authorization, revocation, ordinary-user controls, area switching,
mobile navigation, dark theme and German labels. Screenshots are written under
`.cache/webui-*.png`. Unit tests also cover reverse-proxy prefixes, bearer/cookie
handling, expired sessions, membership roles and stale selector cleanup.
The live smoke test sends real RTNL frames over TLS with a generated box identity,
then verifies SSE delivery, automatic unknown-tag insertion, blue card borders,
tag switching, live metadata, notifications and a complete heartbeat interval.
