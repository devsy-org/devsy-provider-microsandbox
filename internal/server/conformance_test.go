package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/conformance"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
)

const conformanceShell = "/bin/sh"

func TestRealRuntimeConformance(t *testing.T) {
	if os.Getenv("DEVSY_MSB_CONFORMANCE") != "1" {
		t.Skip("requires explicit real microVM qualification")
	}
	require.True(t, filepath.IsAbs(os.Getenv("MSB_PATH")))
	require.True(t, filepath.IsAbs(os.Getenv("MSB_LIBKRUNFW_PATH")))
	home, err := os.MkdirTemp("/tmp", "mc")
	require.NoError(t, err)
	t.Cleanup(func() { cleanConformanceHome(t, home) })
	t.Setenv("MSB_HOME", home)
	t.Setenv("MSB_CONFIG_PATH", filepath.Join(home, "config.json"))
	cfg, err := config.Load(os.Getenv)
	require.NoError(t, err)
	conformance.Run(t, conformance.Options{
		Timeout: 3 * time.Minute,
		Connect: func(t *testing.T) runtimev1.RuntimeDriverClient {
			if strings.HasSuffix(t.Name(), "/signal") {
				t.Skip(
					"msb Go SDK 0.7.13 exposes only an exit code, not an authoritative signal; " +
						"extending the backend event contract is outside this test change",
				)
			}
			driver, err := New(msb.Client{Binary: cfg.MSBPath}, cfg, "conformance")
			require.NoError(t, err)
			listener := bufconn.Listen(1 << 20)
			service := grpc.NewServer()
			runtimev1.RegisterRuntimeDriverServer(service, driver)
			done := make(chan error, 1)
			go func() { done <- service.Serve(listener) }()
			t.Cleanup(
				func() { service.Stop(); require.NoError(t, <-done); require.NoError(t, listener.Close()) },
			)
			conn, err := grpc.NewClient(
				"passthrough:///conformance",
				grpc.WithTransportCredentials(insecure.NewCredentials()),
				grpc.WithContextDialer(
					func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) },
				),
			)
			require.NoError(t, err)
			t.Cleanup(func() { require.NoError(t, conn.Close()) })
			return runtimev1.NewRuntimeDriverClient(conn)
		},
		Workspace: func(_ *testing.T) *runtimev1.RunImageRequest {
			return &runtimev1.RunImageRequest{
				WorkspaceId: "c-" + rand.Text()[:8],
				Image:       "docker.io/library/alpine:3.22",
				Entrypoint:  conformanceShell,
				Args:        []string{"-c", "printf 'conformance logs\\n'; exec sleep 86400"},
			}
		},
		Command: conformanceCommand,
		LogData: []byte("conformance logs\n"),
	})
}

func conformanceCommand(scenario conformance.Scenario, workspace string) *runtimev1.ExecStart {
	commands := map[conformance.Scenario][]string{
		conformance.Echo: {"/bin/cat"},
		conformance.Arguments: append(
			[]string{"/bin/busybox", "printf", "%s\\000"},
			conformance.ArgumentValues()...),
		conformance.Shell:     {conformanceShell, "-c", "printf 'shell stdout\\n'"},
		conformance.Stderr:    {conformanceShell, "-c", "exec cat >&2"},
		conformance.Duplex:    {"/usr/bin/tee", "/dev/stderr"},
		conformance.Nonzero:   {conformanceShell, "-c", "printf diagnostic >&2; exit 7"},
		conformance.Signal:    {conformanceShell, "-c", "kill -TERM $$"},
		conformance.EarlyExit: {"/bin/true"},
		conformance.Block:     {conformanceShell, "-c", "printf 'ready\\n'; exec sleep 86400"},
	}
	return &runtimev1.ExecStart{WorkspaceId: workspace, Argv: commands[scenario]}
}

func cleanConformanceHome(t *testing.T, home string) {
	t.Helper()

	binary := os.Getenv("MSB_PATH")
	command := &exec.Cmd{
		Path:      binary,
		Args:      []string{binary, "ls", "--quiet"},
		WaitDelay: time.Second,
	}
	command.Env = append(
		os.Environ(),
		"MSB_HOME="+home,
		"MSB_CONFIG_PATH="+filepath.Join(home, "config.json"),
	)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	require.NoError(t, command.Start())
	timer := time.AfterFunc(20*time.Second, func() { _ = command.Process.Kill() })
	defer timer.Stop()
	err := command.Wait()

	require.NoError(t, err)

	t.Logf("msb ls (home %s): %s", home, output.String())
	require.Empty(t, strings.TrimSpace(output.String()), "preserving isolated home for cleanup")
	require.NoError(t, os.RemoveAll(home))
}
