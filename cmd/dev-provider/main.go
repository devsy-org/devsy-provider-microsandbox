// Command dev-provider creates a local manifest for the current host.
package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/distribution"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	cfg := distribution.LocalConfig{
		Binary:    filepath.Join(root, "dist", "devsy-runtime-microsandbox"),
		Directory: filepath.Join(root, "dist", "local"),
		MSB:       os.Getenv("MSB_PATH"),
		Firmware:  os.Getenv("MSB_LIBKRUNFW_PATH"),
	}
	if err := checkRuntime(cfg, root); err != nil {
		return err
	}
	path, err := distribution.GenerateLocal(cfg)
	if err != nil {
		return err
	}
	printCommands(path)
	return nil
}

func checkRuntime(cfg distribution.LocalConfig, root string) error {
	if !filepath.IsAbs(cfg.MSB) || !filepath.IsAbs(cfg.Firmware) {
		return errors.New("set absolute MSB_PATH and MSB_LIBKRUNFW_PATH together")
	}
	// The runtime check must never discover the contributor's normal runtime home.
	// #nosec G204 -- the executable is built by the local task in this checkout.
	cmd := exec.Command(cfg.Binary, "runtime-check")
	env := []string{}
	for _, item := range os.Environ() {
		if !strings.HasPrefix(item, "MSB_HOME=") && !strings.HasPrefix(item, "MSB_CONFIG_PATH=") {
			env = append(env, item)
		}
	}
	env = append(
		env,
		"MSB_HOME="+filepath.Join(root, "dist", "m"),
		"MSB_CONFIG_PATH="+filepath.Join(root, "dist", "m", "config.json"),
	)
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runtime check: %w: %s", err, out)
	}
	return nil
}

func printCommands(path string) {
	quoted := "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
	_, _ = fmt.Fprintf(
		os.Stdout,
		"Install:\n  devsy provider add --use=false --name microsandbox-local %s\n"+
			"  devsy provider init microsandbox-local\nRefresh:\n"+
			"  devsy provider set-source --use=false microsandbox-local %s\n"+
			"  devsy provider init microsandbox-local\n",
		quoted,
		quoted,
	)
}
