# GamePanel panel and daemon images.
#   docker build --target panel  -t gamepanel-panel  .
#   docker build --target daemon -t gamepanel-daemon .
# The web UI is committed (internal/web/dist), so the build needs only Go.
# Base images are pinned by digest; Dependabot updates the tag and digest together.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
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
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab AS panel
COPY --from=build /out/panel /usr/local/bin/panel
COPY --from=build --chown=65532:65532 /out/data /data
ENV GAMEPANEL_DB=/data/panel.db GAMEPANEL_ADDR=0.0.0.0:8080
VOLUME /data
EXPOSE 8080
USER nonroot:nonroot
# Ready once it serves requests and its database answers (no curl here: the
# panel asks itself).
HEALTHCHECK --interval=30s --timeout=10s --start-period=30s --retries=3 CMD ["/usr/local/bin/panel", "healthcheck"]
ENTRYPOINT ["/usr/local/bin/panel"]

# The daemon drives the host's Docker through its socket, so server folders
# are host paths: mount the data directory at the same path inside and out
# (see compose.yml). iproute2 and util-linux (nsenter) are for network limits;
# iptables (both variants: it uses the one Docker uses) for isolating servers.
FROM alpine:3.24@sha256:f59abe656b692d901e184be3445e084786602108599d2a51d229c0f5a48c71bf AS daemon
RUN apk add --no-cache ca-certificates iproute2 iproute2-tc util-linux-misc tzdata iptables iptables-legacy
COPY --from=build /out/daemon /usr/local/bin/daemon
ENV GAMEPANEL_DATA_DIR=/var/lib/gamepanel-daemon
EXPOSE 2022
ENTRYPOINT ["/usr/local/bin/daemon"]
