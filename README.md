# lab - TOS multi-client conformance harness

Lab is a multi-client conformance test framework for the TOS ecosystem. It builds and
runs simulator containers that drive multiple client implementations through a common
API, then aggregates results into a standard JSON report format.

## Quick start

```bash
# Build and run a simulator
./lab --sim tos/rpc --client tos-rust,avatar-c
```

## Repository layout

```
lab/
├── cmd/lab/               # Go CLI
├── internal/              # Controller, Docker runner, result writer
├── labsim/                # Simulator SDK
├── clients/               # Client Dockerfiles + metadata
├── simulators/            # Simulator Dockerfiles + code
└── docs/                  # Architecture and API docs
```

## Documentation

- `docs/overview.md`
- `docs/commandline.md`
- `docs/simulators.md`
- `docs/clients.md`
