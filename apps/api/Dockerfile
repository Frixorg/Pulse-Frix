# syntax=docker/dockerfile:1
# Pulse control-plane API image.
#
# The default build is what you want: in-memory store, browser SSH console
# included. Optional build args:
#   --build-arg TAGS=pgx            PostgreSQL store adapter (fetches pgx)
#   --build-arg TAGS=nosshconsole   drop the SSH client (pure-stdlib binary)
# Tags combine: --build-arg TAGS="pgx nosshconsole".
FROM golang:1.22-alpine AS build
WORKDIR /src
# go.sum pins the one direct dependency (golang.org/x/crypto, the SSH client),
# so this layer is cached and verified.
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=0.1.0-dev
ARG TAGS=
ENV CGO_ENABLED=0
# ONLY the optional pgx (PostgreSQL) build fetches the driver — over HTTPS from
# the Go module proxy (no git, no Alpine packages needed). If go.sum already
# contains pgx (pre-generated on the host), `go mod download` uses it.
RUN if echo " ${TAGS} " | grep -q " pgx "; then \
      go mod download github.com/jackc/pgx/v5 2>/dev/null || \
      (go get github.com/jackc/pgx/v5@v5.6.0 && go mod tidy); \
    fi
RUN go build -trimpath -tags "${TAGS}" -ldflags "-s -w" -o /out/pulse-api ./cmd/pulse-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pulse-api /usr/local/bin/pulse-api
USER nonroot:nonroot
EXPOSE 8080
ENV PULSE_API_ADDR=":8080"
ENTRYPOINT ["/usr/local/bin/pulse-api"]
