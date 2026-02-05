[Overview] | [Command Line] | [Simulators] | [Clients]

## Command line

```bash
./labu --sim tos/rpc --client tos-rust,avatar-c
```

### Flags

- `--sim` simulator name (e.g. `tos/rpc`)
- `--client` comma-separated client names
- `--workspace` output directory for logs/results (default: `./workspace`)
- `--vectors` path to vectors directory mounted into simulator at `/vectors`
- `--sim.limit` regex to filter tests
- `--sim.parallelism` test concurrency
- `--sim.randomseed` random seed (0 => auto)
- `--sim.loglevel` simulator log level (0-5)
- `--sim.image` override simulator image name
- `--client.images` override client images (`name=image,name=image`)

[Overview]: ./overview.md
[Command Line]: ./commandline.md
[Simulators]: ./simulators.md
[Clients]: ./clients.md
