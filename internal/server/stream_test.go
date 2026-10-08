package server

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os/exec"
	"time"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const echoCommand = "echo"

type testExecResult struct {
	stdout, stderr []byte
	exit           *runtimev1.ExecExit
}

func (s *runtimeSuite) streamingClient() (context.Context, runtimev1.RuntimeDriverClient) {
	s.T().Helper()
	s.client.info = &msb.Info{Name: sandboxName(testWorkspace), Running: true}
	listener := bufconn.Listen(1 << 20)
	service := grpc.NewServer()
	runtimev1.RegisterRuntimeDriverServer(service, s.runtime)
	done := make(chan error, 1)
	go func() { done <- service.Serve(listener) }()
	s.T().Cleanup(func() { service.Stop(); s.NoError(<-done); s.NoError(listener.Close()) })
	conn, err := grpc.NewClient(
		"passthrough:///runtime",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(
			func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) },
		),
	)
	s.Require().NoError(err)
	s.T().Cleanup(func() { s.NoError(conn.Close()) })
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	s.T().Cleanup(cancel)
	return ctx, runtimev1.NewRuntimeDriverClient(conn)
}

func (s *runtimeSuite) execStream(
	ctx context.Context,
	client runtimev1.RuntimeDriverClient,
) runtimev1.RuntimeDriver_ExecClient {
	s.T().Helper()
	stream, err := client.Exec(ctx)
	s.Require().NoError(err)
	s.Require().
		NoError(stream.Send(&runtimev1.ExecClientMessage{
			Payload: &runtimev1.ExecClientMessage_Start{Start: &runtimev1.ExecStart{
				WorkspaceId: testWorkspace,
				Argv:        []string{echoCommand, "literal; $value"},
				User:        "developer",
			}},
		}))
	return stream
}

func (s *runtimeSuite) TestExecBinaryDuplexAndExitOrdering() {
	ctx, client := s.streamingClient()
	data := bytes.Repeat([]byte{0, 255, 13, 10, 1}, 20000)
	diagnostics := bytes.Repeat([]byte{255, 0, 10}, 16000)
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		defer func() { _ = req.Stdin.Close() }()
		if !bytes.Equal([]byte(req.Argv[1]), []byte("literal; $value")) || req.User != "developer" {
			return errors.New("argv/user changed")
		}
		if _, err := req.Stderr.Write(diagnostics); err != nil {
			return err
		}
		_, err := io.Copy(req.Stdout, req.Stdin)
		return err
	}
	stream := s.execStream(ctx, client)
	sent := make(chan error, 1)
	go func() { sent <- sendTestInput(stream, data) }()
	result, err := readTestOutput(stream)
	s.Require().NoError(err)
	s.Require().NoError(<-sent)
	s.Equal(data, result.stdout)
	s.Equal(diagnostics, result.stderr)
	s.Equal(int32(0), result.exit.GetExitCode())
}

func sendTestInput(stream runtimev1.RuntimeDriver_ExecClient, data []byte) error {
	for len(data) > 0 {
		n := min(len(data), runtimev1.ChunkSize)
		if err := stream.Send(
			&runtimev1.ExecClientMessage{
				Payload: &runtimev1.ExecClientMessage_Stdin{Stdin: data[:n]},
			},
		); err != nil {
			return err
		}
		data = data[n:]
	}
	if err := stream.Send(
		&runtimev1.ExecClientMessage{
			Payload: &runtimev1.ExecClientMessage_CloseStdin{CloseStdin: &runtimev1.CloseStdin{}},
		},
	); err != nil {
		return err
	}
	return stream.CloseSend()
}

func readTestOutput(
	stream runtimev1.RuntimeDriver_ExecClient,
) (testExecResult, error) {
	var stdout, stderr []byte
	var exit *runtimev1.ExecExit
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			if exit == nil {
				return testExecResult{}, errors.New("missing exit")
			}
			return testExecResult{stdout: stdout, stderr: stderr, exit: exit}, nil
		}
		if err != nil {
			return testExecResult{}, err
		}
		if exit != nil {
			return testExecResult{}, errors.New("frame after exit")
		}
		if terminal := frame.GetExit(); terminal != nil {
			exit = terminal
			continue
		}
		if err := collectTestChunk(frame, &stdout, &stderr); err != nil {
			return testExecResult{}, err
		}
	}
}

func collectTestChunk(frame *runtimev1.ExecServerMessage, stdout, stderr *[]byte) error {
	var chunk []byte
	switch payload := frame.Payload.(type) {
	case *runtimev1.ExecServerMessage_Stdout:
		chunk = payload.Stdout.GetData()
		*stdout = append(*stdout, chunk...)
	case *runtimev1.ExecServerMessage_Stderr:
		chunk = payload.Stderr.GetData()
		*stderr = append(*stderr, chunk...)
	default:
		return errors.New("unknown output")
	}
	if len(chunk) == 0 || len(chunk) > runtimev1.ChunkSize {
		return errors.New("invalid output chunk")
	}
	return nil
}

func (s *runtimeSuite) TestExecEarlyCompletionWithoutStdinClose() {
	ctx, client := s.streamingClient()
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		defer func() { _ = req.Stdin.Close() }()
		return nil
	}
	stream := s.execStream(ctx, client)
	result, err := readTestOutput(stream)
	s.Require().NoError(err)
	s.NotNil(result.exit)
}

func (s *runtimeSuite) TestExecCancellationReleasesBackend() {
	ctx, client := s.streamingClient()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started, finished := make(chan struct{}), make(chan struct{})
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		defer close(finished)
		defer func() { _ = req.Stdin.Close() }()
		close(started)
		_, err := io.Copy(req.Stdout, req.Stdin)
		return err
	}
	stream := s.execStream(ctx, client)
	select {
	case <-started:
	case <-ctx.Done():
		s.T().Fatal("backend did not start")
	}
	cancel()
	_, err := stream.Recv()
	s.Equal(codes.Canceled, status.Code(err))
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		s.T().Fatal("backend not released")
	}
}

func (s *runtimeSuite) TestExecMalformedInputCancelsFlowControlledOutput() {
	ctx, client := s.streamingClient()
	finished := make(chan struct{})
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		defer close(finished)
		defer func() { _ = req.Stdin.Close() }()
		_, err := req.Stdout.Write(make([]byte, 8<<20))
		return err
	}
	stream := s.execStream(ctx, client)
	s.Require().NoError(stream.Send(&runtimev1.ExecClientMessage{}))
	// No output is consumed until the handler has released the backend.
	select {
	case <-finished:
	case <-ctx.Done():
		s.T().Fatal("invalid input left output blocked")
	}
	_, err := readTestOutput(stream)
	s.Equal(codes.InvalidArgument, status.Code(err))
}

func (s *runtimeSuite) TestExecValidationAndAbsence() {
	for _, test := range []struct {
		name  string
		start *runtimev1.ExecStart
		code  codes.Code
	}{
		{"missing start", nil, codes.InvalidArgument},
		{"missing argv", &runtimev1.ExecStart{WorkspaceId: testWorkspace}, codes.InvalidArgument},
		{"tty", &runtimev1.ExecStart{
			WorkspaceId: testWorkspace, Argv: []string{echoCommand}, Tty: true,
		}, codes.Unimplemented},
		{"workdir", &runtimev1.ExecStart{
			WorkspaceId: testWorkspace, Argv: []string{echoCommand}, Workdir: "/tmp",
		}, codes.Unimplemented},
		{"environment", &runtimev1.ExecStart{
			WorkspaceId: testWorkspace, Argv: []string{echoCommand}, Environment: map[string]string{"A": "B"},
		}, codes.Unimplemented},
	} {
		s.Run(test.name, func() {
			ctx, client := s.streamingClient()
			stream, err := client.Exec(ctx)
			s.Require().NoError(err)
			s.Require().
				NoError(stream.Send(&runtimev1.ExecClientMessage{Payload: &runtimev1.ExecClientMessage_Start{Start: test.start}}))
			_, err = stream.Recv()
			s.Equal(test.code, status.Code(err))
		})
	}
}

func (s *runtimeSuite) TestLogsBinaryFramesAndStoppedWorkspace() {
	ctx, client := s.streamingClient()
	s.client.info.Running = false
	data := bytes.Repeat([]byte{0, 255, 10}, 40000)
	s.client.logs = func(_ context.Context, w io.Writer) error { _, err := w.Write(data); return err }
	stream, err := client.Logs(ctx, &runtimev1.LogsRequest{WorkspaceId: testWorkspace})
	s.Require().NoError(err)
	var received []byte
	for {
		chunk, err := stream.Recv()
		if err == io.EOF {
			break
		}
		s.Require().NoError(err)
		s.NotEmpty(chunk.GetData())
		s.LessOrEqual(len(chunk.GetData()), runtimev1.ChunkSize)
		received = append(received, chunk.GetData()...)
	}
	s.Equal(data, received)
}

func (s *runtimeSuite) TestExecBackendFailureIsRPCError() {
	ctx, client := s.streamingClient()
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		defer func() { _ = req.Stdin.Close() }()
		return errBackend
	}
	stream := s.execStream(ctx, client)
	_, err := stream.Recv()
	s.Equal(codes.Internal, status.Code(err))
}

func (s *runtimeSuite) TestExecInputProtocolErrors() {
	for _, test := range []struct {
		name  string
		frame *runtimev1.ExecClientMessage
	}{
		{"half-close", nil},
		{"empty stdin", &runtimev1.ExecClientMessage{Payload: &runtimev1.ExecClientMessage_Stdin{Stdin: []byte{}}}},
		{"oversized stdin", &runtimev1.ExecClientMessage{
			Payload: &runtimev1.ExecClientMessage_Stdin{Stdin: make([]byte, runtimev1.ChunkSize+1)},
		}},
		{"second start", &runtimev1.ExecClientMessage{
			Payload: &runtimev1.ExecClientMessage_Start{Start: &runtimev1.ExecStart{}},
		}},
	} {
		s.Run(test.name, func() {
			ctx, client := s.streamingClient()
			s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
				defer func() { _ = req.Stdin.Close() }()
				_, err := io.Copy(io.Discard, req.Stdin)
				return err
			}
			stream := s.execStream(ctx, client)
			if test.frame == nil {
				s.Require().NoError(stream.CloseSend())
			} else {
				s.Require().NoError(stream.Send(test.frame))
			}
			_, err := stream.Recv()
			s.Equal(codes.InvalidArgument, status.Code(err))
		})
	}
}

func (s *runtimeSuite) TestExecMissingAndStoppedWorkspace() {
	ctx, client := s.streamingClient()
	s.client.info = nil
	stream := s.execStream(ctx, client)
	_, err := stream.Recv()
	s.Equal(codes.NotFound, status.Code(err))
	s.client.info = &msb.Info{Running: false}
	stream = s.execStream(ctx, client)
	_, err = stream.Recv()
	s.Equal(codes.FailedPrecondition, status.Code(err))
}

func (s *runtimeSuite) TestLogsCancellation() {
	ctx, client := s.streamingClient()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	started, finished := make(chan struct{}), make(chan struct{})
	s.client.logs = func(ctx context.Context, _ io.Writer) error {
		defer close(finished)
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	stream, err := client.Logs(ctx, &runtimev1.LogsRequest{WorkspaceId: testWorkspace})
	s.Require().NoError(err)
	select {
	case <-started:
	case <-ctx.Done():
		s.T().Fatal("logs did not start")
	}
	cancel()
	_, err = stream.Recv()
	s.Equal(codes.Canceled, status.Code(err))
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		s.T().Fatal("logs backend did not stop")
	}
}

func (s *runtimeSuite) TestCompletedExecIgnoresLateInputFailure() {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	state := &execInputState{cancel: cancel}
	state.finish()
	state.fail(invalidArgument("late half-close"))
	s.Nil(context.Cause(ctx))
}

func (s *runtimeSuite) TestActiveExecInputFailureWinsCompletion() {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	state := &execInputState{cancel: cancel}
	failure := invalidArgument("invalid frame")
	state.fail(failure)
	state.finish()
	s.Equal(failure, context.Cause(ctx))
}

func (s *runtimeSuite) TestExecHalfCloseWithoutStart() {
	ctx, client := s.streamingClient()
	stream, err := client.Exec(ctx)
	s.Require().NoError(err)
	s.Require().NoError(stream.CloseSend())
	_, err = stream.Recv()
	s.Equal(codes.InvalidArgument, status.Code(err))
	s.Empty(s.client.calls)
}

func (s *runtimeSuite) TestExecGuestNonzeroExitIsNotBackendFailure() {
	ctx, client := s.streamingClient()
	s.client.exitCode = 1
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		return req.Stdin.Close()
	}
	stream := s.execStream(ctx, client)
	result, err := readTestOutput(stream)
	s.Require().NoError(err)
	s.Equal(int32(1), result.exit.GetExitCode())
}

func (s *runtimeSuite) TestExecBackendProcessExitIsRPCFailure() {
	ctx, client := s.streamingClient()
	s.client.exec = func(_ context.Context, req msb.ExecRequest) error {
		_ = req.Stdin.Close()
		return &exec.ExitError{}
	}
	stream := s.execStream(ctx, client)
	frame, err := stream.Recv()
	s.Nil(frame)
	s.Equal(codes.Internal, status.Code(err))
}
