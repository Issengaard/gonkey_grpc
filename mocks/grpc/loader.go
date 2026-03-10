package grpcmock

import (
	"fmt"

	"github.com/fullstorydev/grpcurl"
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/dynamic"
	"google.golang.org/grpc/codes"
)

// GrpcLoader reads per-test grpcMocks YAML definitions and calls SetDefinition on the
// appropriate registered GrpcMock, converting JSON response bodies to proto wire bytes.
type GrpcLoader struct {
	registry *GrpcMocks
}

// NewGrpcLoader creates a loader that uses registry to resolve mock instances and the
// registry's DescriptorSource for JSON→proto conversion.
func NewGrpcLoader(registry *GrpcMocks) *GrpcLoader {
	return &GrpcLoader{registry: registry}
}

// toStringMap converts map[interface{}]interface{} (yaml.v2 output) to map[string]interface{}.
func toStringMap(v interface{}) (map[string]interface{}, bool) {
	switch m := v.(type) {
	case map[string]interface{}:
		return m, true
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(m))
		for k, val := range m {
			key, ok := k.(string)
			if !ok {
				return nil, false
			}
			out[key] = val
		}
		return out, true
	}
	return nil, false
}

// Load accepts the map parsed from the grpcMocks YAML field. Each key is a mock name;
// each value is a map with: service, method, responseBody (JSON string), responseStatus (int).
func (l *GrpcLoader) Load(defs map[string]interface{}) error {
	for name, rawDef := range defs {
		defMap, ok := toStringMap(rawDef)
		if !ok {
			return fmt.Errorf("grpc mock %q: expected map, got %T", name, rawDef)
		}

		mock := l.registry.Get(name)
		if mock == nil {
			return fmt.Errorf("grpc mock %q: not registered in GrpcMocks registry", name)
		}

		service, _ := defMap["service"].(string)
		method, _ := defMap["method"].(string)
		responseBody, _ := defMap["responseBody"].(string)

		statusCode := codes.OK
		if v, ok := defMap["responseStatus"]; ok {
			switch val := v.(type) {
			case int:
				statusCode = codes.Code(val)
			case float64:
				statusCode = codes.Code(int(val))
			}
		}

		var responseBytes []byte

		if responseBody != "" {
			if l.registry.descriptorSource == nil {
				return fmt.Errorf(
					"grpc mock %q: responseBody requires a DescriptorSource (pass protoset or reflection source to NewGrpcMocks)",
					name,
				)
			}

			var err error

			responseBytes, err = jsonToProtoBytes(l.registry.descriptorSource, service, method, responseBody)
			if err != nil {
				return fmt.Errorf("grpc mock %q: convert responseBody: %w", name, err)
			}
		}

		var expectedBytes []byte

		if reqBody, _ := defMap["expectedRequest"].(string); reqBody != "" {
			if l.registry.descriptorSource == nil {
				return fmt.Errorf("grpc mock %q: expectedRequest requires a DescriptorSource", name)
			}

			var err error

			expectedBytes, err = jsonToProtoInputBytes(l.registry.descriptorSource, service, method, reqBody)
			if err != nil {
				return fmt.Errorf("grpc mock %q: convert expectedRequest: %w", name, err)
			}
		}

		var metadata map[string]string

		if raw, ok := defMap["metadata"]; ok {
			if m, ok := toStringMap(raw); ok {
				metadata = make(map[string]string, len(m))
				for k, v := range m {
					if s, ok := v.(string); ok {
						metadata[k] = s
					}
				}
			}
		}

		mock.SetDefinition(&GrpcDefinition{
			Service:         service,
			Method:          method,
			ExpectedRequest: expectedBytes,
			Response:        responseBytes,
			ResponseStatus:  statusCode,
			Metadata:        metadata,
		})
	}

	return nil
}

// jsonToProtoInputBytes converts a JSON string to proto wire-format bytes for the input type
// of the given service/method, using ds for descriptor resolution.
func jsonToProtoInputBytes(ds grpcurl.DescriptorSource, service, method, jsonBody string) ([]byte, error) {
	sym, err := ds.FindSymbol(service)
	if err != nil {
		return nil, fmt.Errorf("find service %q: %w", service, err)
	}

	svcDesc, ok := sym.(*desc.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("symbol %q is not a service descriptor (got %T)", service, sym)
	}

	methDesc := svcDesc.FindMethodByName(method)
	if methDesc == nil {
		return nil, fmt.Errorf("method %q not found in service %q", method, service)
	}

	inDesc := methDesc.GetInputType()

	dynMsg := dynamic.NewMessage(inDesc)
	if err := dynMsg.UnmarshalJSON([]byte(jsonBody)); err != nil {
		return nil, fmt.Errorf("unmarshal JSON for %q.%q: %w", service, method, err)
	}

	b, err := dynMsg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal proto for %q.%q: %w", service, method, err)
	}

	return b, nil
}

// jsonToProtoBytes converts a JSON string to proto wire-format bytes for the output type
// of the given service/method, using ds for descriptor resolution.
func jsonToProtoBytes(ds grpcurl.DescriptorSource, service, method, jsonBody string) ([]byte, error) {
	sym, err := ds.FindSymbol(service)
	if err != nil {
		return nil, fmt.Errorf("find service %q: %w", service, err)
	}

	svcDesc, ok := sym.(*desc.ServiceDescriptor)
	if !ok {
		return nil, fmt.Errorf("symbol %q is not a service descriptor (got %T)", service, sym)
	}

	methDesc := svcDesc.FindMethodByName(method)
	if methDesc == nil {
		return nil, fmt.Errorf("method %q not found in service %q", method, service)
	}

	outDesc := methDesc.GetOutputType()

	dynMsg := dynamic.NewMessage(outDesc)
	if err := dynMsg.UnmarshalJSON([]byte(jsonBody)); err != nil {
		return nil, fmt.Errorf("unmarshal JSON for %q.%q: %w", service, method, err)
	}

	b, err := dynMsg.Marshal()
	if err != nil {
		return nil, fmt.Errorf("marshal proto for %q.%q: %w", service, method, err)
	}

	return b, nil
}
