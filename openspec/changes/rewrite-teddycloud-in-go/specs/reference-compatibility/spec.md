## ADDED Requirements

### Requirement: Pinned behavioral inventory
The project SHALL maintain a compatibility inventory anchored to official C commit `78c3b38e36f665d3a5fffe907f73a02725176377`, covering every dispatched route and binary handler, settings key/default, persisted format, CLI mode, integration and utility. Each entry SHALL link its reference source, implementation milestone, test evidence and status; exceptions SHALL be explicit.

#### Scenario: Inventory completeness
- **WHEN** the inventory is checked against the pinned route table and settings definitions
- **THEN** every reference entry is represented, including v3 routes, listener restrictions and non-HTTP binary handling

### Requirement: Differential acceptance
The project SHALL compare C and Go behavior using isolated copied data, sanitized fixtures and a mock upstream. Comparisons SHALL cover status, significant headers, decoded payloads, exact content bytes and persistent effects, with an explicit allowlist of nondeterministic differences.

#### Scenario: Equivalent request replay
- **WHEN** the same deterministic request sequence is replayed against both implementations
- **THEN** the compatibility runner fails for any unallowlisted response or storage difference

#### Scenario: Private data isolation
- **WHEN** reference fixtures are prepared for source control
- **THEN** private certificates, tokens and proprietary audio are excluded or replaced with synthetic fixtures

### Requirement: Evidence-based completion
The project SHALL keep unfinished inventory entries visible and SHALL declare replacement parity only after all in-scope entries pass their checks. Device support claims SHALL include hardware and firmware versions with recorded playback, TLS and settings verification; synthetic tests SHALL be labeled separately.

#### Scenario: Hardware unavailable
- **WHEN** automated device tests pass but no physical target has been exercised
- **THEN** hardware acceptance remains pending and full device support is not declared
