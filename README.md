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

The bundled Docker Compose stack pulls a prebuilt multi-arch image
(`ghcr.io/befabri/replayvod:latest`, linux/amd64 and linux/arm64) and serves
the dashboard from the Go server on one origin. Pick a database profile:
`sqlite` for the simplest single-container deploy, or `postgres` for a
two-container setup:

macOS / Linux:

```bash
curl -fsSL https://replayvod.com/install.sh | sh
```

Or clone the repo and run Docker Compose yourself:

```bash
git clone https://github.com/befabri/replayvod.git
cd replayvod
cp server/.env.example server/.env
$EDITOR server/.env             # fill in Twitch credentials, see below
docker compose --env-file server/.env --profile sqlite up -d
# pulls ghcr.io/befabri/replayvod:latest. Add --build to build from source,
# or --profile postgres to add a Postgres container.
```

Windows (Docker Desktop, PowerShell):

```powershell
git clone https://github.com/befabri/replayvod.git
Set-Location replayvod
Copy-Item server/.env.example server/.env
notepad server/.env             # fill in Twitch credentials, see below
docker compose --env-file server/.env --profile sqlite up -d
# pulls ghcr.io/befabri/replayvod:latest. Add --build to build from source,
# or --profile postgres to add a Postgres container.
```

Open <http://localhost:8080>, sign in with your Twitch account, and the user
listed in `OWNER_TWITCH_ID` is granted the owner role.

To skip the `--profile` flag on every command, set `COMPOSE_PROFILES=sqlite`
(or `=postgres`) in `server/.env`.

Update later by pulling the newest published image:

```bash
docker compose --env-file server/.env --profile sqlite pull
docker compose --env-file server/.env --profile sqlite up -d
```

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
  concurrency, retry policy, scheduler intervals, and server-mode poll interval.

Live detection and title tracking are configured by the owner-facing server
mode. Environment variables are still supported for Docker/ops workflows: when
`SERVER_MODE` is set and complete, env wins and onboarding is skipped. When it
is empty, the dashboard asks the owner to configure the mode. Dashboard-saved
mode changes are applied on the next server start; restart when the dashboard
reports that a restart is required.

Server modes:

- `off` — no live detection, live-dot feed, or mid-stream title tracking.
- `poll` — poll Helix for live detection and mid-stream title changes.
- `direct` — Twitch posts directly to your public
  `WEBHOOK_CALLBACK_URL`.
- `relay` — Twitch posts to the Cloudflare relay and this server dials the
  relay over `RELAY_SUBSCRIBE_URL`.

ReplayVOD Connect supplies that public callback without port forwarding. In that
mode, `RELAY_INGEST_URL` is the public HTTPS URL Twitch posts to, while
`RELAY_SUBSCRIBE_URL` is the outbound WebSocket URL your self-hosted server
dials. The local replay target is separate and defaults to
`http://127.0.0.1:8080/api/v1/webhook/callback`:

```env
SERVER_MODE=relay
RELAY_INGEST_URL=https://relay.replayvod.com/u/<token>
RELAY_SUBSCRIBE_URL=wss://relay.replayvod.com/u/<token>/subscribe
# Optional override only if the local API is not on 127.0.0.1:8080
RELAY_LOCAL_CALLBACK_URL=http://127.0.0.1:8080/api/v1/webhook/callback
```

`RELAY_LOCAL_CALLBACK_URL` must be a loopback URL ending in
`/api/v1/webhook/callback`; it is intentionally not a general-purpose local
forwarding target. The public ingest URL and subscribe URL must use the same
relay host and token, and the subscribe URL must use `wss://`.

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
