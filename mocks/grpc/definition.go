package grpcmock

import "google.golang.org/grpc/codes"

// GrpcDefinition describes mock response for a single gRPC method.
type GrpcDefinition struct {
	Service         string
	Method          string
	ExpectedRequest []byte            // raw proto bytes of expected request (nil = skip verification)
	Response        []byte            // raw proto bytes of response
	ResponseStatus  codes.Code        // gRPC status code
	Metadata        map[string]string // trailing metadata
}

// RecordedRequest is an incoming request captured by the mock server.
type RecordedRequest struct {
	Method  string
	Payload []byte // raw proto bytes
}
