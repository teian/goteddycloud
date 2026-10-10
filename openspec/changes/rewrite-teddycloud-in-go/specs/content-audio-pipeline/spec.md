## ADDED Requirements

### Requirement: Existing content and metadata
The backend SHALL read existing content/library layouts, UID mapping, content.json and tonies/custom-tonies metadata, preserving unknown fields and live/nocloud/cloud_override semantics during supported edits.

#### Scenario: Existing library reuse
- **WHEN** a copied reference deployment is opened
- **THEN** tag-to-audio resolution and metadata match the reference without rewriting audio files

### Requirement: TAF integrity and range delivery
The backend SHALL parse and produce reference-compatible TAF framing, protobuf headers, audio-payload SHA1, lengths, audio IDs, track/page indexes and Ogg block/page layout. Stored delivery SHALL preserve reference range/status/header semantics and use bounded buffering.

#### Scenario: Byte range playback
- **WHEN** a device requests a supported range from an existing TAF
- **THEN** the response has reference-compatible range headers/status and exactly the requested bytes

#### Scenario: Invalid TAF
- **WHEN** a file has a truncated header or inconsistent payload hash/length
- **THEN** validation rejects the file and does not replace valid content

#### Scenario: Generated TAF structure
- **WHEN** a multi-track fixture is converted
- **THEN** the oracle validates its header, checksums, track offsets and 4096-byte layout constraints

### Requirement: Atomic concurrent caching
The backend SHALL serialize publication for the same cache key and expose only complete validated artifacts; failed or cancelled transfers SHALL NOT replace an existing valid file.

#### Scenario: Concurrent cache miss
- **WHEN** two clients request the same uncached content
- **THEN** each receives valid content and the final cache contains one complete artifact

#### Scenario: Partial transfer
- **WHEN** the upstream disconnects during caching
- **THEN** the incomplete transfer is not published as playable cached content

### Requirement: Conversion playlists and live jobs
The backend SHALL support reference file/PCM conversion, playlists, stream conversion and live delivery using Go orchestration with explicit external FFmpeg where needed. Jobs SHALL use argument-vector invocation, bounded concurrency and cancellation with per-box stop semantics.

#### Scenario: Conversion process failure
- **WHEN** FFmpeg exits unsuccessfully
- **THEN** the job reports failure and leaves no successfully published invalid TAF

#### Scenario: Live stop
- **WHEN** a stop command targets a box receiving live audio
- **THEN** delivery and its owned processing stop without interrupting another box, while already buffered device audio is not claimed to stop immediately
