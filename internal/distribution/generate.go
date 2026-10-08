// Package distribution prepares checksum-pinned provider release metadata.
package distribution

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
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

type binary struct {
	OS       string
	Arch     string
	Name     string
	Checksum string
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
		fmt.Fprintf(&checksums, "%s  %s\n", checksum, item.Name)
	}
	manifest, err := renderManifest(tag, binaries)
	if err != nil {
		return Metadata{}, err
	}
	return Metadata{Manifest: manifest, Checksums: checksums.Bytes()}, nil
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

func renderManifest(tag string, binaries []binary) ([]byte, error) {
	parsed, err := template.New("provider").Option("missingkey=error").Parse(manifestTemplate)
	if err != nil {
		return nil, err
	}
	var output bytes.Buffer
	err = parsed.Execute(&output, struct {
		Tag      string
		Binaries []binary
	}{Tag: tag, Binaries: binaries})
	if err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}
