package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	projects "github.com/wundergraph/cosmo/demo/pkg/subgraphs/projects/generated"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// NewConnectHandler exposes the existing projects gRPC implementation through
// Connect, gRPC, and gRPC-Web without duplicating the generated service API.
func NewConnectHandler(implementation projects.ProjectsServiceServer, options ...connect.HandlerOption) (http.Handler, error) {
	serviceDescriptor := projects.File_generated_service_proto.Services().ByName("ProjectsService")
	if serviceDescriptor == nil {
		return nil, errors.New("projects service descriptor not found")
	}

	grpcMethods := make(map[string]grpc.MethodDesc, len(projects.ProjectsService_ServiceDesc.Methods))
	for _, method := range projects.ProjectsService_ServiceDesc.Methods {
		grpcMethods[method.MethodName] = method
	}

	mux := http.NewServeMux()
	for i := range serviceDescriptor.Methods().Len() {
		methodDescriptor := serviceDescriptor.Methods().Get(i)
		grpcMethod, ok := grpcMethods[string(methodDescriptor.Name())]
		if !ok {
			return nil, fmt.Errorf("gRPC handler for %s not found", methodDescriptor.FullName())
		}

		procedure := "/" + string(serviceDescriptor.FullName()) + "/" + string(methodDescriptor.Name())
		handlerOptions := append([]connect.HandlerOption{}, options...)
		handlerOptions = append(handlerOptions,
			connect.WithCodec(&dynamicProtoCodec{requestDescriptor: methodDescriptor.Input()}),
			connect.WithCodec(&dynamicJSONCodec{requestDescriptor: methodDescriptor.Input()}),
		)

		mux.Handle(procedure, connect.NewUnaryHandler[dynamicpb.Message, dynamicpb.Message](
			procedure,
			connectUnaryHandler(implementation, grpcMethod, methodDescriptor),
			handlerOptions...,
		))
	}

	return mux, nil
}

func connectUnaryHandler(
	implementation projects.ProjectsServiceServer,
	grpcMethod grpc.MethodDesc,
	methodDescriptor protoreflect.MethodDescriptor,
) func(context.Context, *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
	return func(ctx context.Context, request *connect.Request[dynamicpb.Message]) (*connect.Response[dynamicpb.Message], error) {
		incomingMetadata, err := metadataFromConnectHeaders(request.Header())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		ctx = metadata.NewIncomingContext(ctx, incomingMetadata)

		requestBytes, err := proto.Marshal(request.Msg)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshal request: %w", err))
		}

		response, err := grpcMethod.Handler(implementation, ctx, func(target any) error {
			message, ok := target.(proto.Message)
			if !ok {
				return fmt.Errorf("decode target is %T, want proto.Message", target)
			}
			return proto.Unmarshal(requestBytes, message)
		}, nil)
		if err != nil {
			return nil, connectErrorFromGRPC(err)
		}

		message, ok := response.(proto.Message)
		if !ok {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("response is %T, want proto.Message", response))
		}
		responseBytes, err := proto.Marshal(message)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("marshal response: %w", err))
		}

		dynamicResponse := dynamicpb.NewMessage(methodDescriptor.Output())
		if err := proto.Unmarshal(responseBytes, dynamicResponse); err != nil {
			return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("decode response: %w", err))
		}
		return connect.NewResponse(dynamicResponse), nil
	}
}

func metadataFromConnectHeaders(headers http.Header) (metadata.MD, error) {
	result := metadata.MD{}
	for key, values := range headers {
		key = strings.ToLower(key)
		if !strings.HasSuffix(key, "-bin") {
			result[key] = append(result[key], values...)
			continue
		}

		for _, value := range values {
			for encodedValue := range strings.SplitSeq(value, ",") {
				decodedValue, err := connect.DecodeBinaryHeader(strings.TrimSpace(encodedValue))
				if err != nil {
					return nil, fmt.Errorf("decode binary metadata %q: %w", key, err)
				}
				result[key] = append(result[key], string(decodedValue))
			}
		}
	}
	return result, nil
}

func connectErrorFromGRPC(err error) error {
	grpcStatus, ok := status.FromError(err)
	if !ok {
		return connect.NewError(connect.CodeInternal, errors.New("internal server error"))
	}
	return connect.NewError(connect.Code(grpcStatus.Code()), errors.New(grpcStatus.Message()))
}

type dynamicProtoCodec struct {
	requestDescriptor protoreflect.MessageDescriptor
}

func (c *dynamicProtoCodec) Name() string { return "proto" }

func (c *dynamicProtoCodec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("marshal value is %T, want proto.Message", value)
	}
	return proto.Marshal(message)
}

func (c *dynamicProtoCodec) Unmarshal(data []byte, value any) error {
	message, ok := value.(*dynamicpb.Message)
	if !ok {
		return fmt.Errorf("unmarshal value is %T, want *dynamicpb.Message", value)
	}
	*message = *dynamicpb.NewMessage(c.requestDescriptor)
	return proto.Unmarshal(data, message)
}

type dynamicJSONCodec struct {
	requestDescriptor protoreflect.MessageDescriptor
}

func (c *dynamicJSONCodec) Name() string { return "json" }

func (c *dynamicJSONCodec) Marshal(value any) ([]byte, error) {
	message, ok := value.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("marshal value is %T, want proto.Message", value)
	}
	return protojson.Marshal(message)
}

func (c *dynamicJSONCodec) Unmarshal(data []byte, value any) error {
	message, ok := value.(*dynamicpb.Message)
	if !ok {
		return fmt.Errorf("unmarshal value is %T, want *dynamicpb.Message", value)
	}
	*message = *dynamicpb.NewMessage(c.requestDescriptor)
	return protojson.Unmarshal(data, message)
}
