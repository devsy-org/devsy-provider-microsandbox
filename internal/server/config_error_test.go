package server

import (
	"context"
	"errors"
	"testing"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type configErrorSuite struct{ suite.Suite }

func TestConfigError(t *testing.T) { suite.Run(t, new(configErrorSuite)) }

func (s *configErrorSuite) TestEveryCallReturnsTheConfigurationError() {
	driver := NewConfigError(errors.New("invalid MICROSANDBOX_MEMORY: bad value"))
	ctx := context.Background()
	_, infoErr := driver.Info(ctx, &runtimev1.InfoRequest{})
	_, preflightErr := driver.Preflight(ctx, &runtimev1.PreflightRequest{})
	_, provisioningErr := driver.ProvisioningPreflight(
		ctx, &runtimev1.ProvisioningPreflightRequest{},
	)
	_, reuseErr := driver.ReusePreflight(ctx, &runtimev1.ReusePreflightRequest{})
	_, findErr := driver.Find(ctx, &runtimev1.FindRequest{})
	_, archErr := driver.TargetArchitecture(ctx, &runtimev1.TargetArchitectureRequest{})
	_, runErr := driver.RunImage(ctx, &runtimev1.RunImageRequest{})
	_, startErr := driver.Start(ctx, &runtimev1.StartRequest{})
	_, stopErr := driver.Stop(ctx, &runtimev1.StopRequest{})
	_, deleteErr := driver.Delete(ctx, &runtimev1.DeleteRequest{})
	execErr := driver.Exec(nil)
	logsErr := driver.Logs(&runtimev1.LogsRequest{}, nil)
	for _, err := range []error{
		infoErr, preflightErr, provisioningErr, reuseErr, findErr, archErr,
		runErr, startErr, stopErr, deleteErr, execErr, logsErr,
	} {
		s.Equal(codes.InvalidArgument, status.Code(err))
		s.Equal("invalid MICROSANDBOX_MEMORY: bad value", status.Convert(err).Message())
	}
}

func (s *configErrorSuite) TestStatusCausesAreNotWrappedTwice() {
	driver := NewConfigError(invalidArgument("host-perms=mirror requires stat virtualization"))
	_, err := driver.Info(context.Background(), &runtimev1.InfoRequest{})
	s.Equal("host-perms=mirror requires stat virtualization", status.Convert(err).Message())
}
