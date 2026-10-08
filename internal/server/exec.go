package server

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"os/exec"
	"sync"

	"github.com/devsy-org/devsy-provider-microsandbox/internal/msb"
	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
)

// Exec bridges non-PTY, literal argv and binary streams to msb exec --stream.
func (r *Runtime) Exec(
	stream grpc.BidiStreamingServer[runtimev1.ExecClientMessage, runtimev1.ExecServerMessage],
) error {
	first, err := stream.Recv()
	if err == io.EOF {
		return invalidArgument("first Exec frame must be start")
	}
	if err != nil {
		return backendError(stream.Context(), err)
	}
	start := first.GetStart()
	if err := validateExec(stream.Context(), start); err != nil {
		return err
	}
	if err := r.requireRunning(stream.Context(), start.GetWorkspaceId()); err != nil {
		return err
	}
	return r.execute(stream, start)
}

func validateExec(ctx context.Context, start *runtimev1.ExecStart) error {
	if start == nil {
		return invalidArgument("first Exec frame must be start")
	}
	if err := validateWorkspace(ctx, start.GetWorkspaceId()); err != nil {
		return err
	}
	if len(start.GetArgv()) == 0 || start.GetArgv()[0] == "" {
		return invalidArgument("Exec requires a nonempty executable")
	}
	if start.GetTty() || start.GetWorkdir() != "" || len(start.GetEnvironment()) > 0 {
		return runtimeError(
			codes.Unimplemented,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_UNSUPPORTED,
			"MicroSandbox Exec does not support TTY, workdir, or environment overrides",
		)
	}
	return nil
}

func (r *Runtime) requireRunning(ctx context.Context, id string) error {
	found, err := r.Find(ctx, &runtimev1.FindRequest{WorkspaceId: id})
	if err != nil {
		return err
	}
	if !found.GetFound() {
		return missingWorkspace()
	}
	if found.GetContainer().GetState().GetStatus() != "running" {
		return runtimeError(
			codes.FailedPrecondition,
			runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_FAILED_PRECONDITION,
			"workspace is not running",
		)
	}
	return nil
}

func (r *Runtime) execute(
	stream grpc.BidiStreamingServer[runtimev1.ExecClientMessage, runtimev1.ExecServerMessage],
	start *runtimev1.ExecStart,
) error {
	ctx, cancel := context.WithCancelCause(stream.Context())
	defer cancel(nil)
	input, writer := io.Pipe()
	defer func() { _ = input.Close(); _ = writer.Close() }()
	stop := context.AfterFunc(ctx, func() { _ = writer.CloseWithError(context.Cause(ctx)) })
	defer stop()
	// Recv is canceled by gRPC when the handler returns. Do not join a pending
	// Recv here: a command may finish before the caller closes its input stream.
	inputState := &execInputState{cancel: cancel}
	go receiveInput(ctx, inputState.fail, stream, writer)
	output := newFrameSender(
		ctx,
		cancel,
		func(frame *runtimev1.ExecServerMessage) error { return stream.Send(frame) },
	)
	err := r.client.Exec(ctx, sandboxName(start.GetWorkspaceId()), msb.ExecRequest{
		Argv: start.GetArgv(), User: start.GetUser(), Stdin: input,
		Stdout: execOutput{sender: output}, Stderr: execOutput{sender: output, stderr: true},
	})
	inputState.finish()
	if cause := context.Cause(ctx); cause != nil {
		return cause
	}
	exit, err := commandExit(err)
	if err != nil {
		return backendError(stream.Context(), err)
	}
	return output.send(
		&runtimev1.ExecServerMessage{Payload: &runtimev1.ExecServerMessage_Exit{Exit: exit}},
	)
}

func receiveInput(
	ctx context.Context,
	fail func(error),
	stream grpc.BidiStreamingServer[runtimev1.ExecClientMessage, runtimev1.ExecServerMessage],
	writer *io.PipeWriter,
) {
	for {
		frame, err := stream.Recv()
		if err == io.EOF {
			err = invalidArgument("CloseStdin is required")
		}
		if err != nil {
			fail(err)
			_ = writer.CloseWithError(err)
			return
		}
		if _, ok := frame.Payload.(*runtimev1.ExecClientMessage_CloseStdin); ok {
			_ = writer.Close()
			return
		}
		data, err := inputBytes(frame)
		if err != nil {
			fail(err)
			_ = writer.CloseWithError(err)
			return
		}
		if _, err := writer.Write(data); err != nil {
			return
		}
		if ctx.Err() != nil {
			return
		}
	}
}

func commandExit(err error) (*runtimev1.ExecExit, error) {
	if err == nil {
		return &runtimev1.ExecExit{}, nil
	}
	var failed *exec.ExitError
	if !errors.As(err, &failed) {
		return nil, err
	}
	return processExit(failed)
}

func inputBytes(frame *runtimev1.ExecClientMessage) ([]byte, error) {
	data, ok := frame.Payload.(*runtimev1.ExecClientMessage_Stdin)
	if !ok || len(data.Stdin) == 0 || len(data.Stdin) > runtimev1.ChunkSize {
		return nil, invalidArgument("expected 1..32768 stdin bytes or CloseStdin")
	}
	return data.Stdin, nil
}

func exitCode(code int) (*runtimev1.ExecExit, error) {
	if code < 0 || int64(code) > math.MaxInt32 {
		return nil, fmt.Errorf("command exit code %d cannot be represented by Runtime v1", code)
	}
	return &runtimev1.ExecExit{ExitCode: int32(code)}, nil
}

// Command completion freezes input failure reporting. A late client half-close
// must not turn a command that stopped reading early into a protocol failure.
type execInputState struct {
	mu       sync.Mutex
	complete bool
	cancel   context.CancelCauseFunc
}

func (s *execInputState) fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.complete {
		s.cancel(err)
	}
}

func (s *execInputState) finish() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.complete = true
}
