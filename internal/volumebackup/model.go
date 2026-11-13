// Package volumebackup backs up the persistent data of applications and
// stacks: named volumes and bind mounts, archived as tar.gz by a throwaway
// helper container. It reuses the database backup machinery for the S3 copy,
// the pinned helper image and the server connector.
package volumebackup

import (
	"regexp"
	"time"

	"github.com/robfig/cron/v3"

	"pier/internal/platform/apperr"
)

const (
	defaultCron      = "0 3 * * *"
	defaultRetention = 7
	maxRetention     = 365

	// Resource kinds: the value of the pier.type label of the containers.
	KindApplication = "application"
	KindStack       = "stack"

	TriggerManual    = "manual"
	TriggerScheduled = "scheduled"

	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"

	MountVolume = "volume"
	MountBind   = "bind"

	// Why a mount is not part of the archive.
	ReasonOutsideDataDir = "outside_data_dir"
	ReasonSystemPath     = "system_path"
	ReasonInvalidName    = "invalid_name"
)

var volumeNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

// Resource is the application or stack a backup belongs to.
type Resource struct {
	Kind     string
	ID       string
	Name     string
	ServerID string
}

// Mount is one persistent mount of a resource: a named volume or a host
// directory. Label is the directory name inside the archive.
type Mount struct {
	Label       string `json:"label"`
	Kind        string `json:"kind"`
	Source      string `json:"source"`
	Destination string `json:"destination"`
	Included    bool   `json:"included"`
	Reason      string `json:"reason,omitempty"`
}

// Policy is the backup configuration of a resource.
type Policy struct {
	Enabled   bool   `json:"enabled"`
	Cron      string `json:"cron"`
	Retention int    `json:"retention"`
	S3        bool   `json:"s3"`
	// Quiesce stops the containers while the archive is written: consistent
	// data at the cost of downtime. Off by default.
	Quiesce bool `json:"quiesce"`
	// IncludeExternal also archives bind mounts outside the Pier data dir.
	IncludeExternal bool `json:"include_external"`
}

// Validate normalizes the policy and reports invalid fields.
func (p *Policy) Validate() error {
	fields := apperr.Fields{}
	if p.Cron == "" {
		p.Cron = defaultCron
	}
	if p.Retention <= 0 {
		p.Retention = defaultRetention
	}
	if p.Retention > maxRetention {
		fields.Add("retention", "Mantenha no máximo %d backups.", maxRetention)
	}
	if _, err := cron.ParseStandard(p.Cron); err != nil {
		fields.Add("cron", "Expressão cron inválida (use 5 campos, ex.: 0 3 * * *).")
	}
	return fields.Err()
}

// Backup is one archive taken from a resource.
type Backup struct {
	ID           string     `json:"id"`
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	Trigger      string     `json:"trigger"`
	Status       string     `json:"status"`
	FilePath     string     `json:"file_path"`
	SizeBytes    int64      `json:"size_bytes"`
	Mounts       []Mount    `json:"mounts"`
	Remote       bool       `json:"remote"`
	Message      string     `json:"message"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	remoteKey    string
	serverID     string
	stopped      []string
}

// Operation is a restore of an archive into the resource.
type Operation struct {
	ID           string     `json:"id"`
	ResourceType string     `json:"resource_type"`
	ResourceID   string     `json:"resource_id"`
	BackupID     string     `json:"backup_id"`
	Kind         string     `json:"kind"`
	Status       string     `json:"status"`
	Message      string     `json:"message"`
	Logs         string     `json:"logs"`
	StartedAt    time.Time  `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
}

// RestoreInput carries the typed confirmation: the resource name.
type RestoreInput struct {
	Confirmation string `json:"confirmation"`
}

// Schedule pairs a resource with the cron of its policy.
type Schedule struct {
	Kind string
	ID   string
	Cron string
}

// ValidKind reports whether kind names a resource type with volume backups.
func ValidKind(kind string) bool { return kind == KindApplication || kind == KindStack }
