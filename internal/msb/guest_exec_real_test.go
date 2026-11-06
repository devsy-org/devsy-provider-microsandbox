package msb

import (
	"bytes"
	"context"
	"io"
	"os"
	"runtime"
	"runtime/pprof"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	microsandbox "github.com/superradcompany/microsandbox/sdk/go"
)

func TestRealGuestEOF(t *testing.T) {
	if os.Getenv("DEVSY_QUALIFY_RUNTIME") != "1" {
		t.Skip("requires isolated MicroSandbox 0.7.13 runtime pair and DEVSY_QUALIFY_SANDBOX")
	}
	name := os.Getenv("DEVSY_QUALIFY_SANDBOX")
	require.NotEmpty(t, name)
	c := Client{Binary: os.Getenv("MSB_PATH")}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	var out bytes.Buffer
	code, err := c.Execute(
		ctx,
		name,
		ExecRequest{
			Argv:   []string{"/bin/cat"},
			Stdin:  io.NopCloser(strings.NewReader("EOF proof\n")),
			Stdout: &out,
		},
	)
	require.NoError(t, err)
	require.Zero(t, code)
	require.Equal(t, "EOF proof\n", out.String())
	testRealEOFCancellation(ctx, t, name)
	baseline := settledGuestGoroutines(t)
	cancelRealGuest(ctx, t, c, name)
	afterOne := settledGuestGoroutines(t)
	for range 50 {
		cancelRealGuest(ctx, t, c, name)
	}
	afterFifty := settledGuestGoroutines(t)
	t.Logf(
		"goroutines: baseline=%d after cancellation=%d after 50 more cancellations=%d",
		baseline,
		afterOne,
		afterFifty,
	)
	require.LessOrEqual(t, afterFifty, baseline)
}

type guestReadyOutput struct {
	once  sync.Once
	ready chan struct{}
}

func (w *guestReadyOutput) Write(p []byte) (int, error) {
	w.once.Do(func() { close(w.ready) })
	return len(p), nil
}

func cancelRealGuest(parent context.Context, t *testing.T, c Client, name string) {
	t.Helper()
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	input, writer := io.Pipe()
	defer func() { _ = writer.Close() }()
	output := &guestReadyOutput{ready: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := c.Execute(
			ctx,
			name,
			ExecRequest{
				Argv:   []string{"/bin/sh", "-c", "echo ready; exec cat"},
				Stdin:  input,
				Stdout: output,
			},
		)
		done <- err
	}()
	select {
	case <-output.ready:
	case err := <-done:
		require.NoError(t, err)
		t.Fatal("execution ended before readiness")
	case <-time.After(10 * time.Second):
		t.Fatal("guest never became ready")
	}
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("Execute hung after cancellation")
	}
}

func settledGuestGoroutines(t *testing.T) int {
	t.Helper()
	time.Sleep(time.Second)
	var dump bytes.Buffer
	require.NoError(t, pprof.Lookup("goroutine").WriteTo(&dump, 2))
	for _, frame := range []string{"closeGuestInput.func", "(*ExecSink).Close", "(*guestPump).forwardInput"} {
		require.NotContains(t, dump.String(), frame)
	}
	n := runtime.NumGoroutine()
	t.Logf("settled goroutines=%d\n%s", n, dump.String())
	return n
}

type observedGuestEOF struct {
	guestInput
	entered chan struct{}
}

func (s *observedGuestEOF) Close() error {
	close(s.entered)
	return s.guestInput.Close()
}

type observedGuestKill struct {
	*nativeExecution
	killed chan struct{}
}

func (e *observedGuestKill) Kill(ctx context.Context) error {
	close(e.killed)
	return e.nativeExecution.Kill(ctx)
}

func testRealEOFCancellation(ctx context.Context, t *testing.T, name string) {
	t.Helper()
	sandbox, err := connectGuest(ctx, name)
	require.NoError(t, err)
	defer func() { require.NoError(t, sandbox.Detach(ctx)) }()
	stream, err := sandbox.ExecStream(
		ctx,
		"/bin/sleep",
		[]string{"60"},
		microsandbox.WithExecStdinPipe(),
	)
	require.NoError(t, err)
	sink := &observedGuestEOF{guestInput: stream.TakeStdin(), entered: make(chan struct{})}
	require.NotNil(t, sink.guestInput)
	execution := &observedGuestKill{
		nativeExecution: &nativeExecution{
			ExecHandle: stream,
			input:      sink,
		},
		killed: make(chan struct{}),
	}
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := pumpGuest(child, execution, ExecRequest{}); done <- err }()
	select {
	case <-sink.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("native EOF did not start")
	}
	started := time.Now()
	cancel()
	select {
	case err := <-done:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(10 * time.Second):
		t.Fatal("native EOF cleanup hung")
	}
	select {
	case <-execution.killed:
	default:
		t.Fatal("cancellation did not Kill before cleanup completed")
	}
	t.Logf("native EOF cancellation cleanup: %s", time.Since(started))
}
