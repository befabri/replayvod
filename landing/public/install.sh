#!/bin/sh
set -eu

REPO_URL=${REPLAYVOD_REPO:-${REPLAYVOD_REPO_URL:-https://github.com/befabri/replayvod.git}}
REF_OVERRIDE=${REPLAYVOD_REF:-${REPLAYVOD_BRANCH:-}}
INSTALL_DIR=${REPLAYVOD_DIR:-${HOME:-.}/replayvod}
TTY_STTY=

umask 077

restore_tty() {
  if [ -n "${TTY_STTY:-}" ]; then
    stty "$TTY_STTY" < /dev/tty 2>/dev/null || true
    printf '\n' > /dev/tty
    TTY_STTY=
  fi
}

cleanup() {
  rm -f server/.env.tmp.$$ 2>/dev/null || true
  restore_tty
}

abort_secret_prompt() {
  restore_tty
  exit 130
}

trap cleanup EXIT

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    printf 'replayvod install: missing required command: %s\n' "$1" >&2
    exit 1
  fi
}

secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  else
    need dd
    need od
    dd if=/dev/urandom bs=32 count=1 2>/dev/null | od -An -tx1 | tr -d ' \n'
  fi
}

has_tty() {
  set +e
  ( : < /dev/tty > /dev/tty ) 2>/dev/null
  ok=$?
  set -e
  return "$ok"
}

get_env() {
  key=$1
  if [ ! -f server/.env ]; then
    return 0
  fi
  line=$(grep -E "^$key=" server/.env 2>/dev/null | tail -n 1 || true)
  value=${line#*=}
  printf '%s' "$value" | sed 's/[[:space:]]#.*$//' | sed 's/^[[:space:]]*//; s/[[:space:]]*$//'
}

set_env() {
  key=$1
  value=$2
  tmp=server/.env.tmp.$$
  awk -v key="$key" -v value="$value" '
    BEGIN { done = 0 }
    $0 ~ "^" key "=" {
      print key "=" value
      done = 1
      next
    }
    { print }
    END {
      if (!done) print key "=" value
    }
  ' server/.env >"$tmp"
  mv "$tmp" server/.env
  chmod 600 server/.env
}

prompt_env() {
  key=$1
  label=$2
  if [ -n "$(get_env "$key")" ] || ! has_tty; then
    return
  fi
  printf '%s: ' "$label" >/dev/tty
  IFS= read -r value </dev/tty || value=
  if [ -n "$value" ]; then
    set_env "$key" "$value"
  fi
}

prompt_secret_env() {
  key=$1
  label=$2
  if [ -n "$(get_env "$key")" ] || ! has_tty; then
    return
  fi

  printf '%s: ' "$label" >/dev/tty
  TTY_STTY=$(stty -g < /dev/tty 2>/dev/null || true)
  if [ -n "$TTY_STTY" ]; then
    stty -echo < /dev/tty 2>/dev/null || true
    trap abort_secret_prompt INT TERM HUP
  fi

  IFS= read -r value </dev/tty || value=
  if [ -n "$TTY_STTY" ]; then
    restore_tty
    trap - INT TERM HUP
  fi

  if [ -n "$value" ]; then
    set_env "$key" "$value"
  fi
}

resolve_latest_tag() {
  # Highest-versioned v* tag on the remote, or empty if none exist yet.
  git ls-remote --tags --refs --sort=-v:refname "$REPO_URL" 2>/dev/null \
    | awk -F/ '$NF ~ /^v[0-9]/ { print $NF; exit }'
}

origin_is_replayvod() {
  origin=$1
  case "$origin" in
    "$REPO_URL" | https://github.com/befabri/replayvod | https://github.com/befabri/replayvod.git | git@github.com:befabri/replayvod.git)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

compose_plugin_available() {
  docker compose version >/dev/null 2>&1
}

compose_v1_available() {
  command -v docker-compose >/dev/null 2>&1 && docker-compose version >/dev/null 2>&1
}

compose_run() {
  if [ "$COMPOSE_BIN" = docker-compose ]; then
    docker-compose "$@"
  else
    docker compose "$@"
  fi
}

compose_command_text() {
  if [ "$COMPOSE_BIN" = docker-compose ]; then
    printf 'docker-compose --env-file server/.env --profile %s up -d' "$PROFILE"
  else
    printf 'docker compose --env-file server/.env --profile %s up -d' "$PROFILE"
  fi
}

need git
need docker

if compose_plugin_available; then
  COMPOSE_BIN=docker
elif compose_v1_available; then
  COMPOSE_BIN=docker-compose
else
  printf 'replayvod install: Docker Compose is required (docker compose or docker-compose).\n' >&2
  exit 1
fi

# Track the latest release tag by default so the cloned source matches the
# published :latest image. Falls back to main until the first tag is cut, and
# REPLAYVOD_REF (or REPLAYVOD_BRANCH) pins a specific tag or branch.
REF=$REF_OVERRIDE
if [ -z "$REF" ]; then
  REF=$(resolve_latest_tag)
fi
if [ -z "$REF" ]; then
  REF=main
  printf 'replayvod install: no release tags found; using %s.\n' "$REF" >&2
fi

if [ -d "$INSTALL_DIR/.git" ]; then
  origin=$(git -C "$INSTALL_DIR" remote get-url origin 2>/dev/null || true)
  if ! origin_is_replayvod "$origin"; then
    printf 'replayvod install: existing checkout origin is not ReplayVOD: %s\n' "$origin" >&2
    exit 1
  fi
  if [ -n "$(git -C "$INSTALL_DIR" status --porcelain)" ]; then
    printf 'replayvod install: existing checkout has local changes; skipping update to %s.\n' "$REF" >&2
  else
    git -C "$INSTALL_DIR" fetch --depth=1 origin "$REF"
    git -C "$INSTALL_DIR" checkout --quiet --detach FETCH_HEAD
  fi
elif [ -e "$INSTALL_DIR" ]; then
  printf 'replayvod install: %s exists and is not a git checkout.\n' "$INSTALL_DIR" >&2
  exit 1
else
  git clone --depth=1 --branch "$REF" "$REPO_URL" "$INSTALL_DIR"
fi

cd "$INSTALL_DIR"

if [ ! -f server/.env ]; then
  cp server/.env.example server/.env
  chmod 600 server/.env
else
  chmod 600 server/.env
fi

if [ -z "$(get_env SESSION_SECRET)" ]; then
  set_env SESSION_SECRET "$(secret)"
fi

PROFILE=${REPLAYVOD_PROFILE:-$(get_env COMPOSE_PROFILES)}
if [ -z "$PROFILE" ]; then
  PROFILE=sqlite
fi
case "$PROFILE" in
  sqlite | postgres) ;;
  *)
    printf "replayvod install: COMPOSE_PROFILES must be 'sqlite' or 'postgres' (got '%s').\n" "$PROFILE" >&2
    exit 1
    ;;
esac
set_env COMPOSE_PROFILES "$PROFILE"

prompt_env TWITCH_CLIENT_ID 'Twitch Client ID'
prompt_secret_env TWITCH_SECRET 'Twitch Client Secret'
prompt_env OWNER_TWITCH_ID 'Owner Twitch numeric user ID'

if [ -z "$(get_env TWITCH_CLIENT_ID)" ] || [ -z "$(get_env TWITCH_SECRET)" ] || [ -z "$(get_env OWNER_TWITCH_ID)" ]; then
  cat >&2 <<EOF

ReplayVOD is downloaded and server/.env was created.
Fill these values in server/.env: TWITCH_CLIENT_ID, TWITCH_SECRET, OWNER_TWITCH_ID.

Then run:

  $(compose_command_text)

EOF
  exit 1
fi

if [ "${REPLAYVOD_NO_START:-0}" = 1 ]; then
  printf 'ReplayVOD is ready. Start it with:\n\n  %s\n' "$(compose_command_text)" >&2
  exit 0
fi

if ! docker info >/dev/null 2>&1; then
  printf 'ReplayVOD is ready, but the Docker daemon is not reachable. Start it with:\n\n  %s\n' "$(compose_command_text)" >&2
  exit 0
fi

compose_run --env-file server/.env --profile "$PROFILE" up -d
base_url=$(get_env PUBLIC_BASE_URL)
if [ -z "$base_url" ]; then
  base_url=http://localhost:8080
fi
printf '\nReplayVOD is starting at %s\n' "$base_url"
