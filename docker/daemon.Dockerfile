# syntax=docker/dockerfile:1

ARG GO_IMAGE=golang:1.27.0-bookworm

FROM ${GO_IMAGE} AS build

WORKDIR /src

COPY daemon/go.* ./
RUN go mod download

COPY daemon/ ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags='-s -w -buildid=' \
    -o /out/symmetry-daemon ./cmd/symmetry-daemon

FROM scratch

COPY --from=build /out/symmetry-daemon /symmetry-daemon
COPY --chown=65532:65532 docker/daemon-config.json /etc/symmetry/daemon.json

USER 65532:65532
ENTRYPOINT ["/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"]
