#!/bin/sh
set -eu

INSTALL_SCRIPT=${INSTALL_SCRIPT:-/workspace/landing/public/install.sh}
BASE=$(mktemp -d "${TMPDIR:-/tmp}/replayvod-install-tests.XXXXXX")

pass_count=0

log() {
  printf '%s\n' "$*" >&2
}

fail() {
  log "FAIL: $*"
  exit 1
}

pass() {
  pass_count=$((pass_count + 1))
  log "ok $pass_count - $*"
}

cleanup() {
  rm -rf "$BASE"
}

trap cleanup EXIT

assert_eq() {
  got=$1
  want=$2
  label=$3
  if [ "$got" != "$want" ]; then
    fail "$label: got '$got', want '$want'"
  fi
}

assert_file_contains() {
  file=$1
  pattern=$2
  label=$3
  if ! grep -Eq -- "$pattern" "$file"; then
    fail "$label: pattern '$pattern' not found in $file"
  fi
}

assert_file_not_contains() {
  file=$1
  pattern=$2
  label=$3
  if [ -f "$file" ] && grep -Eq -- "$pattern" "$file"; then
    fail "$label: pattern '$pattern' unexpectedly found in $file"
  fi
}

assert_env_value() {
  file=$1
  key=$2
  want=$3
  got=$(grep -E "^$key=" "$file" | tail -n 1 | cut -d= -f2-)
  assert_eq "$got" "$want" "$key"
}

assert_env_private() {
  file=$1
  perms=$(stat -c '%a' "$file")
  assert_eq "$perms" 600 "$file permissions"
}

make_fixture_repo() {
  repo=$1
  profile=$2
  creds=$3

  mkdir -p "$repo/server"
  {
    printf 'TWITCH_CLIENT_ID='
    if [ "$creds" = with-creds ]; then printf 'client-id'; fi
    printf '\n'
    printf 'TWITCH_SECRET='
    if [ "$creds" = with-creds ]; then printf 'client-secret'; fi
    printf '\n'
    printf 'OWNER_TWITCH_ID='
    if [ "$creds" = with-creds ]; then printf '123456'; fi
    printf '\n'
    printf 'SESSION_SECRET='
    if [ "$creds" = with-creds ]; then printf 'existing-session'; fi
    printf '\n'
    printf 'PUBLIC_BASE_URL=\n'
    if [ "$profile" != omit ]; then
      printf 'COMPOSE_PROFILES=%s\n' "$profile"
    fi
  } > "$repo/server/.env.example"

  printf 'services: {}\n' > "$repo/docker-compose.yml"
  printf '# fixture\n' > "$repo/README.md"

  git init "$repo" >/dev/null
  git -C "$repo" checkout -b main >/dev/null 2>&1
  git -C "$repo" config user.name 'ReplayVOD Installer Test'
  git -C "$repo" config user.email 'install-test@example.invalid'
  git -C "$repo" add .
  git -C "$repo" commit -m initial >/dev/null
}

make_wrong_checkout() {
  checkout_dir=$1

  mkdir -p "$checkout_dir"
  git init "$checkout_dir" >/dev/null
  git -C "$checkout_dir" checkout -b main >/dev/null 2>&1
  git -C "$checkout_dir" remote add origin https://example.invalid/not-replayvod.git
}

make_fake_bin() {
  fake_dir=$1
  mkdir -p "$fake_dir"

  cat > "$fake_dir/docker" <<'EOF'
#!/bin/sh
cmd=${1:-}
sub=${2:-}

if [ "$cmd" = compose ] && [ "$sub" = version ]; then
  [ "${FAKE_COMPOSE_PLUGIN:-1}" = 1 ] && exit 0
  exit 1
fi

if [ "$cmd" = info ]; then
  [ "${FAKE_DOCKER_INFO_FAIL:-0}" = 1 ] && exit 1
  exit 0
fi

if [ "$cmd" = compose ]; then
  shift
  printf 'docker compose %s\n' "$*" >> "${FAKE_DOCKER_LOG:?}"
  exit 0
fi

printf 'unexpected docker command: %s\n' "$*" >&2
exit 1
EOF

  cat > "$fake_dir/docker-compose" <<'EOF'
#!/bin/sh
cmd=${1:-}

if [ "$cmd" = version ]; then
  [ "${FAKE_COMPOSE_V1:-0}" = 1 ] && exit 0
  exit 1
fi

printf 'docker-compose %s\n' "$*" >> "${FAKE_DOCKER_LOG:?}"
exit 0
EOF

  chmod +x "$fake_dir/docker" "$fake_dir/docker-compose"
}

run_installer() {
  case_dir=$1
  repo=$2
  app=$3
  shift 3

  fake_bin="$case_dir/fake-bin"
  home_dir=$(dirname "$app")
  docker_log="$case_dir/docker.log"
  out="$case_dir/stdout"
  err="$case_dir/stderr"
  mkdir -p "$home_dir"
  : > "$docker_log"

  set +e
  env \
    PATH="$fake_bin:$PATH" \
    HOME="$home_dir" \
    FAKE_DOCKER_LOG="$docker_log" \
    REPLAYVOD_REPO="file://$repo" \
    "$@" \
    sh "$INSTALL_SCRIPT" > "$out" 2> "$err"
  status=$?
  set -e

  printf '%s' "$status"
}

run_installer_tty() {
  case_dir=$1
  repo=$2
  app=$3
  answers=$4
  prompts=$5

  fake_bin="$case_dir/fake-bin"
  home_dir=$(dirname "$app")
  docker_log="$case_dir/docker.log"
  out="$case_dir/stdout"
  err="$case_dir/stderr"
  mkdir -p "$home_dir"
  : > "$docker_log"
  : > "$err"

  if ! command -v python3 >/dev/null 2>&1; then
    printf '127'
    return
  fi

  set +e
  result=$(env \
    PATH="$fake_bin:$PATH" \
    HOME="$home_dir" \
    FAKE_DOCKER_LOG="$docker_log" \
    REPLAYVOD_REPO="file://$repo" \
    python3 - "$INSTALL_SCRIPT" "$out" "$answers" "$prompts" <<'PY'
import errno
import os
import pty
import select
import signal
import sys
import time

install_script, out_path, answers_arg, prompts_arg = sys.argv[1:]
answers = answers_arg.split("|")
prompts = prompts_arg.split("|")

pid, fd = pty.fork()
if pid == 0:
    os.execvpe("sh", ["sh", install_script], os.environ)

output = bytearray()
buffer = ""
answer_index = 0
deadline = time.monotonic() + 30
status = 124

def exit_status(wait_status):
    if os.WIFEXITED(wait_status):
        return os.WEXITSTATUS(wait_status)
    if os.WIFSIGNALED(wait_status):
        return 128 + os.WTERMSIG(wait_status)
    return 1

try:
    while True:
        done_pid, wait_status = os.waitpid(pid, os.WNOHANG)
        if done_pid:
            status = exit_status(wait_status)
            break

        readable, _, _ = select.select([fd], [], [], 0.05)
        if readable:
            try:
                data = os.read(fd, 4096)
            except OSError as exc:
                if exc.errno != errno.EIO:
                    raise
                data = b""
            if data:
                output.extend(data)
                buffer += data.decode("utf-8", "replace")
                while answer_index < len(prompts) and prompts[answer_index] in buffer:
                    os.write(fd, (answers[answer_index] + "\n").encode("utf-8"))
                    answer_index += 1
                    buffer = ""

        if time.monotonic() > deadline:
            os.kill(pid, signal.SIGTERM)
            time.sleep(0.2)
            try:
                os.kill(pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            break
finally:
    while True:
        try:
            data = os.read(fd, 4096)
        except OSError:
            break
        if not data:
            break
        output.extend(data)
    with open(out_path, "wb") as f:
        f.write(output)

print(status)
PY
)
  py_status=$?
  set -e

  if [ "$py_status" -ne 0 ]; then
    printf '%s' "$py_status"
  else
    printf '%s' "$result"
  fi
}

case_dir() {
  case_path="$BASE/$1"
  mkdir -p "$case_path"
  printf '%s' "$case_path"
}

test_missing_credentials_non_interactive() {
  dir=$(case_dir missing-creds)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" sqlite missing-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 1 'missing credentials status'
  assert_env_private "$app/server/.env"
  assert_env_value "$app/server/.env" COMPOSE_PROFILES sqlite
  assert_file_not_contains "$app/server/.env" '^HMAC_SECRET=' 'no HMAC_SECRET written to env'
  assert_file_contains "$app/server/.env" '^SESSION_SECRET=[0-9a-f]{64}$' 'generated session secret'
  assert_file_contains "$dir/stderr" 'Fill these values' 'missing credentials guidance'
  assert_file_not_contains "$dir/docker.log" 'up -d' 'no start on incomplete config'
  pass 'non-interactive missing credentials fails cleanly'
}

test_preserves_postgres_profile() {
  dir=$(case_dir postgres-profile)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" postgres with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 0 'postgres status'
  assert_env_private "$app/server/.env"
  assert_env_value "$app/server/.env" COMPOSE_PROFILES postgres
  assert_env_value "$app/server/.env" SESSION_SECRET existing-session
  assert_file_contains "$dir/docker.log" '--profile postgres up -d' 'postgres compose start'
  pass 'preserves existing postgres profile and secrets'
}

test_starts_with_default_sqlite_profile() {
  dir=$(case_dir start-sqlite)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" omit with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 0 'start status'
  assert_env_value "$app/server/.env" COMPOSE_PROFILES sqlite
  assert_env_value "$app/server/.env" PUBLIC_BASE_URL http://localhost:8080
  assert_file_contains "$dir/docker.log" '--profile sqlite up -d' 'sqlite compose start'
  pass 'starts complete install with default sqlite profile'
}

test_profile_prompt_selects_postgres() {
  if ! command -v python3 >/dev/null 2>&1; then
    pass 'interactive profile prompt skipped because python3 is unavailable'
    return
  fi

  dir=$(case_dir profile-prompt)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" omit with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer_tty "$dir" "$repo" "$app" "||postgres||" "Install directory|Version tag or branch|Database profile|Public URL for ReplayVOD|Start containers now")

  assert_eq "$status" 0 'profile prompt status'
  assert_env_value "$app/server/.env" COMPOSE_PROFILES postgres
  assert_env_value "$app/server/.env" PUBLIC_BASE_URL http://localhost:8080
  assert_file_contains "$dir/stdout" 'Database profile' 'profile prompt output'
  assert_file_contains "$dir/docker.log" '--profile postgres up -d' 'postgres compose start'
  pass 'interactive profile prompt selects postgres'
}

test_prompt_retry_and_skip_start() {
  if ! command -v python3 >/dev/null 2>&1; then
    pass 'interactive retry/start prompt skipped because python3 is unavailable'
    return
  fi

  dir=$(case_dir prompt-retry-skip-start)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" omit with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer_tty "$dir" "$repo" "$app" "||banana|2||n" "Install directory|Version tag or branch|Database profile|Database profile|Public URL for ReplayVOD|Start containers now")

  assert_eq "$status" 0 'prompt retry skip-start status'
  assert_env_value "$app/server/.env" COMPOSE_PROFILES postgres
  assert_file_contains "$dir/stdout" "Please enter 'sqlite' or 'postgres'" 'invalid profile retry prompt'
  assert_file_contains "$dir/stdout" 'Start containers now' 'start prompt output'
  assert_file_contains "$dir/stdout" '--profile postgres' 'manual start command'
  assert_file_not_contains "$dir/docker.log" 'up -d' 'no docker start when prompt answered no'
  pass 'interactive prompts retry invalid profile and can skip start'
}

test_invalid_profile_fails_before_start() {
  dir=$(case_dir invalid-profile)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" invalid with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 1 'invalid profile status'
  assert_file_contains "$dir/stderr" "COMPOSE_PROFILES must be 'sqlite' or 'postgres'" 'invalid profile error'
  assert_file_not_contains "$dir/docker.log" 'up -d' 'no start on invalid profile'
  pass 'invalid profile fails before start'
}

test_wrong_existing_checkout_fails() {
  dir=$(case_dir wrong-checkout)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" sqlite with-creds
  make_wrong_checkout "$app"
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 1 'wrong checkout status'
  assert_file_contains "$dir/stderr" 'origin is not ReplayVOD' 'wrong checkout error'
  assert_file_not_contains "$dir/docker.log" 'up -d' 'no start on wrong checkout'
  pass 'refuses existing checkout with wrong origin'
}

test_dirty_existing_checkout_skips_pull_and_starts() {
  dir=$(case_dir dirty-checkout)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" sqlite with-creds
  make_fake_bin "$dir/fake-bin"

  git clone "file://$repo" "$app" >/dev/null 2>&1
  printf 'local edit\n' >> "$app/README.md"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 0 'dirty checkout status'
  assert_file_contains "$dir/stderr" 'local changes; skipping update' 'dirty checkout message'
  assert_file_contains "$dir/docker.log" '--profile sqlite up -d' 'dirty checkout start'
  pass 'dirty existing checkout skips pull and still starts'
}

test_docker_daemon_unreachable_does_not_start() {
  dir=$(case_dir docker-info-fail)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" sqlite with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app" FAKE_DOCKER_INFO_FAIL=1)

  assert_eq "$status" 0 'docker info fail status'
  assert_file_contains "$dir/stderr" 'daemon is not reachable' 'docker daemon guidance'
  assert_file_not_contains "$dir/docker.log" 'up -d' 'no start without daemon'
  pass 'docker daemon unavailable prints start command without starting'
}

test_docker_compose_v1_fallback() {
  dir=$(case_dir compose-v1)
  repo="$dir/repo"
  app="$dir/home/replayvod"
  make_fixture_repo "$repo" sqlite with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app" FAKE_COMPOSE_PLUGIN=0 FAKE_COMPOSE_V1=1)

  assert_eq "$status" 0 'compose v1 status'
  assert_file_contains "$dir/docker.log" '^docker-compose --env-file server/.env --profile sqlite up -d$' 'docker-compose fallback'
  pass 'falls back to docker-compose when compose plugin is unavailable'
}

test_public_installer_assets() {
  [ -f "$INSTALL_SCRIPT" ] || fail "missing install.sh"
  [ ! -e /workspace/landing/public/install.ps1 ] || fail "install.ps1 should not exist; Windows uses manual docker compose"

  assert_file_not_contains "$INSTALL_SCRIPT" 'REPLAYVOD_PROFILE' 'no profile env override'
  assert_file_not_contains "$INSTALL_SCRIPT" 'REPLAYVOD_DIR' 'no install directory env override'
  assert_file_not_contains "$INSTALL_SCRIPT" 'REPLAYVOD_REF' 'no ref env override'
  assert_file_not_contains "$INSTALL_SCRIPT" 'REPLAYVOD_NO_START' 'no no-start env override'
  assert_file_contains "$INSTALL_SCRIPT" 'prompt_profile' 'unix profile prompt'
  assert_file_contains "$INSTALL_SCRIPT" 'Install directory' 'unix install directory prompt'
  assert_file_contains "$INSTALL_SCRIPT" 'Version tag or branch' 'unix version prompt'
  assert_file_contains "$INSTALL_SCRIPT" 'Public URL for ReplayVOD' 'unix public URL prompt'
  assert_file_contains "$INSTALL_SCRIPT" 'Start containers now' 'unix start prompt'
  assert_file_contains "$INSTALL_SCRIPT" 'prompt_secret_env TWITCH_SECRET' 'unix secret prompt'
  pass 'public macOS/Linux installer asset exists; no Windows installer ships'
}

mkdir -p "$BASE"
export GIT_CONFIG_GLOBAL="$BASE/gitconfig"
git config --global init.defaultBranch main

sh -n "$INSTALL_SCRIPT"
shellcheck "$INSTALL_SCRIPT"

test_public_installer_assets
test_missing_credentials_non_interactive
test_preserves_postgres_profile
test_starts_with_default_sqlite_profile
test_profile_prompt_selects_postgres
test_prompt_retry_and_skip_start
test_invalid_profile_fails_before_start
test_wrong_existing_checkout_fails
test_dirty_existing_checkout_skips_pull_and_starts
test_docker_daemon_unreachable_does_not_start
test_docker_compose_v1_fallback

log "installer tests passed: $pass_count"
