package distribution

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"text/template"

	"github.com/stretchr/testify/suite"
	"gopkg.in/yaml.v3"
)

type distributionSuite struct{ suite.Suite }

func TestDistribution(t *testing.T) { suite.Run(t, new(distributionSuite)) }

func (s *distributionSuite) TestManifestPinsEveryExecutableAndPreservesOptions() {
	metadata, err := Generate("v1.2.3", s.artifacts())
	s.Require().NoError(err)
	var parsed testManifest
	s.Require().NoError(yaml.Unmarshal(metadata.Manifest, &parsed))
	s.Equal("microsandbox", parsed.Name)
	s.Equal("v1.2.3", parsed.Version)
	s.True(parsed.Agent.Local)
	s.Equal("external", parsed.Agent.Driver)
	s.Equal("DEVSY_RUNTIME_DRIVER", parsed.Agent.External.Binary)
	s.Equal([]string{"serve"}, parsed.Agent.External.Args)
	s.Equal("docker", parsed.Agent.External.ImageBackend)
	s.Len(parsed.Options, 12)
	s.Contains(parsed.Options, "MSB_PATH")
	s.Contains(parsed.Options, "MSB_LIBKRUNFW_PATH")
	s.Equal("2048", parsed.Options["MICROSANDBOX_MEMORY"].Default)
	s.Equal("strict", parsed.Options["MICROSANDBOX_WORKSPACE_STAT_VIRTUALIZATION"].Default)
	s.Equal("mirror", parsed.Options["MICROSANDBOX_WORKSPACE_HOST_PERMISSIONS"].Default)
	s.Require().Len(parsed.Agent.Binaries["DEVSY_RUNTIME_DRIVER"], 4)
	for _, item := range parsed.Agent.Binaries["DEVSY_RUNTIME_DRIVER"] {
		expected := fmt.Sprintf("%x", sha256.Sum256([]byte("binary-"+item.Name)))
		s.Equal(expected, item.Checksum)
		s.Equal(
			"https://github.com/devsy-org/devsy-provider-microsandbox/releases/download/v1.2.3/"+item.Name,
			item.Path,
		)
		s.Contains(string(metadata.Checksums), expected+"  "+item.Name+"\n")
	}
	again, err := Generate("v1.2.3", s.artifacts())
	s.Require().NoError(err)
	s.Equal(metadata, again)
}

func (s *distributionSuite) TestInvalidTagOrIncompleteArtifactsProduceNoMetadata() {
	directory := s.artifacts()
	for _, tag := range []string{"1.2.3", "v1.2", "v1.2.3/escape", "v01.2.3", "v1.2.3\nagent:"} {
		metadata, err := Generate(tag, directory)
		s.Error(err)
		s.Empty(metadata)
	}
	path := filepath.Join(directory, "devsy-runtime-microsandbox-linux-arm64")
	s.Require().NoError(os.Remove(path))
	metadata, err := Generate("v1.2.3", directory)
	s.ErrorContains(err, "linux-arm64")
	s.Empty(metadata)
	s.Require().NoError(os.WriteFile(path, nil, 0o600))
	_, err = Generate("v1.2.3", directory)
	s.ErrorContains(err, "nonempty regular file")
}

type testManifest struct {
	Name    string                `yaml:"name"`
	Version string                `yaml:"version"`
	Options map[string]testOption `yaml:"options"`
	Agent   testAgent             `yaml:"agent"`
}
type (
	testOption struct {
		Default string `yaml:"default"`
	}
	testAgent struct {
		Local    bool                    `yaml:"local"`
		Driver   string                  `yaml:"driver"`
		External testExternal            `yaml:"external"`
		Binaries map[string][]testBinary `yaml:"binaries"`
	}
)

type testExternal struct {
	Binary       string   `yaml:"binary"`
	Args         []string `yaml:"args"`
	ImageBackend string   `yaml:"imageBackend"`
}
type testBinary struct {
	OS       string `yaml:"os"`
	Arch     string `yaml:"arch"`
	Name     string `yaml:"name"`
	Path     string `yaml:"path"`
	Checksum string `yaml:"checksum"`
}

func (s *distributionSuite) TestReleaseTemplateRemainsByteIdentical() {
	old, err := os.ReadFile("release_test.tmpl")
	s.Require().NoError(err)
	items := []binary{{OS: "linux", Arch: "amd64", Name: "example", Checksum: "abc"}}
	previous := template.Must(template.New("old").Parse(string(old)))
	var expected bytes.Buffer
	s.Require().NoError(previous.Execute(&expected, struct {
		Tag      string
		Binaries []binary
	}{"v1.2.3", items}))
	actual, err := renderManifest("v1.2.3", items)
	s.Require().NoError(err)
	s.Equal(expected.Bytes(), actual)
}

func (s *distributionSuite) TestLocalPathsAreQuoted() {
	root := s.T().TempDir()
	paths := []string{
		filepath.Join(root, `colon: space`),
		filepath.Join(root, `hash# and "quotes"`),
	}
	for _, path := range paths {
		data, err := render(
			"v0.0.0-dev",
			[]binary{
				{
					OS:       "darwin",
					Arch:     "arm64",
					Name:     "binary",
					Path:     strconv.Quote(path),
					Checksum: "abc",
				},
			},
			&localDefaults{
				Home:     strconv.Quote(path),
				MSB:      strconv.Quote(path),
				Firmware: strconv.Quote(path),
			},
		)
		s.Require().NoError(err)
		var parsed testManifest
		s.Require().NoError(yaml.Unmarshal(data, &parsed))
		s.Equal(path, parsed.Options["MSB_HOME"].Default)
		s.Equal(path, parsed.Options["MSB_PATH"].Default)
		s.Equal(path, parsed.Options["MSB_LIBKRUNFW_PATH"].Default)
		s.Equal(path, parsed.Agent.Binaries["DEVSY_RUNTIME_DRIVER"][0].Path)
	}
}

func (s *distributionSuite) TestLocalValidation() {
	root := s.T().TempDir()
	file := filepath.Join(root, "binary")
	s.Require().NoError(os.WriteFile(file, []byte("test"), 0o600))
	valid := LocalConfig{
		Binary:    file,
		Directory: filepath.Join(root, "dist"),
		MSB:       file,
		Firmware:  file,
	}
	for _, cfg := range []LocalConfig{
		{},
		{Binary: file, Directory: valid.Directory, MSB: file},
		{Binary: file, Directory: valid.Directory, MSB: file, Firmware: filepath.Join(root, "missing")},
	} {
		_, err := GenerateLocal(cfg)
		s.Error(err)
	}
	_, err := os.Stat(valid.Directory)
	s.True(os.IsNotExist(err))
}

func (s *distributionSuite) TestAtomicWritePreservesDestinationOnFailure() {
	root := s.T().TempDir()
	target := filepath.Join(root, "manifest")
	s.Require().NoError(os.Mkdir(target, 0o700))
	s.Error(atomicWrite(target, []byte("new"), 0o600))
	info, err := os.Stat(target)
	s.Require().NoError(err)
	s.True(info.IsDir())
	entries, err := os.ReadDir(root)
	s.Require().NoError(err)
	s.Len(entries, 1)
}

func (s *distributionSuite) TestLocalManifestPinsHostExecutable() {
	if runtime.GOOS == "windows" || (runtime.GOOS == "darwin" && runtime.GOARCH != "arm64") {
		s.T().Skip("local helper does not support this host")
	}
	root := s.T().TempDir()
	executable := filepath.Join(root, "adapter")
	s.Require().NoError(os.WriteFile(executable, []byte("adapter bytes"), 0o600))
	cfg := LocalConfig{
		Binary:    executable,
		Directory: filepath.Join(root, "dist", "local"),
		MSB:       executable,
		Firmware:  executable,
	}
	manifest, err := GenerateLocal(cfg)
	s.Require().NoError(err)
	// #nosec G304 -- manifest is generated under the test directory.
	content, err := os.ReadFile(manifest)
	s.Require().NoError(err)
	var parsed testManifest
	s.Require().NoError(yaml.Unmarshal(content, &parsed))
	s.Equal("microsandbox-local", parsed.Name)
	s.Require().Len(parsed.Agent.Binaries["DEVSY_RUNTIME_DRIVER"], 1)
	item := parsed.Agent.Binaries["DEVSY_RUNTIME_DRIVER"][0]
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("adapter bytes")))
	s.Equal(runtime.GOOS, item.OS)
	s.Equal(runtime.GOARCH, item.Arch)
	s.Equal(digest, item.Checksum)
	s.Equal("devsy-runtime-microsandbox-"+digest, item.Name)
	copied, err := os.ReadFile(item.Path)
	s.Require().NoError(err)
	s.Equal([]byte("adapter bytes"), copied)
}

func (s *distributionSuite) artifacts() string {
	directory := s.T().TempDir()
	for _, name := range []string{
		"devsy-runtime-microsandbox-linux-amd64",
		"devsy-runtime-microsandbox-linux-arm64",
		"devsy-runtime-microsandbox-darwin-arm64",
		"devsy-runtime-microsandbox-windows-amd64.exe",
	} {
		s.Require().
			NoError(os.WriteFile(filepath.Join(directory, name), []byte("binary-"+name), 0o600))
	}
	return directory
}
