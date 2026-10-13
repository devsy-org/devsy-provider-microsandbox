package server

import (
	"context"
	"maps"
	"strconv"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	recreateRuntimeVersion   = "msb 0.7.7"
	recreateOwnershipVersion = "msb 0.7.2"
	recreateCreateCall       = "create"
	recreateInvalidImage     = "image"
	recreatePrepareCall      = "prepare"
	recreateImportCall       = "import"
)

func (s *runtimeSuite) TestRecreateInfoSelectionHasNoSessionState() {
	for _, version := range []string{recreateOwnershipVersion, recreateRuntimeVersion, "unparsed version"} {
		s.client.version = version
		for _, request := range []*runtimev1.InfoRequest{
			nil, {}, {SupportsRecreateOnRun: true}, {}, {SupportsRecreateOnRun: true},
		} {
			info, err := s.runtime.Info(context.Background(), request)
			s.Require().NoError(err)
			s.NoError(runtimev1.ValidateInfo(info))
			want := runtimev1.RecreateMode_RECREATE_MODE_DELETE
			if request.GetSupportsRecreateOnRun() {
				want = runtimev1.RecreateMode_RECREATE_MODE_ON_RUN
			}
			s.Equal(want, info.GetCapabilities().GetRecreateMode())
			info.Capabilities.RecreateMode = runtimev1.RecreateMode_RECREATE_MODE_UNSPECIFIED
		}
	}
}

func (s *runtimeSuite) TestRecreateFalsePreservesRunningAndStoppedVMs() {
	for _, running := range []bool{false, true} {
		s.Run(strconv.FormatBool(running), func() {
			s.SetupTest()
			s.client.version = recreateRuntimeVersion
			s.runtime.config.Defaults.AllowRecreate = true
			_, err := s.runtime.Info(
				context.Background(),
				&runtimev1.InfoRequest{SupportsRecreateOnRun: true},
			)
			s.Require().NoError(err)
			before := s.seedRecreateTarget(running)
			s.client.calls = nil
			_, err = s.runtime.RunImage(context.Background(), s.request)
			s.requireRecreateError(
				err,
				codes.AlreadyExists,
				runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_ALREADY_EXISTS,
			)
			s.Equal(before, *s.client.info)
			s.Equal([]string{versionCall, findCall}, s.client.calls)
		})
	}
}

func (s *runtimeSuite) TestRecreateTrueForwardsWithoutInfoOrExplicitTeardown() {
	for _, running := range []bool{false, true} {
		s.Run(strconv.FormatBool(running), func() {
			s.SetupTest()
			s.client.version = recreateRuntimeVersion
			s.request.AllowRecreate = true
			before := s.seedRecreateTarget(running)
			s.prepareImage()
			_, err := s.runtime.RunImage(context.Background(), s.request)
			s.Require().NoError(err)
			s.True(s.client.created.AllowRecreate)
			s.Equal(s.client.prepared.Reference(), s.client.created.Image)
			s.Equal(before, *s.client.info)
			s.Equal(
				[]string{
					versionCall,
					versionCall,
					findCall,
					recreatePrepareCall,
					recreateImportCall,
					recreateCreateCall,
				},
				s.client.calls,
			)
			s.client.calls = nil
			s.request.AllowRecreate = false
			_, err = s.runtime.RunImage(context.Background(), s.request)
			s.Equal(codes.AlreadyExists, status.Code(err))
			s.Equal(before, *s.client.info)
			s.Equal([]string{versionCall, findCall}, s.client.calls)
		})
	}
}

func (s *runtimeSuite) TestFreshRunOverridesDefaultRecreatePermission() {
	for _, allowed := range []bool{false, true} {
		s.Run(strconv.FormatBool(allowed), func() {
			s.SetupTest()
			s.client.version = recreateRuntimeVersion
			s.runtime.config.Defaults.AllowRecreate = !allowed
			s.request.AllowRecreate = allowed
			s.prepareImage()
			_, err := s.runtime.RunImage(context.Background(), s.request)
			s.Require().NoError(err)
			s.Equal(allowed, s.client.created.AllowRecreate)
		})
	}
}

func (s *runtimeSuite) TestRecreateVersionGateCannotBeBypassedByOffPolicy() {
	for _, version := range []string{recreateOwnershipVersion, "msb 0.7.6", "msb 0.7.7-rc.1", "unknown"} {
		s.Run(version, func() {
			s.SetupTest()
			s.client.version = version
			s.runtime.config.WorkspacePolicy = msb.MountPolicy{
				StatVirtualization: msb.StatOff, HostPermissions: msb.HostPrivate,
			}
			s.request.AllowRecreate = true
			before := s.seedRecreateTarget(true)
			_, err := s.runtime.RunImage(context.Background(), s.request)
			s.requireRecreateError(err, codes.FailedPrecondition,
				runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_INCOMPATIBLE_VERSION)
			s.Equal(before, *s.client.info)
			s.Equal([]string{versionCall}, s.client.calls)
		})
	}
}

func (s *runtimeSuite) TestRecreatePreparationFailuresPreserveTarget() {
	for _, fail := range []string{versionCall, findCall, recreatePrepareCall, recreateImportCall} {
		s.Run(fail, func() {
			s.SetupTest()
			s.client.version = recreateRuntimeVersion
			s.request.AllowRecreate = true
			s.client.fail = fail
			before := s.seedRecreateTarget(true)
			s.prepareImage()
			_, err := s.runtime.RunImage(context.Background(), s.request)
			s.requireRecreateError(
				err,
				codes.Internal,
				runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_RUNTIME_FAILURE,
			)
			s.Equal(before, *s.client.info)
			s.NotContains(s.client.calls, recreateCreateCall)
			s.NotContains(s.client.calls, stopCall)
			s.NotContains(s.client.calls, "remove")
		})
	}
}

func (s *runtimeSuite) TestRecreateInvalidInputPreservesTarget() {
	for _, running := range []bool{false, true} {
		for _, invalid := range []string{recreateInvalidImage, "workspace mount", "additional mount", "CPU", "GPU"} {
			s.Run(strconv.FormatBool(running)+"/"+invalid, func() {
				s.SetupTest()
				s.client.version = recreateRuntimeVersion
				s.request.AllowRecreate = true
				before := s.seedRecreateTarget(running)
				code, detail := s.invalidateRecreateRequest(invalid)
				_, err := s.runtime.RunImage(context.Background(), s.request)
				s.requireRecreateError(err, code, detail)
				s.Equal(before, *s.client.info)
				s.Empty(s.client.calls)
			})
		}
	}
}

func (s *runtimeSuite) invalidateRecreateRequest(
	invalid string,
) (codes.Code, runtimev1.RuntimeErrorCode) {
	s.T().Helper()
	switch invalid {
	case recreateInvalidImage:
		s.request.Image = ""
	case "workspace mount":
		s.request.WorkspaceMount.Source = ""
	case "additional mount":
		s.request.Mounts = []*runtimev1.Mount{{
			Type: runtimev1.MountType_MOUNT_TYPE_BIND, Source: testHostPath,
		}}
	case "CPU":
		s.request.HostRequirements = &runtimev1.HostRequirements{Cpus: -1}
	case "GPU":
		s.request.HostRequirements = &runtimev1.HostRequirements{
			Gpu: &runtimev1.GpuRequirement{Value: runtimev1.GpuRequirementValue_GPU_REQUIRED},
		}
		return codes.Unimplemented, runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_UNSUPPORTED
	default:
		s.T().Fatalf("unknown invalid request case %q", invalid)
	}
	return codes.InvalidArgument, runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_INVALID_ARGUMENT
}

func (s *runtimeSuite) TestRecreateInvalidOwnerPreservesTarget() {
	s.client.version = recreateRuntimeVersion
	s.request.AllowRecreate = true
	s.request.RemoteUser = "missing-account"
	before := s.seedRecreateTarget(false)
	s.prepareImage()
	_, err := s.runtime.RunImage(context.Background(), s.request)
	s.Equal(codes.FailedPrecondition, status.Code(err))
	s.Equal(before, *s.client.info)
	s.Equal([]string{versionCall, versionCall, findCall, recreatePrepareCall}, s.client.calls)
}

func (s *runtimeSuite) TestRecreateCanceledRequestPreservesTarget() {
	s.request.AllowRecreate = true
	before := s.seedRecreateTarget(true)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.runtime.RunImage(ctx, s.request)
	s.Equal(codes.Canceled, status.Code(err))
	s.Equal(before, *s.client.info)
	s.Empty(s.client.calls)
}

func (s *runtimeSuite) seedRecreateTarget(running bool) msb.Info {
	s.T().Helper()
	s.client.info = &msb.Info{
		Name: sandboxName(testWorkspace), Running: running,
		CreatedAt: time.Unix(123, 0), Labels: map[string]string{"generation": "old"},
	}
	before := *s.client.info
	before.Labels = maps.Clone(before.Labels)
	return before
}

func (s *runtimeSuite) requireRecreateError(
	err error,
	code codes.Code,
	detail runtimev1.RuntimeErrorCode,
) {
	s.T().Helper()
	s.Require().Error(err)
	s.Equal(code, status.Code(err))
	details := status.Convert(err).Details()
	s.Require().Len(details, 1)
	actual, ok := details[0].(*runtimev1.RuntimeError)
	s.Require().True(ok)
	s.Equal(detail, actual.GetCode())
}
