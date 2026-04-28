#!/bin/sh
set -eu

INSTALL_SCRIPT=${INSTALL_SCRIPT:-/workspace/landing/public/install.sh}
INSTALL_PS1=${INSTALL_PS1:-/workspace/landing/public/install.ps1}
BASE=${TMPDIR:-/tmp}/replayvod-install-tests.$$

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
    printf 'HMAC_SECRET='
    if [ "$creds" = with-creds ]; then printf 'existing-hmac'; fi
    printf '\n'
    printf 'SESSION_SECRET='
    if [ "$creds" = with-creds ]; then printf 'existing-session'; fi
    printf '\n'
    printf 'PUBLIC_BASE_URL=http://localhost:8080\n'
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
  docker_log="$case_dir/docker.log"
  out="$case_dir/stdout"
  err="$case_dir/stderr"
  : > "$docker_log"

  set +e
  env \
    PATH="$fake_bin:$PATH" \
    FAKE_DOCKER_LOG="$docker_log" \
    REPLAYVOD_REPO="file://$repo" \
    REPLAYVOD_DIR="$app" \
    "$@" \
    sh "$INSTALL_SCRIPT" > "$out" 2> "$err"
  status=$?
  set -e

  printf '%s' "$status"
}

case_dir() {
  case_path="$BASE/$1"
  mkdir -p "$case_path"
  printf '%s' "$case_path"
}

test_missing_credentials_non_interactive() {
  dir=$(case_dir missing-creds)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" sqlite missing-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 1 'missing credentials status'
  assert_env_private "$app/server/.env"
  assert_env_value "$app/server/.env" COMPOSE_PROFILES sqlite
  assert_file_contains "$app/server/.env" '^HMAC_SECRET=[0-9a-f]{64}$' 'generated HMAC secret'
  assert_file_contains "$app/server/.env" '^SESSION_SECRET=[0-9a-f]{64}$' 'generated session secret'
  assert_file_contains "$dir/stderr" 'Fill these values' 'missing credentials guidance'
  assert_file_not_contains "$dir/docker.log" 'up -d --build' 'no start on incomplete config'
  pass 'non-interactive missing credentials fails cleanly'
}

test_preserves_postgres_profile_no_start() {
  dir=$(case_dir postgres-no-start)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" postgres with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app" REPLAYVOD_NO_START=1)

  assert_eq "$status" 0 'no-start status'
  assert_env_private "$app/server/.env"
  assert_env_value "$app/server/.env" COMPOSE_PROFILES postgres
  assert_env_value "$app/server/.env" HMAC_SECRET existing-hmac
  assert_env_value "$app/server/.env" SESSION_SECRET existing-session
  assert_file_contains "$dir/stderr" '--profile postgres' 'postgres start command'
  assert_file_not_contains "$dir/docker.log" 'up -d --build' 'no docker start with no-start flag'
  pass 'preserves existing postgres profile and secrets'
}

test_starts_with_default_sqlite_profile() {
  dir=$(case_dir start-sqlite)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" omit with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 0 'start status'
  assert_env_value "$app/server/.env" COMPOSE_PROFILES sqlite
  assert_file_contains "$dir/docker.log" '--profile sqlite up -d --build' 'sqlite compose start'
  pass 'starts complete install with default sqlite profile'
}

test_profile_override() {
  dir=$(case_dir profile-override)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" omit with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app" REPLAYVOD_PROFILE=postgres)

  assert_eq "$status" 0 'profile override status'
  assert_env_value "$app/server/.env" COMPOSE_PROFILES postgres
  assert_file_contains "$dir/docker.log" '--profile postgres up -d --build' 'postgres compose start'
  pass 'REPLAYVOD_PROFILE overrides default profile'
}

test_invalid_profile_fails_before_start() {
  dir=$(case_dir invalid-profile)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" invalid with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 1 'invalid profile status'
  assert_file_contains "$dir/stderr" "COMPOSE_PROFILES must be 'sqlite' or 'postgres'" 'invalid profile error'
  assert_file_not_contains "$dir/docker.log" 'up -d --build' 'no start on invalid profile'
  pass 'invalid profile fails before start'
}

test_wrong_existing_checkout_fails() {
  dir=$(case_dir wrong-checkout)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" sqlite with-creds
  make_wrong_checkout "$app"
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 1 'wrong checkout status'
  assert_file_contains "$dir/stderr" 'origin is not ReplayVOD' 'wrong checkout error'
  assert_file_not_contains "$dir/docker.log" 'up -d --build' 'no start on wrong checkout'
  pass 'refuses existing checkout with wrong origin'
}

test_dirty_existing_checkout_skips_pull_and_starts() {
  dir=$(case_dir dirty-checkout)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" sqlite with-creds
  make_fake_bin "$dir/fake-bin"

  git clone "file://$repo" "$app" >/dev/null 2>&1
  printf 'local edit\n' >> "$app/README.md"

  status=$(run_installer "$dir" "$repo" "$app")

  assert_eq "$status" 0 'dirty checkout status'
  assert_file_contains "$dir/stderr" 'local changes; skipping pull' 'dirty checkout message'
  assert_file_contains "$dir/docker.log" '--profile sqlite up -d --build' 'dirty checkout start'
  pass 'dirty existing checkout skips pull and still starts'
}

test_docker_daemon_unreachable_does_not_start() {
  dir=$(case_dir docker-info-fail)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" sqlite with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app" FAKE_DOCKER_INFO_FAIL=1)

  assert_eq "$status" 0 'docker info fail status'
  assert_file_contains "$dir/stderr" 'daemon is not reachable' 'docker daemon guidance'
  assert_file_not_contains "$dir/docker.log" 'up -d --build' 'no start without daemon'
  pass 'docker daemon unavailable prints start command without starting'
}

test_docker_compose_v1_fallback() {
  dir=$(case_dir compose-v1)
  repo="$dir/repo"
  app="$dir/app"
  make_fixture_repo "$repo" sqlite with-creds
  make_fake_bin "$dir/fake-bin"

  status=$(run_installer "$dir" "$repo" "$app" FAKE_COMPOSE_PLUGIN=0 FAKE_COMPOSE_V1=1)

  assert_eq "$status" 0 'compose v1 status'
  assert_file_contains "$dir/docker.log" '^docker-compose --env-file server/.env --profile sqlite up -d --build$' 'docker-compose fallback'
  pass 'falls back to docker-compose when compose plugin is unavailable'
}

test_public_installer_assets() {
  [ -f "$INSTALL_SCRIPT" ] || fail "missing install.sh"
  [ -f "$INSTALL_PS1" ] || fail "missing install.ps1"

  assert_file_contains "$INSTALL_SCRIPT" 'REPLAYVOD_PROFILE' 'unix installer profile override'
  assert_file_contains "$INSTALL_SCRIPT" 'prompt_secret_env TWITCH_SECRET' 'unix secret prompt'
  assert_file_contains "$INSTALL_PS1" 'REPLAYVOD_PROFILE' 'windows installer profile override'
  assert_file_contains "$INSTALL_PS1" 'REPLAYVOD_INSTALL_MODE' 'windows installer mode switch'
  assert_file_contains "$INSTALL_PS1" 'Install-Native' 'windows native install mode'
  assert_file_contains "$INSTALL_PS1" 'Install-Docker' 'windows docker fallback mode'
  assert_file_contains "$INSTALL_PS1" 'replayvod.exe' 'windows native executable path'
  assert_file_contains "$INSTALL_PS1" 'REPLAYVOD_WINDOWS_URL' 'windows custom binary URL override'
  assert_file_contains "$INSTALL_PS1" 'Read-Host.*-AsSecureString' 'windows secret prompt'
  assert_file_contains "$INSTALL_PS1" 'docker compose' 'windows compose plugin support'
  assert_file_contains "$INSTALL_PS1" 'docker-compose' 'windows compose fallback support'
  pass 'public macOS/Linux and Windows installer assets exist'
}

mkdir -p "$BASE"
export GIT_CONFIG_GLOBAL="$BASE/gitconfig"
git config --global init.defaultBranch main

sh -n "$INSTALL_SCRIPT"
shellcheck "$INSTALL_SCRIPT"

test_public_installer_assets
test_missing_credentials_non_interactive
test_preserves_postgres_profile_no_start
test_starts_with_default_sqlite_profile
test_profile_override
test_invalid_profile_fails_before_start
test_wrong_existing_checkout_fails
test_dirty_existing_checkout_skips_pull_and_starts
test_docker_daemon_unreachable_does_not_start
test_docker_compose_v1_fallback

log "installer tests passed: $pass_count"
