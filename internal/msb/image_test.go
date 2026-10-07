package msb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
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

const (
	testLinux    = "linux"
	modeLoadFail = "load-fail"
	modeDocker   = "docker"
	modeSaveFail = "save-fail"
	imageSave    = "save"
	imageLoad    = "load"
	hostMarker   = "MARKER=host"
)

func (s *clientSuite) TestImageLocalDockerLoad() {
	s.installDockerHelper()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeDocker)
	s.Require().NoError(s.importImage(context.Background(), testImg, false))
	data, err := os.ReadFile(s.record + ".tar")
	s.Require().NoError(err)
	loaded, err := tarball.Image(
		func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil },
		nil,
	)
	s.Require().NoError(err)
	cfg, err := loaded.ConfigFile()
	s.Require().NoError(err)
	s.Equal([]string{hostMarker}, cfg.Config.Env)
}

func (s *clientSuite) TestImageDockerSaveFailure() {
	s.installDockerHelper()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeSaveFail)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.ErrorContains(s.importImage(ctx, testImg, true), "save final local image")
}

func (s *clientSuite) TestImageMissingBuiltImageDoesNotPull() {
	s.T().Setenv("PATH", s.T().TempDir())
	_, err := s.client.PrepareImage(context.Background(), testImg, true)
	s.ErrorContains(err, "save final local image")
	_, err = os.Stat(s.record)
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *clientSuite) TestImageRegistryAuthentication() {
	var requireAuth atomic.Bool
	handler := registry.New(registry.Logger(log.New(io.Discard, "", 0)))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, password, ok := r.BasicAuth()
		if requireAuth.Load() && (!ok || user != "fixture" || password != "fixture-password") {
			w.Header().Set("WWW-Authenticate", `Basic realm="fixture"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(w, r)
	}))
	defer server.Close()
	ref, err := name.ParseReference(
		server.Listener.Addr().String()+"/test/image:latest",
		name.Insecure,
	)
	s.Require().NoError(err)
	s.Require().NoError(remote.WriteIndex(ref, s.registryIndex()))
	s.T().Setenv("PATH", s.T().TempDir())
	dockerConfig := s.T().TempDir()
	configJSON, err := json.Marshal(map[string]any{"auths": map[string]any{
		server.Listener.Addr().String(): map[string]string{
			"auth": base64.StdEncoding.EncodeToString([]byte("fixture:fixture-password")),
		},
	}})
	s.Require().NoError(err)
	s.Require().NoError(os.WriteFile(filepath.Join(dockerConfig, "config.json"), configJSON, 0o600))
	s.T().Setenv("DOCKER_CONFIG", dockerConfig)
	requireAuth.Store(true)
	s.Require().NoError(s.importImage(context.Background(), ref.Name(), false))
	loaded, err := tarball.ImageFromPath(s.record+".tar", nil)
	s.Require().NoError(err)
	config, err := loaded.ConfigFile()
	s.Require().NoError(err)
	s.Equal(testLinux, config.OS)
	s.Equal(runtime.GOARCH, config.Architecture)
	s.Equal([]string{hostMarker}, config.Config.Env)
	args := s.args()
	s.Equal([]string{imageLoad, "-t"}, args[:2])
	s.Contains(args[2], "devsy-msb-image:")
}

func (s *clientSuite) TestImageCanceledContext() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.ErrorIs(s.importImage(ctx, testImg, true), context.Canceled)
	_, err := os.Stat(s.record)
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *clientSuite) TestImageDockerLoadFailure() {
	s.installDockerHelper()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeLoadFail)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.ErrorContains(s.importImage(ctx, testImg, true), "load rejected")
	s.NoError(ctx.Err())
}

func (s *clientSuite) TestImageRegistryCancellation() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		<-r.Context().Done()
	}))
	defer server.Close()
	s.T().Setenv("PATH", s.T().TempDir())
	s.T().Setenv("DOCKER_CONFIG", s.T().TempDir())
	s.ErrorIs(
		s.importImage(ctx, server.Listener.Addr().String()+"/image:latest", false),
		context.Canceled,
	)
	_, err := os.Stat(s.record)
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *clientSuite) importImage(ctx context.Context, ref string, built bool) error {
	prepared, err := s.client.PrepareImage(ctx, ref, built)
	if err != nil {
		return err
	}
	defer func() { _ = prepared.Close() }()
	return s.client.EnsureImage(ctx, prepared)
}

func (s *clientSuite) installDockerHelper() {
	archive := filepath.Join(s.T().TempDir(), "source.tar")
	tag, err := name.NewTag(testImg)
	s.Require().NoError(err)
	img := s.hostImage()
	s.Require().NoError(tarball.WriteToFile(archive, tag, img))
	s.archive = archive
	s.T().Setenv("DEVSY_MSB_TEST_IMAGE_ARCHIVE", archive)
	binary, err := os.ReadFile(s.client.Binary)
	s.Require().NoError(err)
	dir := s.T().TempDir()
	filename := "docker"
	if runtime.GOOS == testWindows {
		filename += ".exe"
	}
	// #nosec G306,G703 -- the test fixture must be executable; its directory is private.
	s.Require().NoError(os.WriteFile(filepath.Join(dir, filename), binary, 0o700))
	s.T().Setenv("PATH", dir)
}

func imageHelper(args []string, mode string) int {
	switch args[0] {
	case "image":
		return dockerInspectHelper(mode)
	case imageSave:
		return saveHelper(mode)
	case imageLoad:
		return loadModeHelper(mode)
	}
	return 0
}

func loadHelper() int {
	input := os.Stdin
	data, err := io.ReadAll(input)
	if err != nil {
		return 1
	}
	// #nosec G703 -- destination is a private path supplied by the parent test.
	if err := os.WriteFile(os.Getenv("DEVSY_MSB_TEST_RECORD")+".tar", data, 0o600); err != nil {
		return 1
	}
	return 0
}

func dockerInspectHelper(mode string) int {
	if mode == modeDocker || mode == modeSaveFail || mode == modeLoadFail {
		return 0
	}
	return 1
}

func loadModeHelper(mode string) int {
	if mode == modeLoadFail {
		_ = writeHelper(os.Stderr, "load rejected")
		return 7
	}
	return loadHelper()
}

func saveHelper(mode string) int {
	if mode == modeSaveFail {
		return 7
	}
	// #nosec G703 -- private archive supplied by the parent test.
	file, err := os.Open(os.Getenv("DEVSY_MSB_TEST_IMAGE_ARCHIVE"))
	if err != nil {
		return 1
	}
	defer func() { _ = file.Close() }()
	if _, err := io.Copy(os.Stdout, file); err != nil {
		return 1
	}
	return 0
}

func (s *clientSuite) registryIndex() v1.ImageIndex {
	image := s.hostImage()
	otherArch := "arm64"
	if runtime.GOARCH == otherArch {
		otherArch = "amd64"
	}
	other, err := mutate.ConfigFile(empty.Image, &v1.ConfigFile{
		OS: testLinux, Architecture: otherArch,
		Config: v1.Config{Env: []string{"MARKER=other"}},
	})
	s.Require().NoError(err)
	return mutate.AppendManifests(
		empty.Index,
		mutate.IndexAddendum{
			Add: other,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{OS: testLinux, Architecture: otherArch},
			},
		},
		mutate.IndexAddendum{
			Add: image,
			Descriptor: v1.Descriptor{
				Platform: &v1.Platform{OS: testLinux, Architecture: runtime.GOARCH},
			},
		},
	)
}

func (s *clientSuite) hostImage() v1.Image {
	image, err := mutate.ConfigFile(
		empty.Image,
		&v1.ConfigFile{
			OS:           testLinux,
			Architecture: runtime.GOARCH,
			Config:       v1.Config{Env: []string{hostMarker}},
		},
	)
	s.Require().NoError(err)
	return image
}
