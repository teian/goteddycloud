# Integrated Web UI

The owned React/TypeScript overlay in `src/goteddycloud/` extends the pinned
upstream frontend `636a8d65043f6c6e322f76bcb11e958725159e96`. It uses the existing
React router, Ant Design components, theme, login/session handling and runtime
reverse-proxy prefix. Pages render directly in the application, without iframes.

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

The build creates `.cache/goteddycloud-web` from an isolated copy of the pinned
frontend, installs the upstream lockfile's dependencies and applies the owned
source overlay and checked integration anchors using `scripts/prepare-frontend.py`.
It refuses unexpected revisions or changed integration anchors. The generated
stage is marked before reuse; the upstream checkout and sibling C repo remain
untouched. Edit `frontend/src/goteddycloud/` rather than the generated stage.

`install-fixture` accepts only a marked disposable generated deployment. For an
existing deployment, install the **complete** `.cache/goteddycloud-web/dist/` asset
build into its configured `www/web/` directory and refresh the browser. These
assets are independent of the Go binary. Native release archives still require
separately installed frontend assets.

Docker builds fetch the revision in `upstream-revision`, apply the same overlay,
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
```

`PLAYWRIGHT_MODULE` can specify an existing Playwright `index.mjs`, and
`BROWSER_EXECUTABLE_PATH` an installed Chromium executable. The test covers
account setup/login, independent shared-area roles, read-only partial/full grants,
download authorization, revocation, ordinary-user controls, area switching,
mobile navigation, dark theme and German labels. Screenshots are written under
`.cache/webui-*.png`. Unit tests also cover reverse-proxy prefixes, bearer/cookie
handling, expired sessions, membership roles and stale selector cleanup.
