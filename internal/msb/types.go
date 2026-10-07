package msb

import (
	"io"
	"time"
)

// Spec describes a detached MicroSandbox VM.
type Spec struct {
	Image       string
	User        string
	Entrypoint  string
	Cmd         []string
	Memory      uint32
	CPUs        uint8
	Env         map[string]string
	Labels      map[string]string
	Ephemeral   bool
	IdleTimeout time.Duration
	Mounts      []Mount
	MaxMemory   uint32
	MaxCPUs     uint8
	BlockEgress bool
	RootDiskGB  uint32
}

// Mount describes a MicroSandbox bind, named-volume, or tmpfs mount.
type Mount struct {
	Target   string
	Source   string
	Volume   string
	Tmpfs    bool
	ReadOnly bool
	Policy   MountPolicy
}

// Info is the inspect result needed by runtime lifecycle operations.
type Info struct {
	Name      string
	Running   bool
	CreatedAt time.Time
	Labels    map[string]string
	Mounts    []Mount
}

// ExecRequest carries a non-PTY command and its byte streams.
// Exec owns Stdin and closes it on return. Close must unblock a pending Read.
type ExecRequest struct {
	Command string
	Argv    []string
	User    string
	Stdin   io.ReadCloser
	Stdout  io.Writer
	Stderr  io.Writer
}
