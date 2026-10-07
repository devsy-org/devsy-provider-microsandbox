package msb

import (
	"fmt"
	"regexp"

	"github.com/blang/semver/v4"
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
