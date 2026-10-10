# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS build
ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=development
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o internal/app/cc3200-tool.wasm ./cmd/cc3200wasm && cp /usr/local/go/lib/wasm/wasm_exec.js internal/app/cc3200-wasm-exec.js
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/goteddycloud ./cmd/goteddycloud

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587 AS runtime
COPY --from=build /out/goteddycloud /usr/local/bin/goteddycloud
COPY --from=build /go/pkg/mod/google.golang.org/protobuf@v1.36.12/LICENSE /usr/share/doc/goteddycloud/protobuf-LICENSE
COPY --from=build /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY LICENSE NOTICE docs/licenses.md third_party/libopus-COPYING third_party/Go-LICENSE third_party/go-serial-LICENSE third_party/x-sys-LICENSE /usr/share/doc/goteddycloud/
COPY --chown=65532:65532 packaging/container-root/ /data/
WORKDIR /data
USER 65532:65532
VOLUME ["/data"]
EXPOSE 18080 18443 18444
ENTRYPOINT ["/usr/local/bin/goteddycloud"]
CMD ["-base_path", "/data", "-http", ":18080", "-httpsecure", ":18443", "-box", ":18444"]

FROM runtime AS ffmpeg
USER 0:0
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*
USER 65532:65532
