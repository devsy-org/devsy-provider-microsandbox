package msb

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type versionSuite struct{ suite.Suite }

func TestVersion(t *testing.T) { suite.Run(t, new(versionSuite)) }

func (s *versionSuite) TestRequireSupportedAcceptsMinimumAndNewer() {
	for _, output := range []string{"msb 0.7.13", "microsandbox 0.7.14", "msb v0.8.0"} {
		_, err := RequireSupported(output)
		s.NoError(err, output)
	}
}

func (s *versionSuite) TestRequireSupportedRejectsOlderCatalogs() {
	for _, output := range []string{"msb 0.7.2", "msb 0.7.12", "msb 0.7.13-rc.1"} {
		_, err := RequireSupported(output)
		s.ErrorIs(err, ErrUnsupportedVersion, output)
		s.ErrorContains(err, "msb self update", output)
	}
}

func (s *versionSuite) TestRequireSupportedRejectsUnparsedOutput() {
	_, err := RequireSupported("unknown")
	s.ErrorIs(err, ErrUnsupportedVersion)
	s.ErrorContains(err, `unable to parse microsandbox version from "unknown"`)
}
