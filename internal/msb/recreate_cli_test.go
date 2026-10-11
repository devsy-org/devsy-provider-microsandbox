package msb

import (
	"context"
)

func (s *clientSuite) TestCreateForwardsRecreatePermissionPerProcess() {
	ctx := context.Background()
	for _, allowed := range []bool{false, true, false} {
		spec := Spec{Image: testImg, AllowRecreate: allowed}
		s.Require().NoError(s.client.Create(ctx, wsName, spec))
		args := s.args()
		s.Equal("run", args[0])
		s.Equal(wsName, args[3])
		if allowed {
			s.Equal("--replace", args[4])
			s.Equal(testImg, args[5])
		} else {
			s.NotContains(args, "--replace")
			s.Equal(testImg, args[4])
		}
		wantCount := 0
		if allowed {
			wantCount = 1
		}
		s.Equal(wantCount, countArg(args, "--replace"))
	}
}

func countArg(args []string, want string) int {
	count := 0
	for _, arg := range args {
		if arg == want {
			count++
		}
	}
	return count
}
