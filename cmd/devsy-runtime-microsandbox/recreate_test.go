package main

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	fixtureRecordEnv    = "DEVSY_MSB_ENTRYPOINT_RECORD"
	recreateWorkspaceID = "workspace"
)

func (s *entrypointSuite) TestRecreatePermissionIsPerPluginProcess() {
	image := s.loopbackImage()
	dir := s.T().TempDir()

	firstRecord := filepath.Join(dir, "info-then-denied.jsonl")
	first := s.pluginClientWithEnv(
		"DEVSY_MSB_ENTRYPOINT_VERSION=0.7.7",
		"DEVSY_MSB_ENTRYPOINT_EXISTING=1",
		fixtureRecordEnv+"="+firstRecord,
	)
	info, err := first.Info(s.ctx(), &runtimev1.InfoRequest{SupportsRecreateOnRun: true})
	s.Require().NoError(err)
	s.Equal(runtimev1.RecreateMode_RECREATE_MODE_ON_RUN, info.GetCapabilities().GetRecreateMode())
	_, err = first.RunImage(s.ctx(), recreateRequest(image, false))
	s.requireAlreadyExists(err)
	s.assertNoReplaceOrDestructiveCLI(firstRecord)

	// This is a new provider process with no preceding Info negotiation. The
	// explicit field on this request alone must authorize the replacement.
	allowedRecord := filepath.Join(dir, "fresh-allowed.jsonl")
	allowed := s.pluginClientWithEnv(
		"DEVSY_MSB_ENTRYPOINT_VERSION=0.7.7",
		"DEVSY_MSB_ENTRYPOINT_EXISTING=1",
		fixtureRecordEnv+"="+allowedRecord,
	)
	_, err = allowed.RunImage(s.ctx(), recreateRequest(image, true))
	s.Require().NoError(err)
	allowedCalls := s.readMSBInvocations(allowedRecord)
	s.Equal(1, s.countCLI(allowedCalls, "load"))
	s.Equal(1, s.countCLI(allowedCalls, "run"))
	s.Equal(0, s.countCLI(allowedCalls, "stop"))
	s.Equal(0, s.countCLI(allowedCalls, "remove"))
	for _, call := range allowedCalls {
		if len(call) > 0 && call[0] == "run" {
			s.Equal(1, countArgument(call, "--replace"))
		}
	}

	// A third fresh process gets no permission from either earlier process.
	deniedRecord := filepath.Join(dir, "fresh-denied.jsonl")
	denied := s.pluginClientWithEnv(
		"DEVSY_MSB_ENTRYPOINT_VERSION=0.7.7",
		"DEVSY_MSB_ENTRYPOINT_EXISTING=1",
		fixtureRecordEnv+"="+deniedRecord,
	)
	_, err = denied.RunImage(s.ctx(), recreateRequest(image, false))
	s.requireAlreadyExists(err)
	s.assertNoReplaceOrDestructiveCLI(deniedRecord)
}

func recreateRequest(image string, allow bool) *runtimev1.RunImageRequest {
	return &runtimev1.RunImageRequest{
		WorkspaceId:   recreateWorkspaceID,
		Image:         image,
		AllowRecreate: allow,
	}
}

func (s *entrypointSuite) loopbackImage() string {
	s.T().Helper()
	server := httptest.NewServer(registry.New(registry.Logger(log.New(io.Discard, "", 0))))
	s.T().Cleanup(server.Close)
	ref, err := name.NewTag(
		strings.TrimPrefix(server.URL, "http://")+"/fixture:latest",
		name.Insecure,
	)
	s.Require().NoError(err)
	s.Require().NoError(remote.Write(ref, empty.Image))
	return ref.Name()
}

func (s *entrypointSuite) requireAlreadyExists(err error) {
	s.T().Helper()
	s.Require().Error(err)
	s.Equal(codes.AlreadyExists, status.Code(err))
	details := status.Convert(err).Details()
	s.Require().Len(details, 1)
	actual, ok := details[0].(*runtimev1.RuntimeError)
	s.Require().True(ok)
	s.Equal(runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_ALREADY_EXISTS, actual.GetCode())
}

func (s *entrypointSuite) assertNoReplaceOrDestructiveCLI(path string) {
	s.T().Helper()
	calls := s.readMSBInvocations(path)
	s.Equal(0, s.countCLI(calls, "load"))
	s.Equal(0, s.countCLI(calls, "run"))
	s.Equal(0, s.countCLI(calls, "stop"))
	s.Equal(0, s.countCLI(calls, "remove"))
	for _, call := range calls {
		s.NotContains(call, "--replace")
	}
}

func (s *entrypointSuite) readMSBInvocations(path string) [][]string {
	s.T().Helper()
	// #nosec G304 -- The record file is owned by this plugin test fixture.
	file, err := os.Open(path)
	s.Require().NoError(err)
	defer func() { s.NoError(file.Close()) }()
	var calls [][]string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var call []string
		s.Require().NoError(json.Unmarshal(scanner.Bytes(), &call))
		calls = append(calls, call)
	}
	s.Require().NoError(scanner.Err())
	return calls
}

func (s *entrypointSuite) countCLI(calls [][]string, command string) int {
	s.T().Helper()
	count := 0
	for _, call := range calls {
		if len(call) > 0 && call[0] == command {
			count++
		}
	}
	return count
}

func countArgument(args []string, want string) int {
	count := 0
	for _, arg := range args {
		if arg == want {
			count++
		}
	}
	return count
}
