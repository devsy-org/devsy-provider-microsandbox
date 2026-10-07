package config

import (
	"testing"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/stretchr/testify/suite"
)

type configSuite struct{ suite.Suite }

func TestConfig(t *testing.T) { suite.Run(t, new(configSuite)) }

func (s *configSuite) TestDefaults() {
	cfg, err := Load(func(string) string { return "" })
	s.Require().NoError(err)
	s.Equal(msb.StatStrict, cfg.WorkspacePolicy.StatVirtualization)
	s.Equal(msb.HostMirror, cfg.WorkspacePolicy.HostPermissions)
	s.Zero(cfg.Defaults.Memory)
}

func (s *configSuite) TestProviderOptions() {
	env := map[string]string{
		"MICROSANDBOX_MEMORY":                     "512",
		"MICROSANDBOX_MAX_MEMORY":                 "1024",
		"MICROSANDBOX_CPUS":                       "2",
		"MICROSANDBOX_MAX_CPUS":                   "4",
		"MICROSANDBOX_STORAGE":                    "16",
		"MICROSANDBOX_EPHEMERAL":                  "true",
		"MICROSANDBOX_BLOCK_EGRESS":               "true",
		"INACTIVITY_TIMEOUT":                      "10m",
		statPolicyOption:                          "off",
		"MICROSANDBOX_WORKSPACE_HOST_PERMISSIONS": "private",
	}
	cfg, err := Load(func(k string) string { return env[k] })
	s.Require().NoError(err)
	s.Equal(uint32(512), cfg.Defaults.Memory)
	s.Equal(uint32(1024), cfg.Defaults.MaxMemory)
	s.Equal(uint8(2), cfg.Defaults.CPUs)
	s.Equal(uint8(4), cfg.Defaults.MaxCPUs)
	s.Equal(uint32(16), cfg.Defaults.RootDiskGB)
	s.True(cfg.Defaults.Ephemeral)
	s.True(cfg.Defaults.BlockEgress)
	s.Equal(10*time.Minute, cfg.Defaults.IdleTimeout)
	s.Equal(msb.StatOff, cfg.WorkspacePolicy.StatVirtualization)
}

func (s *configSuite) TestInvalidOptions() {
	for _, item := range []struct{ key, value string }{
		{"MICROSANDBOX_MEMORY", "-1"},
		{"MICROSANDBOX_CPUS", "256"},
		{"MICROSANDBOX_EPHEMERAL", "sometimes"},
		{"INACTIVITY_TIMEOUT", "-1m"},
		{statPolicyOption, "invalid"},
		{"MICROSANDBOX_WORKSPACE_HOST_PERMISSIONS", "invalid"},
		{statPolicyOption, "off"},
	} {
		s.Run(item.key+item.value, func() {
			_, err := Load(func(k string) string {
				if k == item.key {
					return item.value
				}
				return ""
			})
			s.Error(err)
		})
	}
}
