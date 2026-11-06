// Command dev-provider installs a local build as the microsandbox-dev provider
// executable and renders its manifest. Manifest paths are host paths: Devsy on
// the host may see this repository at a different path than a dev container.
package main

import (
	"cmp"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/distribution"
)

// keptBuilds bounds dist/dev while leaving recent builds that a provider
// installation may still reference until its next set-source.
const keptBuilds = 3

type options struct {
	build    string
	version  string
	root     string
	firmware string
	msbHome  string
	output   string
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	opts, err := parse(args)
	if err != nil {
		return err
	}
	name, checksum, err := distribution.DevelopmentExecutable(opts.build)
	if err != nil {
		return err
	}
	manifest, err := distribution.GenerateDevelopment(distribution.Development{
		Version:       opts.version,
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		Executable:    filepath.Join(opts.root, name),
		Checksum:      checksum,
		MSBPath:       filepath.Join(opts.root, "runtime", "msb"),
		LibkrunfwPath: filepath.Join(opts.root, "runtime", opts.firmware),
		MSBHome:       opts.msbHome,
	})
	if err != nil {
		return err
	}
	// Install only a build with a valid manifest; the rename is atomic, so
	// adapters that are already running keep their file.
	directory := filepath.Dir(opts.build)
	if err := os.Rename(opts.build, filepath.Join(directory, name)); err != nil {
		return err
	}
	if err := prune(directory, name); err != nil {
		return err
	}
	// #nosec G306 -- the manifest is public metadata in the developer's build directory
	return os.WriteFile(opts.output, manifest, 0o644)
}

func parse(args []string) (options, error) {
	var opts options
	flags := flag.NewFlagSet("dev-provider", flag.ContinueOnError)
	flags.StringVar(&opts.build, "build", "", "freshly built executable to install")
	flags.StringVar(&opts.version, "version", "", "provider version, such as v0.0.0-dev+abc1234")
	flags.StringVar(&opts.root, "root", "", "host path of the build directory")
	flags.StringVar(&opts.firmware, "firmware", "", "firmware file name inside runtime/")
	flags.StringVar(&opts.msbHome, "msb-home", "", "host path of the isolated MicroSandbox home")
	flags.StringVar(&opts.output, "output", "", "manifest file to write")
	if err := flags.Parse(args); err != nil {
		return options{}, err
	}
	if flags.NArg() != 0 || opts.build == "" || opts.output == "" || opts.firmware == "" ||
		filepath.Base(opts.firmware) != opts.firmware {
		return options{}, errors.New(
			"usage: dev-provider -build <file> -version <vSEMVER> -root <host-dir> " +
				"-firmware <file> -msb-home <host-dir> -output <file>",
		)
	}
	return opts, nil
}

// prune removes all but the newest builds, always keeping current.
func prune(directory, current string) error {
	builds, err := previousBuilds(directory, current)
	if err != nil {
		return err
	}
	if len(builds) < keptBuilds {
		return nil
	}
	for _, old := range builds[keptBuilds-1:] {
		if err := os.Remove(filepath.Join(directory, old.name)); err != nil {
			return err
		}
	}
	return nil
}

type build struct {
	name     string
	modified int64
}

// previousBuilds lists development executables other than current, newest first.
func previousBuilds(directory, current string) ([]build, error) {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return nil, err
	}
	var builds []build
	for _, entry := range entries {
		name := entry.Name()
		if name == current || !entry.Type().IsRegular() ||
			!strings.HasPrefix(name, distribution.DevelopmentExecutablePrefix) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		builds = append(builds, build{name: name, modified: info.ModTime().UnixNano()})
	}
	slices.SortFunc(builds, func(a, b build) int {
		return cmp.Compare(b.modified, a.modified)
	})
	return builds, nil
}
