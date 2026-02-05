FROM rust:latest AS builder

ARG GIT_REPO=https://github.com/tos-network/tos
ARG GIT_REF=main

RUN apt-get update && apt-get install -y --no-install-recommends \
    clang cmake libclang-dev pkg-config git protobuf-compiler libprotobuf-dev \
    && rm -rf /var/lib/apt/lists/*

ENV PROTOC_INCLUDE=/usr/include

RUN rustup toolchain install nightly --profile minimal && rustup default nightly

WORKDIR /source
RUN git clone --depth=1 --branch ${GIT_REF} ${GIT_REPO} /source/tos
WORKDIR /source/tos

RUN cargo build --release --bin tos_daemon --bin conformance

FROM debian:bullseye-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates curl && rm -rf /var/lib/apt/lists/*

ENV LAB_STATE_DIR=/state
ENV LAB_RPC_PORT=8080

WORKDIR /tos
COPY --from=builder /source/tos/target/release/conformance /usr/local/bin/tos_conformance

RUN mkdir -p /state

EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --retries=10 \
  CMD curl -f http://localhost:8080/health || exit 1

ENTRYPOINT ["/usr/local/bin/tos_conformance"]
