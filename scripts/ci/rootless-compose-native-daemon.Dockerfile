FROM debian:bookworm-slim

RUN apt-get update -y \
    && apt-get install --no-install-recommends -y ca-certificates libgcc-s1 libstdc++6 \
    && rm -rf /var/lib/apt/lists/*

COPY symmetry-daemon /symmetry-daemon
RUN chmod 0555 /symmetry-daemon \
    && mkdir -p /etc/symmetry /var/lib/symmetry /workspaces /tmp \
    && chown -R 65532:65532 /var/lib/symmetry /workspaces /tmp \
    && chmod 0700 /var/lib/symmetry /workspaces /tmp

USER 65532:65532
ENTRYPOINT ["/symmetry-daemon", "-config", "/etc/symmetry/daemon.json"]
