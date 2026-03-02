package models

const (
	GrpcProtoSourceTypeReflection = "reflection"
	GrpcProtoSourceTypeProtoset   = "protoset"
)

// GrpcProtoSource describes the proto-schema source for schema discovery.
type GrpcProtoSource struct {
	// Type is the source type: "reflection" or "protoset".
	Type string `json:"type" yaml:"type"`
	// ProtosetFile is the path to the .protoset file (used when Type is "protoset").
	ProtosetFile string `json:"protoset_file" yaml:"protoset_file"`
}
