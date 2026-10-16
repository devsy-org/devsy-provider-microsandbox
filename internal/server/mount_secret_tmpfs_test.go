package server

import (
	"context"
	"strconv"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeSuite) TestDevsyManagedSecretTmpfs() {
	for _, test := range []struct{ target, option string }{
		{"/run/devsy/secrets-env", "tmpfs-mode=0700"},
		{"/run/secrets", "tmpfs-mode=0755"},
	} {
		s.Run(test.target, func() {
			s.SetupTest()
			s.prepareImage()
			s.request.Mounts = []*runtimev1.Mount{{
				Type: runtimev1.MountType_MOUNT_TYPE_TMPFS, Target: test.target,
				Options: []string{test.option},
			}}
			_, err := s.runtime.RunImage(context.Background(), s.request)
			s.Require().NoError(err)
			s.Equal(
				[]string{
					versionCall,
					findCall,
					recreatePrepareCall,
					recreateImportCall,
					recreateCreateCall,
				},
				s.client.calls,
			)
			s.Require().Len(s.client.created.Mounts, 2)
			s.Equal(msb.Mount{Target: test.target, Tmpfs: true}, s.client.created.Mounts[1])
		})
	}
}

func (s *runtimeSuite) TestInvalidSecretTmpfsPreservesRecreateTarget() {
	for _, running := range []bool{false, true} {
		for _, test := range []struct {
			name   string
			mutate func(*runtimev1.Mount)
		}{
			{"arbitrary target", func(m *runtimev1.Mount) { m.Target = "/custom-secrets" }},
			{"environment mode mismatch", func(m *runtimev1.Mount) { m.Options = []string{"tmpfs-mode=0755"} }},
			{"file mode mismatch", func(m *runtimev1.Mount) { m.Target = "/run/secrets" }},
			{"other mode", func(m *runtimev1.Mount) { m.Options = []string{"tmpfs-mode=0777"} }},
			{"nonempty source", func(m *runtimev1.Mount) { m.Source = "/host-secrets" }},
			{"read only field", func(m *runtimev1.Mount) { m.ReadOnly = true }},
			{"read only option", func(m *runtimev1.Mount) { m.Options = append(m.Options, "ro") }},
			{"extra option", func(m *runtimev1.Mount) { m.Options = append(m.Options, "tmpfs-size=64m") }},
			{"duplicate option", func(m *runtimev1.Mount) { m.Options = append(m.Options, m.Options[0]) }},
			{"bind mount", func(m *runtimev1.Mount) {
				m.Type = runtimev1.MountType_MOUNT_TYPE_BIND
				m.Source = "/host-secrets"
			}},
			{"named volume", func(m *runtimev1.Mount) { m.Type = runtimev1.MountType_MOUNT_TYPE_VOLUME; m.Source = "secrets" }},
		} {
			s.Run("running="+strconv.FormatBool(running)+"/"+test.name, func() {
				s.SetupTest()
				s.client.version = recreateRuntimeVersion
				s.request.AllowRecreate = true
				before := s.seedRecreateTarget(running)
				mount := &runtimev1.Mount{
					Type:   runtimev1.MountType_MOUNT_TYPE_TMPFS,
					Target: "/run/devsy/secrets-env", Options: []string{"tmpfs-mode=0700"},
				}
				test.mutate(mount)
				s.request.Mounts = []*runtimev1.Mount{mount}
				_, err := s.runtime.RunImage(context.Background(), s.request)
				s.Equal(codes.InvalidArgument, status.Code(err))
				s.Equal(before, *s.client.info)
				s.Empty(s.client.calls)
			})
		}
	}
}
