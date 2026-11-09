package distribution

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// LocalConfig describes the existing host executable and runtime pair.
type LocalConfig struct{ Binary, Directory, MSB, Firmware string }

// GenerateLocal writes a checksum-pinned host manifest and returns its path.
func GenerateLocal(cfg LocalConfig) (string, error) {
	if err := validateLocal(cfg); err != nil {
		return "", err
	}
	data, err := os.ReadFile(cfg.Binary)
	if err != nil {
		return "", err
	}
	checksum := fmt.Sprintf("%x", sha256.Sum256(data))
	if err = os.MkdirAll(cfg.Directory, 0o750); err != nil {
		return "", err
	}
	name := "devsy-runtime-microsandbox-" + checksum
	dest := filepath.Join(cfg.Directory, name)
	if err = atomicWrite(dest, data, 0o755); err != nil {
		return "", err
	}
	home := filepath.Join(filepath.Dir(cfg.Directory), "m")
	defaults := &localDefaults{
		Home:     strconv.Quote(home),
		MSB:      strconv.Quote(cfg.MSB),
		Firmware: strconv.Quote(cfg.Firmware),
	}
	manifest, err := render(
		"v0.0.0-dev",
		[]binary{
			{
				OS:       runtime.GOOS,
				Arch:     runtime.GOARCH,
				Name:     name,
				Path:     strconv.Quote(dest),
				Checksum: checksum,
			},
		},
		defaults,
	)
	if err != nil {
		return "", err
	}
	path := filepath.Join(cfg.Directory, "provider.yaml")
	if err = atomicWrite(path, manifest, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func validateLocal(cfg LocalConfig) error {
	for _, path := range []string{cfg.Binary, cfg.Directory, cfg.MSB, cfg.Firmware} {
		if !filepath.IsAbs(path) {
			return fmt.Errorf("path must be absolute: %q", path)
		}
	}
	for _, path := range []string{cfg.Binary, cfg.MSB, cfg.Firmware} {
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("inspect %s: %w", path, err)
		}
		if !info.Mode().IsRegular() || info.Size() == 0 {
			return fmt.Errorf("%s must be a nonempty regular file", path)
		}
	}
	if err := validateHost(); err != nil {
		return err
	}
	return validateSocketHome(filepath.Join(filepath.Dir(cfg.Directory), "m"))
}

func validateHost() error {
	if runtime.GOOS == osDarwin && runtime.GOARCH == archARM64 {
		return nil
	}
	if runtime.GOOS == osLinux && (runtime.GOARCH == archAMD64 || runtime.GOARCH == archARM64) {
		return nil
	}
	return fmt.Errorf("unsupported host: %s/%s", runtime.GOOS, runtime.GOARCH)
}

func validateSocketHome(home string) error {
	capacity := 108
	if runtime.GOOS == osDarwin {
		capacity = 104
	}
	// MicroSandbox also creates a legacy control socket with a 32 byte hex ID.
	socket := filepath.Join(home, "run", "agent", strings.Repeat("0", 32)+".control.sock")
	if len(socket) >= capacity {
		return fmt.Errorf(
			"runtime home is too long for Unix sockets: %s (%d bytes, limit %d); use a shorter checkout path",
			home,
			len(socket),
			capacity-1,
		)
	}
	return nil
}

func atomicWrite(path string, data []byte, mode os.FileMode) (err error) {
	f, err := os.CreateTemp(filepath.Dir(path), ".provider-*")
	if err != nil {
		return err
	}
	// #nosec G703 -- the temporary file was created in the selected output directory.
	defer func() { _ = os.Remove(f.Name()) }()
	if err = f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if _, err = f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	// #nosec G703 -- both paths belong to the selected output directory.
	return os.Rename(f.Name(), path)
}
