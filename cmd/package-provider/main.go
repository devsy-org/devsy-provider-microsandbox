// Command package-provider generates metadata for a complete native release.
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/distribution"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) != 2 {
		return errors.New("usage: package-provider <vSEMVER> <artifact-directory>")
	}
	metadata, err := distribution.Generate(args[0], args[1])
	if err != nil {
		return err
	}
	for _, output := range []struct {
		name string
		data []byte
	}{
		{"provider.yaml", metadata.Manifest},
		{"checksums.txt", metadata.Checksums},
	} {
		// #nosec G306 G703 -- fixed public metadata filenames beneath the operator-selected output directory
		if err := os.WriteFile(
			filepath.Join(args[1], output.name),
			output.data,
			0o644,
		); err != nil {
			return fmt.Errorf("write %s: %w", output.name, err)
		}
	}
	return nil
}
