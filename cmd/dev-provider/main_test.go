package main

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

type runtimeCheckSuite struct{ suite.Suite }

func TestRuntimeCheck(t *testing.T) { suite.Run(t, new(runtimeCheckSuite)) }

func (s *runtimeCheckSuite) TestResponses() {
	for _, tc := range []struct{ name, response, errorText string }{
		{"compatible", `{"schemaVersion":1,"version":"0.7.13","compatible":true}`, ""},
		{"old", `{"schemaVersion":1,"version":"0.7.8","compatible":false}`, "incompatible"},
		{"missing", `{"schemaVersion":1,"version":"0.7.13"}`, "invalid"},
		{"malformed", `{`, "parse"},
		{"schema", `{"schemaVersion":2,"version":"0.7.13","compatible":true}`, "invalid"},
	} {
		s.Run(tc.name, func() {
			err := validateRuntimeCheck([]byte(tc.response))
			if tc.errorText == "" {
				s.NoError(err)
			} else {
				s.ErrorContains(err, tc.errorText)
			}
		})
	}
}
