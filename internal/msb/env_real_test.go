package msb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestRealEnvStaysOffArgv polls the process table while a real msb creates a
// VM and checks the guest still receives the value. It needs
// DEVSY_QUALIFY_RUNTIME=1, MSB_PATH, and an image that msb can pull.
func TestRealEnvStaysOffArgv(t *testing.T) {
	if os.Getenv("DEVSY_QUALIFY_RUNTIME") != "1" || runtime.GOOS == testWindows {
		t.Skip("requires an isolated MicroSandbox runtime and DEVSY_QUALIFY_RUNTIME=1")
	}
	marker := fmt.Sprintf("tok%x", time.Now().UnixNano())
	secret := marker + "-${x}-\"q\"-$&"
	name := fmt.Sprintf("devsy-env-%d", time.Now().Unix())
	c := Client{Binary: os.Getenv("MSB_PATH")}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	t.Cleanup(func() {
		_ = c.Stop(context.Background(), name)
		_ = c.Remove(context.Background(), name)
	})

	stop := make(chan struct{})
	var wg sync.WaitGroup
	var mu sync.Mutex
	var leaked []string
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			out, _ := exec.Command("ps", "-ww", "-axo", "args").Output()
			for line := range strings.SplitSeq(string(out), "\n") {
				if strings.Contains(line, marker) {
					mu.Lock()
					leaked = append(leaked, line)
					mu.Unlock()
				}
			}
		}
	})

	spec := Spec{
		Image: "alpine",
		Cmd:   []string{"sleep", "300"},
		Env:   map[string]string{"A7_SECRET": secret, "A7_PLAIN": "plain"},
	}
	err := func() error {
		defer func() {
			close(stop)
			wg.Wait()
		}()
		return c.Create(ctx, name, spec)
	}()
	require.NoError(t, err)
	mu.Lock()
	require.Empty(t, leaked, "secret value appeared in the process table")
	mu.Unlock()

	var out bytes.Buffer
	require.NoError(t, c.Exec(ctx, name, ExecRequest{Argv: []string{"env"}, Stdout: &out}))
	require.Contains(t, out.String(), "A7_SECRET="+secret+"\n")
	require.Contains(t, out.String(), "A7_PLAIN=plain\n")
}
