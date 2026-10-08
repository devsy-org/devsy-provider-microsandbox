package msb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

type guestSuite struct{ suite.Suite }

func TestGuestExecution(t *testing.T) { suite.Run(t, new(guestSuite)) }

type guestFixture struct {
	events      []*microsandbox.ExecEvent
	failure     error
	closeErr    error
	input       bytes.Buffer
	eof         chan struct{}
	waitInput   bool
	waitContext bool
	killed      bool
	closed      bool
	once        sync.Once
}

func (g *guestFixture) Recv(ctx context.Context) (*microsandbox.ExecEvent, error) {
	if len(g.events) > 0 {
		event := g.events[0]
		g.events = g.events[1:]
		if g.waitInput && event.Kind == microsandbox.ExecEventExited {
			select {
			case <-g.eof:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return event, nil
	}
	if g.waitContext {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return nil, g.failure
}
func (g *guestFixture) Write(_ context.Context, p []byte) (int, error) { return g.input.Write(p) }

func (g *guestFixture) CloseInput(context.Context) error {
	g.once.Do(func() { close(g.eof) })
	return nil
}

func (g *guestFixture) Kill(
	context.Context,
) error {
	g.killed = true
	return nil
}

func (g *guestFixture) Close() error { g.closed = true; return g.closeErr }

func (s *guestSuite) TestBinaryStreamsAndGuestExitOne() {
	data := bytes.Repeat([]byte{0, 255, 13, 10}, 20000)
	g := &guestFixture{eof: make(chan struct{}), waitInput: true, events: []*microsandbox.ExecEvent{
		{Kind: microsandbox.ExecEventStarted},
		{Kind: microsandbox.ExecEventStdout, Data: data},
		{Kind: microsandbox.ExecEventStderr, Data: []byte{255, 0, 10}},
		{Kind: microsandbox.ExecEventExited, ExitCode: 1},
	}}
	var out, diagnostic bytes.Buffer
	code, err := pumpGuest(
		context.Background(),
		g,
		ExecRequest{Stdin: io.NopCloser(bytes.NewReader(data)), Stdout: &out, Stderr: &diagnostic},
	)
	s.Require().NoError(err)
	s.Equal(1, code)
	s.Equal(data, out.Bytes())
	s.Equal([]byte{255, 0, 10}, diagnostic.Bytes())
	s.Equal(data, g.input.Bytes())
	s.False(g.killed)
	s.True(g.closed)
}

func (s *guestSuite) TestBackendFailureCannotBecomeGuestExit() {
	for _, g := range []*guestFixture{
		{failure: &exec.ExitError{}},
		{events: []*microsandbox.ExecEvent{{
			Kind:    microsandbox.ExecEventFailed,
			Failure: &microsandbox.ExecFailure{Message: "transport lost"},
		}}},
		{events: []*microsandbox.ExecEvent{{Kind: microsandbox.ExecEventDone}}},
		{},
	} {
		g.eof = make(chan struct{})
		code, err := pumpGuest(
			context.Background(),
			g,
			ExecRequest{Stdout: io.Discard, Stderr: io.Discard},
		)
		s.Error(err)
		s.Zero(code)
		s.True(g.killed)
		s.True(g.closed)
	}
}

func (s *guestSuite) TestCancellationReleasesBlockedInputAndKillsGuest() {
	input, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	ctx, cancel := context.WithCancel(context.Background())
	g := &guestFixture{eof: make(chan struct{}), waitContext: true}
	done := make(chan error, 1)
	go func() {
		_, err := pumpGuest(
			ctx,
			g,
			ExecRequest{Stdin: input, Stdout: io.Discard, Stderr: io.Discard},
		)
		done <- err
	}()
	cancel()
	select {
	case err := <-done:
		s.ErrorIs(err, context.Canceled)
	case <-time.After(5 * time.Second):
		s.FailNow("guest cancellation did not finish")
	}
	s.True(g.killed)
	s.True(g.closed)
}

func (s *guestSuite) TestGuestCompletionReleasesInputWithoutEOF() {
	input, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	g := &guestFixture{
		eof:    make(chan struct{}),
		events: []*microsandbox.ExecEvent{{Kind: microsandbox.ExecEventExited}},
	}
	done := make(chan error, 1)
	go func() { _, err := pumpGuest(context.Background(), g, ExecRequest{Stdin: input}); done <- err }()
	select {
	case err := <-done:
		s.NoError(err)
	case <-time.After(5 * time.Second):
		s.FailNow("guest completion waited for input EOF")
	}
	s.False(g.killed)
	s.True(g.closed)
}

type brokenGuestOutput struct{}

func (brokenGuestOutput) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }

func (s *guestSuite) TestOutputFailureKillsGuest() {
	g := &guestFixture{
		eof: make(chan struct{}),
		events: []*microsandbox.ExecEvent{
			{Kind: microsandbox.ExecEventStdout, Data: []byte("output")},
		},
	}
	_, err := pumpGuest(context.Background(), g, ExecRequest{Stdout: brokenGuestOutput{}})
	s.ErrorIs(err, io.ErrClosedPipe)
	s.True(g.killed)
	s.True(g.closed)
}

func (s *guestSuite) TestCompletedGuestIgnoresLateInputFailure() {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	p := &guestPump{ctx: ctx, cancel: cancel}
	s.NoError(p.finish())
	p.fail(errors.New("late input close"))
	s.Nil(context.Cause(ctx))
}

func (s *guestSuite) TestActiveInputFailurePreventsCompletion() {
	ctx, cancel := context.WithCancelCause(context.Background())
	defer cancel(nil)
	p := &guestPump{ctx: ctx, cancel: cancel}
	failure := errors.New("input failed")
	p.fail(failure)
	s.ErrorIs(p.finish(), failure)
}

func (s *guestSuite) TestCleanupFailureDoesNotReturnGuestCode() {
	failure := errors.New("release failed")
	g := &guestFixture{
		eof:      make(chan struct{}),
		closeErr: failure,
		events: []*microsandbox.ExecEvent{
			{Kind: microsandbox.ExecEventExited, ExitCode: 7},
		},
	}
	code, err := pumpGuest(context.Background(), g, ExecRequest{})
	s.ErrorIs(err, failure)
	s.Zero(code)
	s.True(g.closed)
}
