// Package database is the bounded context for managed databases: one-click
// engines with generated credentials, persistent volumes and logical backups.
package database

import (
	"regexp"
	"strings"
	"time"

	"github.com/robfig/cron/v3"

	"pier/internal/compose"
	"pier/internal/docker"
	"pier/internal/platform/apperr"
)

const (
	defaultUsername  = "pier"
	defaultDatabase  = "app"
	defaultRetention = 7
	maxRetention     = 365
)

var (
	identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,30}$`)
	cpusPattern       = regexp.MustCompile(`^\d+(\.\d{1,3})?$`)
	memoryPattern     = regexp.MustCompile(`^\d+[bkmgBKMG]$`)
)

// BackupPolicy schedules automatic dumps.
type BackupPolicy struct {
	S3              bool   `json:"s3"`
	RestoreTest     bool   `json:"restore_test"`
	RestoreTestCron string `json:"restore_test_cron"`
	Enabled         bool   `json:"enabled"`
	Cron            string `json:"cron"`
	Retention       int    `json:"retention"`
}

// Config is the user-editable definition of a database.
type Config struct {
	Image      string       `json:"image"`
	PublicPort int          `json:"public_port"`
	CPUs       string       `json:"cpus"`
	Memory     string       `json:"memory"`
	Backup     BackupPolicy `json:"backup"`
}

// Database is a managed database instance running as one container.
type Database struct {
	ID            string    `json:"id"`
	EnvironmentID string    `json:"environment_id"`
	ServerID      string    `json:"server_id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Engine        Engine    `json:"engine"`
	Config        Config    `json:"config"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// Connection is how applications and people reach a database.
type Connection struct {
	Host        string      `json:"host"`
	Port        int         `json:"port"`
	Credentials Credentials `json:"credentials"`
	InternalURL string      `json:"internal_url"`
	PublicURL   string      `json:"public_url"`
}

// BackupStatus is the outcome of a backup execution.
type BackupStatus string

const (
	BackupRunning   BackupStatus = "running"
	BackupSucceeded BackupStatus = "succeeded"
	BackupFailed    BackupStatus = "failed"
)

// BackupExecution is one dump taken from a database.
type BackupExecution struct {
	ID         string       `json:"id"`
	DatabaseID string       `json:"database_id"`
	Trigger    string       `json:"trigger"`
	Status     BackupStatus `json:"status"`
	FilePath   string       `json:"file_path"`
	SizeBytes  int64        `json:"size_bytes"`
	Message    string       `json:"message"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt *time.Time   `json:"finished_at"`
}

// CreateInput carries what is needed to provision a database.
type CreateInput struct {
	EnvironmentID string `json:"environment_id"`
	ServerID      string `json:"server_id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	Engine        Engine `json:"engine"`
	Image         string `json:"image"`
	Username      string `json:"username"`
	Database      string `json:"database"`
}

// UpdateInput carries the editable fields of a database.
type UpdateInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Config      Config `json:"config"`
}

func (c *Config) validate(spec engineSpec, fields apperr.Fields) {
	c.Image = strings.TrimSpace(c.Image)
	if c.Image == "" {
		c.Image = spec.DefaultImage
	}
	if !docker.ValidImage(c.Image) {
		fields.Add("config.image", "Imagem inválida.")
	}
	if c.PublicPort != 0 && (c.PublicPort < 1024 || c.PublicPort > 65535) {
		fields.Add("config.public_port", "Use uma porta entre 1024 e 65535, ou deixe vazio para não expor.")
	}
	if c.CPUs != "" && !cpusPattern.MatchString(c.CPUs) {
		fields.Add("config.cpus", "Use um número de CPUs, ex.: 0.5 ou 2.")
	}
	if c.Memory != "" && !memoryPattern.MatchString(c.Memory) {
		fields.Add("config.memory", "Use um valor com unidade, ex.: 512m ou 2g.")
	}
	c.validateBackup(spec, fields)
}

func (c *Config) validateBackup(spec engineSpec, fields apperr.Fields) {
	c.Backup.Cron = strings.TrimSpace(c.Backup.Cron)
	if c.Backup.Retention <= 0 {
		c.Backup.Retention = defaultRetention
	}
	if c.Backup.Retention > maxRetention {
		fields.Add("config.backup.retention", "Mantenha no máximo %d backups.", maxRetention)
	}
	if c.Backup.RestoreTest {
		if c.Backup.RestoreTestCron == "" {
			c.Backup.RestoreTestCron = "0 4 * * 0"
		}
		if !supportsRestoreTest(spec.Engine) {
			fields.Add("config.backup.restore_test", "Esta engine não suporta teste de restauração.")
		}
		if _, err := cron.ParseStandard(c.Backup.RestoreTestCron); err != nil {
			fields.Add("config.backup.restore_test_cron", "Expressão cron inválida (use 5 campos, ex.: 0 3 * * *).")
		}
	}
	if !c.Backup.Enabled {
		return
	}
	if spec.Dump == nil {
		fields.Add("config.backup.enabled", "%s ainda não tem backup automático.", spec.Label)
		return
	}
	if _, err := cron.ParseStandard(c.Backup.Cron); err != nil {
		fields.Add("config.backup.cron", "Expressão cron inválida (use 5 campos, ex.: 0 3 * * *).")
	}
}

// composeFor renders the compose file that runs the database.
func composeFor(database Database, spec engineSpec, credentials Credentials) *compose.File {
	volume := database.ID + "_data"
	service := &compose.Service{
		Image: database.Config.Image,
		// The service key, the network alias and the volume keep the ID:
		// connection strings and data do not depend on the name, which
		// can change.
		ContainerName: docker.ReadableName(database.Name, database.ID),
		Restart:       "unless-stopped",
		Labels:        docker.Labels(docker.TypeDatabase, database.ID),
		Volumes:       []string{volume + ":" + spec.DataPath},
		Networks:      map[string]*compose.NetworkAttachment{docker.Network: {Aliases: []string{database.ID}}},
		CPUs:          database.Config.CPUs,
		MemLimit:      database.Config.Memory,
		Healthcheck: &compose.Healthcheck{
			Test: []string{"CMD-SHELL", spec.Healthcheck}, Interval: "5s", Timeout: "5s", Retries: 20, StartPeriod: "10s",
		},
	}
	if spec.Environment != nil {
		service.Environment = spec.Environment(credentials)
	}
	if spec.Command != nil {
		service.Command = spec.Command(credentials)
	}
	if database.Config.PublicPort != 0 {
		service.Ports = []string{portMapping(database.Config.PublicPort, spec.Port)}
	}
	return &compose.File{
		Services: map[string]*compose.Service{database.ID: service},
		Networks: map[string]*compose.Network{docker.Network: {Name: docker.Network, External: true}},
		Volumes:  map[string]*compose.Volume{volume: {Name: volume}},
	}
}
