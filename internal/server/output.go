package server

import (
	"context"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
)

type pendingFrame struct {
	frame *runtimev1.ExecServerMessage
	done  chan error
}

type frameSender struct {
	ctx   context.Context
	queue chan pendingFrame
}

func newFrameSender(
	ctx context.Context,
	cancel context.CancelCauseFunc,
	send func(*runtimev1.ExecServerMessage) error,
) *frameSender {
	sender := &frameSender{ctx: ctx, queue: make(chan pendingFrame)}
	// One sender serializes both output channels. A flow-controlled Send is
	// released by gRPC after handler return; writers can stop on local cancellation.
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case next := <-sender.queue:
				err := send(next.frame)
				next.done <- err
				if err != nil {
					cancel(err)
					return
				}
			}
		}
	}()
	return sender
}

func (s *frameSender) send(frame *runtimev1.ExecServerMessage) error {
	next := pendingFrame{frame: frame, done: make(chan error, 1)}
	select {
	case <-s.ctx.Done():
		return context.Cause(s.ctx)
	case s.queue <- next:
	}
	select {
	case <-s.ctx.Done():
		return context.Cause(s.ctx)
	case err := <-next.done:
		return err
	}
}

type execOutput struct {
	sender *frameSender
	stderr bool
}

func (w execOutput) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		n := min(len(data), runtimev1.ChunkSize)
		chunk := &runtimev1.OutputChunk{Data: append([]byte(nil), data[:n]...)}
		frame := &runtimev1.ExecServerMessage{
			Payload: &runtimev1.ExecServerMessage_Stdout{Stdout: chunk},
		}
		if w.stderr {
			frame.Payload = &runtimev1.ExecServerMessage_Stderr{Stderr: chunk}
		}
		if err := w.sender.send(frame); err != nil {
			return written, err
		}
		written += n
		data = data[n:]
	}
	return written, nil
}
