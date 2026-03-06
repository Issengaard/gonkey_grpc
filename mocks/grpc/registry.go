package grpcmock

import "github.com/fullstorydev/grpcurl"

// GrpcMocks is a registry of named GrpcMock instances, shared with a descriptor source
// used for JSON→proto conversion at load time.
type GrpcMocks struct {
	mocks            map[string]*GrpcMock
	descriptorSource grpcurl.DescriptorSource
}

// NewGrpcMocks creates a registry backed by the given descriptor source.
// ds may be nil if no YAML-driven JSON→proto conversion is needed.
func NewGrpcMocks(ds grpcurl.DescriptorSource) *GrpcMocks {
	return &GrpcMocks{
		mocks:            make(map[string]*GrpcMock),
		descriptorSource: ds,
	}
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
