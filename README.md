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

Bundled Docker setup spins up Postgres and the ReplayVOD server together:

```bash
git clone https://github.com/befabri/replayvod.git
cd replayvod
cp server/.env.example server/.env
$EDITOR server/.env             # fill in Twitch credentials, see below
docker compose up -d
```

Open <http://localhost:8080>, sign in with your Twitch account, and the user
listed in `OWNER_TWITCH_ID` is granted the owner role.

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

For non-local deployments, change the host in the redirect URL to match.

## Configuration

Two files control runtime behavior, both well-commented:

- **`server/.env`** — credentials, database, paths, network. Start from
  `server/.env.example`.
- **`server/config.toml`** — operational tuning: download quality and
  concurrency, retry policy, polling intervals, title-tracking mode (poll /
  webhook / off).

EventSub-driven features need a publicly reachable `WEBHOOK_CALLBACK_URL`. If
yours is not reachable, ReplayVOD falls back to polling automatically.

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
ffmpeg, and yt-dlp on `$PATH`.

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

[GNU General Public License v3.0](LICENSE).
