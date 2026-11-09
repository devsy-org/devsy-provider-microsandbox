package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *entrypointSuite) TestBlankRuntimePathEnvironment() {
	for _, value := range []string{"", " \t\n"} {
		s.Run(value, func() {
			client := s.pluginClientWithEnv(
				"MSB_PATH="+value, "MSB_LIBKRUNFW_PATH="+value,
				"MSB_CONFIG_PATH="+value, "MSB_AGENTD_PATH="+value,
				"DEVSY_MSB_ENTRYPOINT_CHECK_PATH_ENV=1",
			)
			_, err := client.Info(s.ctx(), &runtimev1.InfoRequest{})
			s.Require().NoError(err)
			stream, err := client.Exec(s.ctx())
			s.Require().NoError(err)
			s.Require().NoError(stream.Send(execStart("true")))
			_, err = stream.Recv()
			s.Equal(codes.Internal, status.Code(err))
			s.ErrorContains(err, "sandbox not found: devsy-workspace")
		})
	}
}

func (s *entrypointSuite) TestRuntimeCheckBlankPaths() {
	for _, value := range []string{"", " \t\n"} {
		s.Run(value, func() {
			home := s.T().TempDir()
			binDir, libDir := filepath.Join(home, "bin"), filepath.Join(home, "lib")
			s.Require().NoError(os.MkdirAll(binDir, 0o700))
			s.Require().NoError(os.MkdirAll(libDir, 0o700))
			binaryName, firmwareName := "msb", "libkrunfw.so.5.6.1"
			switch runtime.GOOS {
			case "darwin":
				firmwareName = "libkrunfw.5.dylib"
			case "windows":
				binaryName, firmwareName = "msb.exe", "libkrunfw.dll"
			}
			s.writeFixture(filepath.Join(binDir, binaryName))
			s.Require().NoError(os.WriteFile(filepath.Join(libDir, firmwareName), nil, 0o600))
			cmd := exec.Command(executable, "runtime-check")
			cmd.Env = append(os.Environ(), fixtureEnv+"=1", "MSB_HOME="+home,
				"MSB_PATH="+value, "MSB_LIBKRUNFW_PATH="+value,
				"MSB_CONFIG_PATH="+value, "MSB_AGENTD_PATH="+value,
				"DEVSY_MSB_ENTRYPOINT_CHECK_PATH_ENV=1")
			out, err := cmd.CombinedOutput()
			s.Require().NoError(err, "%s", out)
			var result runtimeCheckResult
			s.Require().NoError(json.Unmarshal(out, &result))
			s.Equal(filepath.Join(binDir, binaryName), result.MSBPath)
			s.Equal(filepath.Join(libDir, firmwareName), result.LibkrunfwPath)
			s.True(result.Compatible)
		})
	}
}
