# server

The ReplayVOD backend. A single Go binary that watches Twitch, records live
streams, stores VODs, and exposes a typed tRPC + REST API to the
[`dashboard`](../dashboard/).

For end-user install instructions see the [project README](../README.md). This
document covers the backend itself: architecture, layout, configuration, and
how to develop on it.

## Contents

- [Overview](#overview)
- [Project layout](#project-layout)
- [Boot sequence](#boot-sequence)
- [Configuration](#configuration)
- [Database](#database)
- [Background work](#background-work)
- [External integrations](#external-integrations)
- [Development](#development)
- [Code generation](#code-generation)
- [Testing](#testing)
- [License](#license)

## Overview

The server is a self-contained Go process. One binary handles:

- **HTTP API** — Chi router with a tRPC mount under `/api/rpc` and REST
  endpoints under `/api/v1`. Auth is Twitch OAuth with encrypted sessions.
- **Recorder** — HLS pull via Twitch playback tokens, segment download with
  retry/backoff, ffmpeg remux to MP4, ffprobe duration check, thumbnail
  extraction, optional S3 upload.
- **EventSub manager** — subscribes to `stream.online`, `stream.offline`,
  `channel.update`; reconciles on boot; falls back to polling when EventSub is
  unavailable.
- **Scheduler** — small task runner with DB-persisted state for periodic jobs
  (polling, category-art backfill, token cleanup, etc.).
- **SSE bus** — typed pub/sub used to push live indicators and task status to
  the dashboard.

It supports both Postgres and SQLite at runtime, swapped behind a single
`Repository` interface. ffmpeg / ffprobe must be on `$PATH` for recording.

## Project layout

```
server/
├── cmd/server/             # main entrypoint
├── internal/
│   ├── api/                # HTTP routes (Chi) + tRPC mount, grouped by domain
│   ├── server/             # HTTP server lifecycle and router wiring
│   ├── auth/               # Twitch OAuth handler + session login
│   ├── session/            # encrypted token store, cookie sessions, whitelist
│   ├── config/             # TOML + env loader, defaults, helpers
│   ├── database/           # pgx pool / sqlite open + migrate runner
│   ├── repository/         # Repository interface
│   │   ├── pgadapter/      # Postgres adapter (uses sqlc-generated pggen/)
│   │   └── sqliteadapter/  # SQLite adapter (uses sqlc-generated sqlitegen/)
│   ├── twitch/             # Helix client + generated EventSub types
│   ├── eventsub/           # generated EventSub bindings
│   ├── service/
│   │   ├── eventsub/       # subscription CRUD, quotas, reconcile
│   │   ├── streammeta/     # title/category hydration + watcher
│   │   ├── categoryart/    # box-art backfill from /games
│   │   └── schedule/       # scheduled-recording rules
│   ├── downloader/         # recording pipeline
│   │   ├── hls/            # segment fetcher
│   │   ├── remux/          # ffmpeg wrapper
│   │   ├── probe/          # ffprobe wrapper
│   │   └── thumbnail/      # JPEG generator
│   ├── scheduler/          # task runner with DB-persisted state
│   ├── eventbus/           # generic Topic[T] pub/sub for SSE
│   ├── relayclient/        # WebSocket client for the Connect relay
│   ├── storage/            # local disk + S3 backend (AWS SDK v2)
│   └── logger/             # slog setup with file + stderr sinks
├── migrations/{postgres,sqlite}/   # versioned .up/.down SQL pairs
├── queries/{postgres,sqlite}/      # sqlc query files
├── tools/twitch-api-gen/           # generator for Helix + EventSub types
├── config.toml             # operational config (download, scheduler, …)
├── .env.example            # credentials and paths
├── sqlc.yaml               # sqlc config (postgres + sqlite)
└── Taskfile.yml            # dev/build/test tasks
```

## Boot sequence

`cmd/server/main.go` wires everything in this order:

1. Load `config.toml` and `.env`, set up the slog logger.
2. Open the database (pgx pool or `database/sql` with modernc.org/sqlite) and
   run embedded migrations.
3. Build the matching `Repository` adapter.
4. Construct the Twitch Helix client, encrypted session manager, storage
   backend.
5. Construct the downloader service and call `Resume()` to pick up jobs left
   in the `RUNNING` state by a previous process.
6. Open the SSE event bus and start the HTTP server in a goroutine.
7. If `RELAY_SUBSCRIBE_URL` is set, dial the Connect relay and start the
   replay agent.
8. Reconcile EventSub subscriptions against the live channel set.
9. Register and start the scheduler.
10. Block on SIGINT / SIGTERM, then stop the scheduler, HTTP server, and
    logger in reverse order.

## Configuration

Two files. Both are committed with sensible defaults; copy and edit:

### `.env` — credentials, paths, network

Grouped by purpose:

| Group       | Variables |
| ----------- | --------- |
| Database    | `DATABASE_DRIVER`, `POSTGRES_*`, `SQLITE_PATH` |
| Twitch      | `TWITCH_CLIENT_ID`, `TWITCH_SECRET`, `HMAC_SECRET`, `TWITCH_SERVICE_ACCOUNT_REFRESH_TOKEN` |
| HTTP        | `HOST`, `PORT`, `CALLBACK_URL`, `WEBHOOK_CALLBACK_URL`, `FRONTEND_URL` |
| Security    | `SESSION_SECRET`, `WHITELIST_ENABLED`, `WHITELISTED_USER_IDS`, `OWNER_TWITCH_ID` |
| Storage     | `VIDEO_DIR`, `THUMBNAIL_DIR`, `SCRATCH_DIR`, `DASHBOARD_DIR` |
| Connect     | `RELAY_SUBSCRIBE_URL`, `RELAY_LOCAL_CALLBACK_URL` |
| Compose     | `PUBLIC_BASE_URL`, `PUBLIC_WEBHOOK_CALLBACK_URL`, `COMPOSE_PROFILES` |

`.env.example` is the source of truth — every supported variable is listed and
commented there.

### `config.toml` — operational tuning

| Section            | Controls |
| ------------------ | -------- |
| `[server]`         | CORS allow-list |
| `[download]`       | concurrency caps, preferred quality, retry budgets, gap tolerance, codec policy (AV1/HEVC) |
| `[storage]`        | `local` vs `s3`; S3 endpoint / bucket / region / keys / path-style |
| `[scheduler]`      | enabled flag and per-task intervals |
| `[logging]`        | log level, file output, sample rate |
| `[postgres]`       | pool sizing and lifetimes |
| `[health]`         | `/api/v1/health` probe |
| `[title_tracking]` | `poll` / `webhook` / `off` and poll interval |

## Database

Postgres and SQLite share a single `Repository` interface in
`internal/repository/`. The two adapters use [sqlc](https://sqlc.dev/) to turn
the SQL files in `queries/{postgres,sqlite}/` into typed Go in
`pgadapter/pggen/` and `sqliteadapter/sqlitegen/`.

Migrations live in `migrations/{postgres,sqlite}/` as numbered
`NNN_name.up.sql` / `NNN_name.down.sql` pairs and are embedded into the
binary. They run automatically on start; the `schema_migrations` table tracks
applied versions.

Use Postgres for production deployments and SQLite for single-host or dev
setups.

## Background work

- **Downloader pipeline** — `internal/downloader/`. Fetches an HLS playback
  token, picks a variant per `[download]` policy, downloads segments
  concurrently with retry, remuxes with ffmpeg, probes with ffprobe, generates
  a thumbnail, and uploads (when storage is S3). Job and segment state live in
  the DB so a crash mid-record resumes cleanly. Graceful shutdown leaves jobs
  `RUNNING`; user-cancelled jobs become `FAILED`.
- **EventSub manager** — `internal/service/eventsub/`. Subscribes to
  `stream.online`, `stream.offline`, and (if enabled) `channel.update`. On
  boot, deletes orphaned subscriptions and creates missing ones for the
  current channel set. Per-call quota snapshots feed the dashboard.
- **Scheduler** — `internal/scheduler/`. A single ticker wakes every 15 s and
  runs due tasks. Each task has its own row with `next_run_at`, `is_enabled`,
  and `last_error`, so the dashboard can pause, schedule, and inspect tasks at
  runtime. Standard tasks: stream-online polling fallback, thumbnail backfill,
  category-art sync, token cleanup.
- **SSE bus** — `internal/eventbus/`. Typed `Topic[T]` with bounded
  subscriber buffers, used to push live indicators and task status to the
  dashboard via `/api/v1/sse`.

## External integrations

- **Twitch Helix** — `internal/twitch/client.go`. OAuth, streams, games,
  users, follows, playback tokens via GQL, master playlist (Usher).
- **Twitch EventSub** — `internal/service/eventsub/`. REST CRUD over
  generated bindings.
- **Generated types** — `tools/twitch-api-gen/` parses the Twitch HTML docs
  and emits Helix + EventSub Go types. Snapshots are committed; CI checks
  drift via `task twitch-api-gen:check`.
- **Connect relay** — `internal/relayclient/`. Optional WebSocket client that
  receives signed EventSub frames from the hosted Cloudflare Worker and
  replays them to a local handler so HMAC verification still happens locally.
- **tRPC** — uses [`befabri/trpcgo`](https://github.com/befabri/trpcgo). The
  `task trpcgen` task generates the dashboard's TypeScript client and Zod
  schemas from the Go procedures.
- **ffmpeg / ffprobe** — required at runtime. The remux package pre-validates
  inputs and detects truncated segments.

## Development

Requires Go 1.26+, [Task](https://taskfile.dev/), and ffmpeg on `$PATH`.

```bash
cp .env.example .env        # fill in Twitch credentials
$EDITOR .env

task dev                    # go run ./cmd/server (port 8080)
task build                  # produces ./server
task                        # list every task
```

The dashboard is a separate Vite project; in dev it runs on port 3000 and
proxies `/api/*` to the backend. See [`dashboard/`](../dashboard/).

## Code generation

Three generators, all driven from the Taskfile:

```bash
task sqlc                   # queries/*.sql      → pggen / sqlitegen
task trpcgen                # tRPC procedures    → dashboard/src/api/generated/
task twitch-api-gen         # Twitch HTML docs   → internal/twitch + eventsub
task gen                    # all of the above
```

Generated files are committed. CI runs the `:check` variants to fail on
drift.

## Testing

Test suites are gated by build tags so the default `go test ./...` stays fast
and dependency-free.

```bash
task test                   # unit tests (no tags)
task test-integration       # //go:build integration  — Docker (Garage S3, pg testcontainer)
task test-ffmpeg            # //go:build ffmpeg       — real ffmpeg/ffprobe
task test-live              # //go:build live         — real Twitch endpoints (opt-in)
task vet
task check                  # vet + test
```

Test fixtures for containerised dependencies live in `internal/testdb/`.

## License

[GPL-3.0](../LICENSE), like the rest of the recorder.
