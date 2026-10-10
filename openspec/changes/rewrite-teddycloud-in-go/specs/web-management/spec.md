## ADDED Requirements

### Requirement: Official frontend and auth contracts
The backend SHALL serve a pinned official frontend build and reference static/legacy routing and plugin discovery. It SHALL preserve management API methods, query/payload schemas, statuses, headers and auth login/logout/refresh behavior, including the pinned baseline's placeholder token behavior. The pinned baseline has no enabled management authorization policy; this SHALL be documented rather than represented as working access control.

#### Scenario: Frontend navigation
- **WHEN** the browser opens a nested /web route and then requests an asset
- **THEN** the route loads the frontend entry point and the asset receives its actual content and MIME type

#### Scenario: Reference placeholder auth
- **WHEN** the frontend submits admin and a present passwordHash field
- **THEN** login returns the baseline test token, logout returns empty success, and refresh accepts only that token

#### Scenario: Management authorization scope
- **WHEN** management APIs are accessed without a frontend token
- **THEN** baseline access remains available and documentation explicitly identifies the absence of management session enforcement

### Requirement: Management inventory coverage
The backend SHALL implement inventory-listed settings/overlays, boxes/tags, metadata/search/reload/update, content/library migration, upload/download/index/file/directory actions, statistics, cache and trigger APIs. Filesystem operations SHALL remain confined to configured roots, including symlink resolution.

#### Scenario: Root escape attempt
- **WHEN** an upload or delete path traverses outside an allowed root directly or through a symlink
- **THEN** the request fails without modifying any outside file

#### Scenario: Settings persistence through API
- **WHEN** a valid setting is changed using the official frontend API
- **THEN** the response contract matches the reference and the value survives restart

### Requirement: Custom tonies transactions
Custom-tonies APIs SHALL preserve documented upsert/delete/rename validation, numeric audio-ID normalization to strings, uppercase SHA1 values, paired-array validation, duplicate rejection, linked-content rename behavior and configured backup retention. Successful writes SHALL reload active metadata; multi-file rename SHALL be recoverable.

#### Scenario: Custom entry normalization
- **WHEN** a valid upsert contains numeric audio IDs and lowercase hashes
- **THEN** the stored entry uses string IDs and uppercase hashes and returns the reference success response

#### Scenario: Rename collision
- **WHEN** rename targets an existing model
- **THEN** the API returns the reference validation failure without altering metadata or linked content

#### Scenario: Interrupted linked rename
- **WHEN** the process terminates during a rename that updates content.json files
- **THEN** restart recovery produces a consistent old or new model relationship without silently orphaning links

### Requirement: Events and proxies
The backend SHALL preserve reference SSE event names/payloads and reverse/cloud/generic proxy contracts, including configured target selection. Slow or disconnected event clients SHALL NOT block box state updates.

#### Scenario: SSE disconnect
- **WHEN** an event consumer disconnects
- **THEN** its subscription is released and remaining clients continue to receive matching events

#### Scenario: Configured generic proxy
- **WHEN** a request selects a configured proxy target
- **THEN** forwarded path, credentials policy and response match the reference target contract

### Requirement: Firmware and certificate tools
The backend SHALL implement reference certificate upload/download/generation and ESP32 firmware upload/extraction/patch workflows using validated inputs and copied firmware images. Invalid images SHALL fail without modifying originals or installed credentials.

#### Scenario: Invalid firmware image
- **WHEN** a malformed image is submitted for extraction or patching
- **THEN** the operation fails clearly and existing firmware/certificates remain intact

#### Scenario: Certificate extraction
- **WHEN** a permitted synthetic or privately supplied known firmware fixture is processed
- **THEN** the extracted identities match the reference fixture results without logging private keys
