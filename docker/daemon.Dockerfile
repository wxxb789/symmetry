# syntax=docker/dockerfile:1

ARG GO_IMAGE=golang:1.27.0-bookworm

FROM ${GO_IMAGE} AS build

WORKDIR /src

COPY daemon/go.* ./
RUN go mod download

COPY daemon/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w -buildid=' \
    -o /out/symmetry-daemon ./cmd/symmetry-daemon \
    && CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w -buildid=' \
    -o /out/symmetry-fake-agent ./cmd/symmetry-fake-agent \
    && mkdir -p /out/state /out/workspaces

FROM debian:bookworm-slim AS certificates

RUN apt-get update -y \
    && apt-get install --no-install-recommends -y ca-certificates \
    && rm -rf /var/lib/apt/lists/*

FROM scratch

COPY --from=build /out/symmetry-daemon /symmetry-daemon
COPY --from=build /out/symmetry-fake-agent /symmetry-fake-agent
COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build --chown=65532:65532 /out/state /var/lib/symmetry
COPY --from=build --chown=65532:65532 /out/workspaces /workspaces
COPY --chown=65532:65532 docker/daemon-config.json /etc/symmetry/daemon.json

USER 65532:65532
ENTRYPOINT ["/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"]
