# syntax=docker/dockerfile:1

# Pier — container image.
# Three stages: the SolidJS interface, the Go binary that embeds it, and a
# small runtime carrying the Docker CLI that Pier shells out to.

ARG DENO_VERSION=2.9.5
ARG GO_VERSION=1.27.4
ARG ALPINE_VERSION=3.24

# The interface and the binary are built on the machine's own architecture and
# cross-compiled, so multi-platform builds need no emulation.
FROM --platform=$BUILDPLATFORM denoland/deno:${DENO_VERSION} AS web
WORKDIR /src/web
COPY web/package.json web/deno.json web/deno.lock ./
RUN deno install --frozen
COPY web/ ./
RUN deno task build

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/ cmd/
COPY internal/ internal/
COPY --from=web /src/web/dist/ internal/webui/dist/
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags="-s -w" -o /out/pier ./cmd/pier

FROM alpine:${ALPINE_VERSION}
LABEL org.opencontainers.image.title="Pier" \
      org.opencontainers.image.source="https://github.com/AllanAdegas/pier" \
      org.opencontainers.image.licenses="Apache-2.0"
# docker-cli-buildx: the static-site build pack needs BuildKit (heredocs).
# tzdata: scheduled tasks and backups follow the TZ of the container.
# tini: reaps the docker/compose processes Pier leaves behind when it cancels.
RUN apk add --no-cache ca-certificates docker-cli docker-cli-buildx docker-cli-compose tini tzdata
COPY --from=build /out/pier /usr/local/bin/pier

# The loopback of a container is not the machine: ports published by the
# host's Docker are reached through the gateway (see compose.yaml).
ENV PIER_ADDR=:8700 \
    PIER_DATA_DIR=/data/pier \
    PIER_LOCAL_HOST=host.docker.internal

EXPOSE 8700
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
    CMD wget -q -O /dev/null http://127.0.0.1:8700/api/v1/version || exit 1

ENTRYPOINT ["/sbin/tini", "--", "pier"]
