// Package msb provides the runtime-specific MicroSandbox CLI client.
package msb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client invokes the MicroSandbox CLI. A zero value discovers msb on PATH or
// in its standard per-user installation directories.
type Client struct {
	Binary string
}

// EnsureInstalled checks discovery without imposing provisioning version policy.
func (c Client) EnsureInstalled(_ context.Context) error {
	bin := c.binary()
	if filepath.IsAbs(bin) {
		if _, err := os.Stat(bin); err == nil {
			return nil
		}
	} else if _, err := exec.LookPath(bin); err == nil {
		return nil
	}
	return errors.New(
		"microsandbox runtime (msb) not found; install it from https://install.microsandbox.dev",
	)
}

// Version returns the unparsed msb version output.
func (c Client) Version(ctx context.Context) (string, error) {
	// #nosec G204 -- args are a resolved binary path and fixed version command
	out, err := exec.CommandContext(ctx, c.binary(), "--version").CombinedOutput()
	if err != nil {
		return "", fmt.Errorf(
			"get microsandbox version: %s: %w",
			strings.TrimSpace(string(out)), err,
		)
	}
	return strings.TrimSpace(string(out)), nil
}

// Create ensures named volumes before starting a detached VM.
func (c Client) Create(ctx context.Context, sandbox string, spec Spec) error {
	if err := c.ensureVolumes(ctx, spec.Mounts); err != nil {
		return err
	}
	return c.run(ctx, runArgs(sandbox, spec)...)
}

// Find returns nil only when msb reports that the VM does not exist.
func (c Client) Find(ctx context.Context, sandbox string) (*Info, error) {
	// #nosec G204 -- args are a resolved binary path and a derived sandbox name
	out, err := exec.CommandContext(ctx, c.binary(), "inspect", sandbox, "--format", "json").
		Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("inspect microsandbox VM %q: %w", sandbox, ctx.Err())
		}
		// A genuine "not found" means the sandbox is absent (nil, nil). Any other
		// failure (permission, crash, bad invocation) is a real error to surface,
		// so callers do not mistake it for an absent sandbox.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && strings.Contains(string(exitErr.Stderr), "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect microsandbox VM %q: %w", sandbox, err)
	}
	type activeConfig struct {
		Labels map[string]string `json:"labels"`
	}
	var raw struct {
		Name         string       `json:"name"`
		Status       string       `json:"status"`
		CreatedAt    string       `json:"created_at"`
		ActiveConfig activeConfig `json:"active_config"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse microsandbox inspect output: %w", err)
	}
	created, _ := time.Parse(time.RFC3339Nano, raw.CreatedAt)
	return &Info{
		Name:      raw.Name,
		Running:   strings.EqualFold(raw.Status, "running"),
		CreatedAt: created,
		Labels:    raw.ActiveConfig.Labels,
	}, nil
}

// Start resumes an existing VM.
func (c Client) Start(ctx context.Context, sandbox string) error {
	return c.run(ctx, "start", sandbox)
}

// Stop halts a VM without removing it.
func (c Client) Stop(ctx context.Context, sandbox string) error {
	return c.run(ctx, "stop", sandbox)
}

// Remove deletes a VM; callers must enforce stop-before-remove ordering.
func (c Client) Remove(ctx context.Context, sandbox string) error {
	return c.run(ctx, "remove", sandbox)
}

// Exec uses --stream (byte-faithful stdio, no PTY) because the agent binary
// injection and the SSH-over-stdio tunnel require it; a PTY stalls the tunnel.
func (c Client) Exec(ctx context.Context, sandbox string, req ExecRequest) error {
	args := []string{"exec", "--stream"}
	if req.User != "" {
		args = append(args, "--user", req.User)
	}
	args = append(args, sandbox, "--")
	if len(req.Argv) > 0 {
		args = append(args, req.Argv...)
	} else {
		args = append(args, "/bin/sh", "-c", req.Command)
	}
	// #nosec G204 -- args are a resolved binary path plus the caller's command
	cmd := exec.CommandContext(ctx, c.binary(), args...)
	cmd.Stdin = req.Stdin
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
	return cmd.Run()
}

// Logs sends both backend output streams to the supplied writer.
func (c Client) Logs(ctx context.Context, sandbox string, w io.Writer) error {
	// #nosec G204 -- args are a resolved binary path and a derived sandbox name
	cmd := exec.CommandContext(ctx, c.binary(), "logs", sandbox)
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func (c Client) ensureVolumes(ctx context.Context, mounts []Mount) error {
	for _, vol := range namedVolumes(mounts) {
		// #nosec G204 -- args are a resolved binary path and a named-volume name
		out, err := exec.CommandContext(ctx, c.binary(), "volume", "create", vol).CombinedOutput()
		if err != nil && !strings.Contains(strings.ToLower(string(out)), "already exists") {
			return fmt.Errorf("create microsandbox volume %q: %s: %w", vol, out, err)
		}
	}
	return nil
}

func (c Client) run(ctx context.Context, args ...string) error {
	// #nosec G204 -- args are a resolved binary path and controlled config values
	out, err := exec.CommandContext(ctx, c.binary(), args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"msb %s: %s: %w",
			redactArgs(args),
			strings.TrimSpace(string(out)),
			err,
		)
	}
	return nil
}

// redactArgs joins args for an error message while masking --env values, which
// may carry secrets from the devcontainer configuration.
func redactArgs(args []string) string {
	out := make([]string, len(args))
	copy(out, args)
	for i := 0; i+1 < len(out); i++ {
		if out[i] != envFlag {
			continue
		}
		if k, _, ok := strings.Cut(out[i+1], "="); ok {
			out[i+1] = k + "=***"
		}
	}
	return strings.Join(out, " ")
}

func (c Client) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	if p, err := exec.LookPath("msb"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, c := range []string{
			filepath.Join(home, ".local", "bin", "msb"),
			filepath.Join(home, ".microsandbox", "bin", "msb"),
		} {
			if _, statErr := os.Stat(c); statErr == nil {
				return c
			}
		}
	}
	return "msb"
}
