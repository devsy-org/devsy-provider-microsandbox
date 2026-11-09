package config

import (
	"fmt"
	"os"
	"strings"
)

// LoadEnvironment validates options and removes blank native path overrides.
// Call once at startup before SDK initialization or child processes: the native
// library reads the process environment, independently of the Go configuration.
func LoadEnvironment() (Config, error) {
	cfg, err := Load(os.Getenv)
	if err != nil {
		return Config{}, err
	}
	for _, name := range []string{
		"MSB_PATH", "MSB_LIBKRUNFW_PATH", "MSB_HOME", "MSB_CONFIG_PATH", "MSB_AGENTD_PATH",
	} {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			if err := os.Unsetenv(name); err != nil {
				return Config{}, fmt.Errorf("unset %s: %w", name, err)
			}
		}
	}
	return cfg, nil
}
