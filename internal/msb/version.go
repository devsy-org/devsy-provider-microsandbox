package msb

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/blang/semver/v4"
)

var MinimumVersion = semver.Version{Minor: 7, Patch: 13}

var ErrUnsupportedVersion = fmt.Errorf(
	"MicroSandbox v%s or newer is required", MinimumVersion,
)

var microsandboxVersionPattern = regexp.MustCompile(
	`(?:^|[^0-9])v?(\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?)`,
)

// ParseVersion accepts the version output emitted by msb.
func ParseVersion(output string) (semver.Version, error) {
	matches := microsandboxVersionPattern.FindStringSubmatch(output)
	if len(matches) != 2 {
		return semver.Version{}, fmt.Errorf("unable to parse microsandbox version from %q", output)
	}
	version, err := semver.Parse(matches[1])
	if err != nil {
		return semver.Version{}, fmt.Errorf(
			"unable to parse microsandbox version from %q: %w", output, err,
		)
	}
	return version, nil
}

func RequireSupported(output string) (semver.Version, error) {
	version, err := ParseVersion(output)
	if err != nil {
		return semver.Version{}, errors.Join(ErrUnsupportedVersion, err)
	}
	if version.LT(MinimumVersion) {
		return version, fmt.Errorf(
			"%w; found v%s, update with msb self update", ErrUnsupportedVersion, version,
		)
	}
	return version, nil
}
