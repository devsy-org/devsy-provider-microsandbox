package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/distribution"
	"github.com/stretchr/testify/suite"
)

type devProviderSuite struct {
	suite.Suite
	dir string
}

func TestDevProvider(t *testing.T) { suite.Run(t, new(devProviderSuite)) }

func (s *devProviderSuite) SetupTest() {
	s.dir = s.T().TempDir()
}

func (s *devProviderSuite) TestInstallsBuildAndKeepsRecentBuilds() {
	old := time.Now().Add(-time.Hour)
	for index, name := range []string{"aaaaaaaaaaaa", "bbbbbbbbbbbb", "cccccccccccc"} {
		path := filepath.Join(s.dir, distribution.DevelopmentExecutablePrefix+name)
		s.Require().NoError(os.WriteFile(path, []byte(name), 0o600))
		modified := old.Add(time.Duration(index) * time.Minute)
		s.Require().NoError(os.Chtimes(path, modified, modified))
	}
	unrelated := filepath.Join(s.dir, "provider.yaml.orig")
	s.Require().NoError(os.WriteFile(unrelated, nil, 0o600))
	build := filepath.Join(s.dir, ".build")
	s.Require().NoError(os.WriteFile(build, []byte("new build"), 0o600))
	output := filepath.Join(s.dir, "provider.yaml")

	s.Require().NoError(run(s.args(build, output)))

	name, checksum, err := distribution.DevelopmentExecutable(
		filepath.Join(s.dir, s.installedName()),
	)
	s.Require().NoError(err)
	s.NoFileExists(build)
	s.ElementsMatch([]string{
		distribution.DevelopmentExecutablePrefix + "bbbbbbbbbbbb",
		distribution.DevelopmentExecutablePrefix + "cccccccccccc",
		name,
	}, s.builds())
	s.FileExists(unrelated)
	// #nosec G304 -- the manifest lives in a test-owned directory.
	manifest, err := os.ReadFile(output)
	s.Require().NoError(err)
	s.Contains(string(manifest), "path: /host/dev/"+name+"\n")
	s.Contains(string(manifest), "checksum: "+checksum+"\n")
	s.Contains(string(manifest), "version: v0.0.0-dev+abc1234\n")
}

func (s *devProviderSuite) TestRejectsIncompleteArguments() {
	build := filepath.Join(s.dir, ".build")
	output := filepath.Join(s.dir, "provider.yaml")
	complete := s.args(build, output)
	traversal := slices.Clone(complete)
	traversal[slices.Index(traversal, "-firmware")+1] = "../libkrunfw.so"
	for _, args := range [][]string{
		{},
		complete[:4],
		append(slices.Clone(complete), "extra"),
		traversal,
	} {
		s.Error(run(args))
	}
	s.NoFileExists(output)
}

func (s *devProviderSuite) args(build, output string) []string {
	return []string{
		"-build", build,
		"-version", "v0.0.0-dev+abc1234",
		"-root", "/host/dev",
		"-firmware", "libkrunfw.so.5.6.1",
		"-msb-home", "/host/home/.microsandbox-dev",
		"-output", output,
	}
}

func (s *devProviderSuite) builds() []string {
	entries, err := os.ReadDir(s.dir)
	s.Require().NoError(err)
	var names []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), distribution.DevelopmentExecutablePrefix) {
			names = append(names, entry.Name())
		}
	}
	return names
}

func (s *devProviderSuite) installedName() string {
	for _, name := range s.builds() {
		// #nosec G304 -- builds live in a test-owned directory.
		content, err := os.ReadFile(filepath.Join(s.dir, name))
		s.Require().NoError(err)
		if string(content) == "new build" {
			return name
		}
	}
	s.FailNow("installed build not found")
	return ""
}
