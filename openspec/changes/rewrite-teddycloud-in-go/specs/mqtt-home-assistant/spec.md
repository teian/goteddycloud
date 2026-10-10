## ADDED Requirements

### Requirement: Telemetry discovery and broker compatibility
The backend SHALL preserve reference MQTT topic structures, payloads, Home Assistant discovery entities, box telemetry/events and embedded MQTT server contracts identified by the inventory. It SHALL derive state from the same box services as HTTP/SSE.

#### Scenario: RTNL telemetry publication
- **WHEN** a box reports playback and volume through RTNL
- **THEN** MQTT and Home Assistant receive the reference-compatible state and discovery identifies actual volume as read-only

### Requirement: Persisted command semantics
The backend SHALL validate MQTT and REST box commands through shared services: speaker/headphone limits 0–3, LED 0–2, slap enable/direction and server-side stop. Settings SHALL persist in box overlays and apply at the next freshness check; unsupported immediate volume, mute and pause/resume SHALL NOT be advertised.

#### Scenario: Deferred volume limit
- **WHEN** a valid MQTT volume-limit command is received
- **THEN** persisted state updates immediately and the box receives the new limit on its next freshness check

#### Scenario: Out-of-range command
- **WHEN** a volume limit of 5 or LED value of 9 is received
- **THEN** the prior setting remains unchanged and no invalid value is published as applied

#### Scenario: REST and MQTT consistency
- **WHEN** equivalent settings commands arrive through each transport
- **THEN** they produce equivalent persistent overlays and freshness responses

### Requirement: Reconnect recovery
MQTT SHALL retry connection failures using reference exponential backoff from 5 seconds up to 60 seconds, reset on success and restore subscriptions, state and discovery after reconnect. Broker unavailability SHALL NOT permanently disable integration.

#### Scenario: Broker outage and recovery
- **WHEN** the broker is unavailable at startup and later becomes reachable
- **THEN** the client retries with bounded backoff then restores subscriptions/discovery without requiring a backend restart
