# Real S3 provider tests

These are isolated local acceptance setups, using disposable credentials and
unversioned buckets. The production client and storage layer are exercised
directly; there is no protocol-normalizing proxy or server patch.

## Silo

Tested [PGSTY Silo](https://github.com/pgsty/silo/releases/tag/RELEASE.2026-09-16T00-00-00Z),
Linux amd64 archive `silo_20260916000000.0.0_linux_amd64.tar.gz`, SHA-256
`381e745510a8fb64323d7bb3207f95984b7f4ed826f4fcad318f97683c420c73`.
Download and extract the official release to a private test directory, then run:

```sh
MINIO_ROOT_USER=goteddytest \
MINIO_ROOT_PASSWORD=goteddy-local-test-secret \
./silo server ./data --address 127.0.0.1:19000 \
  --console-address 127.0.0.1:19001
```

In another shell, provision the bucket and run the suite:

```sh
export AWS_ACCESS_KEY_ID=goteddytest
export AWS_SECRET_ACCESS_KEY=goteddy-local-test-secret
export AWS_REGION=us-east-1
unset AWS_SESSION_TOKEN
curl --fail --aws-sigv4 aws:amz:us-east-1:s3 \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" \
  -X PUT -d '' http://127.0.0.1:19000/goteddy-integration

TEDDYCLOUD_TEST_S3_ENDPOINT=http://127.0.0.1:19000 \
TEDDYCLOUD_TEST_S3_BUCKET=goteddy-integration \
go test -buildvcs=false -race -count=1 -v \
  ./internal/s3api ./internal/storage ./internal/app \
  -run 'TestExternalProvider|TestBackendContract|TestS3' -timeout 180s
```

## NooBaa

Tested the official [NooBaa Core](https://github.com/noobaa/noobaa-core)
`quay.io/noobaa/noobaa-core:master-20261009` image, revision `df54b21`, pinned as
`quay.io/noobaa/noobaa-core@sha256:473caa9b563720d15724511e559843e4cce0c08060a54856948978041da34f6d`.
This tests configured NSFS with native POSIX backing, not a Kubernetes deployment
or a NooBaa object pool. Its simple single-user mode crashed before processing
the first S3 request, so configured account mode is required for this image.

Save the following as `noobaa-test-start.sh` in a private test directory:

```sh
#!/bin/bash
set -eu
mkdir -p /tmp/goteddy-data /tmp/goteddy-config
# HTTP is allowed only for this isolated loopback acceptance deployment.
printf '{"ALLOW_HTTP":true}\n' > /tmp/goteddy-config/config.json
if [ ! -d /tmp/goteddy-config/accounts_by_name ]; then
  node /root/node_modules/noobaa-core/src/cmd/manage_nsfs.js account add \
    --config_root /tmp/goteddy-config --name goteddytest --uid 10001 --gid 0 \
    --new_buckets_path /tmp/goteddy-data \
    --access_key GOTEDDYTEST000000001 \
    --secret_key abcdefghijabcdefghijabcdefghijabcdefghij
fi
exec node /root/node_modules/noobaa-core/src/cmd/nsfs.js \
  --config_root /tmp/goteddy-config --http_port 6001 --https_port 6443
```

Create the isolated container, copy the script and start it:

```sh
podman create --name goteddy-noobaa-test -p 127.0.0.1:19010:6001 \
  --entrypoint /bin/bash \
  quay.io/noobaa/noobaa-core@sha256:473caa9b563720d15724511e559843e4cce0c08060a54856948978041da34f6d \
  /tmp/start.sh
podman cp noobaa-test-start.sh goteddy-noobaa-test:/tmp/start.sh
podman start goteddy-noobaa-test
podman logs goteddy-noobaa-test
```

After startup, provision the bucket and run the same suite:

```sh
export AWS_ACCESS_KEY_ID=GOTEDDYTEST000000001
export AWS_SECRET_ACCESS_KEY=abcdefghijabcdefghijabcdefghijabcdefghij
export AWS_REGION=us-east-1
unset AWS_SESSION_TOKEN
curl --fail --aws-sigv4 aws:amz:us-east-1:s3 \
  --user "$AWS_ACCESS_KEY_ID:$AWS_SECRET_ACCESS_KEY" \
  -X PUT -d '' http://127.0.0.1:19010/goteddy-integration

TEDDYCLOUD_TEST_S3_ENDPOINT=http://127.0.0.1:19010 \
TEDDYCLOUD_TEST_S3_BUCKET=goteddy-integration \
go test -buildvcs=false -race -count=1 -v \
  ./internal/s3api ./internal/storage ./internal/app \
  -run 'TestExternalProvider|TestBackendContract|TestS3' -timeout 180s

podman stop goteddy-noobaa-test
podman rm goteddy-noobaa-test
```

Each test owns a random prefix and cleans up only that prefix. Interrupted tests
can leave test objects; use a disposable bucket. Fault injection is explicitly
skipped with external endpoints. Run the default suite separately, without
`TEDDYCLOUD_TEST_S3_ENDPOINT`, to check injected publication and copy failures.
FFmpeg must be installed to exercise the playlist/live conversion assertions.
