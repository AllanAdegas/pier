// Package application is the core bounded context: applications built from
// source (or pulled as images) and the deployments that put them to run.
package application

import (
	"time"
)

// BuildPack is the strategy that turns the source into a container image.
type BuildPack string

const (
	PackDockerfile BuildPack = "dockerfile"
	PackNixpacks   BuildPack = "nixpacks"
	PackStatic     BuildPack = "static"
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
	Repository   string `json:"repository"`
	Branch       string `json:"branch"`
	Commit       string `json:"commit"`
	PrivateKeyID string `json:"private_key_id"`
	Image        string `json:"image"`
}

// Build configures how the image is produced.
type Build struct {
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

// Storage is a persistent mount.
type Storage struct {
	Kind   StorageKind `json:"kind"`
	Source string      `json:"source"`
	Target string      `json:"target"`
}

// Config is the full, user-editable definition of an application.
type Config struct {
	Source     Source      `json:"source"`
	Build      Build       `json:"build"`
	Network    Network     `json:"network"`
	Health     HealthCheck `json:"health"`
	Limits     Limits      `json:"limits"`
	Hooks      Hooks       `json:"hooks"`
	Storages   []Storage   `json:"storages"`
	AutoDeploy bool        `json:"auto_deploy"`
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
	Error           string           `json:"error"`
	CreatedAt       time.Time        `json:"created_at"`
	StartedAt       *time.Time       `json:"started_at"`
	FinishedAt      *time.Time       `json:"finished_at"`
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
		Source:  Source{Branch: "main"},
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
