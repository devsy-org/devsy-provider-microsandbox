package distribution

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"testing"

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
	s.Len(parsed.Options, 10)
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
