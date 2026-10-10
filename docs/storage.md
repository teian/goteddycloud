# Deployment storage

Filesystem is the default. Set the backend before startup so even settings and
certificates load from the selected storage. All durable deployment data uses
`storage.Backend`: configuration/overlays, management users, catalog and rename
journals/backups, content/library metadata and audio, firmware/OTA, certificates,
web/plugin assets, image caches, RTNL logs, PCAP and TLS key logs. The box TLS
child relays key logs to the parent's authenticated private endpoint.

```sh
export TEDDYCLOUD_STORAGE=s3
export TEDDYCLOUD_S3_BUCKET=my-teddycloud
export TEDDYCLOUD_S3_PREFIX=deployment
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID='your-access-key'
export AWS_SECRET_ACCESS_KEY='your-secret-key'
# Optional for S3-compatible services:
export TEDDYCLOUD_S3_ENDPOINT=https://objects.example.net
export TEDDYCLOUD_S3_PATH_STYLE=true
bin/goteddycloud -base_path /deployment -http 127.0.0.1:18081 \
  -httpsecure 127.0.0.1:19443 -box 127.0.0.1:19444
```

`TEDDYCLOUD_STORAGE=filesystem` restores the filesystem backend. Backend changes
require a restart. Provision the bucket separately. Endpoint URLs may include a
path prefix but cannot contain credentials, query parameters or fragments. With
no custom endpoint, requests use the regional AWS S3 endpoint and virtual-host
addressing. Path-style addressing is optional and commonly useful for custom
endpoints. Select the bucket's actual region; redirects are rejected rather than
forwarding credentials.

| Variable | Behavior |
| --- | --- |
| `TEDDYCLOUD_STORAGE` | `filesystem` (default) or `s3`; unknown values fail startup |
| `TEDDYCLOUD_S3_BUCKET` | Required existing bucket |
| `TEDDYCLOUD_S3_PREFIX` | Optional deployment namespace; no parent traversal |
| `TEDDYCLOUD_S3_ENDPOINT` | Optional S3-compatible HTTP(S) endpoint; use HTTPS for remote deployments |
| `TEDDYCLOUD_S3_PATH_STYLE` | Boolean, default `false` |
| `AWS_REGION`, `AWS_DEFAULT_REGION` | Region, in this precedence order; fallback `us-east-1` |
| `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY` | Environment credentials; partial pairs fail startup |
| `AWS_SESSION_TOKEN` | Optional token for supplied temporary credentials |
| `AWS_SHARED_CREDENTIALS_FILE` | Shared credentials file; default `~/.aws/credentials` |
| `AWS_PROFILE` | Shared credentials profile; default `default` |
| `TEDDYCLOUD_STORAGE_TEMP_DIR` | Optional existing directory for private S3 upload staging |

Environment credentials take precedence over the shared profile. Profiles support
`aws_access_key_id`, `aws_secret_access_key`, and `aws_session_token`.
Credentials load at backend creation: restart after rotation. Automatic IAM role,
web identity, SSO and credential-process discovery are not implemented. Obtain
those credentials externally and provide the resulting key/secret/token.
Credentials are not teddyCloud settings and are never written to the bucket.

## Paths and migration

The base path is a logical deployment root in S3 mode; it need not exist locally.
For example `/deployment/data/library/song.taf` with prefix `deployment` maps to
`deployment/data/library/song.taf` in the bucket. Configured absolute paths must
remain inside the logical base; outside-base paths fail instead of falling back
to disk. Relative paths retain the reference configuration semantics. Object
keys are case-sensitive, including on Windows. S3 has no symlinks.

Stop the current writer, take a verified snapshot, and copy the deployment tree
under the selected bucket prefix with an S3-aware copy tool. Preserve directory
layout, certificate material, settings, users, sidecars and frontend assets.
Directories containing objects are implicit; empty directories need zero-length
keys ending in `/`. Application directory creation maintains these markers,
including parents. Do not copy runtime scratch files or concurrently run the C
and Go servers against the same deployment. Validate the remote copy using the
same environment and `-validate` before cutover. Validation reads storage but
does not create objects or generate certificates. Changing the backend does not
itself copy or delete existing data. Rollback uses the retained snapshot.

Grant list access to the deployment prefix and get/put/delete access to its
objects. CopyObject needs source read and destination write access. Versioned
reads may also require permission to get object versions. Bucket policy and
provider encryption/access controls protect remote private keys: stored POSIX
mode metadata is descriptive and does not replace bucket authorization.

## Publication and consistency

File reads use conditional ranged GETs tied to the ETag/version observed at open,
with at most 1 MiB of read-ahead per handle. Replacement either preserves the
opened version or fails the read; it does not mix generations. Requests have a
60-second transport timeout and do not automatically retry mutations. A network
failure after server commit can leave the publication outcome uncertain; inspect
the destination before retrying a multi-object operation.

Atomic stream writes spool bounded input privately and perform one signed PUT to
the final key. Incomplete or oversized input is rejected before that PUT. Mutable
file handles stage privately and publish on `Sync` or `Close`; callers must check
those errors. Exclusive creation uses conditional PUT. Uploads are limited to
5,000,000,000 bytes, with smaller application-specific limits. Multipart uploads
are not implemented. Staging files are mode 0600 and removed on normal cleanup.
After an abrupt process termination, remove orphaned `teddycloud-s3-*` and
`teddycloud-publish-*` scratch files once no writer is running. Interrupted cache
publication can leave private `.cache-*` objects; verify they are unused before
removing them.

S3 moves use copy then delete. Directory moves copy a snapshot of up to 100,000
objects before deleting any source; directory listings have the same entry cap
and paginate service responses. Moves are not atomic across objects. On copy
failure, retain the source and remove the partial destination before retrying.
On delete failure or process interruption, compare both trees against the
snapshot and preserve a verified complete destination before finishing source
cleanup. Readers may observe both trees during a move. Use exactly one server
writer per deployment: shared resource locks coordinate only this process, and
there is no distributed transaction or lease protocol.

Appending logs in S3 rewrites the current object. Diagnostic writes sync before
returning; RTNL files publish on close. This is suitable for bounded, opt-in
diagnostics, but object-request volume and write cost grow with logging activity.
Filesystem diagnostics retain their existing buffered behavior.

## Local scratch and tools

FFmpeg work files, uploaded conversion inputs, selected-track export files and
live audio spools remain private local temporary files. Use the platform's
temporary-directory settings (for example `TMPDIR`) for these; the S3 staging
override applies specifically to upload staging. Allocate local disk for concurrent
jobs and the largest permitted artifact. Completed output publishes through the
selected backend. Externally growing TAF files require filesystem storage; S3
objects are immutable snapshots. Generated live audio still streams from its
local spool and publishes the completed TAF remotely.

The explicit standalone `-encode`, `-encode_test`, and `-hash` commands operate on
local input/output files. Fixture generation and compatibility/probe tools also
use local isolated test data. Firmware operations and client-certificate
operations that open a deployment use its selected backend. Executables, AWS
credential profiles and temporary files are host resources, not deployment data.

## Verification

`go test -buildvcs=false ./...` includes shared filesystem/S3 contract tests,
conditional creation, cancellation, failed publication, object replacement,
paginated listings and directory-copy failure preservation. S3 application tests
exercise restart persistence, users, catalog linked renames, files, certificates,
RTNL/diagnostics, OTA, firmware upload/patch/extraction, cache/migration, images,
playlists and generated live audio;
they assert that no local deployment directory is created. Signing is checked
against all four published AWS Signature V4 examples. The S3 protocol emulator
is test-only; its incorrect ranged-response HTTP 200 is normalized to HTTP 206.
See [verification results](storage-verification.md) for recorded checks and limits.
Real Silo and NooBaa NSFS endpoints also pass the storage and application suites;
see [provider test setup](s3-providers.md). To use an existing, unversioned test
bucket, supply credentials through the normal AWS environment/profile mechanism:

```sh
TEDDYCLOUD_TEST_S3_ENDPOINT=http://127.0.0.1:19000 \
TEDDYCLOUD_TEST_S3_BUCKET=goteddy-integration \
go test -buildvcs=false -race -count=1 -v \
  ./internal/s3api ./internal/storage ./internal/app \
  -run 'TestExternalProvider|TestBackendContract|TestS3' -timeout 180s
```

Each test creates a random `goteddycloud-test/` prefix and deletes only its own
objects during cleanup. Tests use path-style requests. Fault-injection cases
explicitly skip in external mode; run the default suite separately to exercise
them against the emulator. These environment variables affect tests only.
No live AWS account, versioned bucket, or full NooBaa Kubernetes deployment was
exercised. Physical-device acceptance remains separate.
