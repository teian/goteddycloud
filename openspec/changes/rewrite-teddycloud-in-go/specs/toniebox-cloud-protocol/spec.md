## ADDED Requirements

### Requirement: Device protocol contracts
The backend SHALL implement the pinned v1 time/claim/freshness/log/cloud-reset/OTA routes, v1/v2 content routes and v3 freshness/check-ota/ota/setup-status/chapter/content-meta routes with reference method, path, authentication, status and payload behavior. Protobuf SHALL preserve field numbers, proto2 presence and unknown fields and reject missing required fields.

#### Scenario: Freshness settings
- **WHEN** a known box performs a valid v1 freshness check
- **THEN** the decoded response contains the correct per-box speaker/headphone limits, slap and LED settings and reference content marks

#### Scenario: V3 reference behavior
- **WHEN** a sanitized v3 request is replayed
- **THEN** the response and effects match the pinned handler contract without implying untested TB2 hardware support

#### Scenario: Malformed protocol input
- **WHEN** a truncated or oversized freshness payload is received
- **THEN** processing fails in bounded time without a crash or settings mutation

### Requirement: Cloud routing policy
The backend SHALL preserve per-box cloud enablement, forwarding credentials, nocloud filtering, live handling, overrides, cache and passthrough decisions. Upstream requests SHALL use bounded timeouts and cancellation and SHALL NOT forward tags excluded by the effective policy.

#### Scenario: Protected custom tag
- **WHEN** a tag marked nocloud without an overriding permission is claimed or checked
- **THEN** the request is handled locally and no excluded tag identity reaches the mock upstream

#### Scenario: Upstream unavailable
- **WHEN** a permitted passthrough request fails upstream
- **THEN** the backend follows the reference error/fallback contract without exposing credentials

### Requirement: OTA behavior
The backend SHALL preserve reference firmware selection, upstream/cache policy and device-generation-specific OTA storage behavior, publishing completed downloads atomically and leaving existing valid artifacts intact on failure.

#### Scenario: Interrupted OTA cache
- **WHEN** an upstream firmware download terminates before completion
- **THEN** no partial artifact becomes available as a valid cached firmware file

### Requirement: RTNL transport and state
The backend SHALL accept the reference binary RTNL framing through the device transport and map supported log messages to the correct box state/events. Parsers SHALL bound frame sizes and isolate malformed clients.

#### Scenario: Fragmented binary message
- **WHEN** a valid RTNL frame arrives over several transport reads
- **THEN** it produces the same decoded state/event updates as the reference exactly once

#### Scenario: Box state isolation
- **WHEN** two boxes report different playback states
- **THEN** each state update is associated with its own identity
