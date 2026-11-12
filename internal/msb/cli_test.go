package msb

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
)

const (
	testWindows   = "windows"
	testWait      = "wait"
	testNotFound  = "not found"
	testInvalid   = "invalid"
	testVolume    = "cache"
	helperEnv     = "DEVSY_MSB_TEST_PROCESS"
	stderrFixture = "backend diagnostic\x00\xff"
)

type clientSuite struct {
	suite.Suite
	client  Client
	record  string
	archive string
}

func TestClient(t *testing.T) { suite.Run(t, new(clientSuite)) }

func (s *clientSuite) SetupTest() {
	binary, err := os.Executable()
	s.Require().NoError(err)
	s.client = Client{Binary: binary}
	s.record = filepath.Join(s.T().TempDir(), "args.json")
	s.T().Setenv(helperEnv, "1")
	s.T().Setenv("DEVSY_MSB_TEST_RECORD", s.record)
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "")
}

func (s *clientSuite) TestVersionAndInstallation() {
	s.Require().NoError(s.client.EnsureInstalled(context.Background()))
	version, err := s.client.Version(context.Background())
	s.Require().NoError(err)
	s.Equal("microsandbox 0.7.2", version)
	s.Equal([]string{"--version"}, s.args())
	missing := Client{Binary: filepath.Join(s.T().TempDir(), "missing")}
	s.ErrorContains(missing.EnsureInstalled(context.Background()), testNotFound)
}

func (s *clientSuite) TestFindStates() {
	for _, state := range []string{"running", "stopped", testNotFound, testDenied, testInvalid} {
		s.Run(state, func() {
			s.T().Setenv("DEVSY_MSB_TEST_MODE", state)
			info, err := s.client.Find(context.Background(), wsName)
			switch state {
			case testNotFound:
				s.NoError(err)
				s.Nil(info)
			case testDenied:
				s.ErrorContains(err, "exit status 1")
				s.ErrorContains(err, testDenied)
				s.Nil(info)
			case testInvalid:
				s.ErrorContains(err, "parse microsandbox inspect output")
				s.ErrorContains(err, "microsandbox 0.7.2")
				s.ErrorContains(
					err,
					"https://github.com/devsy-org/devsy-provider-microsandbox/issues",
				)
				s.Nil(info)
			default:
				s.Require().NoError(err)
				s.Require().NotNil(info)
				s.Equal(wsName, info.Name)
				s.Equal(state == "running", info.Running)
				s.Equal(testUser, info.Labels["devsy.sh/user"])
				s.Equal(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), info.CreatedAt)
			}
		})
	}
}

func (s *clientSuite) TestInspectErrorSurvivesStalledVersionProbe() {
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "invalid-stalled-version")
	started := time.Now()
	_, err := s.client.Find(context.Background(), wsName)
	s.ErrorContains(err, "parse microsandbox inspect output")
	s.ErrorContains(err, "unknown (version detection failed)")
	s.Less(time.Since(started), 10*time.Second)
}

func (s *clientSuite) TestExecStreamsAndArgv() {
	input := []byte{0, 255, '\r', '\n', 1}
	var stdout, stderr bytes.Buffer
	err := s.client.Exec(context.Background(), wsName, ExecRequest{
		Argv: []string{"echo", "one argument; $literal"}, User: testUser,
		Stdin: io.NopCloser(bytes.NewReader(input)), Stdout: &stdout, Stderr: &stderr,
	})
	s.Require().NoError(err)
	s.Equal(input, stdout.Bytes())
	s.Equal(stderrFixture, stderr.String())
	s.Equal(
		[]string{
			testExec,
			"--stream",
			"--user",
			testUser,
			wsName,
			"--",
			"echo",
			"one argument; $literal",
		},
		s.args(),
	)
}

func (s *clientSuite) TestExecShellAndExitStatus() {
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "fail")
	err := s.client.Exec(
		context.Background(),
		wsName,
		ExecRequest{Command: "exit 7", Stderr: io.Discard},
	)
	s.Require().Error(err)
	var exitErr interface{ ExitCode() int }
	s.Require().ErrorAs(err, &exitErr)
	s.Equal(7, exitErr.ExitCode())
	s.Equal([]string{testExec, "--stream", wsName, "--", shPath, "-c", "exit 7"}, s.args())
}

type cancelWriter struct{ cancel context.CancelFunc }

func (w cancelWriter) Write(p []byte) (int, error) { w.cancel(); return len(p), nil }

func (s *clientSuite) TestExecCancellation() {
	s.T().Setenv("DEVSY_MSB_TEST_MODE", testWait)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := s.client.Exec(
		ctx,
		wsName,
		ExecRequest{Argv: []string{testWait}, Stdout: cancelWriter{cancel}},
	)
	s.Error(err)
	s.ErrorIs(ctx.Err(), context.Canceled)
}

func (s *clientSuite) TestLifecycleAndLogs() {
	ctx := context.Background()
	for _, op := range []struct {
		name string
		run  func(context.Context, string) error
	}{
		{"start", s.client.Start}, {"stop", s.client.Stop}, {"remove", s.client.Remove},
	} {
		s.Require().NoError(op.run(ctx, wsName))
		s.Equal([]string{op.name, wsName}, s.args())
	}
	var logs bytes.Buffer
	s.Require().NoError(s.client.Logs(ctx, wsName, &logs))
	s.Contains(logs.String(), "stdout log")
	s.Contains(logs.String(), "stderr log")
}

func (s *clientSuite) TestCreateVolumeFailuresAndRedaction() {
	ctx := context.Background()
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "exists")
	spec := Spec{Image: testImg, Mounts: []Mount{{Volume: testVolume, Target: testCachePath}}}
	s.Require().NoError(s.client.Create(ctx, wsName, spec))
	s.Equal("run", s.args()[0])
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "fail")
	s.ErrorContains(
		s.client.Create(
			ctx,
			wsName,
			Spec{Image: testImg, Mounts: []Mount{{Volume: testVolume, Target: testCachePath}}},
		),
		"create microsandbox volume",
	)
	s.Equal([]string{"volume", "create", testVolume}, s.args())
	err := s.client.Create(
		ctx,
		wsName,
		Spec{Image: testImg, Env: map[string]string{testEnvKey: "secret-value"}},
	)
	s.Require().Error(err)
	s.NotContains(err.Error(), "secret-value")
}

func (s *clientSuite) TestCreatePassesEnvOffArgv() {
	const secret = "s3cr${x}t" // #nosec G101 -- test fixture
	s.Require().NoError(s.client.Create(
		context.Background(),
		wsName,
		Spec{Image: testImg, Env: map[string]string{testEnvKey: secret, "A": "1"}},
	))
	args := s.args()
	for _, a := range args {
		s.NotContains(a, secret)
		s.NotEqual("--env", a)
	}
	idx := slices.Index(args, flagRuntime)
	s.Require().GreaterOrEqual(idx, 0)
	conf := args[idx+1]
	data, err := os.ReadFile(s.record + ".runtime")
	s.Require().NoError(err)
	var got struct {
		Conf    string            `json:"conf"`
		Environ map[string]string `json:"environ"`
	}
	s.Require().NoError(json.Unmarshal(data, &got))
	s.JSONEq(`{"env":{"A":"${DEVSY_MSB_ENV_0}","TOKEN":"${DEVSY_MSB_ENV_1}"}}`, got.Conf)
	s.NotContains(got.Conf, secret)
	s.Equal(map[string]string{"DEVSY_MSB_ENV_0": "1", "DEVSY_MSB_ENV_1": secret}, got.Environ)
	_, err = os.Stat(conf)
	s.True(os.IsNotExist(err), "env file must be removed after msb returns")
}

func (s *clientSuite) TestCreatePassesLabelsOffArgv() {
	const (
		secret   = "csecret-7f3a9c41d2" // #nosec G101 -- test fixture
		metadata = `[{"containerEnv":{"T":"` + secret + `"},"remoteEnv":{"U":"${x}"}}]`
	)
	s.Require().NoError(s.client.Create(
		context.Background(),
		wsName,
		Spec{
			Image:  testImg,
			Env:    map[string]string{testEnvKey: secret},
			Labels: map[string]string{"devcontainer.metadata": metadata, testUserLabel: testUser},
		},
	))
	for _, a := range s.args() {
		s.NotContains(a, secret)
		s.NotContains(a, "devcontainer.metadata")
		s.NotEqual("--label", a)
	}
	data, err := os.ReadFile(s.record + ".runtime")
	s.Require().NoError(err)
	var got struct {
		Conf    string            `json:"conf"`
		Environ map[string]string `json:"environ"`
	}
	s.Require().NoError(json.Unmarshal(data, &got))
	s.JSONEq(
		`{"env":{"TOKEN":"${DEVSY_MSB_ENV_0}"},"labels":{"devcontainer.metadata":"${DEVSY_MSB_LABEL_0}",`+
			`"devsy.sh/user":"${DEVSY_MSB_LABEL_1}"}}`,
		got.Conf,
	)
	s.NotContains(got.Conf, secret)
	s.Equal(metadata, got.Environ["DEVSY_MSB_LABEL_0"])
	s.Equal(testUser, got.Environ["DEVSY_MSB_LABEL_1"])
}

func (s *clientSuite) TestCreateResolvesSymlinkedBindSources() {
	root := s.T().TempDir()
	target := filepath.Join(root, "target")
	s.Require().NoError(os.Mkdir(target, 0o750))
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == testWindows && deniedSymlink(err) {
			s.T().Skip("creating symlinks requires privilege on this Windows account")
		}
		s.Require().NoError(err)
	}
	missing := filepath.Join(root, "missing")
	resolvedReal, err := filepath.EvalSymlinks(target)
	s.Require().NoError(err)
	mounts := []Mount{
		{Source: link, Target: "/workspaces/link"},
		{Source: missing, Target: "/workspaces/missing"},
		{Volume: testVolume, Target: link},
	}
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "exists")
	s.Require().NoError(s.client.Create(
		context.Background(), wsName, Spec{Image: testImg, Mounts: mounts},
	))
	args := s.args()
	s.True(hasFlagValue(args, "--mount-dir", resolvedReal+":/workspaces/link"), "%v", args)
	s.True(hasFlagValue(args, "--mount-dir", missing+":/workspaces/missing"), "%v", args)
	s.True(hasFlagValue(args, "--mount-named", testVolume+":"+link), "%v", args)
	s.Equal(link, mounts[0].Source, "caller mounts must not be modified")
}

func (s *clientSuite) TestCreateWithoutEnvOmitsRuntimeConf() {
	s.Require().NoError(s.client.Create(context.Background(), wsName, Spec{Image: testImg}))
	s.NotContains(s.args(), flagRuntime)
}

func (s *clientSuite) TestExecCancellationWithBlockedInput() {
	s.T().Setenv("DEVSY_MSB_TEST_MODE", testWait)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	started := make(chan struct{})
	input := &startedReader{ReadCloser: reader, started: started}
	err := s.client.Exec(ctx, wsName, ExecRequest{
		Argv: []string{testWait}, Stdin: input,
		Stdout: inputCancelWriter{cancel: cancel, started: started},
	})
	s.Error(err)
	s.ErrorIs(ctx.Err(), context.Canceled)
	_, err = writer.Write([]byte("closed"))
	s.ErrorIs(err, io.ErrClosedPipe)
}

func (s *clientSuite) TestExecClosesBlockedInputOnExit() {
	s.T().Setenv("DEVSY_MSB_TEST_MODE", "early-exit")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	reader, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	s.NoError(s.client.Exec(ctx, wsName, ExecRequest{Argv: []string{"exit"}, Stdin: reader}))
	s.NoError(ctx.Err())
	_, err := writer.Write([]byte("closed"))
	s.ErrorIs(err, io.ErrClosedPipe)
}

func (s *clientSuite) TestExecPreservesInputFailure() {
	for _, inputErr := range []error{
		errors.New("fixture input failure"),
		&os.PathError{Op: "write", Path: "input-source", Err: errors.New("input transport failure")},
	} {
		s.Run(inputErr.Error(), func() {
			err := s.client.Exec(context.Background(), wsName, ExecRequest{
				Argv:  []string{"cat"},
				Stdin: io.NopCloser(failedReader{err: inputErr}), Stderr: io.Discard,
			})
			s.ErrorIs(err, inputErr)
		})
	}
}

func (s *clientSuite) TestInstallationRejectsUnusablePaths() {
	dir := s.T().TempDir()
	s.Error((Client{Binary: dir}).EnsureInstalled(context.Background()))
	if runtime.GOOS != testWindows {
		file := filepath.Join(dir, "not-executable")
		s.Require().NoError(os.WriteFile(file, []byte("not executable"), 0o600))
		s.Error((Client{Binary: file}).EnsureInstalled(context.Background()))
	}
}

func (s *clientSuite) TestDiscoverySkipsUnusableHomeCandidate() {
	home := s.T().TempDir()
	s.T().Setenv("HOME", home)
	s.T().Setenv("USERPROFILE", home)
	s.T().Setenv("PATH", s.T().TempDir())
	s.Require().NoError(os.MkdirAll(filepath.Join(home, ".local", "bin", "msb"), 0o700))
	dir := filepath.Join(home, ".microsandbox", "bin")
	s.Require().NoError(os.MkdirAll(dir, 0o700))
	filename := "msb"
	if runtime.GOOS == testWindows {
		filename += ".exe"
	}
	binary, err := os.ReadFile(s.client.Binary)
	s.Require().NoError(err)
	candidate := filepath.Join(dir, filename)
	// #nosec G306,G703 -- executable test fixture in a private temporary home.
	s.Require().NoError(os.WriteFile(candidate, binary, 0o700))
	s.Equal(candidate, (Client{}).binary())
	s.NoError((Client{}).EnsureInstalled(context.Background()))
}

// TestCreateRedactsBindPathForms covers path spelling changes in CLI diagnostics.
func (s *clientSuite) TestCreateRedactsBindPathForms() {
	for _, form := range []string{"relative", "symlink"} {
		s.Run(form, func() {
			root := s.T().TempDir()
			s.T().Chdir(root)
			source := filepath.Join("private-bind", "missing")
			printed, err := filepath.Abs(source)
			s.Require().NoError(err)
			if form == "symlink" {
				printed = filepath.Join(root, "target")
				s.Require().NoError(os.Mkdir(printed, 0o750))
				source = filepath.Join(root, "link")
				if err := os.Symlink(printed, source); err != nil {
					if runtime.GOOS == testWindows && deniedSymlink(err) {
						s.T().Skip("creating symlinks requires privilege on this Windows account")
					}
					s.Require().NoError(err)
				}
				printed, err = filepath.EvalSymlinks(printed)
				s.Require().NoError(err)
			}
			s.T().Setenv("DEVSY_MSB_TEST_MODE", "creation-diagnostic")
			s.T().Setenv("DEVSY_MSB_TEST_DIAGNOSTIC", "rejected "+source+" / "+printed)
			err = s.client.Create(context.Background(), wsName, Spec{
				Image: testImg, Mounts: []Mount{{Source: source, Target: "/private"}},
			})
			s.Require().EqualError(err, "msb run: rejected *** / ***: exit status 1")
		})
	}
}

// TestCreateRedactsMountResolutionFailure rejects a loop without exposing its path or starting msb.
func (s *clientSuite) TestCreateRedactsMountResolutionFailure() {
	source := filepath.Join(s.T().TempDir(), "private-bind")
	if err := os.Symlink(source, source); err != nil {
		if runtime.GOOS == testWindows && deniedSymlink(err) {
			s.T().Skip("creating symlinks requires privilege on this Windows account")
		}
		s.Require().NoError(err)
	}
	err := s.client.Create(context.Background(), wsName, Spec{
		Image: testImg, Mounts: []Mount{{Source: source, Target: "/private"}},
	})
	s.Require().Error(err)
	s.NotContains(err.Error(), source)
	s.Contains(err.Error(), "resolve bind mount host path")
	s.NoFileExists(s.record)
}

func (s *clientSuite) args() []string {
	data, err := os.ReadFile(s.record)
	s.Require().NoError(err)
	var args []string
	s.Require().NoError(json.Unmarshal(data, &args))
	return args
}

func TestMain(m *testing.M) {
	if os.Getenv(helperEnv) == "1" {
		os.Exit(runHelper())
	}
	os.Exit(m.Run())
}

func runHelper() int {
	args := os.Args[1:]
	if err := recordHelper(args); err != nil {
		return 1
	}
	if len(args) == 0 {
		return 1
	}
	mode := os.Getenv("DEVSY_MSB_TEST_MODE")
	if mode == "creation-diagnostic" {
		_ = writeHelper(os.Stderr, os.Getenv("DEVSY_MSB_TEST_DIAGNOSTIC"))
		return 1
	}
	code := dispatchHelper(args, mode)
	if mode == "fail" {
		return 7
	}
	return code
}

func recordHelper(args []string) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	// #nosec G703 -- path is set by the parent test to a private temporary directory.
	if err := os.WriteFile(os.Getenv("DEVSY_MSB_TEST_RECORD"), data, 0o600); err != nil {
		return err
	}
	return recordRuntimeConf(args)
}

// recordRuntimeConf captures what a real msb would read: the --runtime-conf
// file and the host variables it references.
func recordRuntimeConf(args []string) error {
	idx := slices.Index(args, flagRuntime)
	if idx < 0 || idx+1 >= len(args) {
		return nil
	}
	// #nosec G304 G703 -- path comes from the parent test's own invocation.
	conf, err := os.ReadFile(args[idx+1])
	if err != nil {
		return err
	}
	environ := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && isConfVar(k) {
			environ[k] = v
		}
	}
	data, err := json.Marshal(map[string]any{"conf": string(conf), "environ": environ})
	if err != nil {
		return err
	}
	// #nosec G703 -- path is set by the parent test to a private temporary directory.
	return os.WriteFile(os.Getenv("DEVSY_MSB_TEST_RECORD")+".runtime", data, 0o600)
}

// errorPrivilegeNotHeld is the Windows error for symlink creation without privilege.
const errorPrivilegeNotHeld = syscall.Errno(1314)

func deniedSymlink(err error) bool {
	return errors.Is(err, fs.ErrPermission) || errors.Is(err, errorPrivilegeNotHeld)
}

func isConfVar(name string) bool {
	return strings.HasPrefix(name, envConfPrefix) || strings.HasPrefix(name, labelConfPrefix)
}

func dispatchHelper(args []string, mode string) int {
	switch args[0] {
	case "--version":
		if mode == "invalid-stalled-version" {
			time.Sleep(time.Minute)
		}
		return writeHelper(os.Stdout, "microsandbox 0.7.2\n")
	case "inspect":
		return inspectHelper(mode)
	case testExec:
		return execHelper(mode)
	case "logs":
		return logsHelper()
	case "volume":
		return volumeHelper(mode)
	default:
		return imageHelper(args, mode)
	}
}

func volumeHelper(mode string) int {
	if mode == "exists" {
		_ = writeHelper(os.Stderr, "already exists")
		return 1
	}
	return 0
}

func writeHelper(w io.Writer, value string) int {
	if _, err := io.WriteString(w, value); err != nil {
		return 1
	}
	return 0
}

func execHelper(mode string) int {
	if mode == "early-exit" {
		return 0
	}
	if mode == testWait {
		if _, err := io.WriteString(os.Stdout, "ready"); err != nil {
			return 1
		}
		time.Sleep(time.Minute)
		return 1
	}
	if _, err := io.Copy(os.Stdout, os.Stdin); err != nil {
		return 1
	}
	if _, err := io.WriteString(os.Stderr, stderrFixture); err != nil {
		return 1
	}
	return 0
}

func inspectHelper(mode string) int {
	if mode == testNotFound || mode == testDenied {
		_, _ = io.WriteString(os.Stderr, mode)
		return 1
	}
	if mode == testInvalid || mode == "invalid-stalled-version" {
		_, _ = io.WriteString(os.Stdout, "not json")
		return 0
	}
	data := map[string]any{
		"name":          wsName,
		"status":        mode,
		"created_at":    "2026-01-01T00:00:00Z",
		"active_config": map[string]any{"labels": map[string]string{"devsy.sh/user": testUser}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(data); err != nil {
		return 1
	}
	return 0
}

func logsHelper() int {
	if _, err := io.WriteString(os.Stdout, "stdout log\n"); err != nil {
		return 1
	}
	if _, err := io.WriteString(os.Stderr, "stderr log\n"); err != nil {
		return 1
	}
	return 0
}

type startedReader struct {
	io.ReadCloser
	started chan struct{}
	once    sync.Once
}

func (r *startedReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started) })
	return r.ReadCloser.Read(p)
}

type inputCancelWriter struct {
	cancel  context.CancelFunc
	started <-chan struct{}
}

func (w inputCancelWriter) Write(p []byte) (int, error) {
	<-w.started
	w.cancel()
	return len(p), nil
}

type failedReader struct{ err error }

func (r failedReader) Read(_ []byte) (int, error) { return 0, r.err }
