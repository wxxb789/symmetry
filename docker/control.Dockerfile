# syntax=docker/dockerfile:1

ARG ELIXIR_IMAGE=elixir:1.20.4-otp-29-slim
ARG RUNTIME_IMAGE=debian:trixie-slim

FROM ${ELIXIR_IMAGE} AS build

RUN apt-get update -y \
    && apt-get install --no-install-recommends -y build-essential ca-certificates git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV MIX_ENV=prod

RUN mix local.hex --force && mix local.rebar --force

COPY control/mix.exs control/mix.lock ./
RUN mix deps.get --only $MIX_ENV

COPY control/config/ config/
RUN mix deps.compile

COPY control/ ./
RUN if [ -d assets ]; then mix assets.deploy; fi \
    && mix compile \
    && mix release

FROM ${RUNTIME_IMAGE} AS runtime

RUN apt-get update -y \
    && apt-get install --no-install-recommends -y ca-certificates curl gh git gnupg libncurses6 libstdc++6 openssl \
    && mkdir -p /etc/apt/keyrings \
    && curl -sLS https://packages.microsoft.com/keys/microsoft.asc \
      | gpg --dearmor -o /etc/apt/keyrings/microsoft.gpg \
    && chmod go+r /etc/apt/keyrings/microsoft.gpg \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/microsoft.gpg] https://packages.microsoft.com/repos/azure-cli/ bookworm main" \
      > /etc/apt/sources.list.d/azure-cli.list \
    && apt-get update -y \
    && apt-get install --no-install-recommends -y azure-cli \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV HOME=/app \
    GIT_CONFIG_GLOBAL=/app/.config/gh/gitconfig \
    LANG=C.UTF-8 \
    MIX_ENV=prod \
    PHX_SERVER=true

COPY --from=build --chown=nobody:root /app/_build/prod/rel/symmetry_control /app

RUN mkdir -p /app/.config/gh /app/.azure \
    && chown -R nobody:root /app/.config /app/.azure

USER nobody
EXPOSE 4000
ENTRYPOINT ["/app/bin/symmetry_control"]
CMD ["start"]
