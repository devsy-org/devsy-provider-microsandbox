package msb

import (
	"fmt"
	"strconv"
)

const (
	envFlag                    = "--env"
	msbCmdRun                  = "run"
	msbFlagDetach              = "--detach"
	flagRootDisk               = "--root-disk"
	defaultEphemeralRootDiskGB = 8
)

// runArgs builds a detached `msb run` invocation, matching microsandbox's own
// ENTRYPOINT/CMD split: --entrypoint sets the executable, and a trailing "--"
// plus argv overrides the image CMD.
func runArgs(sandbox string, spec Spec) []string {
	args := []string{msbCmdRun, msbFlagDetach, "--name", sandbox}
	if spec.Entrypoint != "" {
		args = append(args, "--entrypoint", spec.Entrypoint)
	}
	args = append(args, runtimeArgs(spec)...)
	args = append(args, resourceArgs(spec)...)
	args = append(args, mountArgs(spec.Mounts)...)
	args = append(args, spec.Image)
	if len(spec.Cmd) > 0 {
		args = append(args, "--")
		args = append(args, spec.Cmd...)
	}
	return args
}

func runtimeArgs(spec Spec) []string {
	var args []string
	if spec.User != "" {
		args = append(args, "--user", spec.User)
	}
	for k, v := range spec.Env {
		args = append(args, envFlag, k+"="+v)
	}
	for k, v := range spec.Labels {
		args = append(args, "--label", k+"="+v)
	}
	if spec.IdleTimeout > 0 {
		args = append(args, "--idle-timeout", spec.IdleTimeout.String())
	}
	if spec.BlockEgress {
		args = append(args, "--net-default-egress", "deny")
	}
	return args
}

func resourceArgs(spec Spec) []string {
	var args []string
	if spec.Memory > 0 {
		args = append(args, "--memory", fmt.Sprintf("%dM", spec.Memory))
	}
	if spec.MaxMemory > 0 {
		args = append(args, "--max-memory", fmt.Sprintf("%dM", spec.MaxMemory))
	}
	if spec.CPUs > 0 {
		args = append(args, "--cpus", strconv.Itoa(int(spec.CPUs)))
	}
	if spec.MaxCPUs > 0 {
		args = append(args, "--max-cpus", strconv.Itoa(int(spec.MaxCPUs)))
	}
	if spec.Ephemeral {
		size := spec.RootDiskGB
		if size == 0 {
			size = defaultEphemeralRootDiskGB
		}
		args = append(args, flagRootDisk, fmt.Sprintf("tmpfs:%dG", size))
	} else if spec.RootDiskGB > 0 {
		args = append(args, flagRootDisk, fmt.Sprintf("%dG", spec.RootDiskGB))
	}
	return args
}

func mountArgs(mounts []Mount) []string {
	var args []string
	for _, m := range mounts {
		switch {
		case m.Tmpfs:
			args = append(args, "--tmpfs", m.Target)
		case m.Volume != "":
			args = append(args, "--mount-named", m.Volume+":"+m.Target)
		case m.Source != "":
			args = append(args, "--mount-dir", bindMountSpec(m))
		}
	}
	return args
}

func namedVolumes(mounts []Mount) []string {
	var vols []string
	for _, m := range mounts {
		if !m.Tmpfs && m.Volume != "" {
			vols = append(vols, m.Volume)
		}
	}
	return vols
}
