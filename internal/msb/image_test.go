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
	imageFixture = "docker image bytes\x00\xff"
	modeDocker   = "docker"
	modePullFail = "pull-fail"
)

func (s *clientSuite) TestImageLocalDockerLoad() {
	s.installDockerHelper()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeDocker)
	s.Require().NoError(s.client.EnsureImage(context.Background(), testImg))
	data, err := os.ReadFile(s.record + ".tar")
	s.Require().NoError(err)
	s.Equal(imageFixture, string(data))
}

func (s *clientSuite) TestImageDockerSaveFailure() {
	s.installDockerHelper()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "save-fail")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.ErrorContains(s.client.EnsureImage(ctx, testImg), "docker save")
}

func (s *clientSuite) TestImagePullWithoutDocker() {
	s.T().Setenv("PATH", s.T().TempDir())
	s.Require().NoError(s.client.EnsureImage(context.Background(), testImg))
	s.Equal([]string{"pull", testImg}, s.args())
}

func (s *clientSuite) TestImageRegistryFallback() {
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
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modePullFail)
	s.Require().NoError(s.client.EnsureImage(context.Background(), ref.Name()))
	loaded, err := tarball.ImageFromPath(s.record+".tar", nil)
	s.Require().NoError(err)
	config, err := loaded.ConfigFile()
	s.Require().NoError(err)
	s.Equal(testLinux, config.OS)
	s.Equal(runtime.GOARCH, config.Architecture)
	s.Equal([]string{"MARKER=host"}, config.Config.Env)
	args := s.args()
	s.Equal([]string{"load", "-i"}, args[:2])
	_, err = os.Stat(args[2])
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *clientSuite) TestImageCanceledContext() {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.ErrorIs(s.client.EnsureImage(ctx, testImg), context.Canceled)
	_, err := os.Stat(s.record)
	s.ErrorIs(err, os.ErrNotExist)
}

func (s *clientSuite) TestImageDockerLoadFailure() {
	s.installDockerHelper()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modeLoadFail)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s.ErrorContains(s.client.EnsureImage(ctx, testImg), "load rejected")
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
	s.T().Setenv("DEVSY_MSB_TEST_MODE", modePullFail)
	s.ErrorIs(
		s.client.EnsureImage(ctx, server.Listener.Addr().String()+"/image:latest"),
		context.Canceled,
	)
	s.Equal("pull", s.args()[0])
}

func (s *clientSuite) installDockerHelper() {
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
	case "save":
		return saveHelper(mode)
	case "pull":
		if mode == modePullFail {
			return 1
		}
	case "load":
		return loadModeHelper(args, mode)
	}
	return 0
}

func loadHelper(args []string) int {
	var input io.Reader = os.Stdin
	if len(args) > 2 && args[1] == "-i" {
		// #nosec G703 -- path is a private tarball created by the client under test.
		file, err := os.Open(args[2])
		if err != nil {
			return 1
		}
		defer func() { _ = file.Close() }()
		input = file
	}
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
	if mode == modeDocker || mode == "save-fail" || mode == modeLoadFail {
		return 0
	}
	return 1
}

func loadModeHelper(args []string, mode string) int {
	if mode == modeLoadFail {
		_ = writeHelper(os.Stderr, "load rejected")
		return 7
	}
	return loadHelper(args)
}

func saveHelper(mode string) int {
	if mode == "save-fail" {
		return 7
	}
	var data io.Reader = bytes.NewBufferString(imageFixture)
	if mode == modeLoadFail {
		data = bytes.NewReader(bytes.Repeat([]byte("x"), 4*1024*1024))
	}
	if _, err := io.Copy(os.Stdout, data); err != nil {
		return 1
	}
	return 0
}

func (s *clientSuite) registryIndex() v1.ImageIndex {
	image, err := mutate.ConfigFile(
		empty.Image,
		&v1.ConfigFile{
			OS:           testLinux,
			Architecture: runtime.GOARCH,
			Config:       v1.Config{Env: []string{"MARKER=host"}},
		},
	)
	s.Require().NoError(err)
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
