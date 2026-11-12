#!/bin/sh
# Installs (or upgrades) Pier with Docker on this machine.
#
#   ./install.sh
#
# It pulls the published image ghcr.io/allanadegas/pier:<version>, where the
# version is that of this checkout (internal/version/version.go). To upgrade:
# check out the new tag and run it again; data stays.
# Overrides, all optional: PIER_PORT, PIER_BIND, PIER_DATA_DIR, TZ.
#   PIER_PORT=8790 ./install.sh
#   PIER_VERSION=0.6.0 ./install.sh   pull another published version
#   PIER_BUILD=1 ./install.sh         build the image here instead of pulling
set -eu
umask 077

cd "$(dirname "$0")"

# Read before .env, which must not override what the caller asked for.
requested_version="${PIER_VERSION:-}"

say() { printf '\033[1m==>\033[0m %s\n' "$*"; }
fail() { printf '\033[31merro:\033[0m %s\n' "$*" >&2; exit 1; }

command -v docker >/dev/null 2>&1 || fail "Docker não encontrado. Instale: https://docs.docker.com/engine/install/"
docker compose version >/dev/null 2>&1 || fail "Falta o plugin compose do Docker (docker compose)."
docker info >/dev/null 2>&1 || fail "O Docker não está respondendo. Ele está ligado? O usuário tem acesso ao socket?"

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
  say "Criando .env (dados em $data_dir)"
  sed -e "s|^PIER_DATA_DIR=.*|PIER_DATA_DIR=$data_dir|" \
      -e "s|^PIER_PORT=.*|PIER_PORT=${PIER_PORT:-8700}|" \
      -e "s|^PIER_BIND=.*|PIER_BIND=${PIER_BIND:-127.0.0.1}|" \
      -e "s|^TZ=.*|TZ=${TZ:-UTC}|" \
      .env.example > .env
else
  say "Usando o .env existente"
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
  ''|*[!A-Za-z0-9._-]*) fail "Versão inválida: '$version'. Use PIER_VERSION=0.6.0 ou rode a partir de um checkout do Pier." ;;
esac
set_env_value PIER_VERSION "$version"
PIER_VERSION=$version
export PIER_VERSION
mkdir -p "$PIER_DATA_DIR" 2>/dev/null || fail "Não foi possível criar $PIER_DATA_DIR. Crie-o com permissão de escrita ou defina PIER_DATA_DIR no .env."

docker network inspect pier-panel >/dev/null 2>&1 || docker network create pier-panel >/dev/null

run_compose() {
  if [ -n "${PIER_DOMAIN:-}" ]; then
    docker compose -f compose.yaml -f compose.proxy.yaml "$@"
  else
    docker compose "$@"
  fi
}

if [ "${PIER_BUILD:-0}" = "1" ]; then
  say "Construindo a imagem e subindo o Pier $version"
  run_compose up --detach --build
else
  say "Baixando a imagem ghcr.io/allanadegas/pier:$version"
  run_compose pull pier || fail "Não foi possível baixar a imagem. Sem rede ou versão não publicada? Para construir aqui: PIER_BUILD=1 ./install.sh"
  say "Subindo o Pier $version"
  run_compose up --detach --no-build
fi

url="http://${PIER_BIND}:${PIER_PORT}"
[ "$PIER_BIND" = "0.0.0.0" ] && url="http://localhost:${PIER_PORT}"
say "Aguardando o Pier responder em $url"
tries=0
until docker compose exec -T pier wget -q -O /dev/null http://127.0.0.1:8700/api/v1/version 2>/dev/null; do
  tries=$((tries + 1))
  [ "$tries" -ge 60 ] && fail "O Pier não respondeu em 60 s. Veja: docker compose logs pier"
  sleep 1
done

# Logs survive a restart and can contain a code that was already consumed.
# Consult current state before presenting the first-account instructions.
auth_status=$(docker compose exec -T pier wget -q -O - http://127.0.0.1:8700/api/v1/auth/status) \
  || fail "Não foi possível verificar a configuração da instância."
code=
if printf '%s' "$auth_status" | grep -q '"needs_setup":true'; then
  code=$(docker compose logs pier 2>/dev/null | sed -n 's/.*setup_code=\([A-Z0-9-]*\).*/\1/p' | tail -n 1)
fi
echo
say "Pier no ar: $url"
if [ -n "$code" ]; then
  echo "    Código de configuração: $code"
  echo "    Use-o para criar a conta de administrador na primeira visita."
else
  echo "    A instância já tem administrador: entre com sua conta."
fi
echo "    Logs: docker compose logs -f pier · Parar: docker compose down (o que o Pier implantou continua rodando)"
