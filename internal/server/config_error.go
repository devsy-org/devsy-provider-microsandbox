package server

import (
	"context"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// ConfigError serves a runtime whose options are invalid. Every call fails with
// the same invalid-argument error, so Devsy shows the option problem in normal
// output instead of a plugin that exited before its handshake.
type ConfigError struct {
	runtimev1.UnimplementedRuntimeDriverServer
	err error
}

// NewConfigError reports cause to every caller.
func NewConfigError(cause error) *ConfigError {
	return &ConfigError{err: invalidArgument(status.Convert(cause).Message())}
}

// Info returns the configuration error.
func (c *ConfigError) Info(
	context.Context,
	*runtimev1.InfoRequest,
) (*runtimev1.InfoResponse, error) {
	return nil, c.err
}

// Preflight returns the configuration error.
func (c *ConfigError) Preflight(
	context.Context,
	*runtimev1.PreflightRequest,
) (*runtimev1.PreflightResponse, error) {
	return nil, c.err
}

// ProvisioningPreflight returns the configuration error.
func (c *ConfigError) ProvisioningPreflight(
	context.Context,
	*runtimev1.ProvisioningPreflightRequest,
) (*runtimev1.ProvisioningPreflightResponse, error) {
	return nil, c.err
}

// ReusePreflight returns the configuration error.
func (c *ConfigError) ReusePreflight(
	context.Context,
	*runtimev1.ReusePreflightRequest,
) (*runtimev1.ReusePreflightResponse, error) {
	return nil, c.err
}

// Find returns the configuration error.
func (c *ConfigError) Find(
	context.Context,
	*runtimev1.FindRequest,
) (*runtimev1.FindResponse, error) {
	return nil, c.err
}

// TargetArchitecture returns the configuration error.
func (c *ConfigError) TargetArchitecture(
	context.Context,
	*runtimev1.TargetArchitectureRequest,
) (*runtimev1.TargetArchitectureResponse, error) {
	return nil, c.err
}

// RunImage returns the configuration error.
func (c *ConfigError) RunImage(
	context.Context,
	*runtimev1.RunImageRequest,
) (*runtimev1.RunImageResponse, error) {
	return nil, c.err
}

// Start returns the configuration error.
func (c *ConfigError) Start(
	context.Context,
	*runtimev1.StartRequest,
) (*runtimev1.StartResponse, error) {
	return nil, c.err
}

// Stop returns the configuration error.
func (c *ConfigError) Stop(
	context.Context,
	*runtimev1.StopRequest,
) (*runtimev1.StopResponse, error) {
	return nil, c.err
}

// Delete returns the configuration error.
func (c *ConfigError) Delete(
	context.Context,
	*runtimev1.DeleteRequest,
) (*runtimev1.DeleteResponse, error) {
	return nil, c.err
}

// Exec returns the configuration error.
func (c *ConfigError) Exec(
	grpc.BidiStreamingServer[runtimev1.ExecClientMessage, runtimev1.ExecServerMessage],
) error {
	return c.err
}

// Logs returns the configuration error.
func (c *ConfigError) Logs(
	*runtimev1.LogsRequest,
	grpc.ServerStreamingServer[runtimev1.OutputChunk],
) error {
	return c.err
}
