package msb

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/stretchr/testify/suite"
)

const (
	numericIdentity   = "1000:1001"
	numericNamedGroup = "1000:developers"
	accountLinkTarget = "other"
	developerGroup    = "developers:x:1001:\n"
	//nolint:gosec // Synthetic account fixture contains no credentials.
	snapshotPasswd = "vscode:x:2000:2001:dev:/home/vscode:/bin/sh\n"
)

type imageUserSuite struct{ suite.Suite }

func TestImageUser(t *testing.T) { suite.Run(t, new(imageUserSuite)) }

func (s *imageUserSuite) TestResolution() {
	img, err := mutate.AppendLayers(empty.Image, s.layer(map[string]string{
		passwdPath: "invalid-row\nroot:x:0:0:root:/root:/bin/sh\nvscode:x:1000:1002:dev:/home/vscode:/bin/sh\n",
		groupPath:  "developers:x:1001:vscode\n",
	}))
	s.Require().NoError(err)
	for _, tt := range []struct {
		user string
		want MountOwner
	}{
		{rootUser, MountOwner{}},
		{"0", MountOwner{}},
		{testUser, MountOwner{1000, 1002}},
		{"1000", MountOwner{1000, 1002}},
		{numericIdentity, MountOwner{1000, 1001}},
		{"vscode:developers", MountOwner{1000, 1001}},
		{"vscode:1003", MountOwner{1000, 1003}},
		{numericNamedGroup, MountOwner{1000, 1001}},
	} {
		s.Run(tt.user, func() {
			owner, err := ownerFromImage(context.Background(), img, tt.user)
			s.Require().NoError(err)
			s.Equal(tt.want, *owner)
		})
	}
	for _, user := range []string{
		"missing", "1009", "vscode:missing", "4294967296:1", "vscode:4294967296", "", "vscode:", "1:2:3",
	} {
		s.Run(
			user,
			func() { _, err := ownerFromImage(context.Background(), img, user); s.Error(err) },
		)
	}
}

func (s *imageUserSuite) TestMissingAccounts() {
	owner, err := ownerFromImage(context.Background(), empty.Image, numericIdentity)
	s.Require().NoError(err)
	s.Equal(MountOwner{1000, 1001}, *owner)
	_, err = ownerFromImage(context.Background(), empty.Image, testUser)
	s.ErrorContains(err, "not found in final image /etc/passwd")
}

//nolint:gosec // Synthetic account rows contain no credentials.
func (s *imageUserSuite) TestFinalLayerOverridesAndWhiteouts() {
	base := s.layer(
		map[string]string{
			passwdPath: "vscode:x:10:11:dev:/home/vscode:/bin/sh\n",
			groupPath:  "old:x:99:\n",
		},
	)
	final := s.layer(
		map[string]string{
			passwdPath:      snapshotPasswd,
			"etc/.wh.group": "",
		},
	)
	img, err := mutate.AppendLayers(empty.Image, base, final)
	s.Require().NoError(err)
	owner, err := ownerFromImage(context.Background(), img, testUser)
	s.Require().NoError(err)
	s.Equal(MountOwner{2000, 2001}, *owner)
	_, err = ownerFromImage(context.Background(), img, "vscode:old")
	s.ErrorContains(err, "not found")
}

func (s *imageUserSuite) TestMalformedAccount() {
	for _, passwd := range []string{
		"vscode:x:no:1000:dev:/home/vscode:/bin/sh\n",
		"vscode:x:4294967296:1000:dev:/home/vscode:/bin/sh\n",
	} {
		_, err := ownerFromAccounts(testUser, passwd, "")
		s.ErrorContains(err, "invalid account IDs")
	}
}

func (s *imageUserSuite) TestBoundedAccountFile() {
	img, err := mutate.AppendLayers(
		empty.Image,
		s.layer(map[string]string{passwdPath: strings.Repeat("x", maxAccountFileSize+1)}),
	)
	s.Require().NoError(err)
	_, err = ownerFromImage(context.Background(), img, testUser)
	s.ErrorContains(err, "size limit")
}

func (s *imageUserSuite) TestCanceledExtraction() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := ownerFromImage(ctx, empty.Image, testUser)
	s.ErrorIs(err, context.Canceled)
}

func (s *imageUserSuite) TestWorkspaceDeveloperIdentity() {
	prepared := s.preparedImage(s.layer(map[string]string{
		passwdPath: "root:x:0:0:root:/root:/bin/sh\n" + snapshotPasswd,
	}))
	for _, tt := range []struct {
		user, remote string
		want         MountOwner
	}{
		{rootUser, testUser, MountOwner{2000, 2001}},
		{testUser, "", MountOwner{2000, 2001}},
		{"", "", MountOwner{}},
		{rootUser, numericIdentity, MountOwner{1000, 1001}},
	} {
		s.Run(tt.user+"/"+tt.remote, func() {
			options := workspaceOwnerOptions()
			options.User, options.RemoteUser = tt.user, tt.remote
			owner, err := ResolveWorkspaceOwner(context.Background(), prepared, options)
			s.Require().NoError(err)
			s.Equal(tt.want, *owner)
			s.Equal(tt.user, options.User)
		})
	}
}

func (s *imageUserSuite) TestWorkspaceWithoutVirtualizedBindSkipsImage() {
	for _, mount := range []*Mount{
		nil,
		{},
		{Source: testBindSrc},
		{Target: testBindDst},
		{Volume: testVolume, Target: testBindDst},
		{Tmpfs: true, Target: testBindDst},
		{Source: testBindSrc, Target: testBindDst, Volume: testVolume},
		{Source: testBindSrc, Target: testBindDst, Tmpfs: true},
		{Source: testBindSrc, Target: testBindDst, Policy: MountPolicy{
			StatVirtualization: StatOff, HostPermissions: HostPrivate,
		}},
	} {
		options := WorkspaceOwnerOptions{
			RemoteUser:     testUser,
			Dockerless:     true,
			WorkspaceMount: mount,
		}
		owner, err := ResolveWorkspaceOwner(context.Background(), nil, options)
		s.NoError(err)
		s.Nil(owner)
	}
}

func (s *imageUserSuite) TestDockerlessDoesNotUseRunnerAccounts() {
	for _, user := range []string{testUser, "1000", numericIdentity, "0", rootUser} {
		s.Run(user, func() {
			options := workspaceOwnerOptions()
			options.User, options.RemoteUser, options.Dockerless = rootUser, user, true
			owner, err := ResolveWorkspaceOwner(context.Background(), nil, options)
			if user == rootUser {
				s.Require().NoError(err)
				s.Equal(MountOwner{}, *owner)
			} else {
				s.ErrorContains(err, "non-root Dockerless workspace owner")
				s.Nil(owner)
			}
		})
	}
}

func (s *imageUserSuite) TestNumericOwnerNeedsNoAccounts() {
	for _, user := range []string{rootUser, "0", numericIdentity, "4294967295:4294967295"} {
		options := workspaceOwnerOptions()
		options.RemoteUser = user
		owner, err := ResolveWorkspaceOwner(context.Background(), nil, options)
		s.Require().NoError(err)
		s.NotNil(owner)
	}
}

func (s *imageUserSuite) TestInvalidPreparedImageAndIdentity() {
	options := workspaceOwnerOptions()
	for _, prepared := range []*PreparedImage{nil, {}, {image: empty.Image}} {
		_, err := ResolveWorkspaceOwner(context.Background(), prepared, options)
		s.ErrorContains(err, "requires a prepared image")
	}
	for _, user := range []string{"vscode:", ":1000", "1:2:3", "4294967296:1"} {
		options.RemoteUser = user
		_, err := ResolveWorkspaceOwner(context.Background(), nil, options)
		s.Error(err)
	}
}

func (s *imageUserSuite) TestWorkspaceCancellationIncludesExplicitOwners() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	options := workspaceOwnerOptions()
	options.RemoteUser = numericIdentity
	_, err := ResolveWorkspaceOwner(ctx, nil, options)
	s.ErrorIs(err, context.Canceled)
}

func (s *imageUserSuite) TestNonRegularAccountFile() {
	for _, kind := range []byte{tar.TypeSymlink, tar.TypeLink, tar.TypeDir} {
		_, err := readAccountFile(strings.NewReader(snapshotPasswd), &tar.Header{
			Name: passwdPath, Typeflag: kind, Linkname: accountLinkTarget,
		})
		s.ErrorContains(err, "not a regular account file")
	}
}

func (s *imageUserSuite) TestMalformedGroup() {
	for _, groups := range []string{"developers:x:no:\n", "developers:x:4294967296:\n"} {
		_, err := ownerFromAccounts(numericNamedGroup, "", groups)
		s.ErrorContains(err, "invalid group ID")
	}
}

func (s *imageUserSuite) TestOwnerAndImportShareLocalSnapshot() {
	binary, err := os.Executable()
	s.Require().NoError(err)
	dir := s.T().TempDir()
	archive, record := filepath.Join(dir, "source.tar"), filepath.Join(dir, "args.json")
	tag, err := name.NewTag(testImg)
	s.Require().NoError(err)
	original := s.preparedImage(s.layer(map[string]string{passwdPath: snapshotPasswd}))
	s.Require().NoError(tarball.WriteToFile(archive, tag, original.Image()))
	s.T().Setenv(helperEnv, "1")
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeDocker)
	s.T().Setenv("DEVSY_MSB_TEST_RECORD", record)
	s.T().Setenv("DEVSY_MSB_TEST_IMAGE_ARCHIVE", archive)
	client := Client{Binary: binary, DockerBinary: binary}
	prepared, err := client.PrepareImage(context.Background(), testImg, true)
	s.Require().NoError(err)
	defer func() { _ = prepared.Close() }()
	s.Require().NoError(tarball.WriteToFile(archive, tag, empty.Image))
	options := workspaceOwnerOptions()
	owner, err := ResolveWorkspaceOwner(context.Background(), prepared, options)
	s.Require().NoError(err)
	s.Equal(MountOwner{2000, 2001}, *owner)
	s.Require().NoError(client.EnsureImage(context.Background(), prepared))
	imported, err := tarball.ImageFromPath(record+".tar", nil)
	s.Require().NoError(err)
	importedOwner, err := ownerFromImage(context.Background(), imported, testUser)
	s.Require().NoError(err)
	s.Equal(owner, importedOwner)
	options.WorkspaceMount.Policy.Owner = owner
	s.Equal(testBindSrc+":"+testBindDst+":stat-virt=strict,host-perms=mirror,uid=2000,gid=2001",
		bindMountSpec(*options.WorkspaceMount))
}

//nolint:gosec // Synthetic account rows contain no credentials.
func (s *imageUserSuite) TestRootWithGroupUsesImageAccount() {
	prepared := s.preparedImage(s.layer(map[string]string{
		passwdPath: "root:x:42:43:root:/root:/bin/sh\n",
		groupPath:  developerGroup,
	}))
	options := workspaceOwnerOptions()
	for _, user := range []string{"root:developers", "root:1001"} {
		options.RemoteUser = user
		owner, err := ResolveWorkspaceOwner(context.Background(), prepared, options)
		s.Require().NoError(err)
		s.Equal(MountOwner{42, 1001}, *owner)
	}
	options.RemoteUser = "root:1001"
	_, err := ResolveWorkspaceOwner(context.Background(), s.preparedImage(), options)
	s.ErrorContains(err, "not found in final image /etc/passwd")
}

func (s *imageUserSuite) TestNumericUserWithNamedGroupNeedsNoPasswd() {
	prepared := s.preparedImage(s.layer(map[string]string{groupPath: developerGroup}))
	options := workspaceOwnerOptions()
	options.RemoteUser = numericNamedGroup
	owner, err := ResolveWorkspaceOwner(context.Background(), prepared, options)
	s.Require().NoError(err)
	s.Equal(MountOwner{1000, 1001}, *owner)
}

func (s *imageUserSuite) TestCancellationDuringLayerRetrieval() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prepared := s.preparedImage(s.layer(map[string]string{passwdPath: snapshotPasswd}))
	prepared.image = cancelingImage{Image: prepared.image, cancel: cancel}
	_, err := ResolveWorkspaceOwner(ctx, prepared, workspaceOwnerOptions())
	s.ErrorIs(err, context.Canceled)
}

func (s *imageUserSuite) TestUnsafeAccountLinkDoesNotExposeLowerLayer() {
	layer := s.headerLayer(&tar.Header{
		Name: passwdPath, Typeflag: tar.TypeSymlink, Linkname: "../../outside",
	})
	prepared := s.preparedImage(s.layer(map[string]string{passwdPath: snapshotPasswd}), layer)
	_, err := ResolveWorkspaceOwner(context.Background(), prepared, workspaceOwnerOptions())
	s.ErrorContains(err, "not a regular account file")
}

func (s *imageUserSuite) TestOpaqueAccountDirectory() {
	for _, marker := range []string{".wh.etc", ".wh..wh..opq", "etc/.wh..wh..opq"} {
		s.Run(marker, func() {
			base := s.layer(map[string]string{passwdPath: snapshotPasswd, groupPath: "old:x:99:\n"})
			prepared := s.preparedImage(base, s.layer(map[string]string{marker: ""}))
			_, err := ResolveWorkspaceOwner(context.Background(), prepared, workspaceOwnerOptions())
			s.ErrorContains(err, "not found")
			prepared = s.preparedImage(
				base,
				s.layer(map[string]string{marker: "", passwdPath: snapshotPasswd}),
			)
			owner, err := ResolveWorkspaceOwner(
				context.Background(),
				prepared,
				workspaceOwnerOptions(),
			)
			s.Require().NoError(err)
			s.Equal(MountOwner{2000, 2001}, *owner)
			options := workspaceOwnerOptions()
			options.RemoteUser = "vscode:old"
			_, err = ResolveWorkspaceOwner(context.Background(), prepared, options)
			s.ErrorContains(err, "not found")
		})
	}
}

func (s *imageUserSuite) TestLowerAccountLinkHiddenByRegularReplacement() {
	layer := s.headerLayer(&tar.Header{
		Name: passwdPath, Typeflag: tar.TypeSymlink, Linkname: "../../outside",
	})
	prepared := s.preparedImage(layer, s.layer(map[string]string{passwdPath: snapshotPasswd}))
	owner, err := ResolveWorkspaceOwner(context.Background(), prepared, workspaceOwnerOptions())
	s.Require().NoError(err)
	s.Equal(MountOwner{2000, 2001}, *owner)
}

func (s *imageUserSuite) TestAccountDirectoryLinkRejected() {
	layer := s.headerLayer(&tar.Header{
		Name: "etc", Typeflag: tar.TypeSymlink, Linkname: accountLinkTarget,
	})
	prepared := s.preparedImage(s.layer(map[string]string{passwdPath: snapshotPasswd}), layer)
	_, err := ResolveWorkspaceOwner(context.Background(), prepared, workspaceOwnerOptions())
	s.ErrorContains(err, "not a regular account directory")
}

func (s *imageUserSuite) TestUnrelatedAccountFileDoesNotBlockResolution() {
	for _, user := range []string{testUser, numericNamedGroup} {
		s.Run(user, func() {
			unrelated := groupPath
			expected := MountOwner{2000, 2001}
			if user != testUser {
				unrelated = passwdPath
				expected = MountOwner{1000, 1001}
			}
			base := s.layer(
				map[string]string{passwdPath: snapshotPasswd, groupPath: developerGroup},
			)
			for _, bad := range []v1.Layer{
				s.headerLayer(&tar.Header{Name: unrelated, Typeflag: tar.TypeSymlink, Linkname: accountLinkTarget}),
				s.layer(map[string]string{unrelated: strings.Repeat("x", maxAccountFileSize+1)}),
			} {
				prepared := s.preparedImage(base, bad)
				options := workspaceOwnerOptions()
				options.RemoteUser = user
				owner, err := ResolveWorkspaceOwner(context.Background(), prepared, options)
				s.Require().NoError(err)
				s.Equal(expected, *owner)
			}
		})
	}
}

func (s *imageUserSuite) preparedImage(layers ...v1.Layer) *PreparedImage {
	img, err := mutate.AppendLayers(empty.Image, layers...)
	s.Require().NoError(err)
	return &PreparedImage{image: img, reference: "devsy-msb-image:test"}
}

func workspaceOwnerOptions() WorkspaceOwnerOptions {
	return WorkspaceOwnerOptions{
		RemoteUser: testUser,
		WorkspaceMount: &Mount{Source: testBindSrc, Target: testBindDst, Policy: MountPolicy{
			StatVirtualization: StatStrict, HostPermissions: HostMirror,
		}},
	}
}

func (s *imageUserSuite) layer(files map[string]string) v1.Layer {
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	for filename, data := range files {
		s.Require().
			NoError(writer.WriteHeader(&tar.Header{Name: filename, Mode: 0o644, Size: int64(len(data))}))
		_, err := writer.Write([]byte(data))
		s.Require().NoError(err)
	}
	s.Require().NoError(writer.Close())
	layer, err := tarball.LayerFromOpener(
		func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(buf.Bytes())), nil },
	)
	s.Require().NoError(err)
	return layer
}

type cancelingImage struct {
	v1.Image
	cancel context.CancelFunc
}

func (img cancelingImage) Layers() ([]v1.Layer, error) {
	img.cancel()
	return img.Image.Layers()
}

func (s *imageUserSuite) headerLayer(header *tar.Header) v1.Layer {
	var buf bytes.Buffer
	writer := tar.NewWriter(&buf)
	s.Require().NoError(writer.WriteHeader(header))
	s.Require().NoError(writer.Close())
	layer, err := tarball.LayerFromOpener(func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(buf.Bytes())), nil
	})
	s.Require().NoError(err)
	return layer
}
