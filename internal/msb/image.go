package msb

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

// PreparedImage keeps the final image available for validation and import.
// Close it only after all lazy image and layer reads have finished.
type PreparedImage struct {
	reference string
	image     v1.Image
	archive   string
}

// Reference is the content-derived tag used when importing and creating a VM.
func (p *PreparedImage) Reference() string { return p.reference }

// Image returns the same snapshot that EnsureImage imports.
func (p *PreparedImage) Image() v1.Image { return p.image }

// Close removes the backing local archive, if any. Repeated calls are safe.
func (p *PreparedImage) Close() error {
	if p.archive == "" {
		return nil
	}
	err := os.Remove(p.archive)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// PrepareImage resolves one final Linux image for the host architecture.
// A locally built image must be saved locally; it never falls back to a registry.
func (c Client) PrepareImage(
	ctx context.Context,
	ref string,
	builtLocally bool,
) (*PreparedImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var prepared *PreparedImage
	var err error
	if builtLocally || c.localImageAvailable(ctx, ref) {
		prepared, err = c.localImage(ctx, ref)
	} else {
		prepared, err = registryImage(ctx, ref)
	}
	if err != nil {
		return nil, err
	}
	digest, err := prepared.image.Digest()
	if err != nil {
		_ = prepared.Close()
		return nil, fmt.Errorf("identify final image %q: %w", ref, err)
	}
	prepared.reference = "devsy-msb-image:" + digest.Hex
	return prepared, nil
}

// EnsureImage imports the validated snapshot, without resolving the original tag again.
func (c Client) EnsureImage(ctx context.Context, prepared *PreparedImage) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if prepared == nil || prepared.image == nil || prepared.reference == "" {
		return errors.New("microsandbox image import requires a prepared image")
	}
	ref, err := name.ParseReference(prepared.reference)
	if err != nil {
		return fmt.Errorf("parse snapshot reference: %w", err)
	}
	return c.importSnapshot(ctx, prepared, ref)
}

func (c Client) importSnapshot(
	ctx context.Context,
	prepared *PreparedImage,
	ref name.Reference,
) error {
	// #nosec G204 -- resolved runtime executable and a content-derived image reference.
	load := exec.CommandContext(ctx, c.binary(), "load", "-t", prepared.reference)
	var output strings.Builder
	load.Stdout, load.Stderr = &output, &output
	writer, err := load.StdinPipe()
	if err != nil {
		return fmt.Errorf("pipe image import: %w", err)
	}
	defer func() { _ = writer.Close() }()
	stopClose := context.AfterFunc(ctx, func() { _ = writer.Close() })
	defer stopClose()
	if err := load.Start(); err != nil {
		return fmt.Errorf("start msb image import: %w", err)
	}
	writeErr := tarball.Write(ref, prepared.image, writer)
	closeErr := writer.Close()
	if err := errors.Join(writeErr, closeErr, ctx.Err()); err != nil {
		_ = load.Process.Kill()
		_ = load.Wait()
		return fmt.Errorf("stream image %q: %s: %w", prepared.reference, output.String(), err)
	}
	if err := load.Wait(); err != nil {
		return fmt.Errorf(
			"msb load %q: %s: %w",
			prepared.reference,
			output.String(),
			errors.Join(err, ctx.Err()),
		)
	}
	return nil
}

func (c Client) dockerBinary() string {
	if c.DockerBinary != "" {
		return c.DockerBinary
	}
	return "docker"
}

func (c Client) localImageAvailable(ctx context.Context, ref string) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	// #nosec G204 -- configured Docker-compatible executable and fixed inspection arguments.
	return exec.CommandContext(ctx, c.dockerBinary(), "image", "inspect", ref).Run() == nil
}

func (c Client) localImage(ctx context.Context, ref string) (*PreparedImage, error) {
	archive, err := os.CreateTemp("", "devsy-msb-image-*.tar")
	if err != nil {
		return nil, err
	}
	prepared := &PreparedImage{archive: archive.Name()}
	// #nosec G204 -- configured Docker-compatible executable, fixed subcommand, image argument.
	cmd := exec.CommandContext(ctx, c.dockerBinary(), "save", ref)
	cmd.Stdout = archive
	var stderr strings.Builder
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	closeErr := archive.Close()
	if err := errors.Join(runErr, closeErr, ctx.Err()); err != nil {
		_ = prepared.Close()
		return nil, fmt.Errorf(
			"save final local image %q: %s: %w",
			ref,
			strings.TrimSpace(stderr.String()),
			err,
		)
	}
	prepared.image, err = tarball.ImageFromPath(prepared.archive, nil)
	if err != nil {
		_ = prepared.Close()
		return nil, fmt.Errorf("read final local image %q: %w", ref, err)
	}
	return prepared, nil
}

func registryImage(ctx context.Context, imageRef string) (*PreparedImage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	ref, err := name.ParseReference(imageRef)
	if err != nil {
		return nil, fmt.Errorf("parse image reference %q: %w", imageRef, err)
	}
	img, err := remote.Image(ref,
		remote.WithContext(ctx),
		remote.WithAuthFromKeychain(authn.DefaultKeychain),
		remote.WithPlatform(v1.Platform{OS: "linux", Architecture: runtime.GOARCH}),
	)
	if err != nil {
		return nil, fmt.Errorf("pull final image %q: %w", imageRef, sanitizeRegistryError(err))
	}
	return &PreparedImage{image: img}, nil
}
