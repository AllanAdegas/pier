#!/bin/sh
# Installs (or upgrades) Pier with Docker on this machine.
#
#   ./install.sh
#
# It pulls the published image ghcr.io/allanadegas/pier:<version>, where the
# version is that of this checkout (internal/version/version.go). To upgrade:
# check out the new tag and run it again; data stays.
# Overrides, all optional: PIER_PORT, PIER_BIND, PIER_DATA_DIR, TZ, PIER_LANG.
#   PIER_PORT=8790 ./install.sh
#   PIER_VERSION=0.6.0 ./install.sh   pull another published version
#   PIER_BUILD=1 ./install.sh         build the image here instead of pulling
#   PIER_LANG=pt ./install.sh         messages in Portuguese (en for English); default
#                                     follows LC_ALL/LC_MESSAGES/LANG, English otherwise
set -eu
umask 077

cd "$(dirname "$0")"

# Read before .env, which must not override what the caller asked for.
requested_version="${PIER_VERSION:-}"

# Messages: English and Portuguese live side by side in msg(), one arm per
# language and key (tools/docpairs checks that every key has both). Text is
# chosen by PIER_LANG (pt|en), else by the first of LC_ALL/LC_MESSAGES/LANG
# that is set: pt* is Portuguese, anything else English.
detect_lang() {
  case "${PIER_LANG:-}" in
    pt*|PT*) echo pt; return ;;
    en*|EN*) echo en; return ;;
  esac
  for locale_value in "${LC_ALL:-}" "${LC_MESSAGES:-}" "${LANG:-}"; do
    [ -n "$locale_value" ] || continue
    case "$locale_value" in
      pt*) echo pt ;;
      *) echo en ;;
    esac
    return
  done
  echo en
}
lang=$(detect_lang)

msg() {
  msg_key=$1
  shift
  case "$lang.$msg_key" in
    pt.error_label) printf 'erro:' ;;
    en.error_label) printf 'error:' ;;
    pt.docker_missing) printf 'Docker não encontrado. Instale: https://docs.docker.com/engine/install/' ;;
    en.docker_missing) printf 'Docker not found. Install it: https://docs.docker.com/engine/install/' ;;
    pt.compose_missing) printf 'Falta o plugin compose do Docker (docker compose).' ;;
    en.compose_missing) printf 'The Docker compose plugin is missing (docker compose).' ;;
    pt.docker_unresponsive) printf 'O Docker não está respondendo. Ele está ligado? O usuário tem acesso ao socket?' ;;
    en.docker_unresponsive) printf 'Docker is not responding. Is it running? Does the user have access to the socket?' ;;
    pt.data_dir_invalid) printf 'O diretório de dados não pode conter aspas simples ou quebra de linha.' ;;
    en.data_dir_invalid) printf 'The data directory cannot contain single quotes or line breaks.' ;;
    pt.creating_env) printf 'Criando .env (dados em %s)' "$@" ;;
    en.creating_env) printf 'Creating .env (data in %s)' "$@" ;;
    pt.using_env) printf 'Usando o .env existente' ;;
    en.using_env) printf 'Using the existing .env' ;;
    pt.invalid_version) printf "Versão inválida: '%s'. Use PIER_VERSION=0.6.0 ou rode a partir de um checkout do Pier." "$@" ;;
    en.invalid_version) printf "Invalid version: '%s'. Use PIER_VERSION=0.6.0 or run from a Pier checkout." "$@" ;;
    pt.data_dir_failed) printf 'Não foi possível criar %s. Crie-o com permissão de escrita ou defina PIER_DATA_DIR no .env.' "$@" ;;
    en.data_dir_failed) printf 'Could not create %s. Create it with write permission or set PIER_DATA_DIR in .env.' "$@" ;;
    pt.building) printf 'Construindo a imagem e subindo o Pier %s' "$@" ;;
    en.building) printf 'Building the image and starting Pier %s' "$@" ;;
    pt.pulling) printf 'Baixando a imagem ghcr.io/allanadegas/pier:%s' "$@" ;;
    en.pulling) printf 'Pulling the image ghcr.io/allanadegas/pier:%s' "$@" ;;
    pt.pull_failed) printf 'Não foi possível baixar a imagem. Sem rede ou versão não publicada? Para construir aqui: PIER_BUILD=1 ./install.sh' ;;
    en.pull_failed) printf 'Could not pull the image. No network, or version not published? To build here: PIER_BUILD=1 ./install.sh' ;;
    pt.starting) printf 'Subindo o Pier %s' "$@" ;;
    en.starting) printf 'Starting Pier %s' "$@" ;;
    pt.waiting) printf 'Aguardando o Pier responder em %s' "$@" ;;
    en.waiting) printf 'Waiting for Pier to respond at %s' "$@" ;;
    pt.not_responding) printf 'O Pier não respondeu em 60 s. Veja: docker compose logs pier' ;;
    en.not_responding) printf 'Pier did not respond within 60 s. See: docker compose logs pier' ;;
    pt.status_failed) printf 'Não foi possível verificar a configuração da instância.' ;;
    en.status_failed) printf 'Could not check the instance setup.' ;;
    pt.ready) printf 'Pier no ar: %s' "$@" ;;
    en.ready) printf 'Pier is up: %s' "$@" ;;
    pt.setup_code) printf '    Código de configuração: %s' "$@" ;;
    en.setup_code) printf '    Setup code: %s' "$@" ;;
    pt.setup_hint) printf '    Use-o para criar a conta de administrador na primeira visita.' ;;
    en.setup_hint) printf '    Use it to create the administrator account on your first visit.' ;;
    pt.has_admin) printf '    A instância já tem administrador: entre com sua conta.' ;;
    en.has_admin) printf '    The instance already has an administrator: sign in with your account.' ;;
    pt.footer) printf '    Logs: docker compose logs -f pier · Parar: docker compose down (o que o Pier implantou continua rodando)' ;;
    en.footer) printf '    Logs: docker compose logs -f pier · Stop: docker compose down (what Pier deployed keeps running)' ;;
    *) printf '%s' "$msg_key" ;;
  esac
}

say() { printf '\033[1m==>\033[0m %s\n' "$(msg "$@")"; }
fail() { printf '\033[31m%s\033[0m %s\n' "$(msg error_label)" "$(msg "$@")" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || fail docker_missing
docker compose version >/dev/null 2>&1 || fail compose_missing
docker info >/dev/null 2>&1 || fail docker_unresponsive

# The data directory is mounted at the same path inside and outside the
# container (Pier hands host paths to the host's Docker). Docker Desktop only
# shares folders under the home directory by default, so macOS gets one there.
default_data_dir() {
  case "$(uname -s)" in
    Darwin) echo "$HOME/.pier" ;;
    *) echo "/data/pier" ;;
  esac
}

if [ ! -f .env ]; then
  data_dir="${PIER_DATA_DIR:-$(default_data_dir)}"
  # A generated value is read by both POSIX sh and Compose. Single quotes
  # preserve spaces, dollar signs and command substitutions in both parsers.
  case "$data_dir" in
    *"'"*|*'
'*) fail data_dir_invalid ;;
  esac
  escaped_data_dir=$(printf '%s' "$data_dir" | sed 's/[\\&|]/\\&/g')
  say creating_env "$data_dir"
  sed -e "s|^PIER_DATA_DIR=.*|PIER_DATA_DIR='$escaped_data_dir'|" \
      -e "s|^PIER_PORT=.*|PIER_PORT=${PIER_PORT:-8700}|" \
      -e "s|^PIER_BIND=.*|PIER_BIND=${PIER_BIND:-127.0.0.1}|" \
      -e "s|^TZ=.*|TZ=${TZ:-UTC}|" \
      .env.example > .env
else
  say using_env
fi

# The checkout is the source of truth for the version, so upgrading is "check
# out the new tag and run this again"; PIER_VERSION on the command line wins.
checkout_version() {
  sed -n 's/^[[:space:]]*Number = "\([0-9][0-9.]*\)".*/\1/p' internal/version/version.go | head -n 1
}

set_env_value() {
  name=$1
  value=$2
  if grep -q "^$name=" .env; then
    sed "s|^$name=.*|$name=$value|" .env > .env.tmp && mv .env.tmp .env
  else
    printf '%s=%s\n' "$name" "$value" >> .env
  fi
}

# shellcheck disable=SC1091
. ./.env
version="${requested_version:-$(checkout_version)}"
case "$version" in
  ''|*[!A-Za-z0-9._-]*) fail invalid_version "$version" ;;
esac
set_env_value PIER_VERSION "$version"
PIER_VERSION=$version
export PIER_VERSION
mkdir -p "$PIER_DATA_DIR" 2>/dev/null || fail data_dir_failed "$PIER_DATA_DIR"

docker network inspect pier-panel >/dev/null 2>&1 || docker network create pier-panel >/dev/null

run_compose() {
  if [ -n "${PIER_DOMAIN:-}" ]; then
    docker compose -f compose.yaml -f compose.proxy.yaml "$@"
  else
    docker compose "$@"
  fi
}

if [ "${PIER_BUILD:-0}" = "1" ]; then
  say building "$version"
  run_compose up --detach --build
else
  say pulling "$version"
  run_compose pull pier || fail pull_failed
  say starting "$version"
  run_compose up --detach --no-build
fi

url="http://${PIER_BIND}:${PIER_PORT}"
[ "$PIER_BIND" = "0.0.0.0" ] && url="http://localhost:${PIER_PORT}"
say waiting "$url"
tries=0
until docker compose exec -T pier wget -q -O /dev/null http://127.0.0.1:8700/api/v1/version 2>/dev/null; do
  tries=$((tries + 1))
  [ "$tries" -ge 60 ] && fail not_responding
  sleep 1
done

# Logs survive a restart and can contain a code that was already consumed.
# Consult current state before presenting the first-account instructions.
auth_status=$(docker compose exec -T pier wget -q -O - http://127.0.0.1:8700/api/v1/auth/status) \
  || fail status_failed
code=
if printf '%s' "$auth_status" | grep -q '"needs_setup":true'; then
  code=$(docker compose logs pier 2>/dev/null | sed -n 's/.*setup_code=\([A-Z0-9-]*\).*/\1/p' | tail -n 1)
fi
echo
say ready "$url"
if [ -n "$code" ]; then
  printf '%s\n' "$(msg setup_code "$code")"
  printf '%s\n' "$(msg setup_hint)"
else
  printf '%s\n' "$(msg has_admin)"
fi
printf '%s\n' "$(msg footer)"
