package grpctest

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	pb "github.com/Issengaard/gonkey_grpc/examples/grpc/proto"
	grpcmock "github.com/Issengaard/gonkey_grpc/mocks/grpc"
	"github.com/Issengaard/gonkey_grpc/runner"
)

// TestGrpc_UserServiceMock demonstrates using GrpcMocks to isolate a service under test
// from a real external gRPC dependency.
//
// Architecture:
//
//	HTTP test client → HTTP proxy (service under test) → gRPC mock (external dependency)
//
// The proxy handler calls the gRPC UserService; the mock is configured per-test
// via the grpcMocks YAML field. The runner resets and reloads definitions automatically.
func TestGrpc_UserServiceMock(t *testing.T) {
	// Create the mock registry. nil = auto-discover proto descriptors from
	// protoregistry.GlobalFiles (populated by importing proto-generated Go packages).
	mocks := grpcmock.NewGrpcMocks(nil)

	mock := grpcmock.New()
	if err := mock.StartServer("localhost:0"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mock.Stop)
	mocks.Add("userServiceMock", mock)

	// Dial the mock once; the connection is reused across test cases.
	conn, err := grpc.NewClient(mock.Addr(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() }) //nolint:errcheck

	client := pb.NewUserServiceClient(conn)

	// HTTP server acts as the "service under test": it proxies GET /user?id=<id>
	// to the external gRPC UserService mock and returns JSON.
	httpSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		resp, grpcErr := client.GetUser(context.Background(), &pb.GetUserRequest{Id: id})
		if grpcErr != nil {
			st, _ := status.FromError(grpcErr)
			if st.Code() == codes.NotFound {
				http.Error(w, st.Message(), http.StatusNotFound)
				return
			}
			http.Error(w, grpcErr.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if encErr := json.NewEncoder(w).Encode(resp); encErr != nil {
			http.Error(w, encErr.Error(), http.StatusInternalServerError)
		}
	}))
	t.Cleanup(httpSrv.Close)

	runner.RunWithTesting(t, &runner.RunWithTestingParams{
		Server:    httpSrv,
		TestsDir:  "testcases_mock",
		GrpcMocks: mocks,
	})
}
