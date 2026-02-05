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
