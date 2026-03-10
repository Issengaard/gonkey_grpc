package grpcmock

import (
	"fmt"
	"sync"

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
		ds = newGlobalRegistrySource()
	}

	return &GrpcMocks{
		mocks:            make(map[string]*GrpcMock),
		descriptorSource: ds,
	}
}

// globalRegistrySource is a DescriptorSource that resolves symbols on-demand from
// protoregistry.GlobalFiles, avoiding the duplicate-pointer issue that occurs when
// grpcurl.DescriptorSourceFromFileDescriptors walks transitive deps of multiple files
// that share the same import (e.g. google/protobuf/descriptor.proto).
type globalRegistrySource struct {
	mu    sync.Mutex
	cache map[string]*desc.FileDescriptor // proto path → wrapped fd (first seen wins)
}

func newGlobalRegistrySource() *globalRegistrySource {
	return &globalRegistrySource{cache: make(map[string]*desc.FileDescriptor)}
}

// wrapFile wraps fd and caches the result. If a file with the same path was already
// cached, the cached instance is returned so that all callers share the same pointer.
func (s *globalRegistrySource) wrapFile(fd protoreflect.FileDescriptor) (*desc.FileDescriptor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	path := string(fd.Path())
	if d, ok := s.cache[path]; ok {
		return d, nil
	}
	d, err := desc.WrapFile(fd)
	if err != nil {
		return nil, err
	}
	s.cache[path] = d
	return d, nil
}

// ListServices returns all fully-qualified service names registered in GlobalFiles.
func (s *globalRegistrySource) ListServices() ([]string, error) {
	var services []string
	seen := make(map[string]struct{})
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		svcs := fd.Services()
		for i := 0; i < svcs.Len(); i++ {
			name := string(svcs.Get(i).FullName())
			if _, ok := seen[name]; !ok {
				seen[name] = struct{}{}
				services = append(services, name)
			}
		}
		return true
	})
	return services, nil
}

// FindSymbol resolves a fully-qualified symbol name (service or message) from GlobalFiles.
func (s *globalRegistrySource) FindSymbol(fullyQualifiedName string) (desc.Descriptor, error) {
	var result desc.Descriptor
	protoregistry.GlobalFiles.RangeFiles(func(fd protoreflect.FileDescriptor) bool {
		wrappedFd, err := s.wrapFile(fd)
		if err != nil {
			return true // skip unreadable files
		}
		for _, svc := range wrappedFd.GetServices() {
			if svc.GetFullyQualifiedName() == fullyQualifiedName {
				result = svc
				return false
			}
		}
		for _, msg := range wrappedFd.GetMessageTypes() {
			if msg.GetFullyQualifiedName() == fullyQualifiedName {
				result = msg
				return false
			}
		}
		return true
	})
	if result != nil {
		return result, nil
	}
	return nil, fmt.Errorf("symbol not found: %s", fullyQualifiedName)
}

// AllExtensionsForType returns nil (extensions not needed for JSON→proto conversion).
func (s *globalRegistrySource) AllExtensionsForType(_ string) ([]*desc.FieldDescriptor, error) {
	return nil, nil
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
