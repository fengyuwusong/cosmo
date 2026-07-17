package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestRecoveryInterceptorReturnsInternalError(t *testing.T) {
	response, err := recoveryInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/service/Method"},
		func(context.Context, interface{}) (interface{}, error) {
			panic("boom")
		},
	)

	require.Nil(t, response)
	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "internal server error", status.Convert(err).Message())
}

func TestErrorInterceptorPreservesStatusErrors(t *testing.T) {
	original := status.Error(codes.InvalidArgument, "invalid project")
	_, err := errorInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/service/Method"},
		func(context.Context, interface{}) (interface{}, error) {
			return nil, original
		},
	)

	require.ErrorIs(t, err, original)
}

func TestErrorInterceptorIncludesNonStatusErrorMessage(t *testing.T) {
	_, err := errorInterceptor(
		context.Background(),
		nil,
		&grpc.UnaryServerInfo{FullMethod: "/service/Method"},
		func(context.Context, interface{}) (interface{}, error) {
			return nil, errors.New("database unavailable")
		},
	)

	require.Equal(t, codes.Internal, status.Code(err))
	require.Equal(t, "internal server error: database unavailable", status.Convert(err).Message())
}
