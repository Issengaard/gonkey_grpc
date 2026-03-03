package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fullstorydev/grpcurl"
	gogoproto "github.com/golang/protobuf/proto" //nolint:staticcheck // deprecated package, required for grpcurl InvocationEventHandler interface
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/lamoda/gonkey/models"
)

// Compile-time interface check
var _ transportExecutor = (*GrpcTransport)(nil)

// GrpcTransport is a transportExecutor implementation for gRPC calls without proto stubs.
// It uses grpcurl as a library for dynamic invocations via server reflection or protoset files.
type GrpcTransport struct {
	cfg             *Config
	mu              sync.Mutex       // protects conn
	conn            *grpc.ClientConn // protected by mu
	descSourceCache sync.Map         // key: path string → grpcurl.DescriptorSource (protoset only)
}

// grpcResponseHandler implements grpcurl.InvocationEventHandler.
// It collects the JSON-serialised response body and trailing metadata.
type grpcResponseHandler struct {
	out       *strings.Builder
	formatter grpcurl.Formatter
	trailers  metadata.MD
	formatErr error
}

func newGrpcTransport(cfg *Config) *GrpcTransport {
	return &GrpcTransport{cfg: cfg}
}

func (t *GrpcTransport) getConn() (*grpc.ClientConn, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn != nil {
		return t.conn, nil
	}

	conn, err := grpc.NewClient(
		t.cfg.GrpcHost,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", t.cfg.GrpcHost, err)
	}

	t.conn = conn

	return t.conn, nil
}

// Close closes the persistent gRPC connection.
func (t *GrpcTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.conn != nil {
		return t.conn.Close()
	}

	return nil
}

// Execute performs a single gRPC call and returns the result.
func (t *GrpcTransport) Execute(ctx context.Context, test models.TestInterface) (*models.Result, error) {
	conn, err := t.getConn()
	if err != nil {
		return nil, err
	}

	descSource, err := t.buildDescriptorSource(ctx, conn, test.GetProtoSource())
	if err != nil {
		return nil, fmt.Errorf("descriptor source: %w", err)
	}

	if md := test.Headers(); len(md) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(md))
	}

	methodName := "/" + test.Path()

	rf, formatter, err := grpcurl.RequestParserAndFormatterFor(
		grpcurl.FormatJSON,
		descSource,
		false,
		false,
		strings.NewReader(test.GetRequest()),
	)
	if err != nil {
		return nil, fmt.Errorf("parse grpc request: %w", err)
	}

	var responseBody strings.Builder
	h := &grpcResponseHandler{out: &responseBody, formatter: formatter}

	invokeErr := grpcurl.InvokeRPC(ctx, descSource, conn, methodName, nil, h, rf.Next)

	if h.formatErr != nil {
		return nil, fmt.Errorf("format grpc response: %w", h.formatErr)
	}

	grpcStatus, ok := status.FromError(invokeErr)
	if !ok {
		return nil, invokeErr
	}

	result := &models.Result{
		Test:              test,
		GrpcStatusCode:    int(grpcStatus.Code()),
		GrpcStatusMessage: grpcStatus.Message(),
	}

	// For non-OK status codes, wrap the message in a JSON object.
	if grpcStatus.Code() != 0 {
		result.ResponseBody = fmt.Sprintf(`{"message": %q}`, grpcStatus.Message())
	} else {
		result.ResponseBody = responseBody.String()
	}

	if h.trailers != nil {
		result.GrpcTrailers = h.trailers
	}

	return result, nil
}

func (t *GrpcTransport) buildDescriptorSource(
	ctx context.Context,
	conn *grpc.ClientConn,
	protoSource *models.GrpcProtoSource,
) (grpcurl.DescriptorSource, error) {
	if protoSource == nil || protoSource.Type == models.GrpcProtoSourceTypeReflection || protoSource.Type == "" {
		// Reflection client is intentionally not cached per-Execute:
		// the service schema may evolve between calls; the gRPC connection itself is persistent.
		refClient := grpcreflect.NewClientAuto(ctx, conn)

		return grpcurl.DescriptorSourceFromServer(ctx, refClient), nil
	}

	if protoSource.Type == models.GrpcProtoSourceTypeProtoset {
		if cached, ok := t.descSourceCache.Load(protoSource.ProtosetFile); ok {
			return cached.(grpcurl.DescriptorSource), nil
		}

		src, err := grpcurl.DescriptorSourceFromProtoSets(protoSource.ProtosetFile)
		if err != nil {
			return nil, err
		}

		t.descSourceCache.Store(protoSource.ProtosetFile, src)

		return src, nil
	}

	return nil, fmt.Errorf("unknown proto_source type: %s", protoSource.Type)
}

func (h *grpcResponseHandler) OnResolveMethod(_ *desc.MethodDescriptor) {}
func (h *grpcResponseHandler) OnSendHeaders(_ metadata.MD)              {}
func (h *grpcResponseHandler) OnReceiveHeaders(_ metadata.MD)           {}

func (h *grpcResponseHandler) OnReceiveResponse(resp gogoproto.Message) {
	if h.formatErr != nil { // guard: don't overwrite first error on streaming responses
		return
	}

	body, err := h.formatter(resp)
	if err != nil {
		h.formatErr = err

		return
	}

	h.out.WriteString(body)
}

func (h *grpcResponseHandler) OnReceiveTrailers(_ *status.Status, md metadata.MD) {
	h.trailers = md
}

func (h *grpcResponseHandler) OnComplete(_ error) {}
