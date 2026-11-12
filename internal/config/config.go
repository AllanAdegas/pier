// Package config loads process configuration from environment variables.
package config

import (
	"fmt"
	"math"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// BuildLimits bounds builds on each target server. CPU and memory limits apply
// to a dedicated BuildKit container; disk and cache sizes are in GiB.
type BuildLimits struct {
	CPUs      float64
	Memory    string
	MinFreeGB int
	CacheGB   int
}

// Config holds every setting the process needs at boot.
type Config struct {
	Addr              string
	DataDir           string
	SecretKey         string
	LocalHost         string
	DevMode           bool
	DeployConcurrency int
	// DeployTimeout bounds one whole deployment (clone, build, release).
	DeployTimeout time.Duration
	Build         BuildLimits
	// TrustedProxies are the peers allowed to tell, through X-Forwarded-*,
	// the address and scheme of the client they forward.
	TrustedProxies      []netip.Prefix
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
	deployTimeout, err := envDuration("PIER_DEPLOY_TIMEOUT", time.Hour)
	if err != nil {
		return Config{}, err
	}
	trustedProxies, err := parsePrefixes("PIER_TRUSTED_PROXIES", defaultTrustedProxies)
	if err != nil {
		return Config{}, err
	}
	build, err := loadBuildLimits()
	if err != nil {
		return Config{}, err
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
		DeployTimeout:       deployTimeout,
		Build:               build,
		TrustedProxies:      trustedProxies,
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

// defaultTrustedProxies keeps working the setups Pier ships: the panel behind
// its own Traefik or a proxy on the same machine. Operators who know the exact
// address of their proxy should set PIER_TRUSTED_PROXIES to it.
const defaultTrustedProxies = "127.0.0.0/8,::1/128,10.0.0.0/8,172.16.0.0/12,192.168.0.0/16,fc00::/7"

// parsePrefixes reads a comma-separated list of CIDRs or addresses. "none"
// trusts no proxy at all.
func parsePrefixes(key, fallback string) ([]netip.Prefix, error) {
	raw := strings.TrimSpace(envOr(key, fallback))
	if raw == "none" {
		return nil, nil
	}
	prefixes := []netip.Prefix{}
	for _, item := range strings.Split(raw, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if !strings.Contains(item, "/") {
			address, err := netip.ParseAddr(item)
			if err != nil {
				return nil, fmt.Errorf("%s: %q is not an address or CIDR", key, item)
			}
			prefixes = append(prefixes, netip.PrefixFrom(address, address.BitLen()))
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			return nil, fmt.Errorf("%s: %q is not an address or CIDR", key, item)
		}
		prefixes = append(prefixes, prefix.Masked())
	}
	return prefixes, nil
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration such as 30m or 2h, got %q", key, raw)
	}
	return value, nil
}

func loadBuildLimits() (BuildLimits, error) {
	cpus, err := strconv.ParseFloat(envOr("PIER_BUILD_CPUS", "1"), 64)
	if err != nil || math.IsNaN(cpus) || math.IsInf(cpus, 0) || cpus < 0.01 || cpus > 1024 {
		return BuildLimits{}, fmt.Errorf("PIER_BUILD_CPUS must be a number between 0.01 and 1024")
	}
	memory := envOr("PIER_BUILD_MEMORY", "2g")
	// Docker accepts a positive integer with an optional binary unit suffix.
	if !buildMemoryPattern.MatchString(memory) {
		return BuildLimits{}, fmt.Errorf("PIER_BUILD_MEMORY must be a positive size such as 512m or 2g")
	}
	minFree, err := envInt("PIER_BUILD_MIN_FREE_GB", 2)
	if err != nil {
		return BuildLimits{}, err
	}
	cache, err := envInt("PIER_BUILD_CACHE_GB", 10)
	if err != nil {
		return BuildLimits{}, err
	}
	if minFree < 0 || cache < 1 {
		return BuildLimits{}, fmt.Errorf("PIER_BUILD_MIN_FREE_GB must be >= 0 and PIER_BUILD_CACHE_GB must be >= 1")
	}
	return BuildLimits{CPUs: cpus, Memory: memory, MinFreeGB: minFree, CacheGB: cache}, nil
}

var buildMemoryPattern = regexp.MustCompile(`^[1-9][0-9]*[bkmgBKMG]?$`)
