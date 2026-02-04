FROM rust:1.86-bullseye AS builder

ARG GIT_REPO=https://github.com/tos-network/tos
ARG GIT_REF=main

RUN apt-get update && apt-get install -y --no-install-recommends \
    clang cmake libclang-dev pkg-config git && rm -rf /var/lib/apt/lists/*

WORKDIR /source
RUN git clone --depth=1 --branch ${GIT_REF} ${GIT_REPO} /source/tos
WORKDIR /source/tos

RUN cargo build --release --bin tos_daemon

FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl && rm -rf /var/lib/apt/lists/*

ENV LAB_STATE_DIR=/state
ENV LAB_RPC_PORT=8080

WORKDIR /tos
COPY --from=builder /source/tos/target/release/tos_daemon /usr/local/bin/tos_daemon

RUN mkdir -p /state

COPY <<'SH' /usr/local/bin/entrypoint.sh
#!/usr/bin/env bash
set -euo pipefail

STATE_DIR="${LAB_STATE_DIR:-/state}"
RPC_PORT="${LAB_RPC_PORT:-8080}"

exec /usr/local/bin/tos_daemon \
  --conformance-mode \
  --state-dir="${STATE_DIR}" \
  --port="${RPC_PORT}" \
  "$@"
SH

RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=10 \
  CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
