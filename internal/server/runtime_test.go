package server

import (
	"context"
	"errors"
	"io"
	"math"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/config"
	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	testHostPath  = "/host"
	testWorkspace = "workspace"
	findCall      = "find"
	stopCall      = "stop"
	versionCall   = "version"
)

var errBackend = errors.New("backend permission denied")

type runtimeSuite struct {
	suite.Suite
	runtime *Runtime
	client  *fakeClient
	request *runtimev1.RunImageRequest
}

func TestRuntime(t *testing.T) { suite.Run(t, new(runtimeSuite)) }

func (s *runtimeSuite) SetupTest() {
	cfg, err := config.Load(func(string) string { return "" })
	s.Require().NoError(err)
	s.client = &fakeClient{version: "msb 0.7.2"}
	s.runtime, err = New(s.client, cfg, "test")
	s.Require().NoError(err)
	s.request = &runtimev1.RunImageRequest{
		WorkspaceId: testWorkspace,
		Image:       "image",
		User:        "service",
		RemoteUser:  "1000:1001",
		WorkspaceMount: &runtimev1.Mount{
			Type:   runtimev1.MountType_MOUNT_TYPE_BIND,
			Source: testHostPath,
			Target: "/workspace",
		},
	}
}

func (s *runtimeSuite) TestPreflightVersionScope() {
	s.client.version = "msb 0.6.0"
	_, err := s.runtime.Preflight(context.Background(), &runtimev1.PreflightRequest{})
	s.Require().NoError(err)
	s.Equal([]string{"installed"}, s.client.calls)
	_, err = s.runtime.ProvisioningPreflight(
		context.Background(),
		&runtimev1.ProvisioningPreflightRequest{},
	)
	s.Equal(codes.FailedPrecondition, status.Code(err))
	s.runtime.config.WorkspacePolicy = msb.MountPolicy{
		StatVirtualization: msb.StatOff,
		HostPermissions:    msb.HostPrivate,
	}
	s.client.calls = nil
	_, err = s.runtime.ProvisioningPreflight(
		context.Background(),
		&runtimev1.ProvisioningPreflightRequest{},
	)
	s.NoError(err)
	s.Empty(s.client.calls)
}

func (s *runtimeSuite) TestInfo() {
	info, err := s.runtime.Info(context.Background(), &runtimev1.InfoRequest{})
	s.Require().NoError(err)
	s.NoError(runtimev1.ValidateInfo(info))
	s.True(info.GetCapabilities().GetLogs())
	s.True(info.GetCapabilities().GetRequiresWorkspaceChown())
}

func (s *runtimeSuite) TestRunImageOwnerAndSnapshot() {
	s.prepareImage()
	_, err := s.runtime.RunImage(context.Background(), s.request)
	s.Require().NoError(err)
	s.Equal([]string{versionCall, findCall, "prepare", "import", "create"}, s.client.calls)
	s.Equal(s.client.prepared.Reference(), s.client.created.Image)
	s.Equal("service", s.client.created.User)
	s.Equal(&msb.MountOwner{UID: 1000, GID: 1001}, s.client.created.Mounts[0].Policy.Owner)
	s.Equal("1000:1001", s.client.created.Labels[workspaceRemoteUserLabel])
}

func (s *runtimeSuite) TestRunImageNeverReplacesExistingVM() {
	s.client.info = &msb.Info{Name: sandboxName(testWorkspace), Running: true}
	_, err := s.runtime.RunImage(context.Background(), s.request)
	s.Equal(codes.AlreadyExists, status.Code(err))
	s.Equal([]string{versionCall, findCall}, s.client.calls)
	s.True(s.client.info.Running)
}

func (s *runtimeSuite) TestInvalidOwnerPreventsImportAndCreation() {
	s.prepareImage()
	s.request.RemoteUser = "missing-account"
	_, err := s.runtime.RunImage(context.Background(), s.request)
	s.Equal(codes.FailedPrecondition, status.Code(err))
	s.Equal([]string{versionCall, findCall, "prepare"}, s.client.calls)
}

func (s *runtimeSuite) TestInvalidMountAndGPUPreventRuntimeCalls() {
	s.request.WorkspaceMount.Source = ""
	_, err := s.runtime.RunImage(context.Background(), s.request)
	s.Equal(codes.InvalidArgument, status.Code(err))
	s.Empty(s.client.calls)
	s.request.WorkspaceMount.Source = testHostPath
	s.request.HostRequirements = &runtimev1.HostRequirements{
		Gpu: &runtimev1.GpuRequirement{Value: runtimev1.GpuRequirementValue_GPU_REQUIRED},
	}
	_, err = s.runtime.RunImage(context.Background(), s.request)
	s.Equal(codes.Unimplemented, status.Code(err))
	s.Empty(s.client.calls)
}

func (s *runtimeSuite) TestLifecycleIdempotence() {
	ctx := context.Background()
	s.client.info = &msb.Info{Running: true}
	_, err := s.runtime.Start(ctx, &runtimev1.StartRequest{WorkspaceId: testWorkspace})
	s.NoError(err)
	s.Equal([]string{findCall}, s.client.calls)
	s.client.calls = nil
	_, err = s.runtime.Stop(ctx, &runtimev1.StopRequest{WorkspaceId: testWorkspace})
	s.NoError(err)
	s.Equal([]string{findCall, stopCall}, s.client.calls)
	s.client.calls = nil
	_, err = s.runtime.Stop(ctx, &runtimev1.StopRequest{WorkspaceId: testWorkspace})
	s.NoError(err)
	s.Equal([]string{findCall}, s.client.calls)
	s.client.calls = nil
	_, err = s.runtime.Delete(ctx, &runtimev1.DeleteRequest{WorkspaceId: testWorkspace})
	s.NoError(err)
	s.Equal([]string{findCall, "remove"}, s.client.calls)
	s.client.calls = nil
	_, err = s.runtime.Delete(ctx, &runtimev1.DeleteRequest{WorkspaceId: testWorkspace})
	s.NoError(err)
	s.Equal([]string{findCall}, s.client.calls)
	_, err = s.runtime.Start(ctx, &runtimev1.StartRequest{WorkspaceId: testWorkspace})
	s.Equal(codes.NotFound, status.Code(err))
}

func (s *runtimeSuite) TestDeleteStopFailurePreservesVM() {
	s.client.info = &msb.Info{Running: true}
	s.client.fail = stopCall
	_, err := s.runtime.Delete(
		context.Background(),
		&runtimev1.DeleteRequest{WorkspaceId: testWorkspace},
	)
	s.Equal(codes.Internal, status.Code(err))
	s.Equal([]string{findCall, stopCall}, s.client.calls)
	s.True(s.client.info.Running)
}

func (s *runtimeSuite) TestFindPreservesFailureAndMetadata() {
	s.client.info = &msb.Info{
		Mounts:    []msb.Mount{{Source: testHostPath, Target: "/workspace"}},
		Name:      "vm",
		Running:   true,
		CreatedAt: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Labels:    map[string]string{userLabel: "service"},
	}
	result, err := s.runtime.Find(
		context.Background(),
		&runtimev1.FindRequest{WorkspaceId: testWorkspace},
	)
	s.Require().NoError(err)
	s.True(result.GetFound())
	s.Equal("2026-01-01T00:00:00Z", result.GetContainer().GetCreatedAt())
	s.Require().Len(result.GetContainer().GetMounts(), 1)
	s.Equal("/workspace", result.GetContainer().GetMounts()[0].GetDestination())
	s.Equal("running", result.GetContainer().GetState().GetStatus())
	s.Equal("service", result.GetContainer().GetConfig().GetUser())
	s.client.fail = findCall
	_, err = s.runtime.Find(
		context.Background(),
		&runtimev1.FindRequest{WorkspaceId: testWorkspace},
	)
	s.Equal(codes.Internal, status.Code(err))
	s.NotEmpty(status.Convert(err).Details())
}

func (s *runtimeSuite) TestFindUnknownCreationTime() {
	s.client.info = &msb.Info{Name: "vm"}
	found, err := s.runtime.Find(
		context.Background(),
		&runtimev1.FindRequest{WorkspaceId: testWorkspace},
	)
	s.Require().NoError(err)
	s.Empty(found.GetContainer().GetCreatedAt())
}

func (s *runtimeSuite) TestCancellationWhileLifecycleBusy() {
	s.client.prepareStarted = make(chan struct{})
	runCtx, stopRun := context.WithCancel(context.Background())
	defer stopRun()
	runDone := make(chan error, 1)
	go func() { _, err := s.runtime.RunImage(runCtx, s.request); runDone <- err }()
	select {
	case <-s.client.prepareStarted:
	case <-time.After(5 * time.Second):
		s.FailNow("image preparation did not start")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	findDone := make(chan error, 1)
	go func() {
		_, err := s.runtime.Find(ctx, &runtimev1.FindRequest{WorkspaceId: testWorkspace})
		findDone <- err
	}()
	select {
	case err := <-findDone:
		s.Equal(codes.Canceled, status.Code(err))
	case <-time.After(5 * time.Second):
		s.FailNow("canceled Find waited for image preparation")
	}
	stopRun()
	s.Equal(codes.Canceled, status.Code(<-runDone))
}

func (s *runtimeSuite) TestResourcePrecedenceAndOverflow() {
	s.request.HostRequirements = &runtimev1.HostRequirements{
		Cpus:         2,
		MemoryBytes:  1<<20 + 1,
		StorageBytes: math.MaxUint64,
	}
	spec, err := s.runtime.buildSpec(context.Background(), s.request)
	s.Require().NoError(err)
	s.Equal(uint32(2), spec.Memory)
	s.Equal(uint32(math.MaxUint32), spec.RootDiskGB)
	s.runtime.config.Defaults.Memory = 512
	spec, err = s.runtime.buildSpec(context.Background(), s.request)
	s.Require().NoError(err)
	s.Equal(uint32(512), spec.Memory)
}

func (s *runtimeSuite) TestSandboxNameStability() {
	s.Equal("devsy-"+testWorkspace, sandboxName(testWorkspace))
	first := sandboxName(strings.Repeat("a", 200))
	second := sandboxName(strings.Repeat("a", 199) + "b")
	s.Len(first, maxSandboxNameLen)
	s.NotEqual(first, second)
}

func (s *runtimeSuite) TestWorkspacePolicyDoesNotAffectAdditionalMounts() {
	s.request.Mounts = []*runtimev1.Mount{
		{
			Type:     runtimev1.MountType_MOUNT_TYPE_BIND,
			Source:   "/extra",
			Target:   "/extra",
			ReadOnly: true,
		},
		{Type: runtimev1.MountType_MOUNT_TYPE_VOLUME, Source: "cache", Target: "/cache"},
		{Type: runtimev1.MountType_MOUNT_TYPE_TMPFS, Target: "/tmp"},
	}
	spec, err := s.runtime.buildSpec(context.Background(), s.request)
	s.Require().NoError(err)
	s.Equal(msb.StatStrict, spec.Mounts[0].Policy.StatVirtualization)
	s.Empty(spec.Mounts[1].Policy.StatVirtualization)
	s.Nil(spec.Mounts[1].Policy.Owner)
	s.True(spec.Mounts[1].ReadOnly)
	s.Equal("cache", spec.Mounts[2].Volume)
	s.True(spec.Mounts[3].Tmpfs)
	s.request.User = ""
	s.request.Labels = []string{userLabel + "=image-user"}
	spec, err = s.runtime.buildSpec(context.Background(), s.request)
	s.Require().NoError(err)
	s.Equal("image-user", spec.Labels[userLabel])
}

func (s *runtimeSuite) prepareImage() {
	server := httptest.NewServer(registry.New())
	s.T().Cleanup(server.Close)
	ref, err := name.ParseReference(strings.TrimPrefix(server.URL, "http://") + "/image:test")
	s.Require().NoError(err)
	s.Require().NoError(remote.Write(ref, empty.Image))
	prepared, err := (msb.Client{DockerBinary: "missing-test-docker"}).PrepareImage(
		context.Background(),
		ref.Name(),
		false,
	)
	s.Require().NoError(err)
	s.T().Cleanup(func() { s.Require().NoError(prepared.Close()) })
	s.client.prepared = prepared
}

type fakeClient struct {
	exitCode       int
	exec           func(context.Context, msb.ExecRequest) error
	logs           func(context.Context, io.Writer) error
	prepareStarted chan struct{}
	calls          []string
	version        string
	info           *msb.Info
	prepared       *msb.PreparedImage
	created        msb.Spec
	fail           string
}

func (c *fakeClient) EnsureInstalled(context.Context) error { return c.record("installed") }

func (c *fakeClient) Version(
	context.Context,
) (string, error) {
	return c.version, c.record(versionCall)
}

func (c *fakeClient) Find(context.Context, string) (*msb.Info, error) {
	return c.info, c.record(findCall)
}

func (c *fakeClient) PrepareImage(
	ctx context.Context,
	_ string,
	_ bool,
) (*msb.PreparedImage, error) {
	if err := c.record("prepare"); err != nil {
		return nil, err
	}
	if c.prepareStarted != nil {
		close(c.prepareStarted)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return c.prepared, nil
}

func (c *fakeClient) EnsureImage(context.Context, *msb.PreparedImage) error {
	return c.record("import")
}

func (c *fakeClient) Create(_ context.Context, _ string, spec msb.Spec) error {
	c.created = spec
	return c.record("create")
}

func (c *fakeClient) Start(context.Context, string) error {
	if err := c.record("start"); err != nil {
		return err
	}
	c.info.Running = true
	return nil
}

func (c *fakeClient) Stop(context.Context, string) error {
	if err := c.record(stopCall); err != nil {
		return err
	}
	c.info.Running = false
	return nil
}

func (c *fakeClient) Remove(context.Context, string) error {
	if err := c.record("remove"); err != nil {
		return err
	}
	c.info = nil
	return nil
}

func (c *fakeClient) Execute(ctx context.Context, _ string, req msb.ExecRequest) (int, error) {
	return c.exitCode, c.exec(ctx, req)
}

func (c *fakeClient) Logs(
	ctx context.Context,
	_ string,
	w io.Writer,
) error {
	return c.logs(ctx, w)
}

func (c *fakeClient) record(call string) error {
	c.calls = append(c.calls, call)
	if c.fail == call {
		return errBackend
	}
	return nil
}
