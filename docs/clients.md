[Overview] | [Command Line] | [Simulators] | [Clients]

## Clients

Clients are defined under `clients/<name>` and must include:

- `Dockerfile` (builds a runnable client image)
- `labu.yaml` (metadata, e.g. roles)

### Required client interface

The client image must:

- Start the client process as the container entrypoint
- Expose RPC (and optionally P2P) endpoints to the simulator
- Accept config via environment variables
- Read optional files mounted at `/labu-files`

### Recommended environment variables

- `LABU_FILES_DIR` path to mounted files (default: `/labu-files`)
- `LABU_NETWORK` network name to boot
- `LABU_LOGLEVEL` log level

The simulator is responsible for passing these variables when launching a client.

[Overview]: ./overview.md
[Command Line]: ./commandline.md
[Simulators]: ./simulators.md
[Clients]: ./clients.md
