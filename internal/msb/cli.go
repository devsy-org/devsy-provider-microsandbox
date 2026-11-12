// Package msb provides the runtime-specific MicroSandbox CLI client.
package msb

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Client uses the CLI for lifecycle and logs and the SDK for guest execution.
// A zero value discovers msb on PATH or in its standard per-user installation directories.
type Client struct {
	Binary string
	// DockerBinary selects a Docker-compatible CLI for local image snapshots.
	DockerBinary string
}

// EnsureInstalled checks discovery without imposing provisioning version policy.
func (c Client) EnsureInstalled(_ context.Context) error {
	if _, err := exec.LookPath(c.binary()); err == nil {
		return nil
	}
	return errors.New(
		"microsandbox runtime (msb) not found; install it from https://install.microsandbox.dev",
	)
}

// Version returns the unparsed msb version output.
func (c Client) Version(ctx context.Context) (string, error) {
	// #nosec G204 -- args are a resolved binary path and fixed version command
	cmd := exec.CommandContext(ctx, c.binary(), "--version")
	cmd.WaitDelay = time.Second
	out, err := cmd.CombinedOutput()
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
	secrets := bindSourceValues(spec.Mounts)
	if err := c.ensureVolumes(ctx, spec.Mounts); err != nil {
		return err
	}
	mounts, err := resolveHostMounts(spec.Mounts)
	if err != nil {
		return errors.New(redactSecrets(err.Error(), secrets))
	}
	spec.Mounts = mounts
	secrets = append(secrets, bindSourceValues(mounts)...)
	conf, err := writeEnvConf(spec)
	if err != nil {
		return err
	}
	defer conf.remove()
	secrets = append(secrets, conf.values()...)
	return c.runWithEnv(ctx, conf.environ, runArgs(sandbox, spec, conf.path), secrets)
}

// Find returns nil only when msb reports that the VM does not exist.
func (c Client) Find(ctx context.Context, sandbox string) (*Info, error) {
	// #nosec G204 -- args are a resolved binary path and a derived sandbox name
	cmd := exec.CommandContext(ctx, c.binary(), "inspect", sandbox, "--format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("inspect microsandbox VM %q: %w", sandbox, ctx.Err())
		}
		// A genuine "not found" means the sandbox is absent (nil, nil). Any other
		// failure (permission, crash, bad invocation) is a real error to surface,
		// so callers do not mistake it for an absent sandbox.
		var exitErr *exec.ExitError
		text := strings.TrimSpace(stderr.String())
		if errors.As(err, &exitErr) && strings.Contains(text, "not found") {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect microsandbox VM %q: %s: %w", sandbox, text, err)
	}
	info, err := parseInfo(stdout.Bytes())
	if err != nil {
		versionCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		version, versionErr := c.Version(versionCtx)
		if versionErr != nil {
			version = "unknown (version detection failed)"
		}
		return nil, fmt.Errorf(
			"%w; msb version %s; report incompatible inspect output at "+
				"https://github.com/devsy-org/devsy-provider-microsandbox/issues",
			err,
			version,
		)
	}
	return info, nil
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
	cmd.Stdout = req.Stdout
	cmd.Stderr = req.Stderr
	return runExec(cmd, req.Stdin)
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
	return c.runWithEnv(ctx, nil, args, nil)
}

// runWithEnv adds environ to the child environment and masks secrets in error
// text, since msb may echo a value it was given.
func (c Client) runWithEnv(ctx context.Context, environ, args, secrets []string) error {
	// #nosec G204 -- args are a resolved binary path and controlled config values
	cmd := exec.CommandContext(ctx, c.binary(), args...)
	if len(environ) > 0 {
		cmd.Env = append(os.Environ(), environ...)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf(
			"msb %s: %s: %w",
			args[0],
			redactSecrets(strings.TrimSpace(string(out)), secrets),
			err,
		)
	}
	return nil
}

func (c Client) binary() string {
	if c.Binary != "" {
		return c.Binary
	}
	if p, err := exec.LookPath("msb"); err == nil {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, candidate := range []string{
			filepath.Join(home, ".local", "bin", "msb"),
			filepath.Join(home, ".microsandbox", "bin", "msb"),
		} {
			if binary, err := exec.LookPath(candidate); err == nil {
				return binary
			}
		}
	}
	return "msb"
}
