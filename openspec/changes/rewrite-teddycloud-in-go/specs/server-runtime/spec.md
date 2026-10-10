## ADDED Requirements

### Requirement: Standalone Go lifecycle
The backend SHALL build with CGO_ENABLED=0 without linking the C implementation. It SHALL support explicit base-path and listener configuration, startup diagnostics, bounded resource usage and graceful cancellation of connections, jobs and subscriptions.

#### Scenario: Shutdown during streaming
- **WHEN** shutdown is requested with active content delivery and conversion
- **THEN** listeners stop accepting work and owned workers/processes terminate within the configured shutdown deadline

#### Scenario: Listener startup failure
- **WHEN** a required listener cannot bind
- **THEN** startup fails clearly and releases previously acquired resources

### Requirement: Configuration and overlay compatibility
The backend SHALL preserve reference config.ini/config.overlay.ini semantics, settings names/types/defaults, base-path resolution, per-box inheritance and unknown persisted keys. Valid mutations SHALL persist atomically; invalid mutations SHALL leave the last valid state intact.

#### Scenario: Box override isolation
- **WHEN** one box overrides its speaker limit
- **THEN** that box receives the override after restart while other boxes retain their effective settings

#### Scenario: Invalid settings update
- **WHEN** an out-of-range value is submitted
- **THEN** the update fails without changing disk or active settings

### Requirement: Certificate identity and listener separation
The backend SHALL preserve imported certificate material and reference listener route exposure. Box HTTPS SHALL select TB1 identity without SNI and TB2 identity with SNI, independently of box generation; this rule SHALL NOT affect web HTTPS or MQTT. Box certificate identity policy SHALL be explicit and SHALL remain separate from web authentication and upstream verification.

#### Scenario: SNI selection
- **WHEN** clients connect to box HTTPS with and without SNI
- **THEN** each receives the corresponding configured certificate and neither handshake regenerates existing keys

#### Scenario: Administrative route isolation
- **WHEN** a client requests a web-only management endpoint on box HTTPS
- **THEN** the endpoint is unavailable under the reference listener access contract

### Requirement: Portable deployment
The project SHALL provide versioned Linux amd64/arm64, Windows amd64 and macOS amd64/arm64 backend builds and Linux amd64/arm64 container packaging, with license notices, FFmpeg requirements and a distinction between build and native runtime verification.

#### Scenario: Codec-free startup
- **WHEN** the backend starts without FFmpeg
- **THEN** existing TAF delivery works and conversion requests report the missing dependency clearly

### Requirement: Isolated legacy TLS compatibility
The backend SHALL preserve reference legacy TLS 1.2 RSA/SHA1 negotiation in an isolated box-listener child process. Enabling the Go legacy signature setting in that child SHALL NOT enable it in the parent web/upstream process. The parent SHALL supervise the child and stop it on shutdown or listener startup failure.

#### Scenario: Legacy box negotiation
- **WHEN** a legacy SHA1-only client connects to the isolated box listener
- **THEN** negotiation proceeds under the compatibility policy while the web/upstream parent retains strict TLS defaults

### Requirement: Selectable deployment storage
The backend SHALL support an injected filesystem or generic S3-compatible implementation for all durable deployment data, including settings, users, catalog, certificates, content, firmware, caches, web assets and diagnostics. Filesystem SHALL remain the default. S3 credentials SHALL support environment variables and shared credential profiles and SHALL NOT be persisted in teddyCloud settings. Local private scratch MAY support FFmpeg, live audio and upload staging.

#### Scenario: Remote restart
- **WHEN** an S3 deployment updates box settings and management users and restarts
- **THEN** those changes load from S3 without creating a local deployment directory

#### Scenario: Namespace confinement
- **WHEN** an S3 deployment selects a path outside its configured base
- **THEN** the operation fails without reading or writing the filesystem or another bucket prefix

#### Scenario: Remote read consistency
- **WHEN** an opened audio object is replaced during ranged reads
- **THEN** subsequent reads use the opened version or fail a condition rather than combining object versions

#### Scenario: Publication failure
- **WHEN** a stream exceeds its limit or is cancelled before publication
- **THEN** the existing destination remains available and private staging is cleaned up

#### Scenario: Directory move interruption
- **WHEN** an S3 directory copy fails
- **THEN** no source objects have been deleted and the documented recovery procedure accounts for partial destination copies
