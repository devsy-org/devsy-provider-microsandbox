package server

import (
	"os/exec"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
)

func processExit(failed *exec.ExitError) (*runtimev1.ExecExit, error) {
	return exitCode(failed.ExitCode())
}
