package grpcmock

import (
	"bytes"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

var _ encoding.Codec = smartCodec{}

// smartCodec handles two cases transparently:
//   - []byte / *[]byte — raw proto frames for UnknownServiceHandler (no schema needed)
//   - proto.Message     — standard protobuf encoding for registered services such as
//     the gRPC reflection service
//
// This allows ForceServerCodec to serve both the reflection endpoint and the
// unknown-service handler on the same gRPC server.
type smartCodec struct{}

func (smartCodec) Marshal(v interface{}) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return b, nil
	}

	msg, ok := v.(proto.Message)
	if !ok {
		return nil, fmt.Errorf("grpcmock: Marshal expects []byte or proto.Message, got %T", v)
	}

	return proto.Marshal(msg)
}

func (smartCodec) Unmarshal(data []byte, v interface{}) error {
	if p, ok := v.(*[]byte); ok {
		*p = data
		return nil
	}

	msg, ok := v.(proto.Message)
	if !ok {
		return fmt.Errorf("grpcmock: Unmarshal expects *[]byte or proto.Message, got %T", v)
	}

	return proto.Unmarshal(data, msg)
}

func (smartCodec) Name() string { return "proto" }

func (m *GrpcMock) unknownServiceHandler(_ interface{}, stream grpc.ServerStream) error {
	method, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "grpcmock: cannot determine method name")
	}

	val, found := m.definitions.Load(method)
	if !found {
		return status.Errorf(codes.Unimplemented, "grpcmock: no mock definition for %s", method)
	}

	def := val.(*GrpcDefinition)

	var payload []byte
	if err := stream.RecvMsg(&payload); err != nil {
		return status.Errorf(codes.Internal, "grpcmock: recv error: %v", err)
	}

	func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		m.requests = append(m.requests, &RecordedRequest{Method: method, Payload: payload})

		if def.ExpectedRequest != nil && !bytes.Equal(payload, def.ExpectedRequest) {
			m.errors = append(m.errors, fmt.Errorf(
				"grpcmock: method %s: request mismatch\nwant: %x\ngot:  %x",
				method, def.ExpectedRequest, payload,
			))
		}
	}()

	if len(def.Metadata) > 0 {
		pairs := make([]string, 0, len(def.Metadata)*2)
		for k, v := range def.Metadata {
			pairs = append(pairs, k, v)
		}

		grpc.SetTrailer(stream.Context(), metadata.Pairs(pairs...))
	}

	if def.ResponseStatus != codes.OK {
		return status.Error(def.ResponseStatus, "")
	}

	// For OK status, always send a response frame (gRPC unary requires exactly one response).
	// nil response is sent as an empty proto message (all fields at default values).
	resp := def.Response
	if resp == nil {
		resp = []byte{}
	}

	if err := stream.SendMsg(resp); err != nil {
		return status.Errorf(codes.Internal, "grpcmock: send error: %v", err)
	}

	return nil
}
