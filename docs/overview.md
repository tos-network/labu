[Overview] | [Command Line] | [Simulators] | [Clients]

## What is Labu?

Labu is a multi-client integration test harness for the TOS ecosystem. It runs simulator
containers that orchestrate multiple client implementations (TOS Rust, Avatar C, etc.)
and compares their behavior using a shared test model. Results are emitted in a standard
JSON schema for tool compatibility.

## How it works

1. **Simulator selection**: `labu` builds the simulator image from `simulators/<name>`.
2. **Client selection**: `labu` builds client images from `clients/<name>` (failures are allowed).
3. **Controller API**: a local HTTP API is started and exposed to the simulator.
4. **Simulation run**: the simulator launches clients through the API and runs tests.
5. **Results**: suite results are written to `workspace/results` using the standard JSON format.

Vector-driven simulators (e.g. `tos/execution`) should read vectors from `/vectors`. Use
the CLI flag `--vectors` to mount `~/tos-spec/vectors` into the simulator container.

## Result format

Labu emits a JSON file per suite (standard format). Example:

```json
{
  "id": 0,
  "name": "rpc",
  "description": "RPC compatibility checks",
  "clientVersions": {"tos-rust": "", "avatar-c": ""},
  "simLog": "simulator-<id>.log",
  "testCases": {
    "1": {
      "name": "tx submit",
      "description": "submit tx via RPC",
      "start": "2026-02-04T12:00:00Z",
      "end": "2026-02-04T12:00:01Z",
      "summaryResult": {"pass": true, "details": ""},
      "clientInfo": {}
    }
  }
}
```

[Overview]: ./overview.md
[Command Line]: ./commandline.md
[Simulators]: ./simulators.md
[Clients]: ./clients.md
