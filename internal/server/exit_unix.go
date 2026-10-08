//go:build !windows

package server

import (
	"fmt"
	"os/exec"
	"syscall"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
)

func processExit(failed *exec.ExitError) (*runtimev1.ExecExit, error) {
	if state, ok := failed.Sys().(syscall.WaitStatus); ok && state.Signaled() {
		return nil, fmt.Errorf("msb CLI terminated by signal %q", state.Signal().String())
	}
	return exitCode(failed.ExitCode())
}
