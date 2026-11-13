#!/bin/sh
# Upgrade from the last published release to this checkout, then roll back.
# Old release: PIER_OLD_SRC (a directory with that release's checkout, as CI
# provides it) or PIER_OLD_REF (default v0.7.0) read from the local git
# repository. Its image is pulled from GHCR (public, no login); the new one is
# built from this checkout.
set -eu
# shellcheck source=lib.sh
. "$(dirname "$0")/lib.sh"
# shellcheck source=seed.sh
. "$(dirname "$0")/seed.sh"

old_ref=${PIER_OLD_REF:-v0.7.0}
old_version=${old_ref#v}

prepare_root
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

log "instalar release anterior / install the previous release $old_ref (published image)"
if [ -n "${PIER_OLD_SRC:-}" ]; then stage_source "$PIER_OLD_SRC"; else stage_git_ref "$old_ref"; fi
run_installer >/dev/null
check "painel antigo executa / the old panel runs $old_version" version_is "$old_version"
old_migrations=$(applied_migration_count)
check "código de configuração aceito / the setup code is accepted" test "$(create_admin "$(setup_code_from "$install_log")")" = 201
seed_resources
check "projeto, aplicação e variável existem antes da atualização / project, application and variable exist before the upgrade" seeded_resources_intact

log "copiar dados de controle (passo 1 documentado) e atualizar / back up the control data (documented step 1), then upgrade"
compose_in_work stop pier >/dev/null
remember_image
mkdir "$INSTALLTEST_ROOT/backup"
backup_data_dir
stage_source "$repo_root"
run_installer PIER_BUILD=1 >/dev/null
wait_healthy
new_version=$(sed -n 's/^[[:space:]]*Number = "\([0-9][0-9.]*\)".*/\1/p' "$work_dir/internal/version/version.go")
check "versão do checkout / checkout version" version_is "$new_version"
check "migrations aplicadas / migrations were applied ($old_migrations -> $(source_migration_count))" \
  test "$(applied_migration_count)" = "$(source_migration_count)"
check "nenhum erro de migration nos logs / no migration error in the logs" no_failed_migration_in_logs
check "instância já tem administrador / the instance already has an administrator" contains "$install_log" "already has an administrator"
check "administrador entra com senha antiga / the administrator signs in with the old password" test "$(login_status)" = 200
check "projeto, aplicação e variável preservados (segredo decifrado) / project, application and variable survived (secret decrypts)" seeded_resources_intact

log "rollback: restaurar cópia e executar release antiga / roll back: restore the backup copy and run the old release"
compose_in_work down >/dev/null
restore_data_dir
if [ -n "${PIER_OLD_SRC:-}" ]; then stage_source "$PIER_OLD_SRC"; else stage_git_ref "$old_ref"; fi
run_installer >/dev/null
check "release antiga executa novamente / the old release runs again" version_is "$old_version"
check "migrations restauradas / migrations are back to $old_migrations" test "$(applied_migration_count)" = "$old_migrations"
check "administrador entra após rollback / the administrator signs in after the rollback" test "$(login_status)" = 200
check "projeto, aplicação e variável intactos após rollback / project, application and variable are intact after the rollback" seeded_resources_intact

log "SUCESSO: atualização e rollback / PASS: upgrade $old_ref -> this checkout, and rollback"
