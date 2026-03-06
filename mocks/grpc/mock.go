package grpcmock

import (
	"net"
	"sync"

	"google.golang.org/grpc"
)

// GrpcMock is a mock gRPC server analogous to mocks.ServiceMock for HTTP.
type GrpcMock struct {
	server      *grpc.Server
	listener    net.Listener
	definitions sync.Map // key: "/Service/Method" -> *GrpcDefinition
	requests    []*RecordedRequest
	mu          sync.Mutex
	errors      []error
}

// New creates a new gRPC mock server with gRPC reflection enabled.
// Reflection is backed by protoregistry.GlobalFiles, so any proto-generated
// Go package imported in the test binary is automatically discoverable.
func New() *GrpcMock {
	m := &GrpcMock{}
	m.server = grpc.NewServer(
		grpc.UnknownServiceHandler(m.unknownServiceHandler),
		grpc.ForceServerCodec(smartCodec{}),
	)

	registerGlobalReflection(m.server)

	return m
}

// StartServer starts the gRPC server on the given address.
func (m *GrpcMock) StartServer(addr string) error {
	lis, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	m.listener = lis
	go m.server.Serve(lis)

	return nil
}

// ResetDefinitions clears all definitions, recorded requests and errors.
func (m *GrpcMock) ResetDefinitions() {
	m.definitions.Range(func(k, _ interface{}) bool {
		m.definitions.Delete(k)
		return true
	})

	m.mu.Lock()
	defer m.mu.Unlock()

	m.requests = nil
	m.errors = nil
}

// SetDefinition registers a mock response definition for a gRPC method.
func (m *GrpcMock) SetDefinition(def *GrpcDefinition) {
	key := "/" + def.Service + "/" + def.Method
	m.definitions.Store(key, def)
}

// Stop gracefully stops the gRPC server.
func (m *GrpcMock) Stop() {
	m.server.GracefulStop()
}

// Addr returns the listener address or empty string if not started.
func (m *GrpcMock) Addr() string {
	if m.listener == nil {
		return ""
	}

	return m.listener.Addr().String()
}

// GetRecordedRequests returns a copy of all recorded incoming requests.
func (m *GrpcMock) GetRecordedRequests() []*RecordedRequest {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]*RecordedRequest, len(m.requests))
	copy(result, m.requests)

	return result
}

// EndRunningContext returns verification errors accumulated during the run.
func (m *GrpcMock) EndRunningContext() []error {
	m.mu.Lock()
	defer m.mu.Unlock()

	result := make([]error, len(m.errors))
	copy(result, m.errors)

	return result
}
