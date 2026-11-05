package msb

import (
	"errors"
	"fmt"
	"regexp"

	"github.com/blang/semver/v4"
)

// MinimumVersion is the oldest msb that accepts the catalog written by the
// embedded SDK library. The SDK upgrades the MSB_HOME database on first use,
// after which older msb releases refuse it.
var MinimumVersion = semver.Version{Minor: 7, Patch: 13}

// ErrUnsupportedVersion identifies an msb release older than MinimumVersion.
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

// RequireSupported rejects version output that is unparseable or below MinimumVersion.
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
