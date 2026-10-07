package server

import (
	"context"
	"fmt"
	"maps"
	"math"
	"runtime"
	"strings"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
)

func (r *Runtime) buildSpec(ctx context.Context, req *runtimev1.RunImageRequest) (msb.Spec, error) {
	if err := validateWorkspace(ctx, req.GetWorkspaceId()); err != nil {
		return msb.Spec{}, err
	}
	if req.GetImage() == "" {
		return msb.Spec{}, invalidArgument("image is required")
	}
	if err := validateRuntimeOptions(req); err != nil {
		return msb.Spec{}, err
	}
	mounts, err := r.mounts(req)
	if err != nil {
		return msb.Spec{}, err
	}
	spec := r.config.Defaults
	spec.Image, spec.User, spec.Entrypoint = req.GetImage(), req.GetUser(), req.GetEntrypoint()
	spec.Cmd = append([]string(nil), req.GetArgs()...)
	spec.Env = maps.Clone(req.GetEnvironment())
	spec.Labels, err = r.labels(req)
	if err != nil {
		return msb.Spec{}, err
	}
	spec.Mounts = mounts
	if err := applyResources(&spec, req.GetHostRequirements()); err != nil {
		return msb.Spec{}, err
	}
	return spec, nil
}

func validateRuntimeOptions(req *runtimev1.RunImageRequest) error {
	if unsupportedSecurityOptions(req) || unsupportedUserNamespace(req) {
		return runtimeError(
			codes.Unimplemented,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_UNSUPPORTED,
			"MicroSandbox does not support Docker privilege, init, capability, security, or user namespace options",
		)
	}
	if req.GetPlatform() != "" && req.GetPlatform() != "linux/"+runtime.GOARCH {
		return invalidArgument("MicroSandbox requires platform linux/" + runtime.GOARCH)
	}
	return nil
}

func applyResources(spec *msb.Spec, req *runtimev1.HostRequirements) error {
	cpus := req.GetCpus()
	if cpus < 0 || cpus > math.MaxUint8 {
		return invalidArgument("host CPU requirement must be between 0 and 255")
	}
	if err := validateGPURequirement(req.GetGpu()); err != nil {
		return err
	}
	if spec.CPUs == 0 {
		spec.CPUs = uint8(cpus)
	}
	if spec.Memory == 0 {
		spec.Memory = ceilUnits(req.GetMemoryBytes(), 1<<20)
	}
	if spec.RootDiskGB == 0 {
		spec.RootDiskGB = ceilUnits(req.GetStorageBytes(), 1<<30)
	}
	return nil
}

func ceilUnits(bytes, unit uint64) uint32 {
	value := bytes / unit
	if bytes%unit != 0 {
		value++
	}
	return uint32(min(value, math.MaxUint32))
}

func (r *Runtime) mounts(req *runtimev1.RunImageRequest) ([]msb.Mount, error) {
	var mounts []msb.Mount
	if req.GetWorkspaceMount() != nil {
		mount, err := convertMount(req.GetWorkspaceMount())
		if err != nil {
			return nil, err
		}
		if mount.Source != "" {
			mount.Policy = r.config.WorkspacePolicy
			mount.Policy.Owner = nil
		}
		mounts = append(mounts, mount)
	}
	for _, wire := range req.GetMounts() {
		mount, err := convertMount(wire)
		if err != nil {
			return nil, err
		}
		mounts = append(mounts, mount)
	}
	return mounts, nil
}

func convertMount(wire *runtimev1.Mount) (msb.Mount, error) {
	if wire.GetTarget() == "" {
		return msb.Mount{}, invalidArgument("mount target is required")
	}
	mount, err := mountByType(wire)
	if err != nil {
		return msb.Mount{}, err
	}
	if err := applyMountOptions(wire.GetOptions(), &mount); err != nil {
		return msb.Mount{}, err
	}

	if mount.ReadOnly && mount.Source == "" {
		return msb.Mount{}, invalidArgument("read-only is supported only for bind mounts")
	}
	return mount, nil
}

func resolveOwner(
	ctx context.Context,
	req *runtimev1.RunImageRequest,
	prepared *msb.PreparedImage,
	spec *msb.Spec,
) error {
	var workspace *msb.Mount
	if req.GetWorkspaceMount() != nil {
		workspace = &spec.Mounts[0]
	}
	owner, err := msb.ResolveWorkspaceOwner(ctx, prepared, msb.WorkspaceOwnerOptions{
		User:           req.GetUser(),
		RemoteUser:     req.GetRemoteUser(),
		Dockerless:     req.GetDockerless(),
		WorkspaceMount: workspace,
	})
	if err != nil {
		if ctx.Err() != nil {
			return backendError(ctx, err)
		}
		return runtimeError(
			codes.FailedPrecondition,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_FAILED_PRECONDITION,
			err.Error(),
		)
	}
	if owner != nil {
		workspace.Policy.Owner = owner
		spec.Labels[workspaceRemoteUserLabel] = effectiveRemoteUser(req)
	}
	return nil
}

func effectiveRemoteUser(req *runtimev1.RunImageRequest) string {
	if req.GetRemoteUser() != "" {
		return req.GetRemoteUser()
	}
	if req.GetUser() != "" {
		return req.GetUser()
	}
	return "root"
}

func (r *Runtime) mountContract() string {
	policy := r.config.WorkspacePolicy
	owner := "remote-user"
	if policy.StatVirtualization == msb.StatOff {
		owner = "none"
	}
	return fmt.Sprintf(
		"v3;stat=%s;host=%s;owner=%s",
		policy.StatVirtualization,
		policy.HostPermissions,
		owner,
	)
}

func unsupportedSecurityOptions(req *runtimev1.RunImageRequest) bool {
	return req.GetPrivileged() || req.GetInit() || len(req.GetCapAdd()) > 0 ||
		len(req.GetSecurityOpt()) > 0
}

func unsupportedUserNamespace(req *runtimev1.RunImageRequest) bool {
	return req.GetUserns() != "" || len(req.GetUidMap()) > 0 || len(req.GetGidMap()) > 0
}

func validateGPURequirement(req *runtimev1.GpuRequirement) error {
	switch req.GetValue() {
	case runtimev1.GpuRequirementValue_GPU_REQUIRED:
		return runtimeError(
			codes.Unimplemented,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_UNSUPPORTED,
			"MicroSandbox does not support required GPUs",
		)
	case runtimev1.GpuRequirementValue_GPU_UNSPECIFIED,
		runtimev1.GpuRequirementValue_GPU_FALSE,
		runtimev1.GpuRequirementValue_GPU_OPTIONAL:
	default:
		return invalidArgument("unknown GPU requirement")
	}

	return nil
}

func mountByType(wire *runtimev1.Mount) (msb.Mount, error) {
	mount := msb.Mount{Target: wire.GetTarget(), ReadOnly: wire.GetReadOnly()}
	switch wire.GetType() {
	case runtimev1.MountType_MOUNT_TYPE_BIND:
		if wire.GetSource() == "" {
			return msb.Mount{}, invalidArgument("bind mount source is required")
		}
		mount.Source = wire.GetSource()
	case runtimev1.MountType_MOUNT_TYPE_VOLUME:
		if wire.GetSource() == "" {
			return msb.Mount{}, invalidArgument("named volume source is required")
		}
		mount.Volume = wire.GetSource()
	case runtimev1.MountType_MOUNT_TYPE_TMPFS:
		mount.Tmpfs = true
	default:
		return msb.Mount{}, invalidArgument("unsupported mount type")
	}
	return mount, nil
}

func applyMountOptions(options []string, mount *msb.Mount) error {
	for _, option := range options {
		switch option {
		case "ro", "readonly":
			mount.ReadOnly = true
		case "rw":
		default:
			return invalidArgument("unsupported mount option " + option)
		}
	}
	return nil
}

func (r *Runtime) labels(req *runtimev1.RunImageRequest) (map[string]string, error) {
	labels := make(map[string]string)
	for _, label := range req.GetLabels() {
		key, value, _ := strings.Cut(label, "=")
		if key == "" {
			return nil, invalidArgument("label key is required")
		}
		labels[key] = value
	}
	if req.GetUser() != "" {
		labels[userLabel] = req.GetUser()
	}
	labels[workspaceMountContractLabel] = r.mountContract()
	return labels, nil
}

func containerMounts(mounts []msb.Mount) []*runtimev1.ContainerMount {
	var out []*runtimev1.ContainerMount
	for _, mount := range mounts {
		kind, source := "bind", mount.Source
		if mount.Tmpfs {
			kind = "tmpfs"
		} else if mount.Volume != "" {
			kind, source = "volume", mount.Volume
		}
		out = append(
			out,
			&runtimev1.ContainerMount{Type: kind, Source: source, Destination: mount.Target},
		)
	}
	return out
}
