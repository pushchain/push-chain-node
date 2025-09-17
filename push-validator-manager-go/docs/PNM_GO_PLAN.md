Push Validator Manager (Go) — Plan & Tasks

Scope & Constraints

- CLI name remains `push-validator-manager` with command parity; omit a dedicated "Recovery" help section.
- Keep snapshot RPCs/domains and chain IDs consistent with current scripts.
- Guided setup for nginx/logrotate (no automatic OS package installs).
- Nested module: `push-validator-manager-go/`.
- Minimal `install.sh` kept shell-only for `curl | bash` UX.

Architecture

- `cmd/push-validator-manager/`: CLI entry (Cobra planned; stdlib stub initially).
- `internal/config`: app config (flags/env/file), persistent state.
- `internal/node`: CometBFT RPC+WS client (status/heights/peers/genesis).
- `internal/process`: start/stop/PID, detached exec, port checks, logs.
- `internal/files`: TOML read/write, peers, backups (afero in tests).
- `internal/statesync`: trust height/hash & config builder.
- `internal/monitor`: sync monitor (WS-first, polling fallback), ETA/progress.
- `internal/validator`: key ops, balances, validator detection, registration.
- `internal/system`: guided nginx/logrotate setup with privilege prompts.
- `pkg/ui`: structured logging and UX helpers.

Command Surface

- Core: `init`, `start`, `stop`, `restart`, `status`, `logs`, `sync`.
- Validator: `register-validator`, `validators`, `balance [address]`.
- Admin: `reset`, `backup`, `setup-nginx`, `setup-logs`.

Testing Strategy

- Unit tests with mocks (clock/fs): statesync trust params, recovery logic (internal only), TOML mutations, process supervisor, monitor math, validator JSON/tx parsing.
- Integration: mock JSON-RPC + WS servers (scripted heights), temp dirs for `~/.pchain` to assert idempotent config changes.
- CLI smoke tests: `status`, `sync` fallback, `register-validator` happy-path via mocks.

Delivery Phases

1) Scaffold module, CLI stub, config types
2) Process supervisor (start/stop/restart/status) and log tailer
3) Node client (RPC/WS) + status/sync monitor (WebSocket only)
4) State sync config (trust params, peers) and idempotent TOML writes
5) Validator flows (keys/balances/registration) with clear UX
6) Admin tasks (reset/backup/setup-nginx/setup-logs; guided prompts)
7) Tests, CI, finalize install.sh, docs

Acceptance Criteria

- Reliable start/stop/status with PID and port checks; stable logs.
- Robust state sync config using snapshot RPC; idempotent and testable.
- Accurate monitor (WebSocket) with progress and ETA.
- Correct validator registration flow with preflight checks and clear errors.
- No external runtime deps (jq/websocat) for core features.
- Minimal `install.sh` works for one-liner; no OS package installs.

Task List (tracked)

- [x] Audit current bash manager and installer
- [x] Draft Go architecture and interfaces
- [x] Scaffold module layout and go.mod
- [x] Add CLI skeleton and command router (stdlib for now)
- [x] Implement config loader (env-only now; file/flags later)
- [x] Implement process supervisor (start/stop/restart, PID, logs)
- [x] Implement node client (HTTP) and WS-only header subscription
- [x] Add minimal `install.sh` template (download binary)
- [x] Write developer README and usage outline
- [x] Outline tests (unit/integration) and CI notes
- [x] Split `init` and `start` commands
- [x] Implement `init`: genesis fetch, peers, state sync enable, unsafe reset
- [x] Implement `status` using HTTP client
- [x] Implement `sync` (WS-only) basic progress
- [x] Remove `set-genesis` from scope

Pending

- [ ] Enhance state sync: trust selection strategy and retries
- [x] Add `reset` and `backup` admin commands
- [x] Add validator flows: `register-validator`, `validators`, `balance`
- [x] Improve CLI UX with per-command flags (stdlib)
- [x] WS monitor: moving average rate and ETA; compact mode
- [x] Add Goreleaser config + align installer artifact naming
- [ ] Improve CLI UX (flags, Cobra migration, structured output)
- [ ] Tests: config store mutations, statesync provider, bootstrap init, supervisor
- [ ] CI pipeline (without Goreleaser); finalize installer usage
