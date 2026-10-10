## ADDED Requirements

### Requirement: Pinned development parity
The implementation SHALL port official develop behavior through `8b39ac9c9cbc312ea1fa948f66b0dc014fd096c1`, with frontend `636a8d65043f6c6e322f76bcb11e958725159e96`, and classify every intervening commit and changed file.

#### Scenario: Audit reproducibility
- **WHEN** the pinned source inventory is regenerated
- **THEN** it records every changed file and commit, corresponding Go behavior or scoped exclusions, without treating simulation as physical acceptance.

### Requirement: Optional management sessions
The server SHALL support persistent users, salted password hashes, bounded expiring sessions, cookie and bearer authentication, login limits, user management and the development environment override. Box protocols SHALL retain their separate identity policy.

#### Scenario: Enforced management protection
- **WHEN** a user enables login protection
- **THEN** private management requests require a session, the login SPA remains public, and management OPTIONS requests return the configured preflight headers before login.

### Requirement: Development content and firmware behavior
The server SHALL support listened sidecars and sync marking, sidecar filtering, TAF lengthSeconds, selected Ogg or ZIP track exports and ESP32 connection-port patching with validated checksums.

#### Scenario: Export and sync
- **WHEN** selected TAF tracks are downloaded or assigned library content is synced
- **THEN** exported Ogg block bytes match the reference, and sync updates only the source file's listened sidecar while preserving unknown fields.

### Requirement: Development hardening
The server SHALL conceal secret settings, validate OTA hashes, isolate uploaded box credentials, require fresh box TLS identity, apply independent crawler/domain detection, prevent private-address downloads by default and safely process malformed input and growing audio.

#### Scenario: Download opt-in
- **WHEN** a user-supplied image URL resolves to a private address
- **THEN** the server refuses the connection unless core.allowPrivateFetch is enabled and dials only the address it validated.
