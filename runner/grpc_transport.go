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

// Compile-time interface check (ES-0053).
var _ TransportExecutor = (*GrpcTransport)(nil)

// GrpcTransport — реализация TransportExecutor для gRPC вызовов без proto-стабов.
// Использует grpcurl как библиотеку для динамических вызовов через reflection или protoset.
type GrpcTransport struct {
	cfg             *Config
	descSourceCache sync.Map // ключ: host string → grpcurl.DescriptorSource (только reflection)
}

// grpcResponseHandler реализует grpcurl.InvocationEventHandler.
// Собирает JSON-сериализованный ответ и trailing metadata.
type grpcResponseHandler struct {
	out       *strings.Builder
	formatter grpcurl.Formatter
	trailers  metadata.MD
}

func newGrpcTransport(cfg *Config) *GrpcTransport { //nolint:unused // called from newTransportExecutor, will be wired in a later task
	return &GrpcTransport{cfg: cfg}
}

// Execute выполняет один gRPC вызов и возвращает результат.
func (t *GrpcTransport) Execute(ctx context.Context, test models.TestInterface) (*models.Result, error) {
	host := t.cfg.GrpcHost

	// 1. Открыть соединение через grpc.NewClient (не grpc.Dial).
	conn, err := grpc.NewClient(
		host,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, fmt.Errorf("grpc dial %s: %w", host, err)
	}
	defer conn.Close()

	// 2. Получить descriptor source.
	descSource, err := t.buildDescriptorSource(ctx, conn, test.GetProtoSource(), host)
	if err != nil {
		return nil, fmt.Errorf("descriptor source: %w", err)
	}

	// 3. Применить metadata из headers.
	if md := test.Headers(); len(md) > 0 {
		ctx = metadata.NewOutgoingContext(ctx, metadata.New(md))
	}

	// 4. Сформировать имя метода из path (формат: "service/method" → "/service/method").
	methodName := "/" + test.Path()

	// 5. Создать парсер запроса и форматтер ответа.
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

	// 6. Вызвать RPC.
	var responseBody strings.Builder
	h := &grpcResponseHandler{out: &responseBody, formatter: formatter}

	invokeErr := grpcurl.InvokeRPC(ctx, descSource, conn, methodName, nil, h, rf.Next)

	// 7. Обработать результат.
	grpcStatus, ok := status.FromError(invokeErr)
	if !ok {
		return nil, invokeErr
	}

	result := &models.Result{
		Test:              test,
		GrpcStatusCode:    int(grpcStatus.Code()),
		GrpcStatusMessage: grpcStatus.Message(),
	}

	// Для ошибочных статусов формируем JSON-обёртку с message.
	if grpcStatus.Code() != 0 {
		result.ResponseBody = fmt.Sprintf(`{"message": %q}`, grpcStatus.Message())
	} else {
		result.ResponseBody = responseBody.String()
	}

	if h.trailers != nil {
		result.GrpcTrailers = map[string][]string(h.trailers)
	}

	return result, nil
}

func (t *GrpcTransport) buildDescriptorSource(
	ctx context.Context,
	conn *grpc.ClientConn,
	protoSource *models.GrpcProtoSource,
	host string,
) (grpcurl.DescriptorSource, error) {
	if protoSource == nil || protoSource.Type == "reflection" || protoSource.Type == "" {
		if cached, ok := t.descSourceCache.Load(host); ok {
			return cached.(grpcurl.DescriptorSource), nil
		}
		refClient := grpcreflect.NewClientAuto(ctx, conn)
		descSource := grpcurl.DescriptorSourceFromServer(ctx, refClient)
		t.descSourceCache.Store(host, descSource)

		return descSource, nil
	}
	if protoSource.Type == "protoset" {
		return grpcurl.DescriptorSourceFromProtoSets(protoSource.ProtosetFile)
	}

	return nil, fmt.Errorf("unknown proto_source type: %s", protoSource.Type)
}

func (h *grpcResponseHandler) OnResolveMethod(_ *desc.MethodDescriptor) {}
func (h *grpcResponseHandler) OnSendHeaders(_ metadata.MD)              {}
func (h *grpcResponseHandler) OnReceiveHeaders(_ metadata.MD)           {}

func (h *grpcResponseHandler) OnReceiveResponse(resp gogoproto.Message) {
	body, err := h.formatter(resp)
	if err != nil {
		return
	}
	h.out.WriteString(body)
}

func (h *grpcResponseHandler) OnReceiveTrailers(_ *status.Status, md metadata.MD) {
	h.trailers = md
}

func (h *grpcResponseHandler) OnComplete(_ error) {}
