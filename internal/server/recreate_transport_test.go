package server

import (
	"context"
	"net"
	"time"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func (s *runtimeSuite) TestRecreatePermissionUsesRealWireRequests() {
	s.client.version = recreateRuntimeVersion
	client, cleanup := s.recreateTransportClient()
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	infoWire, err := proto.Marshal(&runtimev1.InfoRequest{SupportsRecreateOnRun: true})
	s.Require().NoError(err)
	s.Equal(protowire.Number(1), firstWireField(infoWire))
	info, err := client.Info(ctx, &runtimev1.InfoRequest{SupportsRecreateOnRun: true})
	s.Require().NoError(err)
	s.Equal(runtimev1.RecreateMode_RECREATE_MODE_ON_RUN, info.GetCapabilities().GetRecreateMode())

	before := s.seedRecreateTarget(true)
	s.client.info = &before
	s.client.calls = nil
	falseRequest := s.cloneRecreateRequest()
	falseRequest.AllowRecreate = false
	s.NotContains(s.recreateRequestFields(falseRequest), protowire.Number(22))
	_, err = client.RunImage(ctx, falseRequest)
	s.requireRecreateError(
		err,
		codes.AlreadyExists,
		runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_ALREADY_EXISTS,
	)
	s.Equal(before, *s.client.info)
	s.Equal([]string{versionCall, findCall}, s.client.calls)

	trueRequest := s.cloneRecreateRequest()
	trueRequest.AllowRecreate = true
	s.Contains(s.recreateRequestFields(trueRequest), protowire.Number(22))
	s.prepareImage()
	s.client.calls = nil
	_, err = client.RunImage(ctx, trueRequest)
	s.Require().NoError(err)
	s.True(s.client.created.AllowRecreate)
	s.Equal(before, *s.client.info)
	s.Equal(
		[]string{
			versionCall,
			versionCall,
			findCall,
			recreatePrepareCall,
			recreateImportCall,
			recreateCreateCall,
		},
		s.client.calls,
	)

	s.client.calls = nil
	_, err = client.RunImage(ctx, falseRequest)
	s.requireRecreateError(
		err,
		codes.AlreadyExists,
		runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_ALREADY_EXISTS,
	)
	s.Equal(before, *s.client.info)
	s.Equal([]string{versionCall, findCall}, s.client.calls)
}

func (s *runtimeSuite) TestRecreateWireRequestNeedsNoPriorInfo() {
	s.client.version = recreateRuntimeVersion
	client, cleanup := s.recreateTransportClient()
	defer cleanup()
	s.prepareImage()
	request := s.cloneRecreateRequest()
	request.AllowRecreate = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.RunImage(ctx, request)
	s.Require().NoError(err)
	s.True(s.client.created.AllowRecreate)
	s.Equal(
		[]string{
			versionCall,
			versionCall,
			findCall,
			recreatePrepareCall,
			recreateImportCall,
			recreateCreateCall,
		},
		s.client.calls,
	)
}

func (s *runtimeSuite) recreateRequestFields(
	request *runtimev1.RunImageRequest,
) []protowire.Number {
	s.T().Helper()
	data, err := proto.Marshal(request)
	s.Require().NoError(err)
	return wireFields(data)
}

func (s *runtimeSuite) cloneRecreateRequest() *runtimev1.RunImageRequest {
	s.T().Helper()
	request, ok := proto.Clone(s.request).(*runtimev1.RunImageRequest)
	s.Require().True(ok)
	return request
}

func (s *runtimeSuite) recreateTransportClient() (runtimev1.RuntimeDriverClient, func()) {
	s.T().Helper()
	listener := bufconn.Listen(1 << 20)
	service := grpc.NewServer()
	runtimev1.RegisterRuntimeDriverServer(service, s.runtime)
	done := make(chan error, 1)
	go func() { done <- service.Serve(listener) }()
	conn, err := grpc.NewClient(
		"passthrough:///runtime",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return listener.DialContext(ctx)
		}),
	)
	s.Require().NoError(err)
	cleanup := func() {
		s.T().Helper()
		service.Stop()
		s.NoError(conn.Close())
		s.NoError(<-done)
		s.NoError(listener.Close())
	}
	return runtimev1.NewRuntimeDriverClient(conn), cleanup
}

func firstWireField(data []byte) protowire.Number {
	number, _, _ := protowire.ConsumeTag(data)
	return number
}

func wireFields(data []byte) []protowire.Number {
	var fields []protowire.Number
	for len(data) > 0 {
		number, wireType, tagSize := protowire.ConsumeTag(data)
		if tagSize < 0 {
			return fields
		}
		data = data[tagSize:]
		valueSize := protowire.ConsumeFieldValue(number, wireType, data)
		if valueSize < 0 {
			return fields
		}
		fields = append(fields, number)
		data = data[valueSize:]
	}
	return fields
}
