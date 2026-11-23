# Agent instructions

This file is the complete rule set for the pvmon codebase: structure, architecture, code,
tests, dependencies and workflow. [SPEC.md](SPEC.md) describes what the application does.

- **MUST** means the rule is required. A MUST deviation is valid only when section 12.3 lists it.
- **SHOULD** means the rule is required unless a written reason exists.
- **MAY** means the rule is optional.

When the code and this file do not agree, fix one of them in the same change. For behaviour,
SPEC.md wins. For code rules, this file wins. References to "SPEC.md section N" point to the
specification; all other section numbers point to this file.

Write all documentation and comments in English and in ASD-STE100 Simplified Technical
English. Only the German translation file of the UI is German.

## 1. Overview

One Go binary is the whole application. The `go:embed` directive puts these parts into the binary:

- the frontend (`frontend/dist`)
- Swagger UI and the OpenAPI document
- all built-in profiles
- the PDF fonts
- the list of common passwords
- the database migrations

At runtime the application needs only the binary, an optional `config.yaml` and the data directory. Local profile files are optional (SPEC.md section 4.5). The code never reads embedded files from disk.

## 2. Tools

```bash
make check                                  # ./scripts/pre-commit.sh, check-only (no mockery)
golangci-lint fmt --config .golangci.yml    # gofumpt + goimports (prefix github.com/dombyte/pvmon)
golangci-lint run --config .golangci.yml
go tool deadcode -test ./...
go tool govulncheck ./...
go tool mockery                             # regenerate mocks (.mockery.yaml)
go test -race ./...                         # as in CI
go test -race -tags integration ./...       # real devices, never in CI
make assets | build | all | docker          # dists; ./pvmon with ldflags; both; dev image
make e2e | e2e-update                       # Playwright screenshots + axe in the CI image
go run ./cmd [-config <path>]               # server (default ./config.yaml)
go run ./cmd simulate --setup R3            # fake transports for UI work (SPEC.md section 13)
go run ./cmd dump --device <id>             # record a fixture from a real device
go run ./cmd profile validate <file>        # profile authoring
go run ./cmd import-solis --db solis.db --map example/import-solis.map.yaml
go run ./cmd version                        # build info (also -version)
cd frontend && npm run typecheck && npm run lint && npm run knip && npm test
```

- `make check` runs the same checks as CI. CI (`checks.yml`) also runs the tidy check, the mock drift check, the profile golden tests, the frontend checks, the i18n key check, the bundle budget and the Playwright e2e tests. Run `make check` before every merge.
- `.golangci.yml` is the source of the lint and size rules (taken over from solis). Do not relax it without a deviation in section 12.3.
- The toolchain is Go 1.27.4 (`go` directive in `go.mod`). Pin tool versions. Bump them deliberately. Use the same versions locally and in CI.
- Go tools (mockery, deadcode, govulncheck) are `tool` directives in `go.mod`. Run them with `go tool <name>`. Bump them with `go get -tool <module>@vX.Y.Z`. A pinned `go run` checks sum.golang.org on every CI run.
- golangci-lint and goreleaser run as pinned prebuilt binaries. Never use them as Go modules.
- Use only the dependencies in section 10. A new dependency (Go or npm) needs a row there with version, reason and the stdlib alternative that you considered. Add the row in the same change. Use the stdlib first.
- Renovate is the only dependency updater. `renovate.json` MUST track every pinned version:
  - gomod, incl. the Go tools
  - Dockerfiles and compose files
  - GitHub Actions
  - npm in `frontend/` and `docs/`
  - a regex manager for the golangci-lint `version:`

  A change that adds a pin extends `renovate.json`.
- Release: push a SemVer tag `vX.Y.Z` on `main`. `release.yml` runs the checks again. goreleaser (`make assets` in its before hooks) builds the archives and images. The image tag `latest` exists only for stable releases.

## 3. Structure

The repository has this structure:

```
cmd/                 main.go (dispatch, signal ctx, exit code, ldflags), one file per subcommand
internal/
  app/               composition root: Create* factories, health adapters, logger, config mapping
  buildinfo/         build info passed down from cmd
  config/            koanf layering (defaults → DB → file → env), structural Validate()
  configstore/       UI config persistence, config_rev, history, secret encryption
  site/              effective site model, series catalogue, capabilities
  profile/           schema, loader (embedded IDs + explicit local file paths), decoder, status maps
  transport/modbus/  TCP/RTU client, shared connection + FIFO queue per address, reconnect
  transport/http/    GET client, basic/digest auth, no cross-host redirects
  collector/         scheduler, normaliser, counter tracker, integrator, netting, bucket accumulator
  livecache/         newest value per channel; publishes change events
  sitecalc/          live site quantities (debounced)
  derive/            periods, site quantities, ratios, derived_cache, warm-up, retention + carry
  eventbus/          typed events; Publish never blocks; per-subscriber overflow policy
  health/            supervisor, component contract, restart budget, ErrHealthFatal, snapshot
  storage/           sole SQLite owner; write-domain interfaces; read store
  database/          migrations, backups, integrity checks
  auth/              users, sessions, API tokens, CSRF, rate limit
  report/            PURE report model; csv/, pdf/ renderers (signintech/gopdf, embedded Inter)
  service/           read services for HTTP (site, energy, gauge, devices, report, health)
  ws/                subscription protocol, per-client diff, same-origin upgrader
  http/{handler,middleware,router,server}/
  maintenance/       CLI jobs: flock, backup, backfill, vacuum, import-solis, data correction
  simulate/          household model + fake sources for `pvmon simulate` (encoded via profiles)
  export/            interface only (future MQTT, webhooks); no implementation in v1
  logging/           zerolog constructor + optional timberjack file (no global logger)
  util/              math, Clock, Slot[T], RequireAll; util/clocktest = fake Clock
  <pkg>/mocks/       mockery-generated (never hand-edit)
profiles/<vendor>/   built-in YAML profiles (embedded via go:embed); fixtures + golden values next to them
frontend/            Vue 3 + Vite + Pinia + vue-i18n + Tailwind 4 + @lucide/vue + ECharts
docs/                user documentation: Swagger UI + openapi.yaml (built to docs/dist, embedded; planned, not yet in the repository)
example/             docker-compose (TCP, RTU) and config.yaml templates
.dev/                local development material (gitignored, not part of the project)
```

All Go code lives in `internal/`, at most 3 levels deep. Do not add a `pkg/` directory without the maintainer's confirmation.

These package rules apply:

- **config** imports no domain package. `Validate()` reports every problem with its field path (`meters.grid.device: unknown device "meterr"; …`), never only the first. Domain rules are `config.Rule`s from `app.ConfigRules()`. Examples of domain rules: Modbus address, `rollover` HH:MM, profile/role compatibility, exactly one `grid` meter, parent cycles. `app` maps sections onto the own `Settings` of each package.
- **profile**: a device names a built-in profile by ID or a local file by path. A path ends in `.yaml` or `.yml` and is relative to the config file. There is no implicit override directory. An invalid file never falls back silently to the built-in profile (SPEC.md section 4.5). Everything vendor-specific is YAML, never Go. Go code is allowed only for a new transport or a special driver behind the same interface (SPEC.md section 4.1).
- **transport**: it does raw reads and error classification only. Construction never fails on an unreachable device; the transport starts as `Recovering`. A timeout on an open Modbus connection causes an immediate reconnect and one retry. After that, exponential backoff runs from 1 s to 30 s (SPEC.md section 4). The `Reader` interface has no write method. Modbus uses only FC 03/04. HTTP uses only `GET` (SPEC.md section 4.4).
- **collector**: one instance per connection. It owns polling, normalising, counter tracking, netting and the open buckets of its devices. It never talks to another collector.
- **derive**: this is the only place that turns buckets into periods and site quantities. Pure period math lives in its own files without I/O, clock or logging.
- **storage**: one connection, WAL mode. It enforces the write domains (section 5). Sums run in SQL with explicit slot bounds, one grouped query per period.
- **http**: handlers live in `handler`. They contain no business logic and call `service`. Middleware lives in `middleware` and is never mixed with handlers. All routes are in `router/router.go`. The lifecycle is in `server`.
- **frontend**: it has no hard-coded device, register or role list. Everything comes from `GET /api/v1/site` (SPEC.md section 11).

## 4. Architecture

**Dependency injection (MUST).** Every dependency comes in through the constructor. It is a small interface that the consumer declares. Shared interfaces are the exception. Methods do not create dependencies. `internal/app` is the only composition root (`Create*` factories). Only `app` and `cmd` import `config`.

**Required dependencies.**

- Constructors check them with `util.RequireAll`. A nil dependency returns `util.DependencyError` and fails startup.
- There are no optional components behind `if x != nil`. A switchable feature is a separate component or subcommand.
- Missing *data* gives an explicit typed answer (`missing`, `complete: false`).
- Functional options MAY carry 3 or more optional settings. They never carry dependencies.

**No global state.** There are no package-level loggers, singletons, caches or lookup maps. These items are allowed:

- `Err…` sentinels
- compiled `regexp`s
- read-only `//go:embed` file systems
- the linker-set build info in `cmd/main.go`

**Dependency direction.** Dependencies flow down only: `router → handler → service → storage (read interface) / livecache`. Background components (collectors, sitecalc, derive, ws hub) never reference each other. They meet only through the live cache, the event bus and the health supervisor.

| Package | May import |
|---|---|
| collector | transport (interfaces), profile, site, eventbus, health (Reporter), storage types, util |
| transport/* | stdlib, grid-x/modbus (modbus only), util (Clock), injected zerolog.Logger — no config/health |
| profile | yaml, util |
| site | profile, util |
| livecache | eventbus, site |
| sitecalc | livecache, site, eventbus, health (Reporter), util |
| derive | site, storage types, eventbus, health (Reporter), util |
| storage | database, site, util |
| configstore | storage types, util, x/crypto |
| auth | storage types, util, x/crypto |
| ws | eventbus, livecache, site, health (Reporter), util |
| service | storage (read interfaces), livecache, derive (read interface), health, site, report |
| handler | service, auth (interfaces), configstore (types), health, site, buildinfo, util, report (+ renderers) |
| report | util (renderers: report, util) |
| config | util (only imported by cmd and app) |
| buildinfo | stdlib |
| simulate | profile, site, collector (Source, Transport), util |
| app | everything, incl. frontend/docs/profiles embed packages |

"Storage types" means the interfaces, DTOs and sentinels in `storage/interfaces.go`. No package imports `*storage.Storage`.

These imports are forbidden:

- collector ↔ collector
- collector ↔ derive
- handler → storage or transport
- livecache → ws
- any package → `cmd` or `app`

Every interface has a mockery mock. Mocks are generated, never hand-edited. Tests use testify `require` and `assert`.

## 5. Write domains

Each writer has its own write domain. A writer MUST NOT write outside its domain. `storage` is the only owner of SQLite. It enforces the domains.

| Writer | May write | Through |
| --- | --- | --- |
| collector | `b15_gauge`, `b15_energy`, `counter_state`, `status_event`, `series` (new series) | collector store interface, `livecache` |
| derive service | `derived_cache`; `carry` and purges only during retention | derive store interface |
| config service | `config_*`, `audit_log` | config store interface |
| auth service | `user`, `session`, `api_token`, `audit_log` | auth store interface |
| CLI jobs (app stopped, flock) | everything, only after a verified backup | `storage` and `maintenance` |
| all other components | nothing | read-only interfaces |

- Storage rejects a write outside the domain of the caller with a typed error. A test covers this.
- A bucket upsert and its `counter_state` are written in one transaction. The raw base and the booked energy therefore always match after a crash (5.3).
- A write to a past slot deletes the affected `derived_cache` keys in the same transaction (7.3).
- Only `storage` converts bucket values to the fixed-point form (7.2).
- Sums run in SQL with explicit slot bounds and one grouped query for each period.
- Pvmon writes only the rows that changed.

## 6. Lifecycle and concurrency

Every goroutine has an owner. The owner starts, stops and waits for the goroutine. Waiting loops use the injected `util.Clock`. Every channel documents its owner (the sender closes it) and its behaviour when full.

The single supervisor is `internal/health`. Restartable components are each collector, the derive service, sitecalc and the WebSocket hub. Each implements this contract:

```go
Start(ctx context.Context) error // error only for unrecoverable conditions
Stop() error                      // idempotent, graceful, bounded
State() health.State              // Healthy | Recovering | Failed
LastBeat() time.Time              // atomic, updated by the component's own loop
```

- Only a `health.Factory` that the supervisor calls builds a component. The component receives its `health.Reporter` in the constructor.
- There is no `Restart()` method. No component restarts another component. A restart is Stop, then factory, then Start.
- A config change (`config_rev`) rebuilds the affected collectors in the same way.
- A component beats from its **loop**, not only after work. The reconnect loop beats on every attempt and while in backoff.
- The healthy grace is 5 × the longest device interval of that component.
- The collector handles ordinary failures internally as `Recovering`. Examples are a device timeout, a read error and a lost connection. It uses the transport recovery of SPEC.md section 4 (immediate reconnect, backoff 1 s → 30 s). Such failures never count as restarts.
- The restart budget is 3 per component. Restarts are at least one sweep apart. The counter resets only after a restarted instance kept beating for the healthy grace.
- A fatal condition raises `ErrHealthFatal`. It cancels the root context through `context.WithCancelCause`. These conditions are fatal:
  - the budget is exhausted
  - a `Stop()` exceeded its timeout
  - a non-restartable part (storage, HTTP server, event bus) failed

Shutdown is single-phase with one hard deadline of 30 s. `log.Fatal` and `os.Exit` are allowed only in `main`; `cmd` sets the exit codes. SPEC.md sections 8.2 and 8.3 give device health, `/health`, the shutdown order and the exit codes.

## 7. Code rules

**Errors**

- Wrap errors with `%w` and a package prefix (`storage: upsert bucket %d: %w`).
- Use sentinels for branching. Use `…Error` types when callers need data.
- Never swallow an error. `_ = f()` needs a `//nolint` with a reason.
- Log an error once, where the code handles it.
- No panic.

**Logging**

- Use the injected zerolog logger. It writes JSON to stdout. The container runtime or journald rotates the output. An optional log file is rotated by timberjack (SPEC.md section 3).
- Scope the logger with `component`, and with `device` where it applies. Use context fields, not formatted strings.
- Use `debug` per cycle, `info` for lifecycle, `warn` for recovered failures and `error` when someone must act.
- Never log secrets, serials or tokens.

**Style**

- Use gofumpt and goimports (prefix `github.com/dombyte/pvmon`).
- Lines have at most 100 characters. Functions have at most 40 lines and 40 statements. Cyclomatic complexity is below 8. A function has at most 5 parameters. Magic numbers are not allowed. golangci-lint enforces all of this. Do not relax `.golangci.yml` without a deviation in section 12.3.
- Use stdlib naming: `ID`, `URL`, one-letter receivers, verb + noun for function names.
- Doc comments on packages and exported types are MUST. Doc comments on exported functions are SHOULD. Inline comments explain *why*.

**Behaviour rules that are easy to get wrong**

- Energy comes only from counters (Δ Wh per poll). Live power is for display only and is never stored as energy.
- Lifetime = `carry + Σ wh`.
- Missing data is never 0. Incomplete periods show `n/m days`. Charts leave gaps.
- Exactly one meter has the role `grid`. A sub-meter is subtracted from its parent, never added.
- A profile with `read: direct` MUST NOT switch to `range` by config. The reverse is allowed.
- The application stores 15-minute buckets as raw data. Everything else is derived and cached in `derived_cache`. The application can drop this cache at any time.
- Fetch only what you need: sum in SQL with explicit slot bounds, one grouped query per period. Write only what changed.

**Project rules**

- I/O functions take the caller's `ctx`. Below `cmd` and `app` there is no `context.Background()`. SQL uses the `…Context` variants. Every outgoing call has a timeout.
- Dangerous behaviour belongs in CLI jobs, never in config toggles.
- The code MUST NOT access devices from the HTTP path. Only collectors talk to devices. The setup probe (`POST /api/v1/config/probe`) goes through the collector of that connection, never through a second client on the same bus.
- The code MUST NOT call `time.Now()` in the pipeline. Capture the poll instant once from the injected `util.Clock`. Derive everything else from it. All timestamps are server time, never device time (SPEC.md section 5.2).
- The code MUST NOT write outside its write domain (section 5).
- The code MUST NOT read runtime files besides config and data. The UI, docs, profiles, fonts and migrations are embedded. A local profile file is read only when a device names its path.
- The code MUST NOT contain vendor knowledge in Go. Registers, JSON paths, scales, signs, status maps and counter kinds belong in profiles.
- The UI MUST NOT flip signs. Profiles normalise signs per role (SPEC.md section 2). The frontend shows what the API delivers.
- The code MUST NOT write to devices. Version 1 is read-only. The `Commander` interface is reserved and not implemented.
- The code MUST NOT add locks on top of the Modbus library. The per-address FIFO queue serialises requests. Lock only state that you own.
- The code MUST NOT contain EMS logic, costs, savings, metrics export or telemetry (sections 1 and 17).

**Frontend**

- The frontend MUST NOT flip signs. It shows what the API delivers.
- Components format numbers and dates only through the formatter module, with the locale of the chosen language. They never use the browser region, `toFixed` or `toLocaleString`.
- Components use colours only from Graphite tokens, never hex.
- A rounded container never gets a coloured border.
- Icons come only from `@lucide/vue`.
- Components get live data only through `useSubscription(keys)`.
- The frontend has no hard-coded device, register or role list. It derives everything from `GET /api/v1/site`.
- The frontend loads no external fonts or CDNs.

## 8. Version

Binary, UI and docs share one version: `<tag without v>-<short commit>`. The version is `dev` without git or tag. The data flows in this order:

1. ldflags set the version.
2. `buildinfo.Info` carries it.
3. `pvmon version`, the startup log and `GET /api/v1/version` show it.

The frontend and Swagger UI get `VITE_APP_VERSION` from the same source. The frontend polls `/api/v1/version`. A different version shows the update banner. A `dev` build never reports an update.

## 9. Testing

Unit tests use no network, no real clock and no sleeps. Deterministic fixtures pin every behaviour. Tests are table-driven with testify. Mocks come from mockery and are never edited by hand. `make check` runs the same checks as CI. It MUST pass before every merge.

| Level | Scope | Tool |
| --- | --- | --- |
| Profile golden | recorded or documentation-based fixture → expected normalised channels for every built-in profile | Go |
| Counter and netting | lifetime overflow, glitch hold, meter replacement, daily reset incl. reset group, integration across gaps, netting methods 1 and 2, resolution switch | Go, table-driven, fake clock |
| Replay | sample sequences of whole days incl. midnight, DST (Europe/Berlin) and outages → expected buckets and day values per series | Go |
| Reference setups | R1–R7 configs + replay → site quantities, `/api/v1/site`, reports | Go |
| Storage | write domains, cache invalidation on past writes, retention with carry, migrations, backup verification | Go + SQLite |
| Transport | Modbus TCP and RTU (virtual serial pair + simulator, shared bus with two unit IDs), HTTP drivers against recorded responses | Go |
| Auth | password hashing parameters, session expiry, CSRF, rate limit, token scope | Go |
| API | handlers with `httptest` and mockery mocks of the services; OpenAPI contract test | Go |
| UI logic | widget `requires`, flow `layout()` for all 64 combinations of PV/battery × category subsets at desktop and phone width (no overlapping boxes, texts inside the viewBox, minimum gaps), balance bars (always 100 % width, totals < 1 kW, single segment, 0 W), formatters per language | Vitest |
| UI visual | screenshots of overview, day view, history and settings for R1, R3+R4 and PV-only, light and dark, desktop and phone | Playwright |
| Accessibility | axe checks on every page | Playwright |

Rules for tests:

- Use `t.Parallel()` where possible. Always run tests with `-race`.
- Use `util/clocktest` to drive polling, debounce, backoff, graces, rollover and retention.
- Put real-device tests behind the build tag `integration`. CI never runs them.
- New code MUST come with tests. A bug fix MUST come with a regression test.
- Every built-in profile ships a fixture (`pvmon dump` or documentation-based) and golden values. CI decodes the fixture and compares the result. A profile without a real dump keeps `verified: false`.
- Counter, netting and replay tests cover midnight, DST (`Europe/Berlin`), outages, `u32` overflow, glitches, meter replacement and daily resets incl. reset groups.
- Reference setups R1–R7 run as replay tests (site quantities, `/api/v1/site`, reports).
- Storage tests cover write-domain guards, cache invalidation on past writes, retention with carry and migrations.
- HTTP tests use `httptest` and the mockery service mocks.
- Frontend: Vitest covers `layout()` (all capability combinations, desktop and phone width), widget `requires`, balance bars and formatters. Playwright covers screenshots and axe per page.

**Gates:**

- `-race`.
- Coverage > 90 % per Go package (`cmd` and `app` through a startup test).
- golangci-lint with the rule set in `.golangci.yml` (taken over from solis; limits in section 7).
- `deadcode`, `govulncheck`, `vue-tsc`, ESLint and knip.
- i18n key check and bundle budget.
- Mock drift check: the mockery output MUST match the committed mocks.

## 10. Dependencies

Use the standard library first. Add a third-party module only when the stdlib has no equivalent and a own implementation is large or risky (database engine, Modbus framing, cryptography, PDF). Every dependency MUST be actively maintained. A new dependency needs a row in this section with version, reason and stdlib alternative, in the same change. The versions below were checked against proxy.golang.org and registry.npmjs.org on 2026-11-19. Renovate keeps them current after that date.

### Go (runtime)

The Go toolchain is **1.27.4** (`go 1.27.4` in go.mod, same as solis).

| Module | Version | Released | Why it is needed | Stdlib / alternative considered |
| --- | --- | --- | --- | --- |
| `modernc.org/sqlite` | v1.62.0 | 2026-11-07 | SQLite without cgo: required for the `scratch` image and cross-compiling to arm | `mattn/go-sqlite3` needs cgo |
| `github.com/grid-x/modbus` | v1.5.1 | 2026-09-04 | Modbus TCP, RTU and RTU-over-TCP client; actively maintained, used in production by evcc for hundreds of device types | `simonvetter/modbus` (solis) last released 2025-11 and depends on `goburrow/serial` (2017) |
| `github.com/coder/websocket` | v1.8.15 | 2026-06-15 | WebSocket server; context-aware, no dependencies (formerly nhooyr.io/websocket) | `gorilla/websocket` (solis) last released 2024-06 |
| `go.yaml.in/yaml/v3` | v3.0.6 | 2026-10-13 | YAML config and profiles; the official continuation of `gopkg.in/yaml.v3` by the YAML org | `v4.0.0` (2026-11-04) is two weeks old; switch via Renovate once it has settled |
| `golang.org/x/crypto` | v0.59.0 | 2026-11-11 | Argon2id (passwords), XChaCha20-Poly1305 (secrets in the DB) | not in the stdlib |
| `github.com/signintech/gopdf` | v0.38.4 | 2026-10-31 | PDF reports with embedded Inter font (as in solis) | no stdlib PDF writer |
| `github.com/rs/zerolog` | v1.35.1 | 2026-04-20 | structured JSON logging, proven in solis; chosen over `log/slog` to reuse a stable, known library | `log/slog` |
| `github.com/DeRuina/timberjack` | v1.4.10 | 2026-11-08 | optional log file with size- and time-based rotation and compression; maintained fork of lumberjack with the same API | `natefinch/lumberjack.v2` (last release 2023-02) |
| `github.com/knadh/koanf/v2` | v2.4.1 | 2026-10-18 | layered configuration: defaults → DB layer → `config.yaml` → `PVMON_` env, merged into typed structs | `spf13/viper` (solis) pulls ~25 extra modules |
| `github.com/knadh/koanf/parsers/yaml` | v1.1.1 | 2026-08-04 | YAML parser for koanf (uses `go.yaml.in/yaml/v3`) | – |
| `github.com/knadh/koanf/providers/file` | v1.2.1 | 2025-12-19 | reads `config.yaml` | – |
| `github.com/knadh/koanf/providers/env/v2` | v2.0.2 | 2026-10-05 | `PVMON_` environment overrides | – |
| `github.com/knadh/koanf/providers/confmap` | v1.0.1 | 2026-08-04 | feeds defaults and the DB configuration layer into koanf as maps | – |

koanf merges all layers. No own merge code is needed.

`internal/config` also imports `go-viper/mapstructure/v2` (pulled in by koanf) directly. It uses it for its decode hook (unit-checked durations, sizes, energies and secret references).

koanf and timberjack add these indirect modules:

- `knadh/koanf/maps`
- `go-viper/mapstructure/v2`
- `mitchellh/copystructure`
- `mitchellh/reflectwalk`
- `fsnotify/fsnotify` (file provider)
- `klauspost/compress` (log compression)

#### Test only (not linked into the binary)

| Module | Version | Released | Why |
| --- | --- | --- | --- |
| `github.com/stretchr/testify` | v1.12.1 | 2026-08-17 | `require` / `assert` and `mock`; chosen over plain `testing` to avoid hand-written assertion and fake code |
| `github.com/vektra/mockery/v3` | v3.9.1 | 2026-11-09 | generates a mock for every interface (`go tool mockery`); CI fails on mock drift |

Indirect only:

- `github.com/grid-x/serial`: serial port access for RTU, pulled in by grid-x/modbus. The code is stable. The last change was in 2021.
- `modernc.org/libc` and related modules, pulled in by modernc sqlite.
- `golang.org/x/sys`.

#### Replaced by the standard library

| solis used | pvmon uses | Note |
| --- | --- | --- |
| `go-chi/chi` | `net/http` `ServeMux` | method and path patterns since Go 1.22 cover all routes |
| CBOR for `derived_cache` | `encoding/json` | SPEC.md section 7.1 changes the cache value from CBOR to JSON |
| JSONPath library | own subset parser (`$.a.b[0]`), about 100 lines | profiles need only dotted paths and indices |
| flock wrapper | `syscall.Flock` | linux and darwin only, as before |
| UUID library | `crypto/rand` + `encoding/base32` | tokens and setup codes |

#### Go tools (`tool` directives in go.mod, not linked into the binary)

| Tool | Module | Version |
| --- | --- | --- |
| mockery | `github.com/vektra/mockery/v3` | v3.9.1 |
| govulncheck | `golang.org/x/vuln` | v1.10.0 |
| deadcode | `golang.org/x/tools` | v0.53.0 |

golangci-lint and goreleaser run as pinned prebuilt binaries (as in solis). They are not Go modules.

### Frontend (runtime, shipped to the browser)

| Package | Version | Released | Why |
| --- | --- | --- | --- |
| `vue` | 3.5.45 | 2026-11-12 | UI framework |
| `vue-router` | 5.5.1 | 2026-11-13 | routing |
| `pinia` | 4.1.0 | 2026-10-14 | state (site model, live values, preferences) |
| `vue-i18n` | 11.4.23 | 2026-11-19 | German and English from day one |
| `echarts` | 6.1.0 | 2026-05-19 | interactive charts (tooltips, crosshair, zoom, large series); imported tree-shaken from `echarts/core` |
| `@lucide/vue` | 1.76.0 | 2026-11-19 | icons; `lucide-vue-next` is deprecated in favour of this package |
| `@fontsource-variable/inter` | 5.3.0 | 2026-07-19 | self-hosted Inter (no external font requests) |

#### Not used (deliberately)

| Package | Replacement |
| --- | --- |
| `vue-echarts` | a composable of about 60 lines around `echarts/core` (init, resize observer, theme switch, dispose) |
| `@vueuse/core` | `matchMedia`, `ResizeObserver` and `visibilitychange` directly in 3 small composables |
| `date-fns` | `Intl.DateTimeFormat` / `Intl.NumberFormat` for formatting; local-date arithmetic in a small helper module (days, months, years only) |
| chart.js, zustand, radix, react-day-picker (solis) | covered by echarts, pinia and own components |

### Frontend (build and test only)

| Package | Version | Note |
| --- | --- | --- |
| `vite` | 8.3.8 | |
| `@vitejs/plugin-vue` | 6.0.11 | |
| `tailwindcss`, `@tailwindcss/vite` | 4.3.5 | design tokens as CSS variables |
| `typescript` | 6.0.3 | pinned to 6.x for now: TypeScript 7.0 (native compiler) came out on 2026-11-19, and `vue-tsc` builds on the TypeScript JS API; switch only after `vue-tsc` confirms 7.x support (unchecked) |
| `vue-tsc` | 3.3.14 | type check of `.vue` files |
| `eslint` | 10.15.0 | |
| `typescript-eslint` | 8.73.0 | |
| `eslint-plugin-vue` | 10.12.1 | |
| `@intlify/eslint-plugin-vue-i18n` | 4.5.1 | missing or unused translation keys fail CI |
| `vitest` | 5.1.1 | unit tests |
| `@vue/test-utils` | 2.6.0 | component tests |
| `happy-dom` | 21.0.4 | DOM for vitest |
| `@playwright/test` | 1.65.1 | screenshots and e2e; run in the image `mcr.microsoft.com/playwright:v1.65.1-noble` (same version, grouped by Renovate) so baselines match CI |
| `@axe-core/playwright` | 4.14.0 | accessibility checks |
| `knip` | 6.46.1 | unused files, exports and dependencies |

### Totals

- Go: 13 direct runtime modules (6 core + zerolog + timberjack + 5 koanf parts) and about 15 indirect modules (solis: about 60). Tests add testify and mockery.
- Browser: 7 packages (solis: 20).
- Build and test: 15 packages.

### Logging output

By default, zerolog writes JSON to stdout. Docker or journald rotate it. When `log.file` is set, zerolog also writes to that file through timberjack. These keys control the rotation:

- `log.max_size`: default 50 MB.
- `log.max_backups`: default 5.
- `log.max_age`: default 30d.
- `log.rotate_at`: optional daily times, for example `00:00` (timberjack `RotateAt`).
- `log.compression`: `gzip` | `zstd` | `none`, default `gzip`.

## 11. Git

- Work on a branch named `type/description` and open a pull request. CI MUST be green before the merge. The maintainer MAY commit to `main` directly after `make check`.
- Use regular merges. Use squash or rebase only on request.
- Several small changes MAY share one batch pull request.
- Commit subjects (MUST) use the format `<type>(<scope>)!: <imperative lowercase subject>`. The changelog is built from the subjects.
- Allowed types: `feat`, `fix`, `perf`, `refactor`, `docs`, `chore`, `build`, `ci`, `test`, `style`. Use `<type>(deps)` for dependency bumps.
- Allowed scopes: `ui`, `api`, `ws`, `auth`, `health`, `collector`, `transport`, `profile`, `site`, `derive`, `storage`, `config`, `report`, `cli`, `docker`, `release`, `ci`, `deps`, `docs`.
- For a profile change, use the scope `profile` and name the profile ID in the subject.
- Add `!` when the change breaks upgrading users. Examples: a change to the REST or WS protocol, removed config keys, changed migration paths, profile channel renames without `renamed_from`, changed runtime requirements. A footer alone is not enough.

```
feat(profile): add eastron/sdm630 with app netting default
fix(collector)!: book netted grid energy per poll instead of per bucket
```

## 12. Status

### 12.1 Implementation status

Phases 1, 2 and 3 are implemented. This includes history, reports, status, the day view and `import-solis`.

Of Phase 4, these parts exist:

- the device pages
- the auth backend (users, sessions, tokens, CSRF, audit)
- the config store (UI configuration applies without restart, `/api/v1/config/*`)
- the login, setup and settings pages
- the setup wizard with the device probe

Gate 2 is checked by these tests:

- `TestReferenceSetups` in `internal/app` (R1/R3/R4 replay and `/api/v1/site` goldens)
- the netting tests in `internal/collector`
- the overview e2e test (`make e2e`)

Gate 3 was checked by hand against a real solis database. Gate 1 was checked by hand against a real recording. Section 12.2 lists what is missing.

### 12.2 Known gaps

Gaps between this document and the repository:

- **Gate 1 has no automated check in CI.** Anyone with a recording can repeat it.
  1. Record a dump that starts before one midnight and ends after the next: `pvmon dump --device wr1 --count <n> --interval 10s -o solis.fixture.json`.
  2. Create the expected values from the solis database: `scripts/gate1-expected.sh <solis.db> <day> > expected.json`.
  3. Put both files in one directory.
  4. Run `PVMON_GATE1_DIR=<dir> go test -run Gate1 ./internal/collector`.
- **Gate 3 has no automated check against a real solis DB.** A manual run of `import-solis` on a real database with 164 days matched every month, the year and the lifetime of the six mapped series exactly. The fixture test covers DST, carry and mismatches.
- **Built-in fixtures are documentation-based.** Real dumps of Shelly Pro 3EM, go-e and WARP3 (one sample each) decode plausibly. They do not replace the fixtures yet. The go-e and the WARP were not charging during the dump. The power units of `nrg[11]` and of value ID 74 are not verified.
- **Frontend.**
  - Built: shell, overview (flow, "Power now", KPI tiles, energy table, 14-day trend, last-7-days list), day view, history (wide ranges scroll), devices with detail and status.
  - Built: reports (preview, PDF and CSV download), login, first-start setup, settings (app settings, users, API tokens, audit log, configuration overview with export).
  - Built: the setup wizard (SPEC.md section 11.14, steps 2–6: devices with probe and unit ID search, meter proposal, hierarchy, live balance check, saving as one revision).
  - Playwright screenshots and axe cover every built page.
  - Missing: history does not split loadpoints yet.
  - Missing: the device detail groups channels per meter, not by AC/DC/battery/temperature, and has no netting check yet.
  - Missing: the wizard has no "reverse direction" per meter. pvmon has no config key to flip the sign of a meter.
  - Missing: the wizard has no netting check (step 5). That check needs the daily drift check below.
- **derive.** It answers `/api/v1/energy` with the derived cache, but it is not a supervised component yet. The midnight warm-up and the retention with carry (sections 7.3 and 7.4) are missing.
- **CLI.** These commands exist: `serve`, `version`, `profile validate`, `dump`, `simulate`, `import-solis`, `user`, `token` and `config validate|export`. These commands from SPEC.md section 13 are not built: `profile plan|probe`, `backfill`, `restore`, `data exclude|swap|reassign` and `vacuum`. `vacuum` comes with the retention. The retention itself never runs `VACUUM` (SPEC.md section 7.4).
- **Audit.** `import-solis` writes no audit entry yet (SPEC.md section 10). The audit log is not purged yet (retention, SPEC.md section 7.4).
- **Old databases.** Databases written before the `daily_reset` gap fix (pvmon stopped over midnight) keep 0 Wh slots in the night that look measured. They also keep the energy of the night after the restart. Only `data exclude` (not built) can correct them.
- **Reports.** The data-quality section does not list device changes in the period, because no device history is stored.
- **Netting `app`.** The daily drift check of coarse counters against the device totals (section 6) is not implemented. Device health `stale` (SPEC.md section 8.2) is not detected yet.
- **docs/.** Swagger UI and OpenAPI do not exist yet. The dev `Dockerfile` gets its docs stage back with them.
- **Lint.** `errcheck` and `gosec` in `.golangci.yml` do not flag `Close` and file paths. For this reason, `_ = x.Close()` carries a comment instead of the `//nolint` that section 7 asks for.

### 12.3 Deviations

None.

A MUST deviation in this file is valid only when this section lists it. When the code and this document disagree, fix one of them in the same change.
