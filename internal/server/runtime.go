// Package server maps Runtime Protocol v1 lifecycle calls to MicroSandbox.
package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"runtime"
	"time"

	"github.com/blang/semver/v4"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
)

const (
	driverName                  = "microsandbox"
	userLabel                   = "devsy.sh/user"
	workspaceMountContractLabel = "devsy.sh/microsandbox-workspace-mount"
	workspaceRemoteUserLabel    = "devsy.sh/microsandbox-workspace-user"
	maxSandboxNameLen           = 128
)

// Client is the runtime capability needed by the protocol adapter.
type Client interface {
	EnsureInstalled(context.Context) error
	Version(context.Context) (string, error)
	Find(context.Context, string) (*msb.Info, error)
	PrepareImage(context.Context, string, bool) (*msb.PreparedImage, error)
	EnsureImage(context.Context, *msb.PreparedImage) error
	Create(context.Context, string, msb.Spec) error
	Start(context.Context, string) error
	Stop(context.Context, string) error
	Remove(context.Context, string) error
	Execute(context.Context, string, msb.ExecRequest) (int, error)
	Logs(context.Context, string, io.Writer) error
}

// Runtime implements Runtime Protocol v1 lifecycle and non-PTY streaming operations.
type Runtime struct {
	runtimev1.UnimplementedRuntimeDriverServer
	client  Client
	config  config.Config
	version string
	gate    chan struct{}
}

// New validates operator policy before any runtime interaction.
func New(client Client, cfg config.Config, version string) (*Runtime, error) {
	if client == nil {
		return nil, invalidArgument("runtime client is required")
	}
	if version == "" {
		return nil, invalidArgument("driver version is required")
	}
	if err := cfg.Validate(); err != nil {
		return nil, invalidArgument(err.Error())
	}
	return &Runtime{
		client:  client,
		config:  cfg,
		version: version,
		gate:    make(chan struct{}, 1),
	}, nil
}

// Info advertises the runtime capabilities supported by this adapter.
func (r *Runtime) Info(
	ctx context.Context,
	_ *runtimev1.InfoRequest,
) (*runtimev1.InfoResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, backendError(ctx, err)
	}
	raw, err := r.client.Version(ctx)
	if err != nil {
		return nil, backendError(ctx, err)
	}
	return &runtimev1.InfoResponse{
		ApiMajor:       runtimev1.APIMajor,
		ApiMinor:       runtimev1.APIMinor,
		DriverName:     driverName,
		DriverVersion:  r.version,
		RuntimeName:    driverName,
		RuntimeVersion: raw,
		Capabilities: &runtimev1.Capabilities{
			MountTypes: []runtimev1.MountType{
				runtimev1.MountType_MOUNT_TYPE_BIND,
				runtimev1.MountType_MOUNT_TYPE_VOLUME,
				runtimev1.MountType_MOUNT_TYPE_TMPFS,
			},
			RequiresWorkspaceChown: true,
			RecreateMode:           runtimev1.RecreateMode_RECREATE_MODE_DELETE,
			ProvisioningPreflight:  true,
			ReusePreflight:         true,
			Logs:                   true,
		},
	}, nil
}

// Preflight checks installation without gating existing workspace operations on version.
func (r *Runtime) Preflight(
	ctx context.Context,
	_ *runtimev1.PreflightRequest,
) (*runtimev1.PreflightResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, backendError(ctx, err)
	}
	if err := r.client.EnsureInstalled(ctx); err != nil {
		if ctx.Err() != nil {
			return nil, backendError(ctx, err)
		}
		return nil, runtimeError(
			codes.Unavailable,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_UNAVAILABLE,
			err.Error(),
		)
	}
	return &runtimev1.PreflightResponse{}, nil
}

// ProvisioningPreflight scopes the ownership version gate to create-time policy.
func (r *Runtime) ProvisioningPreflight(
	ctx context.Context,
	_ *runtimev1.ProvisioningPreflightRequest,
) (*runtimev1.ProvisioningPreflightResponse, error) {
	if err := ctx.Err(); err != nil {
		return nil, backendError(ctx, err)
	}
	if r.config.WorkspacePolicy.StatVirtualization == msb.StatOff {
		return &runtimev1.ProvisioningPreflightResponse{}, nil
	}
	raw, err := r.client.Version(ctx)
	if err != nil {
		return nil, backendError(ctx, err)
	}
	version, err := msb.ParseVersion(raw)
	if err != nil {
		return nil, runtimeError(
			codes.FailedPrecondition,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_INCOMPATIBLE_VERSION,
			err.Error(),
		)
	}
	if version.LT(semver.MustParse("0.7.2")) {
		return nil, runtimeError(
			codes.FailedPrecondition,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_INCOMPATIBLE_VERSION,
			"MicroSandbox v0.7.2 or newer is required for workspace ownership synchronization; update with msb self update",
		)
	}
	return &runtimev1.ProvisioningPreflightResponse{}, nil
}

// Find distinguishes an absent workspace from an invocation or permission failure.
func (r *Runtime) Find(
	ctx context.Context,
	req *runtimev1.FindRequest,
) (*runtimev1.FindResponse, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	defer r.release()
	info, err := r.find(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	if info == nil {
		return &runtimev1.FindResponse{}, nil
	}
	state := "stopped"
	if info.Running {
		state = "running"
	}
	created := ""
	if !info.CreatedAt.IsZero() {
		created = info.CreatedAt.Format(time.RFC3339Nano)
	}
	return &runtimev1.FindResponse{Found: true, Container: &runtimev1.ContainerDetails{
		Id:        info.Name,
		CreatedAt: created,
		State:     &runtimev1.ContainerState{Status: state},
		Config:    &runtimev1.ContainerConfig{Labels: info.Labels, User: info.Labels[userLabel]},
		Mounts:    containerMounts(info.Mounts),
	}}, nil
}

// TargetArchitecture reports the Linux guest architecture selected by image preparation.
func (r *Runtime) TargetArchitecture(
	ctx context.Context,
	req *runtimev1.TargetArchitectureRequest,
) (*runtimev1.TargetArchitectureResponse, error) {
	if err := validateWorkspace(ctx, req.GetWorkspaceId()); err != nil {
		return nil, err
	}
	return &runtimev1.TargetArchitectureResponse{Architecture: runtime.GOARCH}, nil
}

// RunImage validates and imports an immutable snapshot without replacing an existing VM.
func (r *Runtime) RunImage(
	ctx context.Context,
	req *runtimev1.RunImageRequest,
) (*runtimev1.RunImageResponse, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	defer r.release()
	spec, err := r.buildSpec(ctx, req)
	if err != nil {
		return nil, err
	}
	if err := r.requireAbsent(ctx, req.GetWorkspaceId()); err != nil {
		return nil, err
	}
	prepared, err := r.prepareValidatedImage(ctx, req, &spec)
	if err != nil {
		return nil, err
	}
	defer func() { _ = prepared.Close() }()
	spec.Image = prepared.Reference()
	if err := r.client.EnsureImage(ctx, prepared); err != nil {
		return nil, backendError(ctx, err)
	}
	if err := r.client.Create(ctx, sandboxName(req.GetWorkspaceId()), spec); err != nil {
		return nil, backendError(ctx, err)
	}
	return &runtimev1.RunImageResponse{}, nil
}

// Start treats an already-running workspace as success.
func (r *Runtime) Start(
	ctx context.Context,
	req *runtimev1.StartRequest,
) (*runtimev1.StartResponse, error) {
	if err := r.transition(ctx, req.GetWorkspaceId(), true); err != nil {
		return nil, err
	}
	return &runtimev1.StartResponse{}, nil
}

// Stop treats an already-stopped workspace as success.
func (r *Runtime) Stop(
	ctx context.Context,
	req *runtimev1.StopRequest,
) (*runtimev1.StopResponse, error) {
	if err := r.transition(ctx, req.GetWorkspaceId(), false); err != nil {
		return nil, err
	}
	return &runtimev1.StopResponse{}, nil
}

// Delete stops a running VM before removal and succeeds for absent workspaces.
func (r *Runtime) Delete(
	ctx context.Context,
	req *runtimev1.DeleteRequest,
) (*runtimev1.DeleteResponse, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	defer r.release()
	info, err := r.find(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	if info == nil {
		return &runtimev1.DeleteResponse{}, nil
	}
	if info.Running {
		if err := r.client.Stop(ctx, sandboxName(req.GetWorkspaceId())); err != nil {
			return nil, backendError(ctx, err)
		}
	}
	if err := r.client.Remove(ctx, sandboxName(req.GetWorkspaceId())); err != nil {
		return nil, backendError(ctx, err)
	}
	return &runtimev1.DeleteResponse{}, nil
}

func (r *Runtime) find(ctx context.Context, id string) (*msb.Info, error) {
	if err := validateWorkspace(ctx, id); err != nil {
		return nil, err
	}
	info, err := r.client.Find(ctx, sandboxName(id))
	return info, backendError(ctx, err)
}

func (r *Runtime) transition(ctx context.Context, id string, running bool) error {
	if err := r.acquire(ctx); err != nil {
		return err
	}
	defer r.release()
	info, err := r.find(ctx, id)
	if err != nil {
		return err
	}
	if info == nil {
		return missingWorkspace()
	}
	if info.Running == running {
		return nil
	}
	if running {
		return backendError(ctx, r.client.Start(ctx, sandboxName(id)))
	}
	return backendError(ctx, r.client.Stop(ctx, sandboxName(id)))
}

func sandboxName(id string) string {
	name := "devsy-" + id
	if len(name) <= maxSandboxNameLen {
		return name
	}
	sum := sha256.Sum256([]byte(id))
	suffix := "-" + hex.EncodeToString(sum[:])[:12]
	return name[:maxSandboxNameLen-len(suffix)] + suffix
}

// A canceled RPC must not wait for another lifecycle operation's image I/O.
func (r *Runtime) acquire(ctx context.Context) error {
	select {
	case r.gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return backendError(ctx, ctx.Err())
	}
}

func (r *Runtime) release() { <-r.gate }

func (r *Runtime) requireAbsent(ctx context.Context, id string) error {
	if _, err := r.ProvisioningPreflight(
		ctx,
		&runtimev1.ProvisioningPreflightRequest{},
	); err != nil {
		return err
	}
	info, err := r.find(ctx, id)
	if err != nil {
		return err
	}
	if info != nil {
		return runtimeError(
			codes.AlreadyExists,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_ALREADY_EXISTS,
			"workspace already exists; replacement requires explicit deletion",
		)
	}
	return nil
}

func (r *Runtime) prepareValidatedImage(
	ctx context.Context,
	req *runtimev1.RunImageRequest,
	spec *msb.Spec,
) (*msb.PreparedImage, error) {
	prepared, err := r.client.PrepareImage(ctx, req.GetImage(), req.GetImageBuiltLocally())
	if err != nil {
		return nil, backendError(ctx, err)
	}
	if prepared == nil {
		return nil, backendError(ctx, errors.New("image preparation returned no snapshot"))
	}
	if err := resolveOwner(ctx, req, prepared, spec); err != nil {
		_ = prepared.Close()
		return nil, err
	}
	return prepared, nil
}
