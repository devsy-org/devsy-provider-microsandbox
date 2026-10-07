package server

import (
	"context"
	"net"
	"time"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

func (s *runtimeSuite) TestUnaryTransportContract() {
	listener := bufconn.Listen(1 << 20)
	service := grpc.NewServer()
	runtimev1.RegisterRuntimeDriverServer(service, s.runtime)
	done := make(chan error, 1)
	go func() { done <- service.Serve(listener) }()
	defer func() { service.Stop(); s.NoError(<-done); s.NoError(listener.Close()) }()
	conn, err := grpc.NewClient(
		"passthrough:///runtime",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(
			func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) },
		),
	)
	s.Require().NoError(err)
	defer func() { s.NoError(conn.Close()) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := runtimev1.NewRuntimeDriverClient(conn)
	info, err := client.Info(ctx, &runtimev1.InfoRequest{})
	s.Require().NoError(err)
	s.NoError(runtimev1.ValidateInfo(info))
	found, err := client.Find(ctx, &runtimev1.FindRequest{WorkspaceId: testWorkspace})
	s.Require().NoError(err)
	s.False(found.GetFound())
	_, err = client.Start(ctx, &runtimev1.StartRequest{WorkspaceId: testWorkspace})
	s.Equal(codes.NotFound, status.Code(err))
	s.Len(status.Convert(err).Details(), 1)
	_, err = client.Delete(ctx, &runtimev1.DeleteRequest{WorkspaceId: testWorkspace})
	s.NoError(err)
}
