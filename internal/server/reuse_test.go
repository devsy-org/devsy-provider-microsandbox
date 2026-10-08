package server

import (
	"context"
	"maps"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	reuseDeveloperUser = "workspace-owner"
	reuseRootUser      = "root"
)

func (s *runtimeSuite) TestReusePreflightPreservesVM() {
	for _, tc := range []struct {
		name, contract, savedUser, user string
		code                            codes.Code
	}{
		{"same policy", s.runtime.mountContract(), reuseDeveloperUser, reuseDeveloperUser, codes.OK},
		{"changed user", s.runtime.mountContract(), reuseDeveloperUser, reuseRootUser, codes.FailedPrecondition},
		{"changed policy", "old-policy", reuseDeveloperUser, reuseDeveloperUser, codes.FailedPrecondition},
		{"missing policy", "", reuseDeveloperUser, reuseDeveloperUser, codes.FailedPrecondition},
		{"no owner", s.runtime.mountContract(), "", reuseRootUser, codes.OK},
		{"missing user", s.runtime.mountContract(), reuseDeveloperUser, "", codes.InvalidArgument},
	} {
		s.Run(tc.name, func() {
			s.client.info = &msb.Info{Running: true, Labels: map[string]string{
				workspaceMountContractLabel: tc.contract, workspaceRemoteUserLabel: tc.savedUser,
			}}
			before := *s.client.info
			before.Labels = maps.Clone(before.Labels)
			s.client.calls = nil
			_, err := s.runtime.ReusePreflight(
				context.Background(),
				&runtimev1.ReusePreflightRequest{
					WorkspaceId: testWorkspace, RemoteUser: tc.user,
				},
			)
			s.Equal(tc.code, status.Code(err))
			if tc.code == codes.FailedPrecondition {
				s.ErrorContains(err, "--recreate")
				s.NotEmpty(status.Convert(err).Details())
			}
			if tc.code == codes.InvalidArgument {
				s.Empty(s.client.calls)
			} else {
				s.Equal([]string{findCall}, s.client.calls)
			}
			s.Equal(before, *s.client.info)
		})
	}
}

func (s *runtimeSuite) TestReusePreflightStatOffAllowsIdentityChange() {
	s.runtime.config.WorkspacePolicy.StatVirtualization = msb.StatOff
	s.client.info = &msb.Info{Running: false, Labels: map[string]string{
		workspaceMountContractLabel: s.runtime.mountContract(),
		workspaceRemoteUserLabel:    "old-user",
	}}
	_, err := s.runtime.ReusePreflight(context.Background(), &runtimev1.ReusePreflightRequest{
		WorkspaceId: testWorkspace, RemoteUser: reuseRootUser,
	})
	s.NoError(err)
	s.Equal([]string{findCall}, s.client.calls)
	s.False(s.client.info.Running)
}

func (s *runtimeSuite) TestReusePreflightPreservesErrors() {
	req := &runtimev1.ReusePreflightRequest{WorkspaceId: testWorkspace, RemoteUser: reuseRootUser}
	_, err := s.runtime.ReusePreflight(context.Background(), req)
	s.Equal(codes.NotFound, status.Code(err))
	s.client.fail = findCall
	_, err = s.runtime.ReusePreflight(context.Background(), req)
	s.Equal(codes.Internal, status.Code(err))
	s.ErrorContains(err, errBackend.Error())
	s.client.calls = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = s.runtime.ReusePreflight(ctx, req)
	s.Equal(codes.Canceled, status.Code(err))
	s.Empty(s.client.calls)
}
