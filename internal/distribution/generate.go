// Package distribution prepares provider manifests: checksum-pinned releases and local development builds.
package distribution

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/blang/semver/v4"
)

//go:embed provider.yaml.tmpl
var manifestTemplate string

const (
	releaseName     = "microsandbox"
	developmentName = "microsandbox-dev"
	// DevelopmentExecutablePrefix starts the content-addressed name of each local build.
	DevelopmentExecutablePrefix = "devsy-runtime-microsandbox-dev-"
	releaseURL                  = "https://github.com/devsy-org/devsy-provider-microsandbox/releases/download/"
)

type binary struct {
	OS       string
	Arch     string
	Name     string
	Path     string
	Checksum string
}

// Development describes a locally built provider installed from host paths.
// All paths are absolute paths on the host that runs Devsy.
type Development struct {
	Version       string
	OS            string
	Arch          string
	Executable    string
	Checksum      string
	MSBPath       string
	LibkrunfwPath string
	MSBHome       string
}

type manifestData struct {
	Name     string
	Tag      string
	Binaries []binary
	Dev      *Development
}

// Metadata contains the manifest and executable checksums for one release.
type Metadata struct {
	Manifest  []byte
	Checksums []byte
}

// Generate requires every supported native executable before producing a manifest.
// Checksums describe raw executables, matching Devsy's post-download verification.
func Generate(tag, directory string) (Metadata, error) {
	if !strings.HasPrefix(tag, "v") {
		return Metadata{}, errors.New("release tag must begin with v")
	}
	if _, err := semver.Parse(strings.TrimPrefix(tag, "v")); err != nil {
		return Metadata{}, fmt.Errorf("invalid release tag: %w", err)
	}
	binaries := []binary{
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "windows", Arch: "amd64"},
	}
	var checksums bytes.Buffer
	for index := range binaries {
		item := &binaries[index]
		item.Name = "devsy-runtime-microsandbox-" + item.OS + "-" + item.Arch
		if item.OS == "windows" {
			item.Name += ".exe"
		}
		checksum, err := fileChecksum(filepath.Join(directory, item.Name))
		if err != nil {
			return Metadata{}, fmt.Errorf("checksum %s: %w", item.Name, err)
		}
		item.Checksum = checksum
		item.Path = releaseURL + tag + "/" + item.Name
		fmt.Fprintf(&checksums, "%s  %s\n", checksum, item.Name)
	}
	manifest, err := renderManifest(manifestData{Name: releaseName, Tag: tag, Binaries: binaries})
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{Manifest: manifest, Checksums: checksums.Bytes()}, nil
}

// GenerateDevelopment renders the microsandbox-dev manifest for one host platform.
// Devsy requires a checksum for external drivers, so each build is installed
// under a content-addressed name: a path never changes its bytes, and Devsy
// never discards an executable whose checksum predates a rebuild.
func GenerateDevelopment(dev Development) ([]byte, error) {
	if err := dev.validate(); err != nil {
		return nil, err
	}
	return renderManifest(manifestData{
		Name: developmentName,
		Tag:  dev.Version,
		Binaries: []binary{{
			OS:       dev.OS,
			Arch:     dev.Arch,
			Name:     filepath.Base(dev.Executable),
			Path:     dev.Executable,
			Checksum: dev.Checksum,
		}},
		Dev: &dev,
	})
}

// DevelopmentExecutable returns the content-addressed file name and checksum
// of a locally built executable.
func DevelopmentExecutable(path string) (name, checksum string, err error) {
	checksum, err = fileChecksum(path)
	if err != nil {
		return "", "", err
	}
	return DevelopmentExecutablePrefix + checksum[:12], checksum, nil
}

func fileChecksum(path string) (string, error) {
	// #nosec G304 -- fixed artifact names beneath the operator-selected release directory
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 {
		return "", errors.New("artifact must be a nonempty regular file")
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func renderManifest(data manifestData) ([]byte, error) {
	parsed, err := template.New("provider").Option("missingkey=error").Parse(manifestTemplate)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	if err := parsed.Execute(&output, data); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func (dev Development) validate() error {
	if !strings.HasPrefix(dev.Version, "v") {
		return errors.New("development version must begin with v")
	}
	if _, err := semver.Parse(strings.TrimPrefix(dev.Version, "v")); err != nil {
		return fmt.Errorf("invalid development version: %w", err)
	}
	if dev.OS == "" || dev.Arch == "" {
		return errors.New("development platform is required")
	}
	if decoded, err := hex.DecodeString(dev.Checksum); err != nil || len(decoded) != sha256.Size {
		return errors.New("development executable requires a SHA-256 checksum")
	}
	return dev.validatePaths()
}

func (dev Development) validatePaths() error {
	for name, path := range map[string]string{
		"executable":        dev.Executable,
		"msb":               dev.MSBPath,
		"libkrunfw":         dev.LibkrunfwPath,
		"MicroSandbox home": dev.MSBHome,
	} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("%s path %q must be absolute", name, path)
		}
	}
	return nil
}
