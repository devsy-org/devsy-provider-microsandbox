package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
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

func (s *entrypointSuite) TestRealPluginHandshakeAndStreams() {
	client := s.pluginClient()
	info, err := client.Info(s.ctx(), &runtimev1.InfoRequest{})
	s.Require().NoError(err)
	s.NoError(runtimev1.ValidateInfo(info))
	s.Equal("test", info.GetDriverVersion())
	s.True(info.GetCapabilities().GetLogs())
	stream, err := client.Exec(s.ctx())
	s.Require().NoError(err)
	s.Require().NoError(stream.Send(execStart("echo")))
	data := bytes.Repeat([]byte{0, 255, 13, 10}, 20000)
	sent := make(chan error, 1)
	go func() { sent <- sendFixtureInput(stream, data) }()
	stdout, stderr, exit := s.readOutput(stream)
	s.NoError(<-sent)
	s.Equal(data, stdout)
	s.Equal([]byte("diagnostic\x00\xff"), stderr)
	s.Equal(int32(0), exit.GetExitCode())
	logs, err := client.Logs(s.ctx(), &runtimev1.LogsRequest{WorkspaceId: "workspace"})
	s.Require().NoError(err)
	chunk, err := logs.Recv()
	s.Require().NoError(err)
	s.Equal([]byte("logs\x00\xff"), chunk.GetData())
	_, err = logs.Recv()
	s.ErrorIs(err, io.EOF)
}

func (s *entrypointSuite) TestRealPluginNonzeroExit() {
	client := s.pluginClient()
	stream, err := client.Exec(s.ctx())
	s.Require().NoError(err)
	s.Require().NoError(stream.Send(execStart("exit7")))
	_, _, exit := s.readOutput(stream)
	s.Equal(int32(7), exit.GetExitCode())
	s.Empty(exit.GetSignal())
}

func (s *entrypointSuite) TestRealPluginCLISignalFailure() {
	if runtime.GOOS == "windows" {
		s.T().Skip("Unix CLI signal termination")
	}
	client := s.pluginClient()
	stream, err := client.Exec(s.ctx())
	s.Require().NoError(err)
	s.Require().NoError(stream.Send(execStart("signal")))
	_, err = stream.Recv()
	s.Equal(codes.Internal, status.Code(err))
	s.ErrorContains(err, "msb CLI terminated by signal")
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

func sendFixtureInput(stream runtimev1.RuntimeDriver_ExecClient, data []byte) error {
	for len(data) > 0 {
		n := min(len(data), runtimev1.ChunkSize)
		if err := stream.Send(
			&runtimev1.ExecClientMessage{
				Payload: &runtimev1.ExecClientMessage_Stdin{Stdin: data[:n]},
			},
		); err != nil {
			return err
		}
		data = data[n:]
	}
	if err := stream.Send(
		&runtimev1.ExecClientMessage{
			Payload: &runtimev1.ExecClientMessage_CloseStdin{CloseStdin: &runtimev1.CloseStdin{}},
		},
	); err != nil {
		return err
	}
	return stream.CloseSend()
}

func (s *entrypointSuite) readOutput(
	stream runtimev1.RuntimeDriver_ExecClient,
) (stdout, stderr []byte, exit *runtimev1.ExecExit) {
	s.T().Helper()
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			break
		}
		s.Require().NoError(err)
		s.Require().Nil(exit, "frame after exit")
		switch p := frame.Payload.(type) {
		case *runtimev1.ExecServerMessage_Stdout:
			stdout = append(stdout, p.Stdout.GetData()...)
		case *runtimev1.ExecServerMessage_Stderr:
			stderr = append(stderr, p.Stderr.GetData()...)
		case *runtimev1.ExecServerMessage_Exit:
			exit = p.Exit
		default:
			s.T().Fatal("unknown output frame")
		}
	}
	s.Require().NotNil(exit)
	return stdout, stderr, exit
}

func msbFixture(args []string) int {
	if len(args) == 0 {
		return 2
	}
	switch args[0] {
	case "--version":
		_, _ = fmt.Fprintln(os.Stdout, "msb 0.7.2")
	case "inspect":
		_, _ = fmt.Fprintln(os.Stdout, `{"name":"devsy-workspace","status":"Running"}`)
	case "logs":
		_, _ = os.Stdout.Write([]byte("logs\x00\xff"))
	case "exec":
		return fixtureExec(args)
	default:
		return 2
	}
	return 0
}

func fixtureExec(args []string) int {
	separator := slices.Index(args, "--")
	if separator < 0 || separator+1 >= len(args) {
		return 2
	}
	if args[separator+1] == "signal" {
		process, err := os.FindProcess(os.Getpid())
		if err != nil {
			return 2
		}
		if err := process.Signal(os.Interrupt); err != nil {
			return 2
		}
		time.Sleep(time.Second)
		return 2
	}
	if args[separator+1] == "exit7" {
		return 7
	}
	_, _ = os.Stderr.Write([]byte("diagnostic\x00\xff"))
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		return 3
	}
	return 0
}
