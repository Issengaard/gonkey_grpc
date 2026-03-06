package grpcmock

import (
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// allServicesGRPCServer wraps *grpc.Server and overrides GetServiceInfo to return
// all services found in protoregistry.GlobalFiles. This lets reflection.Register use
// the standard grpc-go reflection machinery (which already resolves file descriptors
// from protoregistry.GlobalFiles) while listing every service registered via proto
// generated Go package imports — no concrete handler registration required.
type allServicesGRPCServer struct {
	*grpc.Server
}

func (w *allServicesGRPCServer) GetServiceInfo() map[string]grpc.ServiceInfo {
	result := make(map[string]grpc.ServiceInfo)

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		for i := range fd.Services().Len() {
			result[string(fd.Services().Get(i).FullName())] = grpc.ServiceInfo{}
		}

		return true
	})

	return result
}

// registerGlobalReflection registers the gRPC reflection service (v1 + v1alpha) on s,
// backed by protoregistry.GlobalFiles. Any proto-generated Go package imported in the
// test binary makes its services and types discoverable through reflection.
func registerGlobalReflection(s *grpc.Server) {
	reflection.Register(&allServicesGRPCServer{s})
}
