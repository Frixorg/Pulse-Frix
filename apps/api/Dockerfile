# syntax=docker/dockerfile:1
# Pulse control-plane API image. Default build uses the in-memory store; pass
# --build-arg TAGS=pgx (and vendor pgx) for the PostgreSQL adapter.
FROM golang:1.22-alpine AS build
WORKDIR /src
COPY go.mod ./
RUN go mod download || true
COPY . .
ARG VERSION=0.1.0-dev
ARG TAGS=
ENV CGO_ENABLED=0
RUN go build -trimpath -tags "${TAGS}" -ldflags "-s -w" -o /out/pulse-api ./cmd/pulse-api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/pulse-api /usr/local/bin/pulse-api
USER nonroot:nonroot
EXPOSE 8080
ENV PULSE_API_ADDR=":8080"
ENTRYPOINT ["/usr/local/bin/pulse-api"]
