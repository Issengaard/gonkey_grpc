package server

import (
	"context"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/Issengaard/gonkey_grpc/examples/grpc/proto"
)

// UserService is a minimal UserService implementation for testing.
type UserService struct {
	pb.UnimplementedUserServiceServer
	users map[string]string // id → name
}

// NewUserService creates a UserService with pre-populated users.
func NewUserService(users map[string]string) *UserService {
	return &UserService{users: users}
}

func (s *UserService) GetUser(_ context.Context, req *pb.GetUserRequest) (*pb.GetUserResponse, error) {
	name, ok := s.users[req.GetId()]
	if !ok {
		return nil, status.Errorf(codes.NotFound, "user %q not found", req.GetId())
	}

	return &pb.GetUserResponse{
		User: &pb.GetUserResponse_User{
			Id:   req.GetId(),
			Name: name,
		},
	}, nil
}
