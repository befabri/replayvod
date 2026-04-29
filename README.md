<h1 align="center">ReplayVOD</h1>

<p align="center">
  <a href="LICENSE"><img alt="License: GPL-3.0" src="https://img.shields.io/badge/license-GPL--3.0-blue.svg"></a>
  <a href="https://go.dev/"><img alt="Go 1.26+" src="https://img.shields.io/badge/go-1.26%2B-00ADD8?logo=go&logoColor=white"></a>
  <a href="https://reactjs.org/"><img alt="React 19" src="https://img.shields.io/badge/react-19-61DAFB?logo=react&logoColor=white"></a>
</p>

ReplayVOD is a self-hosted Twitch VOD recorder for your homelab. It watches the
channels you follow, automatically records the streams that match your rules,
archives them on your own storage, and gives you a dashboard to browse and
replay the ones you missed.

> Status: active development. Stable enough for personal use, expect breaking
> changes between releases.

## Features

- **Auto-record** live streams from followed channels, gated by per-schedule
  rules: quality, viewer threshold, category match, tag match
- **Capture stream context** — title and category history are recorded
  throughout the broadcast, plus a snapshot strip the dashboard cycles through
  on hover
- **Browse and watch** with category, channel and date views, then play back
  in-browser
- **Live indicators** — followed channels show a live dot pushed via EventSub
  deltas, no polling
- **Multi-language** — interface in English and French
- **Lightweight runtime** — single Go binary, choice of Postgres or SQLite,
  optional S3-compatible storage backend

## Screenshots

_(coming soon)_

## Quick start

Bundled Docker setup is production-oriented: it builds the dashboard and
serves it from the Go server. Pick a database profile — `sqlite` for the
simplest single-container deploy, or `postgres` for a two-container setup:

macOS / Linux:

```bash
curl -fsSL https://replayvod.com/install.sh | sh
```

Windows PowerShell with Docker Desktop:

```powershell
$env:REPLAYVOD_INSTALL_MODE = "docker"
irm https://replayvod.com/install.ps1 | iex
```

Native Windows `.exe` installs are supported by the installer once release
archives are published. Until then, use the Docker Desktop path above.

Or install manually:

```bash
git clone https://github.com/befabri/replayvod.git
cd replayvod
cp server/.env.example server/.env
$EDITOR server/.env             # fill in Twitch credentials, see below
docker compose --env-file server/.env --profile sqlite up -d --build
# or: --profile postgres        # adds a Postgres container
```

Open <http://localhost:8080>, sign in with your Twitch account, and the user
listed in `OWNER_TWITCH_ID` is granted the owner role.

To skip the `--profile` flag on every command, set `COMPOSE_PROFILES=sqlite`
(or `=postgres`) in `server/.env`.

For real deployments, set `PUBLIC_BASE_URL=https://your-domain` in
`server/.env` before starting. The compose file derives the OAuth callback,
webhook callback, and frontend redirect URLs from that base URL.

### Twitch credentials

Register an application in the
[Twitch developer console](https://dev.twitch.tv/console/apps) with this
OAuth redirect URL:

```
http://localhost:8080/api/v1/auth/twitch/callback
```

Copy the resulting Client ID and Client Secret into `server/.env`:

```env
TWITCH_CLIENT_ID=...
TWITCH_SECRET=...
HMAC_SECRET=...        # any random 32+ byte hex (openssl rand -hex 32)
SESSION_SECRET=...     # any random 32+ byte hex
OWNER_TWITCH_ID=...    # your numeric Twitch user id
```

If you are not using Docker compose and are running the backend + Vite dev
server separately, keep `FRONTEND_URL=http://localhost:3000` for local dev.

## Configuration

Two files control runtime behavior, both well-commented:

- **`server/.env`** — credentials, database, paths, network. Start from
  `server/.env.example`.
- **`server/config.toml`** — operational tuning: download quality and
  concurrency, retry policy, polling intervals, title-tracking mode (poll /
  webhook / off).

EventSub-driven features need a publicly reachable `WEBHOOK_CALLBACK_URL`. If
yours is not reachable, ReplayVOD falls back to polling automatically.

ReplayVOD Connect supplies that public callback without port forwarding. In that
mode, `WEBHOOK_CALLBACK_URL` stays public and points at the relay ingest URL,
while `RELAY_SUBSCRIBE_URL` is the outbound WebSocket URL your self-hosted
server dials. The local replay target is separate and defaults to
`http://127.0.0.1:8080/api/v1/webhook/callback`:

```env
WEBHOOK_CALLBACK_URL=https://relay.replayvod.com/u/<token>
RELAY_SUBSCRIBE_URL=wss://relay.replayvod.com/u/<token>/subscribe
# Optional override only if the local API is not on 127.0.0.1:8080
RELAY_LOCAL_CALLBACK_URL=http://127.0.0.1:8080/api/v1/webhook/callback
```

`RELAY_LOCAL_CALLBACK_URL` must be a loopback URL ending in
`/api/v1/webhook/callback`; it is intentionally not a general-purpose local
forwarding target. The public ingest URL and subscribe URL must use the same
relay host and token, and the subscribe URL must use `wss://`.

With Docker Compose, use `PUBLIC_WEBHOOK_CALLBACK_URL` for the relay ingest URL;
the compose file maps it into `WEBHOOK_CALLBACK_URL` without changing the OAuth
or frontend URLs derived from `PUBLIC_BASE_URL`.

## Storage

```
data/
├── videos/        recorded VODs
├── thumbnails/    poster images and snapshot strips
└── replayvod.db   SQLite (when DATABASE_DRIVER=sqlite)
```

Override individual paths with `VIDEO_DIR`, `THUMBNAIL_DIR`, `SQLITE_PATH` in
`server/.env`. S3-compatible storage is wired through `[storage.s3]` in
`server/config.toml`.

## Development

Requires Go 1.26+, Node 22+, [Task](https://taskfile.dev/installation/),
and ffmpeg on `$PATH`.

```bash
task setup    # go mod download + npm install
task dev      # server on :8080, dashboard on :3000

task          # list every available task
task check    # vet + lint + typecheck
task test     # full test suite
task build    # production builds
```

The dashboard's Vite proxy forwards `/api/*` and `/trpc/*` to the backend, so
the SPA works at <http://localhost:3000> during development.

The bundled `docker-compose.yml` is not a live-reload dev environment; it is a
deployment path that serves the built SPA from the backend on :8080.

```
server/        Go backend (cmd/server, internal/api, internal/downloader, ...)
dashboard/     React SPA (TanStack Router + Query, Base UI, Tailwind v4)
```

## Built with

ReplayVOD is built on top of a sibling project of mine:

- **[trpcgo](https://github.com/befabri/trpcgo)** — tRPC server and TypeScript
  type generator for Go. Drives every API route in `server/internal/api/` and
  produces the typed client used by the dashboard.

## License

The recorder, dashboard, and supporting code in this repository are licensed
under the [GNU General Public License v3.0](LICENSE).

The webhook relay in [`relay/`](relay/) is licensed separately under the
[MIT License](relay/LICENSE) — it's a small piece of generic infrastructure
licensed permissively so it can be audited, self-hosted, or embedded in
unrelated projects without copyleft obligations.
