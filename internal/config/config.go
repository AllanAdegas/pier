// Package config loads process configuration from environment variables.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// Config holds every setting the process needs at boot.
type Config struct {
	Addr                string
	DataDir             string
	SecretKey           string
	LocalHost           string
	DevMode             bool
	DeployConcurrency   int
	StatusPollInterval  time.Duration
	SessionTTL          time.Duration
	ShutdownGracePeriod time.Duration
}

// Load reads configuration from the environment, applying defaults.
func Load() (Config, error) {
	dataDir, err := filepath.Abs(envOr("PIER_DATA_DIR", "./data"))
	if err != nil {
		return Config{}, fmt.Errorf("resolve data dir: %w", err)
	}
	concurrency, err := envInt("PIER_DEPLOY_CONCURRENCY", 2)
	if err != nil {
		return Config{}, err
	}
	if concurrency < 1 {
		return Config{}, fmt.Errorf("PIER_DEPLOY_CONCURRENCY must be >= 1, got %d", concurrency)
	}
	return Config{
		// Loopback by default: a panel that runs code on servers is not
		// published to the network by accident. The container image sets
		// PIER_ADDR=:8700 and compose binds it to 127.0.0.1 on the host.
		Addr:                envOr("PIER_ADDR", "127.0.0.1:8700"),
		DataDir:             dataDir,
		SecretKey:           os.Getenv("PIER_SECRET_KEY"),
		LocalHost:           envOr("PIER_LOCAL_HOST", "127.0.0.1"),
		DevMode:             os.Getenv("PIER_DEV") == "1",
		DeployConcurrency:   concurrency,
		StatusPollInterval:  20 * time.Second,
		SessionTTL:          14 * 24 * time.Hour,
		ShutdownGracePeriod: 10 * time.Second,
	}, nil
}

// DatabasePath is where the SQLite file lives.
func (c Config) DatabasePath() string { return filepath.Join(c.DataDir, "pier.db") }

// SecretKeyPath is where the auto-generated encryption key is persisted.
func (c Config) SecretKeyPath() string { return filepath.Join(c.DataDir, "secret.key") }

// LocalRuntimeDir is the default data directory of the built-in local server.
func (c Config) LocalRuntimeDir() string { return filepath.Join(c.DataDir, "runtime") }

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", key, err)
	}
	return value, nil
}
