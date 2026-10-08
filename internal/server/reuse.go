package server

import (
	"context"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
)

// ReusePreflight rejects changed ownership policy without modifying the existing VM.
func (r *Runtime) ReusePreflight(
	ctx context.Context,
	req *runtimev1.ReusePreflightRequest,
) (*runtimev1.ReusePreflightResponse, error) {
	if err := r.acquire(ctx); err != nil {
		return nil, err
	}
	defer r.release()
	if err := validateWorkspace(ctx, req.GetWorkspaceId()); err != nil {
		return nil, err
	}
	if req.GetRemoteUser() == "" {
		return nil, invalidArgument("remote_user is required")
	}
	info, err := r.find(ctx, req.GetWorkspaceId())
	if err != nil {
		return nil, err
	}
	if info == nil {
		return nil, missingWorkspace()
	}
	if reason := r.reuseRequirement(info, req.GetRemoteUser()); reason != "" {
		return nil, runtimeError(codes.FailedPrecondition,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_FAILED_PRECONDITION,
			reason+"; rerun with --recreate")
	}
	return &runtimev1.ReusePreflightResponse{}, nil
}

func (r *Runtime) reuseRequirement(info *msb.Info, remoteUser string) string {
	if info.Labels[workspaceMountContractLabel] != r.mountContract() {
		return "MicroSandbox workspace mount policy changed"
	}
	if r.config.WorkspacePolicy.StatVirtualization != msb.StatOff {
		if saved := info.Labels[workspaceRemoteUserLabel]; saved != "" && saved != remoteUser {
			return "MicroSandbox workspace developer identity changed"
		}
	}
	return ""
}
