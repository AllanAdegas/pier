# shellcheck shell=sh disable=SC2034
# Shared helpers for the install tests. Sourced, not executed. POSIX sh.
#
# Everything the tests create is named pier-v08-installtest-* (container,
# network, compose project) and lives under $INSTALLTEST_ROOT; cleanup removes
# exactly that. No secret is needed: the admin account is created on a
# throwaway instance bound to 127.0.0.1.

tools_dir=$(cd "$(dirname "$0")" && pwd)
repo_root=$(cd "$tools_dir/../.." && pwd)

INSTALLTEST_ROOT=${INSTALLTEST_ROOT:-$HOME/.pier-v08-installtest}
INSTALLTEST_PORT=${INSTALLTEST_PORT:-18700}
test_network=pier-v08-installtest-net
test_container=pier-v08-installtest-pier
admin_email=admin@installtest.invalid
admin_password='installtest-Pass-2026!'
base_url="http://127.0.0.1:$INSTALLTEST_PORT"

work_dir=$INSTALLTEST_ROOT/checkout
data_dir=$INSTALLTEST_ROOT/data
cookie_jar=$INSTALLTEST_ROOT/cookies.txt
install_log=$INSTALLTEST_ROOT/install.log

export COMPOSE_PROJECT_NAME=pier-v08-installtest
export COMPOSE_FILE="compose.yaml:$tools_dir/compose.installtest.yaml"
export PIER_PORT="$INSTALLTEST_PORT"
export PIER_BIND=127.0.0.1
export PIER_DATA_DIR="$data_dir"
export PIER_LANG=en
export TZ=UTC
export PIER_UPDATE_CHECK=0
export PIER_DOMAIN=
unset PIER_VERSION PIER_BUILD PIER_SECRET_KEY

log() { printf '\n[installtest] %s\n' "$*"; }
die() { printf '\n[installtest] FALHA / FAIL: %s\n' "$*" >&2; exit 1; }

check() { # check <description> <command...>
  check_desc=$1
  shift
  "$@" || die "$check_desc"
  printf '  ok  %s\n' "$check_desc"
}

contains() { grep -q -- "$2" "$1"; } # contains <file> <text>

network_preexisted=
prepare_root() {
  case "$INSTALLTEST_ROOT" in
    /*pier-v08-installtest*) ;;
    *) die "Use an absolute isolated path / Use um caminho absoluto isolado" ;;
  esac
  case "$INSTALLTEST_ROOT" in
    *"'"*|*".."*|*'
'*) die "Unsafe path / Caminho inseguro" ;;
    *pier-v08-installtest*) ;;
    *) die "INSTALLTEST_ROOT deve conter pier-v08-installtest / INSTALLTEST_ROOT must contain pier-v08-installtest (refusing to delete $INSTALLTEST_ROOT)" ;;
  esac
  docker info >/dev/null 2>&1 || die "Docker não responde / Docker is not responding"
  docker ps -a --format '{{.Names}}' | grep -qx "$test_container" && die "container já existe / $test_container already exists"
  # Claim a fresh directory atomically; never erase a previous run or symlink.
  mkdir "$INSTALLTEST_ROOT" || die "Use a fresh INSTALLTEST_ROOT / Use um INSTALLTEST_ROOT novo"
  mkdir -p "$work_dir"
  : > "$install_log"
  docker network create "$test_network" >/dev/null
  # install.sh always makes sure pier-panel exists; remember if it was not ours.
  if docker network inspect pier-panel >/dev/null 2>&1; then network_preexisted=1; fi
}

# The container writes the data as root (mode 0600), so the host user cannot
# always read or delete it. These helpers run a shell as root with the data
# directory (and the scratch root) mounted, using the Pier image that is
# already local, so no pull happens. The image name is kept for after "down".
pier_image=
remember_image() { pier_image=$(docker inspect -f '{{.Config.Image}}' "$test_container" 2>/dev/null || true); }

as_root() { # as_root <script>
  [ -n "$pier_image" ] || remember_image
  [ -n "$pier_image" ] || return 1
  docker run --rm --entrypoint sh -v "$INSTALLTEST_ROOT:$INSTALLTEST_ROOT" -v "$data_dir:$data_dir" \
    "$pier_image" -c "$1" >/dev/null 2>&1
}

backup_data_dir() { as_root "cp -a '$data_dir/.' '$INSTALLTEST_ROOT/backup/'"; }
restore_data_dir() { as_root "rm -rf '$data_dir'/* '$data_dir'/.[!.]* && cp -a '$INSTALLTEST_ROOT/backup/.' '$data_dir/'"; }

cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [ "$status" -ne 0 ] && docker ps -a --format '{{.Names}}' | grep -qx "$test_container"; then
    printf '\n[installtest] últimas linhas do log Pier / last Pier log lines:\n' >&2
    docker logs --tail 40 "$test_container" >&2 2>&1 || true
  fi
  as_root "rm -rf '$data_dir'/* '$data_dir'/.[!.]* '$INSTALLTEST_ROOT'/backup" || true
  if [ -d "$work_dir" ]; then
    (cd "$work_dir" && docker compose down --volumes --remove-orphans >/dev/null 2>&1) || true
  fi
  docker rm -f "$test_container" >/dev/null 2>&1 || true
  docker network rm "$test_network" >/dev/null 2>&1 || true
  [ -n "$network_preexisted" ] || docker network rm pier-panel >/dev/null 2>&1 || true
  rm -rf "$INSTALLTEST_ROOT" 2>/dev/null || true
  exit "$status"
}

# Copies a source tree (checkout) into the working directory, keeping .env,
# the way "git checkout <tag>" keeps untracked files.
stage_source() { # stage_source <directory>
  find "$work_dir" -mindepth 1 -maxdepth 1 ! -name .env -exec rm -rf {} +
  tar -C "$1" --exclude=.git --exclude=node_modules --exclude=old-release --exclude=.env -cf - . | tar -C "$work_dir" -xf -
}

stage_git_ref() { # stage_git_ref <ref>: from the local repository
  git_stage=$INSTALLTEST_ROOT/ref
  rm -rf "$git_stage" && mkdir -p "$git_stage"
  git -C "$repo_root" archive -o "$INSTALLTEST_ROOT/ref.tar" "$1"
  tar -C "$git_stage" -xf "$INSTALLTEST_ROOT/ref.tar"
  rm "$INSTALLTEST_ROOT/ref.tar"
  stage_source "$git_stage"
  rm -rf "$git_stage"
}

run_installer() { # run_installer [VAR=value...]: runs ./install.sh in the work dir
  installer_compose=$COMPOSE_FILE
  for installer_arg in "$@"; do
    if [ "$installer_arg" = PIER_BUILD=1 ]; then
      installer_compose="$COMPOSE_FILE:$tools_dir/build.installtest.yaml"
    fi
  done
  (cd "$work_dir" && env COMPOSE_FILE="$installer_compose" "$@" ./install.sh) >"$install_log" 2>&1 \
    || { cat "$install_log" >&2; die "install.sh falhou / install.sh failed"; }
  cat "$install_log"
}

compose_in_work() { (cd "$work_dir" && docker compose "$@"); }

# --- HTTP ---------------------------------------------------------------

http_status() { curl --connect-timeout 5 --max-time 15 -s -o /dev/null -w '%{http_code}' "$@"; }

wait_healthy() {
  tries=0
  until [ "$(http_status "$base_url/healthz")" = 200 ]; do
    tries=$((tries + 1))
    [ "$tries" -lt 60 ] || die "/healthz não respondeu 200 / /healthz never answered 200"
    sleep 1
  done
}

json_field() { # json_field <field>: first string value of "field" from stdin
  sed -n "s/.*\"$1\" *: *\"\\([^\"]*\\)\".*/\\1/p" | head -n 1
}

api() { # api <method> <path> [json-body]: prints the body, fails on HTTP >= 400
  api_method=$1
  api_path=$2
  api_out=$INSTALLTEST_ROOT/api.out
  if [ $# -ge 3 ]; then
    api_code=$(curl --connect-timeout 5 --max-time 15 -s -o "$api_out" -w '%{http_code}' -X "$api_method" -b "$cookie_jar" -c "$cookie_jar" \
      -H 'X-Pier-Client: installtest' -H 'Content-Type: application/json' -d "$3" "$base_url$api_path")
  else
    api_code=$(curl --connect-timeout 5 --max-time 15 -s -o "$api_out" -w '%{http_code}' -X "$api_method" -b "$cookie_jar" -c "$cookie_jar" \
      -H 'X-Pier-Client: installtest' "$base_url$api_path")
  fi
  cat "$api_out"
  [ "$api_code" -lt 400 ] || { printf '\n%s %s -> HTTP %s\n' "$api_method" "$api_path" "$api_code" >&2; return 1; }
}

# --- Assertions over the data -------------------------------------------

source_migration_count() { find "$work_dir/internal/platform/sqlite/migrations" -name '*.sql' | wc -l | tr -d ' '; }

applied_migration_count() { # reads a consistent copy (database plus WAL) through the container
  # Stop writes before copying SQLite and its WAL as one snapshot.
  compose_in_work stop pier >/dev/null
  copy=$INSTALLTEST_ROOT/db-copy
  rm -rf "$copy" && mkdir "$copy"
  docker cp "$test_container:$data_dir/pier.db" "$copy/pier.db"
  for db_file in pier.db-wal pier.db-shm; do
    docker cp "$test_container:$data_dir/$db_file" "$copy/$db_file" 2>/dev/null || true
  done
  compose_in_work start pier >/dev/null
  wait_healthy
  sqlite3 "$copy/pier.db" 'SELECT COUNT(*) FROM schema_migrations;'
}

no_failed_migration_in_logs() { ! compose_in_work logs pier 2>&1 | grep -Ei 'apply migration|migration .*(fail|error)'; }

# The code is printed in English by this checkout and in Portuguese only by 0.6.x.
setup_code_from() { grep -E 'Setup code|Código de configuração' "$1" | sed 's/.*: //' | tail -n 1; }

create_admin() { # create_admin <setup-code>: the first administrator, through the setup API
  http_status -X POST -H 'Content-Type: application/json' -c "$cookie_jar" \
    -d "{\"name\":\"Install Test\",\"email\":\"$admin_email\",\"password\":\"$admin_password\",\"setup_code\":\"$1\"}" \
    "$base_url/api/v1/auth/setup"
}

login_status() {
  http_status -X POST -H 'Content-Type: application/json' -c "$cookie_jar" \
    -d "{\"email\":\"$admin_email\",\"password\":\"$admin_password\"}" "$base_url/api/v1/auth/login"
}

version_is() {
  version_body=$(curl --connect-timeout 5 --max-time 15 -fsS "$base_url/api/v1/version") || return 1
  test "$(printf '%s' "$version_body" | json_field version)" = "$1"
}
