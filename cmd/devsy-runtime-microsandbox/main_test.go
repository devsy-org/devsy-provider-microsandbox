package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	sdkplugin "github.com/devsy-org/devsy-runtime-sdk/plugin"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	hplugin "github.com/hashicorp/go-plugin"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const fixtureEnv = "DEVSY_MSB_ENTRYPOINT_FIXTURE"

var executable string

func TestMain(m *testing.M) {
	if os.Getenv(fixtureEnv) == "1" {
		os.Exit(msbFixture(os.Args[1:]))
	}
	dir, err := os.MkdirTemp("", "microsandbox-entrypoint-")
	if err != nil {
		panic(err)
	}
	executable = filepath.Join(dir, "runtime.exe")
	// #nosec G204 -- build a fixed local package to a private test directory
	cmd := exec.Command(
		"go",
		"build",
		"-race",
		"-ldflags",
		"-X main.version=test",
		"-o",
		executable,
		".",
	)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		os.Exit(1)
	}
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

type entrypointSuite struct{ suite.Suite }

func TestEntrypoint(t *testing.T) { suite.Run(t, new(entrypointSuite)) }

func (s *entrypointSuite) TestVersionAndArgumentValidation() {
	out, err := exec.Command(executable, "--version").Output()
	s.Require().NoError(err)
	s.Equal("devsy-runtime-microsandbox test\n", string(out))
	for _, args := range [][]string{nil, {"unknown"}, {"serve", "extra"}} {
		// #nosec G204 -- private locally built binary and fixed invalid argument fixtures
		out, err = exec.Command(executable, args...).Output()
		s.Error(err)
		s.Empty(out)
	}
}

func (s *entrypointSuite) TestRealPluginHandshakeAndLogs() {
	client := s.pluginClient()
	info, err := client.Info(s.ctx(), &runtimev1.InfoRequest{})
	s.Require().NoError(err)
	s.NoError(runtimev1.ValidateInfo(info))
	s.Equal("test", info.GetDriverVersion())
	s.True(info.GetCapabilities().GetLogs())
	logs, err := client.Logs(s.ctx(), &runtimev1.LogsRequest{WorkspaceId: "workspace"})
	s.Require().NoError(err)
	chunk, err := logs.Recv()
	s.Require().NoError(err)
	s.Equal([]byte("logs\x00\xff"), chunk.GetData())
	_, err = logs.Recv()
	s.ErrorIs(err, io.EOF)
}

func (s *entrypointSuite) TestRealPluginRejectsOldExecutionBackend() {
	client := s.pluginClient()
	stream, err := client.Exec(s.ctx())
	s.Require().NoError(err)
	s.Require().NoError(stream.Send(execStart("echo")))
	_, err = stream.Recv()
	s.Equal(codes.Unimplemented, status.Code(err))
	s.ErrorContains(err, "requires msb 0.7.7")
}

func (s *entrypointSuite) TestRealPluginNativeBackendFailure() {
	s.T().Setenv("DEVSY_MSB_ENTRYPOINT_VERSION", "0.7.7")
	client := s.pluginClient()
	stream, err := client.Exec(s.ctx())
	s.Require().NoError(err)
	s.Require().NoError(stream.Send(execStart("echo")))
	frame, err := stream.Recv()
	s.Nil(frame)
	s.Equal(codes.Internal, status.Code(err))
	s.ErrorContains(err, "sandbox not found: devsy-workspace")
}

func (s *entrypointSuite) pluginClient() runtimev1.RuntimeDriverClient {
	s.T().Helper()
	dir := s.T().TempDir()
	fixture := filepath.Join(dir, "msb")
	if runtime.GOOS == "windows" {
		fixture += ".exe"
	}
	self, err := os.Executable()
	s.Require().NoError(err)
	// #nosec G304 -- copy only this running test executable
	data, err := os.ReadFile(self)
	s.Require().NoError(err)
	// #nosec G306 G703 -- fixed msb filename in a private testing-owned directory; must be executable
	s.Require().NoError(os.WriteFile(fixture, data, 0o700))
	cmd := exec.Command(executable, "serve")
	cmd.Env = append(
		os.Environ(),
		fixtureEnv+"=1",
		"PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"),
		"MSB_HOME="+s.T().TempDir(),
	)
	client := hplugin.NewClient(&hplugin.ClientConfig{
		HandshakeConfig: sdkplugin.Handshake(),
		VersionedPlugins: map[int]hplugin.PluginSet{
			sdkplugin.ProtocolVersion: sdkplugin.ClientPlugins(),
		},
		Cmd:              cmd,
		SkipHostEnv:      true,
		AllowedProtocols: []hplugin.Protocol{hplugin.ProtocolGRPC},
		StartTimeout:     15 * time.Second,
	})
	s.T().Cleanup(client.Kill)
	rpc, err := client.Client()
	s.Require().NoError(err)
	raw, err := rpc.Dispense(sdkplugin.Name)
	s.Require().NoError(err)
	result, ok := raw.(runtimev1.RuntimeDriverClient)
	s.Require().True(ok)
	return result
}

func (s *entrypointSuite) ctx() context.Context {
	s.T().Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	s.T().Cleanup(cancel)
	return ctx
}

func execStart(command string) *runtimev1.ExecClientMessage {
	return &runtimev1.ExecClientMessage{
		Payload: &runtimev1.ExecClientMessage_Start{
			Start: &runtimev1.ExecStart{WorkspaceId: "workspace", Argv: []string{command}},
		},
	}
}

func msbFixture(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "--version":
		_, _ = fmt.Fprintln(os.Stdout, "msb "+fixtureVersion())
	case "inspect":
		_, _ = fmt.Fprintln(os.Stdout, `{"name":"devsy-workspace","status":"Running"}`)
	case "logs":
		_, _ = os.Stdout.Write([]byte("logs\x00\xff"))
	default:
		return 2
	}
	return 0
}

func fixtureVersion() string {
	if version := os.Getenv("DEVSY_MSB_ENTRYPOINT_VERSION"); version != "" {
		return version
	}
	return "0.7.2"
}
