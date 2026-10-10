# Official frontend interaction acceptance

Pending on 2026-10-09. The official pinned frontend builds and static/API tests pass, but the available UI tool reports no enabled browsers (`apps: [], browsers: []`). No frontend click-through acceptance is claimed. This is separate from missing physical Toniebox hardware.

Use only a fresh generated deployment for this rehearsal:

```sh
go run -buildvcs=false ./cmd/fixturegen -output .cache/frontend-acceptance
scripts/frontend.sh build
scripts/frontend.sh install-fixture .cache/frontend-acceptance
bin/goteddycloud --base_path .cache/frontend-acceptance \
  --http 127.0.0.1:18089 --httpsecure off --box off
```

Open http://127.0.0.1:18089/web in an available browser. Record the frontend commit, backend source digest, browser/version and observed results. Inspect browser errors and failed network requests for each step. The generated deployment has two synthetic box overlays, disposable keys and WAV inputs; never use its identities on physical hardware.

| Interaction | Required result |
| --- | --- |
| Login/logout | Create a user while protection is disabled; enable protection, log in, verify cookie/bearer API access and logout. Password changes, user deletion, rate limits and the environment override follow develop. |
| Box settings | Change one box's speaker/headphone limits and LED/slap settings, reload the page, and confirm persistence without changing the other box/global settings. |
| Tag assignment | Assign a generated local TAF to a synthetic UID; check metadata/source mapping, listed title and content access after page reload. |
| File conversion | Upload tone1.wav/tone2.wav from the generated fixture, convert with two chapters, verify progress/events and resulting valid TAF. |
| Custom models | Create, edit, search, rename and delete a synthetic custom model; verify linked tag rename and backups, and restart/reload without resurrecting deleted data. |
| Events | Open two frontend sessions, replay synthetic RTNL state/events, verify volume/playback/content updates and reconnect after restarting the backend. |
| Static/navigation | Navigate directly to an extensionless nested frontend URL, reload, and confirm assets/MIME/SPA fallback and browser history work. |

Retain sanitized screenshots and network/event summaries only. Record failures as individually scoped follow-up tasks; do not close task 6.15 from backend API tests alone. Stop the isolated process after the rehearsal.
