package server

import (
	"sync"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc"
)

// Logs preserves merged backend log bytes in bounded frames without following.
func (r *Runtime) Logs(
	req *runtimev1.LogsRequest,
	stream grpc.ServerStreamingServer[runtimev1.OutputChunk],
) error {
	found, err := r.Find(
		stream.Context(),
		&runtimev1.FindRequest{WorkspaceId: req.GetWorkspaceId()},
	)
	if err != nil {
		return err
	}
	if !found.GetFound() {
		return missingWorkspace()
	}
	output := &logOutput{send: stream.Send}
	return backendError(
		stream.Context(),
		r.client.Logs(stream.Context(), sandboxName(req.GetWorkspaceId()), output),
	)
}

type logOutput struct {
	mu   sync.Mutex
	send func(*runtimev1.OutputChunk) error
}

func (w *logOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	written := 0
	for len(data) > 0 {
		n := min(len(data), runtimev1.ChunkSize)
		if err := w.send(
			&runtimev1.OutputChunk{Data: append([]byte(nil), data[:n]...)},
		); err != nil {
			return written, err
		}
		written += n
		data = data[n:]
	}
	return written, nil
}
