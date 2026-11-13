#!/bin/sh
# Clean install of this checkout, built locally (PIER_BUILD=1 ./install.sh).
# Proves: the panel answers, the setup code is printed, every migration is
# applied, a second run is idempotent, and PIER_LANG picks the language.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"

prepare_root
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

log "instalação limpa (build do checkout) / clean install (build from the checkout)"
stage_source "$repo_root"
run_installer PIER_BUILD=1 >/dev/null
setup_code=$(setup_code_from "$install_log")

check "código de configuração exibido / setup code is printed" test -n "$setup_code"
check "/healthz responde 200 / /healthz answers 200" test "$(http_status "$base_url/healthz")" = 200
check "/api/v1/version responde com versão / /api/v1/version answers with a version" sh -c "curl -sf '$base_url/api/v1/version' | grep -q '\"version\"\\|[0-9]\\.[0-9]'"
check "todas as migrations aplicadas / all migrations are applied ($(source_migration_count))" \
  test "$(applied_migration_count)" = "$(source_migration_count)"
check "nenhum erro de migration nos logs / no migration error in the logs" no_failed_migration_in_logs
check "código de configuração aceito / the setup code is accepted" test "$(create_admin "$setup_code")" = 201

log "segunda execução idempotente / second run is idempotent"
version_before=$(curl -sf "$base_url/api/v1/version")
run_installer PIER_BUILD=1 >/dev/null
wait_healthy
check "versão inalterada / the version is unchanged" test "$(curl -sf "$base_url/api/v1/version")" = "$version_before"
check ".env existente reutilizado / the existing .env is reused" contains "$install_log" "Using the existing .env"
check "instância informa administrador existente / the instance reports an administrator" contains "$install_log" "already has an administrator"
check "administrador ainda consegue entrar / the administrator can still sign in" test "$(login_status)" = 200
check "quantidade de migrations inalterada / migration count unchanged" test "$(applied_migration_count)" = "$(source_migration_count)"

log "seleção de idioma / language selection"
run_installer PIER_BUILD=1 PIER_LANG=pt >/dev/null
check "PIER_LANG=pt exibe português / PIER_LANG=pt prints Portuguese" contains "$install_log" "Usando o .env existente"
run_installer PIER_BUILD=1 PIER_LANG=en >/dev/null
check "PIER_LANG=en exibe inglês / PIER_LANG=en prints English" contains "$install_log" "Using the existing .env"
run_installer PIER_BUILD=1 PIER_LANG= LC_ALL=pt_BR.UTF-8 >/dev/null
check "LC_ALL=pt_BR seleciona português com PIER_LANG vazio / LC_ALL=pt_BR selects Portuguese when PIER_LANG is empty" contains "$install_log" "Usando o .env existente"

log "SUCESSO: instalação limpa / PASS: clean install"
