# GamePanel panel and daemon images.
#   docker build --target panel  -t gamepanel-panel  .
#   docker build --target daemon -t gamepanel-daemon .
# The web UI is committed (internal/web/dist), so the build needs only Go.
ARG GO_VERSION=1.27.4

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
ARG TARGETOS TARGETARCH VERSION=dev COMMIT="" DATE=""
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags "-s -w -X github.com/ZsombiiHUN/gamepanel/internal/version.Version=${VERSION} -X github.com/ZsombiiHUN/gamepanel/internal/version.Commit=${COMMIT} -X github.com/ZsombiiHUN/gamepanel/internal/version.Date=${DATE}" \
      -o /out/ ./cmd/panel ./cmd/daemon
# An empty /data owned by distroless's nonroot user (65532), so a new volume
# starts out writable by the panel.
RUN mkdir -p /out/data && chown 65532:65532 /out/data && chmod 0700 /out/data

# The panel: one static binary and a SQLite file. Runs as a non-root user and
# keeps its database and key file in /data.
FROM gcr.io/distroless/static-debian12:nonroot AS panel
COPY --from=build /out/panel /usr/local/bin/panel
COPY --from=build --chown=65532:65532 /out/data /data
ENV GAMEPANEL_DB=/data/panel.db GAMEPANEL_ADDR=0.0.0.0:8080
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/usr/local/bin/panel"]

# The daemon drives the host's Docker through its socket, so server folders
# are host paths: mount the data directory at the same path inside and out
# (see compose.yml). iproute2 and util-linux (nsenter) are for network limits.
FROM alpine:3.24 AS daemon
RUN apk add --no-cache ca-certificates iproute2 iproute2-tc util-linux-misc tzdata
COPY --from=build /out/daemon /usr/local/bin/daemon
ENV GAMEPANEL_DATA_DIR=/var/lib/gamepanel-daemon
EXPOSE 2022
ENTRYPOINT ["/usr/local/bin/daemon"]
