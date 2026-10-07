// Package config loads the provider's existing MicroSandbox environment options.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
)

const statPolicyOption = "MICROSANDBOX_WORKSPACE_STAT_VIRTUALIZATION"

// Config holds runtime defaults and the primary workspace bind-mount policy.
type Config struct {
	Defaults        msb.Spec
	WorkspacePolicy msb.MountPolicy
}

// Load reads provider options without accessing process-global environment state.
func Load(getenv func(string) string) (Config, error) {
	cfg := Config{
		WorkspacePolicy: msb.MountPolicy{
			StatVirtualization: msb.StatStrict,
			HostPermissions:    msb.HostMirror,
		},
	}
	if err := loadResources(getenv, &cfg); err != nil {
		return Config{}, err
	}
	if err := loadFlags(getenv, &cfg); err != nil {
		return Config{}, err
	}
	if raw := strings.TrimSpace(getenv("INACTIVITY_TIMEOUT")); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration < 0 {
			return Config{}, fmt.Errorf("invalid INACTIVITY_TIMEOUT %q", raw)
		}
		cfg.Defaults.IdleTimeout = duration
	}
	if raw := strings.TrimSpace(getenv(statPolicyOption)); raw != "" {
		cfg.WorkspacePolicy.StatVirtualization = msb.StatVirtualization(raw)
	}
	if raw := strings.TrimSpace(getenv("MICROSANDBOX_WORKSPACE_HOST_PERMISSIONS")); raw != "" {
		cfg.WorkspacePolicy.HostPermissions = msb.HostPermissions(raw)
	}
	return cfg, cfg.Validate()
}

func loadFlags(getenv func(string) string, cfg *Config) error {
	for _, field := range []struct {
		name   string
		target *bool
	}{
		{"MICROSANDBOX_EPHEMERAL", &cfg.Defaults.Ephemeral},
		{"MICROSANDBOX_BLOCK_EGRESS", &cfg.Defaults.BlockEgress},
	} {
		raw := strings.TrimSpace(getenv(field.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", field.name, err)
		}
		*field.target = value
	}
	return nil
}

// Validate rejects policy combinations that cannot preserve workspace permissions.
func (cfg Config) Validate() error {
	switch cfg.WorkspacePolicy.StatVirtualization {
	case msb.StatStrict, msb.StatRelaxed, msb.StatOff:
	default:
		return fmt.Errorf(
			"invalid workspace stat virtualization %q",
			cfg.WorkspacePolicy.StatVirtualization,
		)
	}
	switch cfg.WorkspacePolicy.HostPermissions {
	case msb.HostPrivate, msb.HostMirror:
	default:
		return fmt.Errorf(
			"invalid workspace host permissions %q",
			cfg.WorkspacePolicy.HostPermissions,
		)
	}
	if cfg.WorkspacePolicy.StatVirtualization == msb.StatOff &&
		cfg.WorkspacePolicy.HostPermissions == msb.HostMirror {
		return errors.New("host-perms=mirror requires workspace stat virtualization")
	}
	return nil
}

func loadResources(getenv func(string) string, cfg *Config) error {
	for _, field := range []struct {
		name   string
		target *uint32
	}{
		{"MICROSANDBOX_MEMORY", &cfg.Defaults.Memory},
		{"MICROSANDBOX_MAX_MEMORY", &cfg.Defaults.MaxMemory},
		{"MICROSANDBOX_STORAGE", &cfg.Defaults.RootDiskGB},
	} {
		raw := strings.TrimSpace(getenv(field.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseUint(raw, 10, 32)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", field.name, err)
		}
		*field.target = uint32(value)
	}
	return loadCPUs(getenv, cfg)
}

func loadCPUs(getenv func(string) string, cfg *Config) error {
	for _, field := range []struct {
		name   string
		target *uint8
	}{
		{"MICROSANDBOX_CPUS", &cfg.Defaults.CPUs},
		{"MICROSANDBOX_MAX_CPUS", &cfg.Defaults.MaxCPUs},
	} {
		raw := strings.TrimSpace(getenv(field.name))
		if raw == "" {
			continue
		}
		value, err := strconv.ParseUint(raw, 10, 8)
		if err != nil {
			return fmt.Errorf("invalid %s: %w", field.name, err)
		}
		*field.target = uint8(value)
	}
	return nil
}
