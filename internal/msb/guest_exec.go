package msb

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/blang/semver/v4"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

// ErrExecUnsupported identifies an installation too old for structured execution.
var ErrExecUnsupported = errors.New("structured MicroSandbox execution requires msb 0.7.7 or newer")

// Execute returns only authoritative guest completion; backend errors never carry a guest code.
// Like Exec, it owns and closes the request's input.
func (c Client) Execute(ctx context.Context, name string, req ExecRequest) (code int, err error) {
	pendingInput := req.Stdin
	defer func() {
		if pendingInput != nil {
			_ = pendingInput.Close()
		}
	}()
	if err := c.validateExecution(ctx, req); err != nil {
		return 0, err
	}
	sandbox, err := connectGuest(ctx, name)
	if err != nil {
		return 0, err
	}
	// Detach releases the SDK handle without acquiring ownership of the VM lifecycle.
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err = errors.Join(err, sandbox.Detach(cleanup))
		if err != nil {
			code = 0
		}
	}()
	stream, err := sandbox.ExecStream(
		ctx,
		req.Argv[0],
		req.Argv[1:],
		microsandbox.WithExecUser(req.User),
		microsandbox.WithExecStdinPipe(),
	)
	if err != nil {
		return 0, fmt.Errorf("start guest execution: %w", err)
	}
	input := stream.TakeStdin()
	if input == nil {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		killErr := stream.Kill(cleanup)
		stop()
		return 0, errors.Join(
			errors.New("guest execution has no stdin sink"),
			killErr,
			stream.Close(),
		)
	}
	pendingInput = nil
	return pumpGuest(ctx, &nativeExecution{ExecHandle: stream, input: input}, req)
}

type guestExecution interface {
	Recv(context.Context) (*microsandbox.ExecEvent, error)
	Write(context.Context, []byte) (int, error)
	CloseInput(context.Context) error
	Kill(context.Context) error
	Close() error
}

type nativeExecution struct {
	*microsandbox.ExecHandle
	input *microsandbox.ExecSink
}

func (e *nativeExecution) Write(ctx context.Context, data []byte) (int, error) {
	return e.input.WriteCtx(ctx, data)
}

func (e *nativeExecution) CloseInput(ctx context.Context) error {
	// ExecStdin with an empty payload is guest EOF, the same wire message as
	// ExecSink.Close. WriteCtx also makes EOF delivery cancellable.
	_, err := e.input.WriteCtx(ctx, nil)
	return err
}

type guestPump struct {
	ctx       context.Context
	cancel    context.CancelCauseFunc
	execution guestExecution
	req       ExecRequest
	copied    chan struct{}
	mu        sync.Mutex
	complete  bool
}

func (c Client) validateExecution(ctx context.Context, req ExecRequest) error {
	if len(req.Argv) == 0 || req.Argv[0] == "" {
		return errors.New("guest execution requires an executable")
	}
	raw, err := c.Version(ctx)
	if err != nil {
		return err
	}
	version, err := ParseVersion(raw)
	if err != nil {
		return err
	}
	if version.LT(semver.Version{Major: 0, Minor: 7, Patch: 7}) {
		return ErrExecUnsupported
	}
	return nil
}

func pumpGuest(
	parent context.Context,
	execution guestExecution,
	req ExecRequest,
) (code int, err error) {
	if req.Stdout == nil {
		req.Stdout = io.Discard
	}
	if req.Stderr == nil {
		req.Stderr = io.Discard
	}
	ctx, cancel := context.WithCancelCause(parent)
	pump := &guestPump{
		ctx:       ctx,
		cancel:    cancel,
		execution: execution,
		req:       req,
		copied:    make(chan struct{}),
	}
	go pump.forwardInput()
	defer func() {
		err = errors.Join(err, pump.close())
		if err != nil {
			code = 0
		}
	}()
	return pump.receive()
}

func (p *guestPump) close() error {
	p.cancel(nil)
	if p.req.Stdin != nil {
		_ = p.req.Stdin.Close()
	}
	var killErr error
	if !p.complete {
		cleanup, stop := context.WithTimeout(context.Background(), 5*time.Second)
		killErr = p.execution.Kill(cleanup)
		stop()
	}
	<-p.copied
	return errors.Join(killErr, p.execution.Close())
}

func (p *guestPump) fail(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.complete && p.ctx.Err() == nil {
		p.cancel(err)
	}
}

func (p *guestPump) finish() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.complete = true
	return context.Cause(p.ctx)
}

func (p *guestPump) forwardInput() {
	defer close(p.copied)
	if p.req.Stdin != nil {
		_, err := io.Copy(guestInputWriter{ctx: p.ctx, execution: p.execution}, p.req.Stdin)
		if err != nil {
			// An early guest stdin close is non-terminal; Recv remains authoritative.
			if _, writeErr := errors.AsType[*guestWriteError](err); !writeErr {
				p.fail(fmt.Errorf("read execution input: %w", err))
			}
			return
		}
	}
	if err := p.execution.CloseInput(p.ctx); err != nil {
		p.fail(fmt.Errorf("close guest input: %w", err))
	}
}

func (p *guestPump) receive() (int, error) {
	for {
		event, err := p.execution.Recv(p.ctx)
		if err != nil {
			if cause := context.Cause(p.ctx); cause != nil {
				return 0, cause
			}
			return 0, fmt.Errorf("receive guest execution: %w", err)
		}
		if event == nil {
			return 0, errors.New("guest execution ended without completion")
		}
		if event.Kind == microsandbox.ExecEventExited {
			if err := p.finish(); err != nil {
				return 0, err
			}
			return event.ExitCode, nil
		}
		if err := p.output(event); err != nil {
			return 0, err
		}
	}
}

func (p *guestPump) output(event *microsandbox.ExecEvent) error {
	switch event.Kind {
	case microsandbox.ExecEventStdout:
		return writeGuestOutput(p.req.Stdout, event.Data)
	case microsandbox.ExecEventStderr:
		return writeGuestOutput(p.req.Stderr, event.Data)
	case microsandbox.ExecEventStarted, microsandbox.ExecEventStdinError:
		return nil
	case microsandbox.ExecEventFailed:
		return fmt.Errorf("guest execution failed to start: %v", event.Failure)
	case microsandbox.ExecEventDone:
		return errors.New("guest execution ended without completion")
	default:
		return fmt.Errorf("unexpected guest execution event %v", event.Kind)
	}
}

type guestInputWriter struct {
	ctx       context.Context
	execution guestExecution
}

func (w guestInputWriter) Write(p []byte) (int, error) {
	n, err := w.execution.Write(w.ctx, p)
	if err != nil {
		return n, &guestWriteError{err: err}
	}
	return n, nil
}

type guestWriteError struct{ err error }

func (e *guestWriteError) Error() string { return e.err.Error() }
func (e *guestWriteError) Unwrap() error { return e.err }

func writeGuestOutput(output io.Writer, data []byte) error {
	n, err := output.Write(data)
	if err == nil && n != len(data) {
		return io.ErrShortWrite
	}
	return err
}

func connectGuest(ctx context.Context, name string) (*microsandbox.Sandbox, error) {
	handle, err := microsandbox.GetSandbox(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("get execution sandbox: %w", err)
	}
	sandbox, err := handle.Connect(ctx)
	if err != nil {
		return nil, fmt.Errorf("connect execution sandbox: %w", err)
	}
	return sandbox, nil
}
