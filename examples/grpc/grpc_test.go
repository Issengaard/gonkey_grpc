package grpctest

import (
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/Issengaard/gonkey_grpc/examples/grpc/proto"
	"github.com/Issengaard/gonkey_grpc/examples/grpc/server"
	"github.com/Issengaard/gonkey_grpc/runner"
)

// TestGrpc_UserService demonstrates direct gRPC endpoint testing with gonkey.
//
// Architecture:
//
//	gonkey runner → gRPC server (service under test)
//
// Test cases are loaded from testcases/ and executed directly against the gRPC
// server using the reflection-based transport. No mocks are involved.
func TestGrpc_UserService(t *testing.T) {
	addr := startGrpcServer(t)

	runner.RunWithTesting(t, &runner.RunWithTestingParams{
		GrpcHost: addr,
		TestsDir: "testcases",
	})
}

func startGrpcServer(t *testing.T) string {
	t.Helper()

	lis, err := net.Listen("tcp", "localhost:0")
	if err != nil {
		t.Fatal(err)
	}

	s := grpc.NewServer()

	svc := server.NewUserService(map[string]string{
		"123": "Alice",
		"456": "Bob",
	})
	pb.RegisterUserServiceServer(s, svc)
	reflection.Register(s)

	t.Cleanup(s.GracefulStop)
	go s.Serve(lis) //nolint:errcheck

	return lis.Addr().String()
}
