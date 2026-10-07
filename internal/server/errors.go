package server

import (
	"context"
	"errors"

	"github.com/devsy-org/devsy-runtime-sdk/runtimev1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func runtimeError(code codes.Code, detail runtimev1.RuntimeErrorCode, message string) error {
	result, err := status.New(code, message).
		WithDetails(&runtimev1.RuntimeError{Code: detail, Message: message})
	if err != nil {
		return status.Error(code, message)
	}
	return result.Err()
}

func backendError(ctx context.Context, err error) error {
	if err == nil {
		return nil
	}
	if ctx.Err() != nil {
		return status.FromContextError(ctx.Err()).Err()
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return status.FromContextError(err).Err()
	}
	return runtimeError(
		codes.Internal,
		runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_RUNTIME_FAILURE,
		err.Error(),
	)
}

func validateWorkspace(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return status.FromContextError(err).Err()
	}
	if id == "" {
		return invalidArgument("workspace_id is required")
	}
	return nil
}

func invalidArgument(message string) error {
	return runtimeError(
		codes.InvalidArgument,
		runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_INVALID_ARGUMENT,
		message,
	)
}

func missingWorkspace() error {
	return runtimeError(
		codes.NotFound,
		runtimev1.RuntimeErrorCode_RUNTIME_ERROR_CODE_NOT_FOUND,
		"workspace does not exist",
	)
}
