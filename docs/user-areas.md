# Accounts, areas and member roles

Open **Settings → Users and areas** (`/web/settings/users`) in the integrated Web UI. It provides account management, shared-area creation, area selection, member roles, box assignments and own-password changes. The header provides an area selector; on mobile it appears in the navigation drawer. Switching areas reloads the application and clears old explicit area/device selections. The backend enforces the same rights for API clients. Build/install the integrated assets as described in [frontend integration](../frontend/README.md). User management is part of the application, not a plugin. Legacy user-page links redirect to `/web/settings/users`.

An account may belong to several areas with a different role in each. For example, Alice can administer the **Family** area and read the **School** area, while Bob edits Family content. Each area owns its content, library, pictures, custom catalog, box settings, live state, statistics, cache and event stream. Members access the same data; their membership roles determine allowed operations.

| Role in an area | Read content and assigned boxes | Change content and permitted box settings | Rename area and manage members |
| --- | --- | --- | --- |
| `admin` (area administrator) | Yes | Yes | Yes |
| `editor` | Yes | Yes | No |
| `reader` | Yes | No | No |

**Global server administrators** can access all areas, manage server accounts and configuration, create/remove areas and assign registered boxes. Area administrators have no global administrator privileges: they cannot create accounts, flash firmware, edit credentials, assign boxes or manage other areas. They can add existing accounts by username to their own area and set any of the three member roles there. Global administrators retain access even if their explicit membership has a lesser role or is removed.

The account-level `role` field remains compatible with the previous API: `admin` grants global administration; `editor` and `reader` are ordinary accounts and provide the initial/default-area role. Changing this field through `/api/auth/users/access` updates that account's role in its default area only; roles in other areas remain independent. Changing a member role through the area API does not change the account role. The built-in UI labels these scopes separately.

## Areas, selection and boxes

Every new account receives a default personal area. A global administrator may share that existing area by adding members, or create a separate common area. Data stays at its existing location; sharing does not copy or move files. All members can select the areas they belong to. Session selection affects every tab using that session; reload the main frontend after switching. A request can override selection with `?area=<area-id>`.

A box belongs to exactly one area. All members of that area can read its permitted information; editors and area administrators may change speaker/headphone limits, slap controls, LED mode, and supported Toniebox 2 baby-mode/light brightness settings. Global administrators control identity, generation, credentials, API-access policy and box assignment. Unassigned boxes retain global behavior. Device requests and MQTT commands/freshness follow area assignment independently of any particular member's account. Removing/deleting a member does not move boxes or content out of their area.

A normal user initially selects their personal area. Posting an empty selection resets to that area, provided they are still a member. Removing membership immediately denies access, including through an existing session selection or explicit ID; the user can select another accessible area through the header area selector. An administrator's empty selection opens global administration. Global server settings, identity controls and firmware tools continue to act on the global deployment regardless of selected content area.

IDs are canonical, stable selectors. Usernames remain aliases for their current default areas for compatibility; unique area names are also accepted. Use IDs to avoid ambiguous names or collisions with account aliases. Areas may have identical display names; the UI includes an ID prefix. Unknown areas and nonmembers receive denied access without disclosing their contents.

## Migration, storage and deletion

`config/web_users.json` stores accounts and the independent `areas` registry together, using the existing atomic filesystem/S3 publication. Previous databases without an area registry are migrated in memory: each user's previous area ID, box assignment and role become an area with that user as a member. The next successful mutation writes the new registry. Role-less legacy accounts remain global administrators. Existing private storage paths are preserved exactly.

Area files live under `<base>/users/<SHA256(area-id)>/` using the configured filesystem or S3 backend. Password changes preserve default-area IDs and all independent memberships. Account deletion removes that username from every area and revokes sessions; areas remain accessible to remaining members and global administrators. Recreating the username creates a new private area and does not restore previous memberships. Areas without members remain under global administrator control.

Global administrators may remove independent or orphaned areas from the registry. An existing account's default area cannot be removed. Removing an area releases its box assignment and access, but retains files for administrator recovery; a new area receives a fresh ID. Open area instances are released at server shutdown. The registry is bounded to 64 areas and 32 accounts, and the whole account/area database is bounded to 64 KiB. Remove unused/orphaned areas when needed.

Existing global content remains in the global deployment. Official `tonies.json` metadata is copied when an area first opens; custom catalogs remain private to each area. Service defaults (cloud, encoding, metadata behavior and box defaults) are inherited when an area opens; restart to refresh defaults for already-open areas. Assigning a new box overlay inherits administrator configuration and does not migrate content from another area.

## Enforcement

Ordinary accounts force login protection, even if `frontend.web_auth_enabled` was false. Disabling protection is rejected while these accounts exist. The recovery override `TEDDYCLOUD_WEB_AUTH_DISABLE=1|true|yes` deliberately grants global administrator access. The first account defaults to global administrator; later accounts default to ordinary editors. At least one global administrator must remain while any accounts exist. Deleting the final account retains the previous setup behavior and disables login.

Authorization reads current account and area membership state at each request. Account/role/member changes close active SSE streams; reconnecting checks current membership. Password changes and account deletion revoke that user's sessions. Already-authorized finite requests may finish. Users, area memberships and box assignments are validated and published atomically in one object. Invalid/corrupt databases fail startup; failed publication retains prior in-memory access state.

Restricted APIs use exact method/path allowlists. Filesystem roots reject symlink escapes; S3 paths stay in the area namespace. Device credentials are explicit read-only references outside that namespace and are not exposed through content routes. Private media downloads carry sandbox CSP and `nosniff` headers. FFmpeg receives staged media through stdin with only the pipe protocol enabled, blocking secondary file/network references. Remote streaming applies DNS/IP policy; private-network fetching requires the global `core.allowPrivateFetch` setting.

The existing legacy name-based device identity policy is unchanged. MQTT remains a global administrator-controlled integration: broker access is not a substitute for membership-aware web login. The official frontend may display unsupported global menus to ordinary users; the backend denies them, and the built-in page exposes the area/membership controls directly.

## API examples

Use the management session cookie or bearer token. Administrative account APIs always target the global database. `GET /api/auth/status` and `/api/auth/profile` expose `selectedArea` and `areaRole`; the old `role` remains account-level. Profiles include `defaultArea` and accessible `areas`, with public member roles and box IDs; password hashes and salts are never included.

Create an independent area as a global administrator:

```http
POST /api/auth/areas/create
Content-Type: application/json

{"name":"Family","members":[{"username":"alice","role":"admin"},{"username":"bob","role":"editor"}],"boxes":["001122334455"]}
```

The response contains `area.id`. Omitting `members` initially adds the signed-in creator as an area administrator. A member with area-administrator rights can replace the member list or rename the area:

```http
POST /api/auth/areas/update
Content-Type: application/json

{"area":"<area-id>","members":[{"username":"alice","role":"admin"},{"username":"bob","role":"reader"}]}
```

Omitted fields remain unchanged; `members:[]` removes all explicit memberships. Usernames must identify existing accounts and are normalized to their stored spelling. Repeated users, unknown users/roles, invalid names and duplicate box assignments are rejected. Only global administrators may supply `boxes`, including `[]` to unassign them. A global administrator can always recover an area with no area administrator.

Other routes:

- `GET /api/auth/areas`: accessible areas; global administrators see all areas. Includes members and assigned boxes.
- `POST /api/auth/area` with `{"area":"<area-id>"}`: select an accessible area for this session. An empty value resets selection as described above.
- `POST /api/auth/areas/delete` with `{"area":"<area-id>"}`: global administrator only; removes registry access, retaining files. Default-area references return 409.
- `GET /api/auth/users/details`: global administrators only; public account profiles.
- `POST /api/auth/users/create` with `{"username":"alice","password":"choose-a-password","role":"editor"}`: creates an account and default area; role optional.
- `POST /api/auth/users/access` with `{"username":"alice","role":"reader","boxes":[]}`: legacy account/default-area controls; shared member roles remain unchanged. Box exclusivity includes every area.
- `POST /api/auth/password` with `{"currentPassword":"old-password","password":"new-password"}`: own password change, cookie removal and session revocation; area identities/memberships remain intact.

## Verification

`internal/app/shared_areas_test.go` tests independent member roles, shared files and boxes, permission escalation rejection, member removal/SSE invalidation, deletion/recreation behavior, filesystem/S3 persistence, malformed registries and unchanged storage paths during migration. Existing area/storage tests continue covering confined roots, session revocation, credential sharing and symlink escapes. `scripts/roles-smoke.py` checks the native process with private/shared content, different roles per area, actual synthetic-client TLS requests for assigned devices, restart persistence and shutdown. These checks do not substitute for browser visual/interactive acceptance, a physical Toniebox, or live Silo/NooBaa acceptance.

## Read-only catalog sharing

Open **Tonies → Shared catalogs** (`/web/tonies/shared`) after login. The native page uses the existing theme, language, navigation and authentication session. `/tools/shares` remains a standalone fallback.
Choose a catalog, enter an existing recipient's username, and either select
individual files or enable **Entire catalog**. Saving again replaces the grant
for that recipient; **Revoke** removes it. Recipients select received catalogs
on the same page and download their available files. This grants no area
membership, write access, device access or permission to share onward.

Editors may share their own default area's catalog. Area administrators may
share any area they administer; global administrators may manage all area
catalogs. Readers and editors of other/shared areas cannot issue grants. Grants
belong to the area and remain until explicitly revoked or the recipient/area is
removed, even if the administrator who created them leaves. Removing a membership
does not revoke an independent catalog grant; revoke that separately as needed. Administrators can
recover and manage an orphaned area's grants.

A catalog comprises stored audio under the area's default content and library
roots, with textual title/model/track metadata. Supported files are TAF (including
extensionless `XXXXXXXX/XXXXXXXX` content paths), Ogg/Opus, MP3, WAV, FLAC,
M4A/M4B and AAC. JSON sidecars, pictures, certificates, hidden files and symlink
aliases are excluded. Remote/live sources are not fetched or shared. Metadata
is explicitly projected to public description fields; cloud credentials and
internal source paths are never returned through the sharing API. The original
area APIs continue to require membership. Global deployment content outside
areas is not included.

A full grant includes future files in those roots. A partial grant contains exact
root-relative file paths (maximum 256 per recipient); replacement at a selected
path shares the new file, while renaming a file requires updating the grant.
Deleted files disappear from the listing. Select individual files rather than
folders; directory selection is not a recursive grant. Full listings are bounded
to 10,000 visited entries and 32 directory levels and return HTTP 413 when either
limit is exceeded. Partial listings inspect only their selected files.

Grants are stored with the area in `config/web_users.json`, using the same atomic
filesystem/S3 publication and 64 KiB total database limit. They use stable account
identities, survive password changes and restarts, and are removed when a
recipient account is deleted. Recreating its username restores no grant.
Authorization is checked on every request; a download already authorized before
revocation may finish. A failed save keeps the prior live and durable grants.
Sharing requires an authenticated account even when management login protection
is otherwise disabled.

API (all require an authenticated management session):

- `GET /api/auth/catalog/shares`: readable catalogs and editable outgoing grants.
- `GET /api/auth/catalog/items?area=<id>`: only permitted stored audio and safe descriptions.
- `GET /api/auth/catalog/download?area=<id>&root=content|library&path=<relative-path>`: exact authorized file, with standard HTTP byte ranges.
- `POST /api/auth/catalog/grant`: replace one recipient's grant, e.g.
  `{"area":"<id>","username":"bob","all":false,"items":[{"root":"library","path":"stories/story.taf"}]}`.
  For a full grant use `"all":true,"items":[]`.
- `POST /api/auth/catalog/revoke`: `{"area":"<id>","username":"bob"}`.

The sharing page uses safe DOM text rendering and the same local script and
session policies as the user-management page. Existing third-party frontends
can integrate these APIs; their normal file browser does not automatically merge
received catalogs into a recipient's private library.
