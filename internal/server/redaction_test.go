package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	creationDiagnostic         = "tmpfs root disk size (2048 MiB) must not exceed sandbox memory (1024 MiB)"
	redactedCreationDiagnostic = "tmpfs *** disk size (2048 MiB) must not exceed sandbox memory (1024 MiB)"
	redactedRejection          = "rejected ***"
	creationFixtureEnv         = "DEVSY_MSB_CREATION_DIAGNOSTIC"
)

// TestMain isolates diagnostic subprocesses from the test runner.
func TestMain(m *testing.M) {
	if diagnostic := os.Getenv(creationFixtureEnv); diagnostic != "" {
		_, _ = fmt.Fprintln(os.Stderr, diagnostic)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

type creationClient struct {
	*fakeClient
	backend msb.Client
}

func (c *creationClient) Create(ctx context.Context, name string, spec msb.Spec) error {
	return c.backend.Create(ctx, name, spec)
}

// TestCreationDiagnosticRedaction exercises the provenance boundary through a real child process.
func (s *runtimeSuite) TestCreationDiagnosticRedaction() {
	for _, tc := range []struct {
		name, diagnostic, want string
		labels                 []string
		env                    map[string]string
	}{
		{name: "generated user", diagnostic: creationDiagnostic, want: creationDiagnostic},
		{
			name: "request label", labels: []string{"private=root"}, diagnostic: creationDiagnostic,
			want: redactedCreationDiagnostic,
		},
		{
			name: "request environment", env: map[string]string{"PRIVATE": reuseRootUser}, diagnostic: creationDiagnostic,
			want: redactedCreationDiagnostic,
		},
		{
			name: "overwritten user label", labels: []string{userLabel + "=root"}, diagnostic: creationDiagnostic,
			want: redactedCreationDiagnostic,
		},
		{
			name: "overwritten contract", labels: []string{workspaceMountContractLabel + "=private-contract"},
			diagnostic: "rejected private-contract", want: redactedRejection,
		},
		{
			name: "overwritten remote user", labels: []string{workspaceRemoteUserLabel + "=private-user"},
			diagnostic: "rejected private-user", want: redactedRejection,
		},
		{
			name: "longest first", env: map[string]string{"SHORT": "abc", "LONG": "abc-private-token"},
			diagnostic: "rejected abc-private-token and abc", want: "rejected *** and ***",
		},
		{
			name: "short value", env: map[string]string{"PRIVATE": "x"},
			diagnostic: "rejected x", want: redactedRejection,
		},
		{
			name: "metadata", labels: []string{`devcontainer.metadata=[{"remoteEnv":{"TOKEN":"private-token"}}]`},
			diagnostic: `rejected [{"remoteEnv":{"TOKEN":"private-token"}}]`, want: redactedRejection,
		},
	} {
		s.Run(tc.name, func() {
			s.SetupTest()
			s.prepareImage()
			binary, err := os.Executable()
			s.Require().NoError(err)
			s.runtime.client = &creationClient{
				fakeClient: s.client,
				backend:    msb.Client{Binary: binary},
			}
			s.runtime.config.Defaults.Ephemeral = true
			s.runtime.config.Defaults.RootDiskGB = 2
			s.runtime.config.Defaults.Memory = 1024
			s.request.User, s.request.RemoteUser, s.request.Dockerless = reuseRootUser, reuseRootUser, true
			s.request.Labels, s.request.Environment = tc.labels, tc.env
			s.T().Setenv(creationFixtureEnv, tc.diagnostic)
			_, err = s.runtime.RunImage(context.Background(), s.request)
			s.Require().Error(err)
			s.ErrorContains(err, tc.want)
		})
	}
}

// TestCreationBindDiagnosticRedaction protects both protocol error messages from creation inputs.
func (s *runtimeSuite) TestCreationBindDiagnosticRedaction() {
	for _, diagnostic := range []string{
		"image error: registry error: Not authorized",
		"image error: registry error: connection refused",
		"invalid config: tmpfs root disk size exceeds sandbox memory",
		"build_vm: invalid max vCPU count: 255",
		"mount-dir source rejected",
	} {
		s.Run(diagnostic, func() {
			s.SetupTest()
			s.prepareImage()
			binary, err := os.Executable()
			s.Require().NoError(err)
			s.runtime.client = &creationClient{
				fakeClient: s.client,
				backend:    msb.Client{Binary: binary},
			}
			s.request.WorkspaceMount = nil
			s.request.User, s.request.RemoteUser = reuseRootUser, reuseRootUser
			root := s.T().TempDir()
			short := filepath.Join(root, "private-bind")
			long := short + "-long"
			s.Require().NoError(os.Mkdir(short, 0o750))
			s.Require().NoError(os.Mkdir(long, 0o750))
			resolved, err := filepath.EvalSymlinks(long)
			s.Require().NoError(err)
			missing := filepath.Join(root, "missing-bind")
			s.request.Mounts = []*runtimev1.Mount{
				{Type: runtimev1.MountType_MOUNT_TYPE_BIND, Source: short, Target: "/short"},
				{Type: runtimev1.MountType_MOUNT_TYPE_BIND, Source: long, Target: "/long"},
				{Type: runtimev1.MountType_MOUNT_TYPE_BIND, Source: missing, Target: "/missing"},
			}
			s.request.Environment = map[string]string{"CREATION_PRIVATE": "private-env-value"}
			s.request.Labels = []string{"private=private-label-value"}
			output := fmt.Sprintf("%s: %s %s %s %s", diagnostic, long, short, resolved, missing)
			s.T().Setenv(creationFixtureEnv, output+" private-env-value private-label-value")
			_, err = s.runtime.RunImage(context.Background(), s.request)
			s.Require().Error(err)
			st := status.Convert(err)
			s.Equal(codes.Internal, st.Code())
			want := "msb run: " + diagnostic + ": *** *** *** *** *** ***: exit status 1"
			s.Equal(want, st.Message())
			s.Require().Len(st.Details(), 1)
			detail, ok := st.Details()[0].(*runtimev1.RuntimeError)
			s.Require().True(ok)
			s.Equal(runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_RUNTIME_FAILURE, detail.GetCode())
			s.Equal(want, detail.GetMessage())
		})
	}
}
