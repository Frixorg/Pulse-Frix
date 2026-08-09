# syntax=docker/dockerfile:1
# ---------------------------------------------------------------------------
# Pulse agent image. Builds a static Go binary and runs it as a non-root user.
# The agent needs READ-ONLY Docker socket access for container discovery; mount
# the socket read-only (":ro") or use a read-only socket proxy. See SECURITY.md.
# ---------------------------------------------------------------------------
FROM golang:1.22-alpine AS build
WORKDIR /src

# Cache modules first (agent has no external deps, but this stays correct if any
# are added later).
COPY go.mod ./
RUN go mod download || true

COPY . .

ARG VERSION=0.1.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown
ENV CGO_ENABLED=0
RUN go build -trimpath \
      -ldflags "-s -w \
        -X github.com/frix-me/pulse/agent/internal/version.Version=${VERSION} \
        -X github.com/frix-me/pulse/agent/internal/version.Commit=${COMMIT} \
        -X github.com/frix-me/pulse/agent/internal/version.BuildDate=${BUILD_DATE}" \
      -o /out/pulse-agent ./cmd/pulse-agent && \
    go build -trimpath -ldflags "-s -w" -o /out/pulse ./cmd/pulse

# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pulse-agent /usr/local/bin/pulse-agent
COPY --from=build /out/pulse /usr/local/bin/pulse

# Runs as the distroless "nonroot" user (uid 65532). To read the Docker socket,
# the socket's group must be granted to this user at run time (or use a proxy).
USER nonroot:nonroot
ENV PULSE_DATA_DIR=/data
VOLUME ["/data"]
ENTRYPOINT ["/usr/local/bin/pulse-agent"]
