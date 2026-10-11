package msb

import (
	"slices"
	"testing"
)

func TestRunArgsReplacementIsExplicit(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		spec := Spec{
			Image: testImg, AllowRecreate: allowed, User: testUser,
			Memory: 1024, CPUs: 2, Labels: map[string]string{"generation": "new"},
			Mounts: []Mount{{Source: testBindSrc, Target: testBindDst}},
			Cmd:    []string{"sh", "-c", "echo preserved"},
		}
		args := runArgs(wsName, spec)
		want := []string{msbCmdRun, msbFlagDetach, "--name", wsName}
		if allowed {
			want = append(want, "--replace")
		}
		want = append(want, "--user", testUser, "--label", "generation=new",
			"--memory", "1024M", "--cpus", "2", "--mount-dir", testBindSrc+":"+testBindDst,
			testImg, "--", "sh", "-c", "echo preserved")
		if !slices.Equal(args, want) {
			t.Fatalf("allow=%v: got %v, want %v", allowed, args, want)
		}
	}
}
