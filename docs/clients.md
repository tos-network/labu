[Overview] | [Command Line] | [Simulators] | [Clients]

## Clients

Clients are defined under `clients/<name>` and must include:

- `Dockerfile` (builds a runnable client image)
- `lab.yaml` (metadata, e.g. roles)

### Required client interface

The client image must:

- Start the client process as the container entrypoint
- Expose RPC (and optionally P2P) endpoints to the simulator
- Accept config via environment variables
- Read optional files mounted at `/lab-files`

### Recommended environment variables

- `LAB_FILES_DIR` path to mounted files
- `LAB_NETWORK` network name to boot
- `LAB_LOGLEVEL` log level

The simulator is responsible for passing these variables when launching a client.

[Overview]: ./overview.md
[Command Line]: ./commandline.md
[Simulators]: ./simulators.md
[Clients]: ./clients.md
