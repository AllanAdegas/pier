// Package application is the core bounded context: applications built from
// source (or pulled as images) and the deployments that put them to run.
package application

import (
	"net/url"
	"strings"
	"time"

	"pier/internal/diagnose"
)

// BuildPack is the strategy that turns the source into a container image.
type BuildPack string

const (
	PackDockerfile BuildPack = "dockerfile"
	PackNixpacks   BuildPack = "nixpacks"
	PackStatic     BuildPack = "static"
	PackRailpack   BuildPack = "railpack"
	PackImage      BuildPack = "image"
)

// StorageKind distinguishes named volumes from host directories.
type StorageKind string

const (
	StorageVolume StorageKind = "volume"
	StorageBind   StorageKind = "bind"
)

// Source says where the code (or the ready-made image) comes from.
type Source struct {
	Repository   string   `json:"repository"`
	Branch       string   `json:"branch"`
	Commit       string   `json:"commit"`
	PrivateKeyID string   `json:"private_key_id"`
	Image        string   `json:"image"`
	WatchPaths   []string `json:"watch_paths"`
	// Tag, when set, is deployed instead of the branch.
	Tag string `json:"tag"`
	// TagPattern (e.g. "v*") makes push webhooks deploy matching tags and
	// ignore branch pushes.
	TagPattern string `json:"tag_pattern"`
}

// Build configures how the image is produced.
type Build struct {
	Target string `json:"target"`
	Pack             BuildPack `json:"pack"`
	BaseDirectory    string    `json:"base_directory"`
	DockerfilePath   string    `json:"dockerfile_path"`
	PublishDirectory string    `json:"publish_directory"`
	SPA              bool      `json:"spa"`
	InstallCommand   string    `json:"install_command"`
	BuildCommand     string    `json:"build_command"`
	StartCommand     string    `json:"start_command"`
}

// Network configures how the application is reached.
type Network struct {
	Domains      []string `json:"domains"`
	Port         int      `json:"port"`
	PortMappings []string `json:"port_mappings"`
}

// HealthCheck decides when a new container is ready to receive traffic.
type HealthCheck struct {
	Enabled            bool   `json:"enabled"`
	Path               string `json:"path"`
	Command            string `json:"command"`
	IntervalSeconds    int    `json:"interval_seconds"`
	TimeoutSeconds     int    `json:"timeout_seconds"`
	Retries            int    `json:"retries"`
	StartPeriodSeconds int    `json:"start_period_seconds"`
}

// Limits caps the resources of the container. Empty means unlimited.
type Limits struct {
	CPUs   string `json:"cpus"`
	Memory string `json:"memory"`
}

// Hooks are commands executed inside the container around a deployment.
type Hooks struct {
	PreDeploy  string `json:"pre_deploy"`
	PostDeploy string `json:"post_deploy"`
}

// Release configures a command in a disposable container from the new image.
type Release struct {
	Command string `json:"command"`
}

// Process is one command of the application (Procfile style). Every process
// runs from the same image, with the same variables and release; only "web"
// receives domains, published ports and the health check.
type Process struct {
	Name     string `json:"name"`
	Command  string `json:"command"`
	Replicas int    `json:"replicas"`
	Limits   Limits `json:"limits"`
}

// WebProcess is the process that serves HTTP traffic.
const WebProcess = "web"

// Storage is a persistent mount.
type Storage struct {
	Kind   StorageKind `json:"kind"`
	Source string      `json:"source"`
	Target string      `json:"target"`
}

// Config is the full, user-editable definition of an application.
type Config struct {
	Runtime Runtime `json:"runtime"`
	Source     Source      `json:"source"`
	Build      Build       `json:"build"`
	Network    Network     `json:"network"`
	Health     HealthCheck `json:"health"`
	Limits     Limits      `json:"limits"`
	Hooks      Hooks       `json:"hooks"`
	Release    Release     `json:"release"`
	Storages   []Storage   `json:"storages"`
	AutoDeploy bool        `json:"auto_deploy"`
	// Processes empty means a single "web" process, as before processes existed.
	Processes []Process `json:"processes"`
	// DockerOptions are validated extra container settings (docker_options.go).
	DockerOptions DockerOptions `json:"docker_options"`
}

// Application is a deployable unit running as one container.
type Application struct {
	ID            string    `json:"id"`
	EnvironmentID string    `json:"environment_id"`
	ServerID      string    `json:"server_id"`
	Name          string    `json:"name"`
	Description   string    `json:"description"`
	Config        Config    `json:"config"`
	Status        string    `json:"status"`
	CurrentImage  string    `json:"current_image"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

// WithoutSecrets is the application as a caller without access to secrets
// sees it: an https repository URL may carry a token (https://user:token@host).
func (a Application) WithoutSecrets() Application {
	a.Config.Source.Repository = withoutCredentials(a.Config.Source.Repository)
	return a
}

// withoutCredentials also handles malformed historical URLs. A fragment or an
// invalid escape in userinfo must never turn a parse failure into disclosure.
func withoutCredentials(repository string) string {
	if parsed, err := url.Parse(repository); err == nil && parsed.User != nil {
		if parsed.Scheme == "ssh" {
			if _, hasPassword := parsed.User.Password(); !hasPassword {
				return repository
			}
		}
		parsed.User = nil
		return parsed.Scheme + "://***@" + strings.TrimPrefix(parsed.String(), parsed.Scheme+"://")
	}
	start := strings.Index(repository, "://")
	if start < 0 {
		return repository
	}
	start += 3
	// '#' and '?' can be malformed userinfo; use the last @ before the path.
	end := strings.Index(repository[start:], "/")
	if end < 0 {
		end = len(repository) - start
	}
	if at := strings.LastIndex(repository[start:start+end], "@"); at >= 0 {
		return repository[:start] + "***@" + repository[start+at+1:]
	}
	return repository
}

// DeploymentStatus is the lifecycle state of a deployment.
type DeploymentStatus string

const (
	DeploymentQueued     DeploymentStatus = "queued"
	DeploymentInProgress DeploymentStatus = "in_progress"
	DeploymentFinished   DeploymentStatus = "finished"
	DeploymentFailed     DeploymentStatus = "failed"
	DeploymentCancelled  DeploymentStatus = "cancelled"
)

// Terminal reports whether the deployment will no longer change.
func (s DeploymentStatus) Terminal() bool {
	return s == DeploymentFinished || s == DeploymentFailed || s == DeploymentCancelled
}

// Trigger records what started a deployment.
type Trigger string

const (
	TriggerManual   Trigger = "manual"
	TriggerWebhook  Trigger = "webhook"
	TriggerAPI      Trigger = "api"
	TriggerRollback Trigger = "rollback"
	// TriggerArchive builds a source archive uploaded by `pierctl up`.
	TriggerArchive Trigger = "archive"
	// TriggerPromotion releases the current image of another application.
	TriggerPromotion Trigger = "promotion"
)

// Deployment is one attempt to release a version of an application.
type Deployment struct {
	ID              string           `json:"id"`
	ApplicationID   string           `json:"application_id"`
	ApplicationName string           `json:"application_name,omitempty"`
	Status          DeploymentStatus `json:"status"`
	Trigger         Trigger          `json:"trigger"`
	CommitSHA       string           `json:"commit_sha"`
	CommitMessage   string           `json:"commit_message"`
	Image           string           `json:"image"`
	RollbackImage   string           `json:"rollback_image"`
	// Source describes where the code came from when it is not the
	// configured branch (an uploaded archive, a tag, a promotion).
	Source string `json:"source"`
	// Ref is the Git tag deployed instead of the configured branch.
	Ref string `json:"ref"`
	// PromotedFrom is the application whose image was promoted.
	PromotedFrom  string          `json:"promoted_from"`
	Error         string          `json:"error"`
	Hints         []diagnose.Hint `json:"hints"`
	SuggestedPort int             `json:"suggested_port,omitempty"`
	RetiredLogs   []RetiredLog    `json:"retired_logs"`
	CreatedAt     time.Time       `json:"created_at"`
	StartedAt     *time.Time      `json:"started_at"`
	FinishedAt    *time.Time      `json:"finished_at"`
}

// RetiredLog preserves the bounded, redacted tail of a replaced container.
type RetiredLog struct {
	Container string `json:"container"`
	Output    string `json:"output"`
}

// LogLevel classifies a deployment log line.
type LogLevel string

const (
	LogInfo    LogLevel = "info"
	LogCommand LogLevel = "command"
	LogOutput  LogLevel = "output"
	LogError   LogLevel = "error"
	LogSuccess LogLevel = "success"
)

// LogEntry is one line of a deployment log.
type LogEntry struct {
	ID        int64     `json:"id"`
	Level     LogLevel  `json:"level"`
	Line      string    `json:"line"`
	CreatedAt time.Time `json:"created_at"`
}

// DefaultConfig returns sensible defaults for a new application of the given pack.
func DefaultConfig(pack BuildPack) Config {
	config := Config{
		Source:  Source{Branch: "main", WatchPaths: []string{}},
		Build:   Build{Pack: pack, BaseDirectory: "/", DockerfilePath: "Dockerfile", PublishDirectory: "/"},
		Network: Network{Domains: []string{}, Port: 3000, PortMappings: []string{}},
		Health: HealthCheck{
			Path: "/", IntervalSeconds: 5, TimeoutSeconds: 5, Retries: 10, StartPeriodSeconds: 5,
		},
		Storages: []Storage{},
	}
	if pack == PackStatic {
		config.Network.Port = staticPort
	}
	return config
}
