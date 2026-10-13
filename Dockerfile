# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:24-alpine3.24@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS frontend
RUN apk add --no-cache ca-certificates git python3
WORKDIR /src
COPY frontend/upstream-revision frontend/upstream-revision
RUN git init /upstream && git -C /upstream remote add origin https://github.com/toniebox-reverse-engineering/teddycloud_web.git && \
    git -C /upstream fetch --depth=1 origin "$(cat frontend/upstream-revision)" && git -C /upstream checkout --detach FETCH_HEAD
COPY scripts/prepare-frontend.py scripts/frontend-notices.py scripts/
COPY frontend/src frontend/src
RUN python3 scripts/prepare-frontend.py /upstream
WORKDIR /src/.cache/goteddycloud-web
RUN --mount=type=cache,target=/root/.npm npm ci --ignore-scripts --no-audit --no-fund
RUN npm run build && python3 /src/scripts/frontend-notices.py . /out/frontend-notices

FROM --platform=$BUILDPLATFORM golang:1.26.8-alpine3.24@sha256:8ac98ca534ac3f51e1f420a1dd2c15e74c75cfa0f23f3ad27eb5d7236c349a0c AS build
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o internal/app/cc3200-tool.wasm ./cmd/cc3200wasm && cp /usr/local/go/lib/wasm/wasm_exec.js internal/app/cc3200-wasm-exec.js
ARG VERSION=0.1.0
RUN if [ "$TARGETARCH" = arm ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/goteddycloud ./cmd/goteddycloud

FROM alpine:3.24.2@sha256:294b683cb724975bec92580e1e685676bd4b50bda910ddb8c51d4cabeaec77e6 AS runtime-deps
RUN apk add --no-cache ca-certificates

# Package installation depends only on the pinned OS and package instructions.
# Application, UI and notice changes cannot invalidate the FFmpeg layer.
FROM runtime-deps AS ffmpeg-deps
RUN apk add --no-cache ffmpeg

FROM runtime-deps AS runtime
COPY --link --from=build /go/pkg/mod/google.golang.org/protobuf@v1.36.12/LICENSE /usr/share/doc/goteddycloud/protobuf-LICENSE
COPY --link --from=frontend /out/frontend-notices/ /usr/share/doc/goteddycloud/frontend/
COPY --link LICENSE NOTICE docs/licenses.md third_party/libopus-COPYING third_party/Go-LICENSE third_party/go-serial-LICENSE third_party/x-sys-LICENSE third_party/kong-COPYING third_party/x-term-LICENSE /usr/share/doc/goteddycloud/
COPY --link --from=frontend /src/.cache/goteddycloud-web/dist/ /usr/share/goteddycloud/www/web/
COPY --link --chown=65532:65532 packaging/container-root/ /data/
COPY --link --from=build /out/goteddycloud /usr/local/bin/goteddycloud
ENV GOTEDDYCLOUD_BUNDLED_WWW=/usr/share/goteddycloud/www
WORKDIR /data
USER 65532:65532
VOLUME ["/data"]
EXPOSE 18080 18443 18444
ENTRYPOINT ["/usr/local/bin/goteddycloud"]
CMD ["-base_path", "/data", "-http", ":18080", "-httpsecure", ":18443", "-box", ":18444"]

FROM ffmpeg-deps AS ffmpeg
# Keep payloads separate and independent of preceding layers. COPY --link
# preserves unchanged UI/notices when only the server executable changes.
COPY --link --from=runtime /usr/share/doc/goteddycloud/ /usr/share/doc/goteddycloud/
COPY --link --from=runtime /usr/share/goteddycloud/www/ /usr/share/goteddycloud/www/
COPY --link --chown=65532:65532 --from=runtime /data/ /data/
COPY --link --from=runtime /usr/local/bin/goteddycloud /usr/local/bin/goteddycloud
ENV GOTEDDYCLOUD_BUNDLED_WWW=/usr/share/goteddycloud/www
WORKDIR /data
USER 65532:65532
VOLUME ["/data"]
EXPOSE 18080 18443 18444
ENTRYPOINT ["/usr/local/bin/goteddycloud"]
CMD ["-base_path", "/data", "-http", ":18080", "-httpsecure", ":18443", "-box", ":18444"]
