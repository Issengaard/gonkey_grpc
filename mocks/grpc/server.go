package grpcmock

import (
	"bytes"
	"fmt"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/encoding"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var _ encoding.Codec = rawBytesCodec{}

// rawBytesCodec passes raw proto frames without deserialization.
type rawBytesCodec struct{}

func (rawBytesCodec) Marshal(v interface{}) ([]byte, error) {
	b, ok := v.([]byte)
	if !ok {
		return nil, fmt.Errorf("grpcmock: Marshal expects []byte, got %T", v)
	}

	return b, nil
}

func (rawBytesCodec) Unmarshal(data []byte, v interface{}) error {
	p, ok := v.(*[]byte)
	if !ok {
		return fmt.Errorf("grpcmock: Unmarshal expects *[]byte, got %T", v)
	}

	*p = data

	return nil
}

func (rawBytesCodec) Name() string { return "proto" }

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

	if def.Response != nil {
		if err := stream.SendMsg(def.Response); err != nil {
			return status.Errorf(codes.Internal, "grpcmock: send error: %v", err)
		}
	}

	if def.ResponseStatus != codes.OK {
		return status.Error(def.ResponseStatus, "")
	}

	return nil
}
