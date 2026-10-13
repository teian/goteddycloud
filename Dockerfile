# syntax=docker/dockerfile:1
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim@sha256:d6aa754f16b3197301076f047b5def2f02ea1dbbc2ca920407d46d7ec7f87b20 AS frontend
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates git python3 && rm -rf /var/lib/apt/lists/*
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

FROM --platform=$BUILDPLATFORM golang:1.26.8-bookworm@sha256:dc9ad6c05acc7a88e5b71bde60a5fe3bd4b9f0db209011711b464107438a8107 AS build
ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT
ARG VERSION=0.1.0
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w" -o internal/app/cc3200-tool.wasm ./cmd/cc3200wasm && cp /usr/local/go/lib/wasm/wasm_exec.js internal/app/cc3200-wasm-exec.js
RUN if [ "$TARGETARCH" = arm ]; then export GOARM="${TARGETVARIANT#v}"; fi; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -buildvcs=false -trimpath -ldflags="-s -w -X main.version=$VERSION" -o /out/goteddycloud ./cmd/goteddycloud

FROM debian:bookworm-slim@sha256:7c7b2c966bc9ee8cedfeef67e0e279108992c77681fa595db4a9d65c06ccc587 AS runtime-deps
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates && rm -rf /var/lib/apt/lists/*

# Package installation depends only on the pinned OS and package instructions.
# Application, UI and notice changes cannot invalidate the FFmpeg layer.
FROM runtime-deps AS ffmpeg-deps
RUN apt-get update && apt-get install -y --no-install-recommends ffmpeg && rm -rf /var/lib/apt/lists/*

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
