package grpcmock

import (
	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/desc"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// GrpcMocks is a registry of named GrpcMock instances, shared with a descriptor source
// used for JSON→proto conversion at load time.
type GrpcMocks struct {
	mocks            map[string]*GrpcMock
	descriptorSource grpcurl.DescriptorSource
}

// NewGrpcMocks creates a registry backed by ds.
// Pass nil to auto-discover descriptors from protoregistry.GlobalFiles — this works
// out of the box when the test binary imports proto-generated Go packages.
func NewGrpcMocks(ds grpcurl.DescriptorSource) *GrpcMocks {
	if ds == nil {
		ds, _ = descriptorSourceFromGlobalRegistry() // best-effort; nil → loader reports error per call
	}

	return &GrpcMocks{
		mocks:            make(map[string]*GrpcMock),
		descriptorSource: ds,
	}
}

// descriptorSourceFromGlobalRegistry builds a DescriptorSource from every proto file
// registered in protoregistry.GlobalFiles. The registry is populated automatically
// when proto-generated Go packages are imported (no protoset file required).
func descriptorSourceFromGlobalRegistry() (grpcurl.DescriptorSource, error) {
	var fds []*desc.FileDescriptor

	var lastErr error

	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		d, err := desc.WrapFile(fd)
		if err != nil {
			lastErr = err
			return false
		}

		fds = append(fds, d)

		return true
	})

	if lastErr != nil {
		return nil, lastErr
	}

	if len(fds) == 0 {
		return nil, nil
	}

	return grpcurl.DescriptorSourceFromFileDescriptors(fds...)
}

// Add registers a mock under name.
func (g *GrpcMocks) Add(name string, mock *GrpcMock) {
	g.mocks[name] = mock
}

// Get returns the mock registered under name, or nil.
func (g *GrpcMocks) Get(name string) *GrpcMock {
	return g.mocks[name]
}

// GetNames returns all registered mock names.
func (g *GrpcMocks) GetNames() []string {
	names := make([]string, 0, len(g.mocks))
	for name := range g.mocks {
		names = append(names, name)
	}

	return names
}

// ResetAll calls ResetDefinitions on every registered mock.
func (g *GrpcMocks) ResetAll() {
	for _, m := range g.mocks {
		m.ResetDefinitions()
	}
}
