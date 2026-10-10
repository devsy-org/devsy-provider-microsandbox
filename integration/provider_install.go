//go:build ignore

// Command provider_install verifies release downloads using Devsy's real resolver.
// Run explicitly from the pinned Devsy checkout, which supplies the host packages.
// The ignore tag keeps host-only imports out of this provider module.
package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/devsy-org/devsy/pkg/provider"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) != 3 {
		return fmt.Errorf("usage: provider_install <artifact-directory> <tag>")
	}
	directory, tag := os.Args[1], os.Args[2]
	manifest, err := os.Open(filepath.Join(directory, "provider.yaml"))
	if err != nil {
		return err
	}
	defer manifest.Close()
	cfg, err := provider.ParseProvider(manifest)
	if err != nil {
		return err
	}
	if cfg.Version != tag {
		return fmt.Errorf("manifest version %q != %q", cfg.Version, tag)
	}
	if err := provider.ValidateExternalDriverConfig(cfg.Agent); err != nil {
		return err
	}
	option := cfg.Options["MICROSANDBOX_WORKSPACE_STAT_VIRTUALIZATION"]
	if option == nil {
		return fmt.Errorf("missing workspace stat virtualization option")
	}
	values := make([]string, 0, len(option.Enum))
	for _, choice := range option.Enum {
		values = append(values, choice.Value)
	}
	if got := strings.Join(values, ","); got != "strict,relaxed,off" {
		return fmt.Errorf("workspace stat virtualization values %q != strict,relaxed,off", got)
	}
	server := httptest.NewServer(http.FileServer(http.Dir(directory)))
	defer server.Close()
	for _, binary := range cfg.Agent.Binaries[cfg.Agent.External.Binary] {
		binary.Path = server.URL + "/" + binary.Name
	}
	target, err := os.MkdirTemp("", "microsandbox-download-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(target)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := provider.DownloadBinaries(ctx, cfg.Agent.Binaries, target); err != nil {
		return err
	}
	executable, err := provider.ResolveExternalRuntimeBinary(cfg.Agent, target)
	if err != nil {
		return err
	}
	output, err := exec.CommandContext(ctx, executable, "--version").Output()
	if err != nil {
		return err
	}
	if string(output) != "devsy-runtime-microsandbox "+tag+"\n" {
		return fmt.Errorf("unexpected executable version %q", output)
	}
	for _, binary := range cfg.Agent.Binaries[cfg.Agent.External.Binary] {
		if binary.OS == runtime.GOOS && binary.Arch == runtime.GOARCH {
			binary.Checksum = strings.Repeat("0", 64)
		}
	}
	if _, err := provider.ResolveExternalRuntimeBinary(cfg.Agent, target); err == nil {
		return fmt.Errorf("incorrect checksum accepted")
	}
	return nil
}
