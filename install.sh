#!/bin/sh
# Installs (or upgrades) Pier with Docker on this machine.
#
#   ./install.sh
#
# Re-running it rebuilds the image and restarts the container; data stays.
# Overrides, all optional: PIER_PORT, PIER_BIND, PIER_DATA_DIR, TZ.
#   PIER_PORT=8790 ./install.sh
set -eu

cd "$(dirname "$0")"

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

# shellcheck disable=SC1091
. ./.env
mkdir -p "$PIER_DATA_DIR" 2>/dev/null || fail "Não foi possível criar $PIER_DATA_DIR. Crie-o com permissão de escrita ou defina PIER_DATA_DIR no .env."

say "Construindo a imagem e subindo o Pier"
docker compose up --detach --build

url="http://${PIER_BIND}:${PIER_PORT}"
[ "$PIER_BIND" = "0.0.0.0" ] && url="http://localhost:${PIER_PORT}"
say "Aguardando o Pier responder em $url"
tries=0
until docker compose exec -T pier wget -q -O /dev/null http://127.0.0.1:8700/api/v1/version 2>/dev/null; do
  tries=$((tries + 1))
  [ "$tries" -ge 60 ] && fail "O Pier não respondeu em 60 s. Veja: docker compose logs pier"
  sleep 1
done

code=$(docker compose logs pier 2>/dev/null | sed -n 's/.*setup_code=\([A-Z0-9-]*\).*/\1/p' | tail -n 1)
echo
say "Pier no ar: $url"
if [ -n "$code" ]; then
  echo "    Código de configuração: $code"
  echo "    Use-o para criar a conta de administrador na primeira visita."
else
  echo "    A instância já tem administrador: entre com sua conta."
fi
echo "    Logs: docker compose logs -f pier · Parar: docker compose down (o que o Pier implantou continua rodando)"
