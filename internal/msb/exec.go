package msb

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

func runExec(cmd *exec.Cmd, input io.ReadCloser) error {
	if input == nil {
		return cmd.Run()
	}
	var pending <-chan struct{}
	defer func() {
		_ = input.Close()
		if pending != nil {
			<-pending
		}
	}()
	writer, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("create exec stdin pipe: %w", err)
	}
	defer func() { _ = writer.Close() }()
	if err := cmd.Start(); err != nil {
		return err
	}
	copied := make(chan error, 1)
	finished := make(chan struct{})
	pending = finished
	go func() {
		_, err := io.Copy(writer, input)
		// Publish input failures before EOF can make the child exit successfully.
		copied <- err
		_ = writer.Close()
		close(finished)
	}()
	if err := cmd.Wait(); err != nil {
		return err
	}
	select {
	case err := <-copied:
		return execInputError(err)
	default:
		// The child has exited without consuming all input. Closing the owned
		// reader interrupts its pending Read before the copier is joined.
		return nil
	}
}

func execInputError(err error) error {
	var pathErr *os.PathError
	if errors.As(err, &pathErr) && pathErr.Op == "write" {
		// A successful command may stop reading before stdin reaches EOF.
		return nil
	}
	if err != nil {
		return fmt.Errorf("copy exec stdin: %w", err)
	}
	return nil
}
