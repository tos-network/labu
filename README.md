![LABU](./LABU.png)

# LABU - TOS multi-client conformance harness

Labu is a multi-client conformance test framework for the TOS ecosystem. It builds and
runs simulator containers that drive multiple client implementations through a common
API, then aggregates results into a standard JSON report format.

## Scenario/Expected Comparison

Labu does not define scenarios. Simulators do.

- **Scenarios** are delivered as **vectors** (JSON) produced by `tos-spec`.
- **Simulators** execute vectors and assert expected results.
- **Labu** only orchestrates containers and aggregates results.

This aligns with the Hive model: simulator-owned assertions, harness-owned orchestration.

## Quick start

```bash
# Build and run a simulator
./labu --sim tos/rpc --client tos-rust,avatar-c
```

## Local (non-Docker) single-client run

This path is for debugging on a Mac host without Docker. It runs the TOS conformance
server locally and uses the lightweight runner to execute vectors.

1. Build and start the conformance server:
```bash
cd ~/tos
cargo build -p tos_daemon --bin conformance
LABU_STATE_DIR=/tmp/labu-state \
LABU_NETWORK=devnet \
LABU_ACCOUNTS_PATH=~/tos-spec/vectors/accounts.json \
./target/debug/conformance
```

2. Run vectors against the local server:
```bash
cd ~/labu
python3 tools/local_execution_runner.py --vectors ~/tos-spec/vectors/execution
```

Notes:
- Avoid `vectors/unmapped/*` (legacy payloads) when running locally.
- For genesis-based loading, set `LABU_GENESIS_STATE_PATH` and provide a valid
  `genesis_state.json` file.

## Repository layout

```
labu/
├── cmd/labu/              # Go CLI
├── internal/              # Controller, Docker runner, result writer
├── labusim/                # Simulator SDK
├── clients/               # Client Dockerfiles + metadata
├── simulators/            # Simulator Dockerfiles + code
└── docs/                  # Architecture and API docs
```

## Documentation

- `docs/overview.md`
- `docs/commandline.md`
- `docs/simulators.md`
- `docs/clients.md`
