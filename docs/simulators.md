[Overview] | [Command Line] | [Simulators] | [Clients]

## Simulators

Simulators are Dockerized programs that drive clients via the Lab simulation API.
They live under `simulators/` and must include a `Dockerfile`.

### Build Context Override

If a simulator needs access to shared modules (e.g. `labsim`), add a
`lab_context.txt` file next to its Dockerfile. The file should contain a relative
path to the build context root.

Lab exposes the API URL to the simulator via `LAB_SIMULATOR`.
Optional environment variables:

- `LAB_TEST_PATTERN` regex filter for suites/tests
- `LAB_PARALLELISM` integer concurrency
- `LAB_RANDOM_SEED` integer random seed
- `LAB_LOGLEVEL` 0-5 log level

## Simulation API

Lab implements the simulation API endpoints:

- `POST /testsuite`
- `DELETE /testsuite/{suite}`
- `POST /testsuite/{suite}/test`
- `POST /testsuite/{suite}/test/{test}`
- `GET /clients`
- `POST /testsuite/{suite}/test/{test}/node`
- `GET /testsuite/{suite}/test/{test}/node/{container}`
- `POST /testsuite/{suite}/test/{test}/node/{container}/exec`
- `DELETE /testsuite/{suite}/test/{test}/node/{container}`
- `POST /testsuite/{suite}/network/{network}`
- `DELETE /testsuite/{suite}/network/{network}`
- `POST /testsuite/{suite}/network/{network}/{container}`
- `DELETE /testsuite/{suite}/network/{network}/{container}`
- `GET /testsuite/{suite}/network/{network}/{container}`

[Overview]: ./overview.md
[Command Line]: ./commandline.md
[Simulators]: ./simulators.md
[Clients]: ./clients.md
