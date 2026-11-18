// Package sealedfields is the single authoritative list of database
// locations that hold secretbox ciphertexts. UpgradeLegacy rewrites only
// these locations, so text stored anywhere else (descriptions, logs, audit
// entries) is never touched even when it contains a copied ciphertext.
//
// Every production file that calls Box.Seal must appear here as the owner of
// the fields it writes; a guard test scans the source tree and fails when a
// new sealing store is not registered.
package sealedfields

import (
	"slices"

	"pier/internal/platform/secretbox"
)

// entry ties a field to the source file that seals the values stored in it.
type entry struct {
	owner string
	field secretbox.Field
}

func whole(owner, table, column string) entry {
	return entry{owner: owner, field: secretbox.Field{Table: table, Column: column}}
}

func setting(owner, key string, jsonPaths ...string) entry {
	return entry{owner: owner, field: secretbox.Field{
		Table: "settings", Column: "value",
		Row:       secretbox.RowMatch{Column: "key", Value: key},
		JSONPaths: jsonPaths,
	}}
}

var registry = []entry{
	whole("internal/application/store.go", "applications", "webhook_secret"),
	whole("internal/cluster/service.go", "clusters", "kubeconfig"),
	whole("internal/database/store.go", "databases", "credentials"),
	whole("internal/envvar/envvar.go", "env_vars", "value"),
	whole("internal/envvar/shared.go", "shared_variables", "value"),
	whole("internal/gitprovider/connections_service.go", "git_connections", "encrypted_token"),
	whole("internal/gitprovider/feedback.go", "git_provider_feedback", "encrypted_token"),
	whole("internal/gitprovider/service.go", "git_provider_config", "encrypted"),
	whole("internal/identity/oidc.go", "access_oidc_config", "sealed"),
	whole("internal/identity/oidc.go", "access_oidc_states", "sealed"),
	whole("internal/identity/twofactor.go", "user_totp", "secret"),
	setting("internal/instance/schedule.go", "instance_backup_config", "passphrase"),
	whole("internal/logdrain/service.go", "log_drains", "token"),
	whole("internal/notify/service.go", "notification_channels", "config"),
	whole("internal/provision/service.go", "cloud_credentials", "token"),
	whole("internal/registry/registry.go", "registries", "credential"),
	whole("internal/server/keys.go", "private_keys", "private_key"),
	setting("internal/settings/s3.go", "backup_s3"),
	whole("internal/stack/git_store.go", "stack_git_sources", "repository"),
	whole("internal/stack/git_store.go", "stack_git_sources", "webhook_secret"),
}

// All returns every registered field as a deep copy the caller may keep.
func All() []secretbox.Field {
	fields := make([]secretbox.Field, len(registry))
	for i, item := range registry {
		fields[i] = item.field
		fields[i].JSONPaths = slices.Clone(item.field.JSONPaths)
	}
	return fields
}
