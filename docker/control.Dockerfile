# syntax=docker/dockerfile:1

ARG ELIXIR_IMAGE=elixir:1.20.4-otp-29-slim
ARG RUNTIME_IMAGE=debian:bookworm-slim

FROM ${ELIXIR_IMAGE} AS build

RUN apt-get update -y \
    && apt-get install --no-install-recommends -y build-essential git \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV MIX_ENV=prod

RUN mix local.hex --force && mix local.rebar --force

COPY control/mix.exs control/mix.lock ./
RUN mix deps.get --only $MIX_ENV

COPY control/config/config.exs config/config.exs
RUN mix deps.compile

COPY control/ ./
RUN if [ -d assets ]; then mix assets.deploy; fi \
    && mix compile \
    && mix release

FROM ${RUNTIME_IMAGE} AS runtime

RUN apt-get update -y \
    && apt-get install --no-install-recommends -y ca-certificates libncurses6 libstdc++6 openssl \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
ENV HOME=/app \
    LANG=C.UTF-8 \
    MIX_ENV=prod \
    PHX_SERVER=true

COPY --from=build --chown=nobody:root /app/_build/prod/rel/symmetry_control /app

USER nobody
EXPOSE 4000
ENTRYPOINT ["/app/bin/symmetry_control"]
CMD ["start"]
