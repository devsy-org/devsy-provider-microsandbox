package msb

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
)

func (s *clientSuite) TestLocalSnapshotSurvivesSourceChange() {
	s.installDockerHelper()
	tag, err := name.NewTag(testImg)
	s.Require().NoError(err)
	s.Require().
		NoError(tarball.WriteToFile(s.archive, tag, s.layeredImage()))
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeDocker)
	prepared, err := s.client.PrepareImage(context.Background(), testImg, true)
	s.Require().NoError(err)
	defer func() { _ = prepared.Close() }()
	digest, err := prepared.Image().Digest()
	s.Require().NoError(err)
	s.Equal("devsy-msb-image:"+digest.Hex, prepared.Reference())
	s.Equal([]string{imageSave, testImg}, s.args())
	// Replacing the mutable source must not affect validation or runtime import.
	s.Require().
		NoError(os.WriteFile(s.archive, []byte("changed"), 0o600))
	s.Require().NoError(s.client.EnsureImage(context.Background(), prepared))
	s.Equal([]string{imageLoad, "-t", prepared.Reference()}, s.args())
	s.assertImportedDigest(digest)
	s.Require().NoError(prepared.Close())
	_, err = os.Stat(prepared.archive)
	s.ErrorIs(err, os.ErrNotExist)
	s.NoError(prepared.Close())
}

func (s *clientSuite) TestConfiguredDockerBinary() {
	s.installDockerHelper()
	s.T().Setenv("PATH", s.T().TempDir())
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeDocker)
	s.client.DockerBinary = s.client.Binary
	prepared, err := s.client.PrepareImage(context.Background(), testImg, true)
	s.Require().NoError(err)
	defer func() { _ = prepared.Close() }()
	s.Equal([]string{imageSave, testImg}, s.args())
	s.Require().NoError(s.client.EnsureImage(context.Background(), prepared))
	digest, err := prepared.Image().Digest()
	s.Require().NoError(err)
	s.assertImportedDigest(digest)
}

func (s *clientSuite) TestRegistrySnapshotSurvivesTagChange() {
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	defer server.Close()
	ref, err := name.ParseReference(
		server.Listener.Addr().String()+"/snapshot:latest",
		name.Insecure,
	)
	s.Require().NoError(err)
	image := s.layeredImage()
	s.Require().NoError(remote.Write(ref, image))
	s.T().Setenv("PATH", s.T().TempDir())
	s.T().Setenv("DOCKER_CONFIG", s.T().TempDir())
	prepareCtx, cancelPrepare := context.WithCancel(context.Background())
	defer cancelPrepare()
	prepared, err := s.client.PrepareImage(prepareCtx, ref.Name(), false)
	s.Require().NoError(err)
	defer func() { _ = prepared.Close() }()
	digest, err := image.Digest()
	s.Require().NoError(err)
	s.Require().NoError(remote.Write(ref, empty.Image))
	cancelPrepare()
	server.Close()
	importCtx, cancelImport := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelImport()
	s.Require().NoError(s.client.EnsureImage(importCtx, prepared))
	s.Equal([]string{imageLoad, "-t", "devsy-msb-image:" + digest.Hex}, s.args())
	s.assertImportedDigest(digest)
}

func (s *clientSuite) TestLocalSnapshotFailureCleansArchive() {
	for _, mode := range []string{modeSaveFail, modeDocker} {
		s.Run(mode, func() {
			s.installDockerHelper()
			temporary := s.T().TempDir()
			for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
				s.T().Setenv(key, temporary)
			}
			s.T().Setenv("DEVSY_MSB_TEST_MODE", mode)
			s.Require().
				NoError(os.WriteFile(s.archive, []byte("invalid"), 0o600))
			prepared, err := s.client.PrepareImage(context.Background(), testImg, true)
			s.Error(err)
			s.Nil(prepared)
			files, readErr := os.ReadDir(temporary)
			s.Require().NoError(readErr)
			s.Empty(files)
		})
	}
}

func (s *clientSuite) TestImportRejectsUnpreparedImage() {
	for _, prepared := range []*PreparedImage{nil, {}} {
		s.ErrorContains(
			s.client.EnsureImage(context.Background(), prepared),
			"requires a prepared image",
		)
	}
	_, err := os.Stat(s.record)
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *clientSuite) TestImportEarlyExitDoesNotBlockLargeImage() {
	prepared := &PreparedImage{reference: "devsy-msb-image:test", image: s.layeredImage()}
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeLoadFail)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.ErrorContains(s.client.EnsureImage(ctx, prepared), "load rejected")
	s.NoError(ctx.Err())
}

func (s *clientSuite) assertImportedDigest(want v1.Hash) {
	image, err := tarball.ImageFromPath(s.record+".tar", nil)
	s.Require().NoError(err)
	digest, err := image.Digest()
	s.Require().NoError(err)
	s.Equal(want, digest)
}

func (s *clientSuite) layeredImage() v1.Image {
	data := make([]byte, 1024*1024)
	_, err := rand.Read(data)
	s.Require().NoError(err)
	layer, err := tarball.LayerFromOpener(
		func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
	)
	s.Require().NoError(err)
	image, err := mutate.AppendLayers(empty.Image, layer)
	s.Require().NoError(err)
	cfg, err := image.ConfigFile()
	s.Require().NoError(err)
	cfg.OS = testLinux
	image, err = mutate.ConfigFile(image, cfg)
	s.Require().NoError(err)
	return image
}

func (s *clientSuite) TestSnapshotClosePreservesRemovalError() {
	dir := s.T().TempDir()
	s.Require().NoError(os.WriteFile(filepath.Join(dir, "entry"), nil, 0o600))
	prepared := &PreparedImage{archive: dir}
	s.Error(prepared.Close())
}

func (s *clientSuite) TestPreparationCancellationDuringLayerDownload() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var armed atomic.Bool
	handler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if armed.Load() && r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/blobs/") {
			cancel()
			<-r.Context().Done()
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ref, err := name.ParseReference(server.Listener.Addr().String()+"/cancel:latest", name.Insecure)
	s.Require().NoError(err)
	s.Require().NoError(remote.Write(ref, s.layeredImage()))
	s.T().Setenv("PATH", s.T().TempDir())
	s.T().Setenv("DOCKER_CONFIG", s.T().TempDir())
	temporary := s.T().TempDir()
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		s.T().Setenv(key, temporary)
	}
	armed.Store(true)
	prepared, err := s.client.PrepareImage(ctx, ref.Name(), false)
	s.ErrorIs(err, context.Canceled)
	s.Nil(prepared)
	files, readErr := os.ReadDir(temporary)
	s.Require().NoError(readErr)
	s.Empty(files)
}

func (s *clientSuite) TestImportCancellationWithSeparateContext() {
	s.installDockerHelper()
	prepared, err := s.client.PrepareImage(context.Background(), testImg, true)
	s.Require().NoError(err)
	defer func() { _ = prepared.Close() }()
	s.Require().NoError(os.Remove(s.record))
	s.T().Setenv("DEVSY_MSB_TEST_MODE", testWait)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() { result <- s.client.EnsureImage(ctx, prepared) }()
	s.Require().Eventually(func() bool {
		_, err := os.Stat(s.record)
		return err == nil
	}, 5*time.Second, 10*time.Millisecond)
	cancel()
	select {
	case err := <-result:
		s.ErrorIs(err, context.Canceled)
	case <-time.After(5 * time.Second):
		s.Fail("canceled import did not return")
	}
}
