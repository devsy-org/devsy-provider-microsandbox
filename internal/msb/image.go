package msb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// EnsureImage loads a locally built Docker image when available, otherwise pulls
// with msb and falls back to a registry tarball using host Docker credentials.
func (c Client) EnsureImage(ctx context.Context, imageRef string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if dockerImageExists(ctx, imageRef) {
		return c.loadFromDocker(ctx, imageRef)
	}
	// #nosec G204 -- args are a resolved binary path and a validated image ref
	out, err := exec.CommandContext(ctx, c.binary(), "pull", imageRef).CombinedOutput()
	if err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if loadErr := c.loadViaRegistry(ctx, imageRef); loadErr != nil {
		return fmt.Errorf("msb pull %s: %s: %w; registry fallback: %w", imageRef, out, err, loadErr)
	}
	return nil
}

func dockerImageExists(ctx context.Context, image string) bool {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return false
	}
	// #nosec G204 -- docker path is resolved and the image ref is validated
	return exec.CommandContext(ctx, docker, "image", "inspect", image).Run() == nil
}

func (c Client) loadFromDocker(ctx context.Context, image string) error {
	docker, err := exec.LookPath("docker")
	if err != nil {
		return fmt.Errorf("docker not found to load built image %q: %w", image, err)
	}
	// #nosec G204 -- docker/msb paths are resolved and the image ref is validated
	save := exec.CommandContext(ctx, docker, "save", image)
	// #nosec G204 -- docker/msb paths are resolved and the image ref is validated
	load := exec.CommandContext(ctx, c.binary(), "load", "-t", image)
	reader, writer, err := os.Pipe()
	if err != nil {
		return fmt.Errorf("pipe docker save: %w", err)
	}
	defer func() { _ = reader.Close(); _ = writer.Close() }()
	save.Stdout = writer
	load.Stdin = reader
	var loadErr strings.Builder
	load.Stderr = &loadErr
	if err := load.Start(); err != nil {
		return fmt.Errorf("start msb load: %w", err)
	}
	// Only the consumer keeps the read end: if it exits early, docker save must
	// see a broken pipe instead of blocking behind a reader held by this process.
	_ = reader.Close()
	saveErr := save.Run()
	_ = writer.Close()
	if saveErr != nil {
		// load is still running on the broken pipe; kill and reap it.
		_ = load.Process.Kill()
		_ = load.Wait()
		return fmt.Errorf("docker save %q: %w; msb load: %s", image, saveErr, loadErr.String())
	}
	if err := load.Wait(); err != nil {
		return fmt.Errorf("msb load %q: %s: %w", image, loadErr.String(), err)
	}
	return nil
}

func (c Client) loadViaRegistry(ctx context.Context, imageRef string) error {
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return fmt.Errorf("parse image reference %q: %w", imageRef, err)
	}
	img, err := remote.Image(
		ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}),
	)
	if err != nil {
		return fmt.Errorf("pull image %q: %w", imageRef, sanitizeRegistryError(err))
	}

	tmp, err := os.CreateTemp("", "devsy-msb-*.tar")
	if err != nil {
		return fmt.Errorf("create image tarball: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() { _ = os.Remove(tmpPath) }()

	if err := tarball.WriteToFile(tmpPath, ref, img); err != nil {
		return fmt.Errorf("write image tarball: %w", err)
	}

	// #nosec G204 -- args are a resolved binary path and an internally-created file
	load := exec.CommandContext(ctx, c.binary(), "load", "-i", tmpPath, "-t", imageRef)
	if out, err := load.CombinedOutput(); err != nil {
		return fmt.Errorf("msb load %q: %s: %w", imageRef, out, err)
	}
	return nil
}
