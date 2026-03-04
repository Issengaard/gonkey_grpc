package grpctest

import (
	"net"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	pb "github.com/lamoda/gonkey/examples/grpc/proto"
	"github.com/lamoda/gonkey/examples/grpc/server"
	"github.com/lamoda/gonkey/runner"
)

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
