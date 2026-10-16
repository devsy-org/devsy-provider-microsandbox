package server

import (
	"context"
	"strconv"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	consistencyBindSource = "/consistency-extra"
	consistencyBindTarget = "/consistency-data"
)

func (s *runtimeSuite) TestDockerConsistencyHintsPreserveMountContract() {
	for _, option := range []string{
		"consistency='consistent'",
		"consistency=consistent",
		`consistency="consistent"`,
		"consistency=default",
		"consistency=cached",
		"consistency='delegated'",
	} {
		s.Run(option, func() {
			s.SetupTest()
			s.prepareImage()
			s.request.WorkspaceMount.Options = []string{option}
			s.request.Mounts = []*runtimev1.Mount{{
				Type:   runtimev1.MountType_MOUNT_TYPE_BIND,
				Source: consistencyBindSource, Target: consistencyBindTarget,
				Options: []string{option, "ro"},
			}}

			_, err := s.runtime.RunImage(context.Background(), s.request)
			s.Require().NoError(err)
			s.Equal([]string{
				versionCall, findCall, recreatePrepareCall, recreateImportCall, recreateCreateCall,
			}, s.client.calls)
			s.Require().Len(s.client.created.Mounts, 2)
			s.Equal(msb.Mount{
				Source: testHostPath, Target: s.request.WorkspaceMount.Target,
				Policy: msb.MountPolicy{
					StatVirtualization: msb.StatStrict, HostPermissions: msb.HostMirror,
					Owner: &msb.MountOwner{UID: 1000, GID: 1001},
				},
			}, s.client.created.Mounts[0])
			s.Equal(msb.Mount{
				Source: consistencyBindSource, Target: consistencyBindTarget, ReadOnly: true,
			}, s.client.created.Mounts[1])
		})
	}
}

func (s *runtimeSuite) TestUnsupportedMountOptionPreservesRecreateTarget() {
	for _, running := range []bool{false, true} {
		for _, workspace := range []bool{false, true} {
			s.Run(
				"running="+strconv.FormatBool(running)+"/workspace="+strconv.FormatBool(workspace),
				func() {
					s.SetupTest()
					s.client.version = recreateRuntimeVersion
					s.request.AllowRecreate = true
					before := s.seedRecreateTarget(running)
					mount := s.request.WorkspaceMount
					if !workspace {
						mount = &runtimev1.Mount{
							Type:   runtimev1.MountType_MOUNT_TYPE_BIND,
							Source: consistencyBindSource, Target: consistencyBindTarget,
						}
						s.request.Mounts = []*runtimev1.Mount{mount}
					}
					mount.Options = []string{"consistency='consistent'", "bind-propagation=shared"}

					_, err := s.runtime.RunImage(context.Background(), s.request)
					s.Equal(codes.InvalidArgument, status.Code(err))
					s.Contains(err.Error(), "unsupported mount option bind-propagation=shared")
					s.Equal(before, *s.client.info)
					s.Empty(s.client.calls)
				},
			)
		}
	}
}
