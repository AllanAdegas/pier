# shellcheck shell=sh
# Resources created through the public API on the OLD release, checked again
# after the upgrade. Sourced after lib.sh.
seed_project_name='Install Test Project'
seed_app_name='install-test-app'
seed_var_key='INSTALLTEST_SECRET'
seed_var_value='s3cr3t-value-that-must-survive'

seed_resources() {
  project=$(api POST /api/v1/projects "{\"name\":\"$seed_project_name\",\"description\":\"created by installtest\"}") \
    || die "não foi possível criar projeto / could not create the project"
  environment_id=$(printf '%s' "$project" | sed -n 's/.*"environments":\[{[^}]*"id":"\([^"]*\)".*/\1/p')
  [ -n "$environment_id" ] || die "projeto sem ambiente / the project has no environment: $project"
  server_id=$(api GET /api/v1/servers | json_field id) || die "não foi possível listar servidores / could not list servers"
  [ -n "$server_id" ] || die "nenhum servidor registrado / no server registered"
  application=$(api POST /api/v1/applications "{\"environment_id\":\"$environment_id\",\"server_id\":\"$server_id\",\
\"name\":\"$seed_app_name\",\"config\":{\"build\":{\"pack\":\"image\"},\"source\":{\"image\":\"nginx:1.27-alpine\"},\"network\":{\"port\":80}}}") \
    || die "falha ao criar aplicação / could not create the application: $application"
  application_id=$(printf '%s' "$application" | json_field id)
  api POST "/api/v1/applications/$application_id/envs" \
    "{\"key\":\"$seed_var_key\",\"value\":\"$seed_var_value\",\"build_time\":false}" >/dev/null \
    || die "não foi possível definir variável / could not set the variable"
  printf '%s\n' "$application_id" > "$INSTALLTEST_ROOT/application_id"
}

seeded_resources_intact() {
  application_id=$(cat "$INSTALLTEST_ROOT/application_id")
  api GET /api/v1/projects > "$INSTALLTEST_ROOT/projects.json" || return 1
  api GET "/api/v1/applications/$application_id" > "$INSTALLTEST_ROOT/application.json" || return 1
  api GET "/api/v1/applications/$application_id/envs" > "$INSTALLTEST_ROOT/envs.json" || return 1
  grep -Fq "$seed_project_name" "$INSTALLTEST_ROOT/projects.json" \
    && grep -Fq "$seed_app_name" "$INSTALLTEST_ROOT/application.json" \
    && grep -Fq "$seed_var_value" "$INSTALLTEST_ROOT/envs.json"
}
