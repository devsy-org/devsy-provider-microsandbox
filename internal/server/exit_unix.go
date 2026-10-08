//go:build !windows

package server

import (
	"os/exec"
	"syscall"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
)

func processExit(failed *exec.ExitError) (*runtimev1.ExecExit, error) {
	if state, ok := failed.Sys().(syscall.WaitStatus); ok && state.Signaled() {
		return &runtimev1.ExecExit{Signal: state.Signal().String()}, nil
	}
	return exitCode(failed.ExitCode())
}
