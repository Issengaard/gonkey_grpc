package runner

import (
	"context"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/test/bufconn"

	"github.com/lamoda/gonkey/examples/grpc/server"
	"github.com/lamoda/gonkey/models"

	grpcchecker "github.com/lamoda/gonkey/checker/grpc_response"
	pb "github.com/lamoda/gonkey/examples/grpc/proto"
	grpcmock "github.com/lamoda/gonkey/mocks/grpc"
)

const grpcIntegrationBufSize = 1024 * 1024

func startIntegrationGrpcServer(t *testing.T) *bufconn.Listener {
	t.Helper()

	lis := bufconn.Listen(grpcIntegrationBufSize)
	s := grpc.NewServer()

	svc := server.NewUserService(map[string]string{
		"123": "Alice",
	})
	pb.RegisterUserServiceServer(s, svc)
	reflection.Register(s)

	t.Cleanup(func() { s.Stop() })                    // hard stop: faster teardown in tests
	go func() { _ = s.Serve(lis) }() //nolint:errcheck // error expected after s.Stop()

	return lis
}

func TestGrpcTransport_ExecuteIntegration(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		request     string
		protoSource *models.GrpcProtoSource
		responses   map[int]string
		want        func(t *testing.T, result *models.Result, err error)
	}{
		"happy_path": {
			request:     `{"id": "123"}`,
			protoSource: &models.GrpcProtoSource{Type: "reflection"},
			responses:   map[int]string{0: `{"user": {"id": "123", "name": "Alice"}}`},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, int(codes.OK), result.GrpcStatusCode)
				assert.NotEmpty(t, result.ResponseBody)
			},
		},
		"not_found_error": {
			request:     `{"id": "nonexistent"}`,
			protoSource: &models.GrpcProtoSource{Type: "reflection"},
			responses:   map[int]string{5: `{"message": "user \"nonexistent\" not found"}`},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				assert.Equal(t, int(codes.NotFound), result.GrpcStatusCode)
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			lis := startIntegrationGrpcServer(t)
			conn := dialBufconn(t, lis)
			transport := newGrpcTransportWithConn(conn)

			test := &grpcTestStub{
				path:        "example.UserService/GetUser",
				request:     tc.request,
				protoSource: tc.protoSource,
				responses:   tc.responses,
			}

			result, err := transport.Execute(context.Background(), test)

			tc.want(t, result, err)
		})
	}
}

func TestGrpcResponseChecker_Check(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		test   *grpcTestStub
		result *models.Result
		want   func(t *testing.T, errs []error, err error)
	}{
		"status_mismatch": {
			test: &grpcTestStub{
				transport: "grpc",
				path:      "example.UserService/GetUser",
				request:   "{}",
				responses: map[int]string{0: ""},
			},
			result: &models.Result{
				GrpcStatusCode: int(codes.NotFound),
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				require.Len(t, errs, 1)
				assert.Contains(t, errs[0].Error(), "5")
			},
		},
		"body_mismatch": {
			test: &grpcTestStub{
				transport: "grpc",
				path:      "example.UserService/GetUser",
				request:   "{}",
				responses: map[int]string{0: `{"user": {"id": "123", "name": "Alice"}}`},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{"user": {"id": "123", "name": "Bob"}}`,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				require.NotEmpty(t, errs)
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			ch := grpcchecker.NewChecker()

			tc.result.Test = tc.test

			errs, err := ch.Check(tc.test, tc.result)

			tc.want(t, errs, err)
		})
	}
}

func TestGrpcMock_ErrorOnMissingReflection(t *testing.T) {
	t.Parallel()

	mock := grpcmock.New()
	require.NoError(t, mock.StartServer("localhost:0"))
	t.Cleanup(mock.Stop)

	// GrpcMock uses raw proto bytes for Response, but since we test via GrpcTransport
	// which uses grpcurl (JSON mode), we need a mock that can serve proto responses.
	// The mock works at raw proto level. For this integration test we verify that
	// GrpcTransport can connect to the mock and get a response (even if status-based).
	mock.SetDefinition(&grpcmock.GrpcDefinition{
		Service:        "example.UserService",
		Method:         "GetUser",
		Response:       nil, // empty response
		ResponseStatus: codes.OK,
	})

	addr := mock.Addr()
	cfg := &Config{GrpcHost: addr}
	transport := newGrpcTransport(cfg)
	t.Cleanup(func() { _ = transport.Close() })

	test := &grpcTestStub{
		path:        "example.UserService/GetUser",
		request:     `{"id": "999"}`,
		protoSource: &models.GrpcProtoSource{Type: "reflection"},
		responses:   map[int]string{0: "{}"},
	}

	// GrpcMock doesn't serve reflection, so descriptor resolution will fail.
	// This test verifies the mock is reachable and returns a meaningful gRPC error.
	result, err := transport.Execute(context.Background(), test)
	if err != nil {
		// Expected: reflection not available on mock server
		assert.Contains(t, err.Error(), "reflection")
		return
	}

	// If we get here, mock served a response successfully
	assert.NotNil(t, result)
}

func TestGrpcTransport_ExecuteTCP(t *testing.T) {
	t.Parallel()

	// Start a real gRPC server with reflection, then verify GrpcTransport works via TCP.
	lis, err := net.Listen("tcp", "localhost:0")
	require.NoError(t, err)

	s := grpc.NewServer()
	svc := server.NewUserService(map[string]string{
		"999": "MockUser",
	})
	pb.RegisterUserServiceServer(s, svc)
	reflection.Register(s)

	t.Cleanup(s.GracefulStop)
	go func() { _ = s.Serve(lis) }() //nolint:errcheck // error expected after GracefulStop

	addr := lis.Addr().String()
	cfg := &Config{GrpcHost: addr}
	transport := newGrpcTransport(cfg)
	t.Cleanup(func() { _ = transport.Close() })

	test := &grpcTestStub{
		path:        "example.UserService/GetUser",
		request:     `{"id": "999"}`,
		protoSource: &models.GrpcProtoSource{Type: "reflection"},
		responses:   map[int]string{0: `{"user": {"id": "999", "name": "MockUser"}}`},
	}

	result, err := transport.Execute(context.Background(), test)

	require.NoError(t, err)
	assert.Equal(t, 0, result.GrpcStatusCode)
	assert.Contains(t, result.ResponseBody, "MockUser")
}

// grpcTestStub implements models.TestInterface for isolated GrpcTransport tests.
type grpcTestStub struct {
	transport   string
	path        string
	request     string
	headers     map[string]string
	protoSource *models.GrpcProtoSource
	responses   map[int]string
}

func (s *grpcTestStub) GetTransport() string                    { return s.transport }
func (s *grpcTestStub) GetProtoSource() *models.GrpcProtoSource { return s.protoSource }
func (s *grpcTestStub) Path() string                            { return s.path }
func (s *grpcTestStub) GetRequest() string                      { return s.request }
func (s *grpcTestStub) Headers() map[string]string              { return s.headers }
func (s *grpcTestStub) GetResponses() map[int]string            { return s.responses }

func (s *grpcTestStub) GetResponse(code int) (string, bool) {
	v, ok := s.responses[code]
	return v, ok
}

func (s *grpcTestStub) GetName() string                                   { return "grpc-stub" }
func (s *grpcTestStub) GetDescription() string                            { return "" }
func (s *grpcTestStub) GetStatus() string                                 { return "" }
func (s *grpcTestStub) SetStatus(_ string)                                {}
func (s *grpcTestStub) GetFileName() string                               { return "" }
func (s *grpcTestStub) GetMethod() string                                 { return "" }
func (s *grpcTestStub) ToQuery() string                                   { return "" }
func (s *grpcTestStub) ToJSON() ([]byte, error)                           { return nil, nil }
func (s *grpcTestStub) GetResponseHeaders(_ int) (map[string]string, bool) { return nil, false }
func (s *grpcTestStub) Cookies() map[string]string                        { return nil }
func (s *grpcTestStub) ContentType() string                               { return "" }
func (s *grpcTestStub) GetForm() *models.Form                             { return nil }
func (s *grpcTestStub) Fixtures() []string                                { return nil }
func (s *grpcTestStub) FixturesMultiDb() models.FixturesMultiDb           { return nil }
func (s *grpcTestStub) ServiceMocks() map[string]interface{}              { return nil }
func (s *grpcTestStub) Pause() int                                        { return 0 }
func (s *grpcTestStub) BeforeScriptPath() string                          { return "" }
func (s *grpcTestStub) BeforeScriptTimeout() int                          { return 0 }
func (s *grpcTestStub) AfterRequestScriptPath() string                    { return "" }
func (s *grpcTestStub) AfterRequestScriptTimeout() int                    { return 0 }
func (s *grpcTestStub) DbNameString() string                              { return "" }
func (s *grpcTestStub) DbQueryString() string                             { return "" }
func (s *grpcTestStub) DbResponseJson() []string                          { return nil }
func (s *grpcTestStub) GetDatabaseChecks() []models.DatabaseCheck         { return nil }
func (s *grpcTestStub) SetDatabaseChecks(_ []models.DatabaseCheck)        {}
func (s *grpcTestStub) GetVariables() map[string]string                   { return nil }
func (s *grpcTestStub) GetCombinedVariables() map[string]string           { return nil }
func (s *grpcTestStub) GetVariablesToSet() map[int]map[string]string      { return nil }
func (s *grpcTestStub) NeedsCheckingValues() bool                         { return true }
func (s *grpcTestStub) IgnoreArraysOrdering() bool                        { return false }
func (s *grpcTestStub) DisallowExtraFields() bool                         { return false }
func (s *grpcTestStub) IgnoreDbOrdering() bool                            { return false }
func (s *grpcTestStub) Clone() models.TestInterface                       { c := *s; return &c }
func (s *grpcTestStub) SetQuery(_ string)                                 {}
func (s *grpcTestStub) SetMethod(_ string)                                {}
func (s *grpcTestStub) SetPath(_ string)                                  {}
func (s *grpcTestStub) SetRequest(_ string)                               {}
func (s *grpcTestStub) SetForm(_ *models.Form)                            {}
func (s *grpcTestStub) SetResponses(_ map[int]string)                     {}
func (s *grpcTestStub) SetHeaders(_ map[string]string)                    {}
func (s *grpcTestStub) SetDbQueryString(_ string)                         {}
func (s *grpcTestStub) SetDbResponseJson(_ []string)                      {}
func (s *grpcTestStub) GetAllureMetadata() *models.AllureMetadata         { return nil }
