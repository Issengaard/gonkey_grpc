package runner

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/fullstorydev/grpcurl"
	gogoproto "github.com/golang/protobuf/proto" //nolint:staticcheck // deprecated package, required by grpcurl InvocationEventHandler
	"github.com/jhump/protoreflect/desc"
	"github.com/jhump/protoreflect/grpcreflect"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/Issengaard/gonkey_grpc/models"
)

var _ transportExecutor = (*GrpcTransport)(nil)
var _ grpcurl.InvocationEventHandler = (*grpcResponseHandler)(nil)

// GrpcTransport is a transportExecutor implementation for gRPC calls without proto stubs.
// It uses grpcurl as a library for dynamic invocations via server reflection or protoset files.
type GrpcTransport struct {
	cfg             *Config
	connMu          sync.RWMutex
	conn            *grpc.ClientConn
	descSourceCache sync.Map // key: path string → grpcurl.DescriptorSource (protoset only)
}

// grpcResponseHandler implements grpcurl.InvocationEventHandler.
// It collects the JSON-serialised response body, the server-side gRPC status,
// and trailing metadata.
type grpcResponseHandler struct {
	out        *strings.Builder
	formatter  grpcurl.Formatter
	trailers   metadata.MD
	grpcStatus *status.Status // captured from OnReceiveTrailers; may be nil if call never reached the server
	formatErr  error
}

// newGrpcTransport creates a GrpcTransport for the given config.
func newGrpcTransport(cfg *Config) *GrpcTransport {
	return &GrpcTransport{cfg: cfg}
}

func (t *GrpcTransport) getConn() (*grpc.ClientConn, error) {
	t.connMu.RLock()
	conn := t.conn
	t.connMu.RUnlock()

	if conn != nil {
		return conn, nil
	}

	t.connMu.Lock()
	defer t.connMu.Unlock()

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
	t.connMu.Lock()
	defer t.connMu.Unlock()

	if t.conn != nil {
		err := t.conn.Close()
		t.conn = nil

		return err
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

	var body strings.Builder
	h := &grpcResponseHandler{out: &body, formatter: formatter}

	invokeErr := grpcurl.InvokeRPC(ctx, descSource, conn, test.Path(), buildGrpcHeaders(test.Headers()), h, rf.Next)
	if h.formatErr != nil {
		return nil, fmt.Errorf("format grpc response: %w", h.formatErr)
	}

	grpcStatus, err := resolveGrpcStatus(h, invokeErr)
	if err != nil {
		return nil, err
	}

	return buildGrpcResult(test, body.String(), grpcStatus, h.trailers), nil
}

func buildGrpcHeaders(headers map[string]string) []string {
	out := make([]string, 0, len(headers))
	for k, v := range headers {
		out = append(out, k+": "+v)
	}

	return out
}

// resolveGrpcStatus extracts a gRPC status from the handler or the invocation error.
// When the call reaches the server, the handler captures the status via OnReceiveTrailers.
// For pre-flight failures (symbol resolution, descriptor errors) the status is extracted
// from invokeErr via status.FromError.
func resolveGrpcStatus(h *grpcResponseHandler, invokeErr error) (*status.Status, error) {
	if h.grpcStatus != nil {
		return h.grpcStatus, nil
	}

	st, ok := status.FromError(invokeErr)
	if !ok {
		return nil, invokeErr
	}

	return st, nil
}

func buildGrpcResult(
	test models.TestInterface,
	responseBody string,
	st *status.Status,
	trailers metadata.MD,
) *models.Result {
	result := &models.Result{
		Test:              test,
		GrpcStatusCode:    int(st.Code()),
		GrpcStatusMessage: st.Message(),
	}

	if st.Code() != 0 {
		result.ResponseBody = fmt.Sprintf(`{"message": %q}`, st.Message())
	} else {
		result.ResponseBody = responseBody
	}

	if trailers != nil {
		result.GrpcTrailers = trailers
	}

	return result
}

// isReflectionSource reports whether the given proto source should use gRPC server reflection.
// It returns true for nil sources, "reflection" type, or empty type (backward compatible default).
func isReflectionSource(ps *models.GrpcProtoSource) bool {
	return ps == nil || ps.Type == models.GrpcProtoSourceTypeReflection || ps.Type == ""
}

// buildDescriptorSource returns a grpcurl.DescriptorSource for the given proto source.
//
// For reflection sources, a fresh reflection client is created each time because the service
// schema may evolve between calls; the underlying gRPC connection itself is persistent.
//
// For protoset sources, parsed descriptors are cached in descSourceCache. A concurrent first
// access may parse the same file more than once, but sync.Map.LoadOrStore guarantees only one
// value is retained. Use singleflight if contention becomes a concern.
func (t *GrpcTransport) buildDescriptorSource(
	ctx context.Context,
	conn *grpc.ClientConn,
	protoSource *models.GrpcProtoSource,
) (grpcurl.DescriptorSource, error) {
	if isReflectionSource(protoSource) {
		refClient := grpcreflect.NewClientAuto(ctx, conn)

		return grpcurl.DescriptorSourceFromServer(ctx, refClient), nil
	}

	if protoSource.Type == models.GrpcProtoSourceTypeProtoset {
		if cached, ok := t.descSourceCache.Load(protoSource.ProtosetFile); ok {
			if src, ok := cached.(grpcurl.DescriptorSource); ok {
				return src, nil
			}
		}

		src, err := grpcurl.DescriptorSourceFromProtoSets(protoSource.ProtosetFile)
		if err != nil {
			return nil, fmt.Errorf("load protoset %s: %w", protoSource.ProtosetFile, err)
		}

		actual, _ := t.descSourceCache.LoadOrStore(protoSource.ProtosetFile, src)
		if loaded, ok := actual.(grpcurl.DescriptorSource); ok {
			return loaded, nil
		}

		return src, nil
	}

	return nil, fmt.Errorf("unknown proto_source type: %s", protoSource.Type)
}

func (h *grpcResponseHandler) OnResolveMethod(_ *desc.MethodDescriptor) {}
func (h *grpcResponseHandler) OnSendHeaders(_ metadata.MD)              {}
func (h *grpcResponseHandler) OnReceiveHeaders(_ metadata.MD)           {}

func (h *grpcResponseHandler) OnReceiveResponse(resp gogoproto.Message) {
	if h.formatErr != nil {
		return
	}

	body, err := h.formatter(resp)
	if err != nil {
		h.formatErr = err

		return
	}

	h.out.WriteString(body)
}

func (h *grpcResponseHandler) OnReceiveTrailers(stat *status.Status, md metadata.MD) {
	h.grpcStatus = stat
	h.trailers = md
}

func (h *grpcResponseHandler) OnComplete(_ error) {}
