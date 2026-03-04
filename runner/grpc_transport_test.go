package runner

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"testing"

	"github.com/fullstorydev/grpcurl"
	gogoproto "github.com/golang/protobuf/proto" //nolint:staticcheck // deprecated package, required by grpcurl InvocationEventHandler
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/interop/grpc_testing"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protodesc"
	"google.golang.org/protobuf/types/descriptorpb"

	"github.com/lamoda/gonkey/models"
	grpc_mocks "github.com/lamoda/gonkey/runner/mocks/grpc_testing"
	"github.com/lamoda/gonkey/testloader/yaml_file"
)

const bufSize = 1024 * 1024

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// newBufconnServer starts a gRPC server over bufconn and returns the listener.
// The server is stopped via t.Cleanup.
func newBufconnServer(t *testing.T, register func(*grpc.Server)) *bufconn.Listener {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	srv := grpc.NewServer()
	register(srv)
	reflection.Register(srv)

	t.Cleanup(func() { srv.Stop() })
	go func() { _ = srv.Serve(lis) }() // error expected after srv.Stop()

	return lis
}

// dialBufconn creates a client connection to a bufconn listener.
// The "passthrough:///" scheme is required so that grpc.NewClient does not
// attempt DNS resolution of the in-memory "bufnet" target name.
func dialBufconn(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	require.NoError(t, err)

	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// newGrpcTransportWithConn creates a GrpcTransport with a pre-injected connection,
// bypassing the real dial so tests can use bufconn.
func newGrpcTransportWithConn(conn *grpc.ClientConn) *GrpcTransport {
	return &GrpcTransport{
		cfg:  &Config{GrpcHost: "bufnet"},
		conn: conn,
	}
}

// writeTestProtoset serialises the grpc_testing file descriptors into a temp
// .protoset file and returns its path.
func writeTestProtoset(t *testing.T) string {
	t.Helper()

	fds := &descriptorpb.FileDescriptorSet{
		File: []*descriptorpb.FileDescriptorProto{
			protodesc.ToFileDescriptorProto(grpc_testing.File_grpc_testing_empty_proto),
			protodesc.ToFileDescriptorProto(grpc_testing.File_grpc_testing_messages_proto),
			protodesc.ToFileDescriptorProto(grpc_testing.File_grpc_testing_payloads_proto),
			protodesc.ToFileDescriptorProto(grpc_testing.File_grpc_testing_test_proto),
		},
	}

	data, err := proto.Marshal(fds)
	require.NoError(t, err)

	f, err := os.CreateTemp(t.TempDir(), "*.protoset")
	require.NoError(t, err)

	_, err = f.Write(data)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	return f.Name()
}

// ---------------------------------------------------------------------------
// TestGrpcTransport_buildDescriptorSource
// ---------------------------------------------------------------------------

func TestGrpcTransport_buildDescriptorSource(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup func(t *testing.T) (*GrpcTransport, *grpc.ClientConn, *models.GrpcProtoSource)
		want  func(*testing.T, *GrpcTransport, interface{}, error)
	}{
		"happy_path": {
			setup: func(t *testing.T) (*GrpcTransport, *grpc.ClientConn, *models.GrpcProtoSource) {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				conn := dialBufconn(t, lis)
				return newGrpcTransportWithConn(conn), conn, nil
			},
			want: func(t *testing.T, _ *GrpcTransport, src interface{}, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		"protoset_cache_miss": {
			setup: func(t *testing.T) (*GrpcTransport, *grpc.ClientConn, *models.GrpcProtoSource) {
				t.Helper()
				protosetPath := writeTestProtoset(t)
				return &GrpcTransport{cfg: &Config{}}, nil, &models.GrpcProtoSource{
					Type:         models.GrpcProtoSourceTypeProtoset,
					ProtosetFile: protosetPath,
				}
			},
			want: func(t *testing.T, _ *GrpcTransport, src interface{}, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		"protoset_cache_hit": {
			setup: func(t *testing.T) (*GrpcTransport, *grpc.ClientConn, *models.GrpcProtoSource) {
				t.Helper()
				protosetPath := writeTestProtoset(t)
				transport := &GrpcTransport{cfg: &Config{}}
				protoSource := &models.GrpcProtoSource{
					Type:         models.GrpcProtoSourceTypeProtoset,
					ProtosetFile: protosetPath,
				}
				// prime the cache with the first call
				src1, err := transport.buildDescriptorSource(context.Background(), nil, protoSource)
				require.NoError(t, err)
				require.NotNil(t, src1)
				return transport, nil, protoSource
			},
			want: func(t *testing.T, _ *GrpcTransport, src interface{}, err error) {
				require.NoError(t, err)
				assert.NotNil(t, src)
			},
		},
		"unknown_type": {
			setup: func(t *testing.T) (*GrpcTransport, *grpc.ClientConn, *models.GrpcProtoSource) {
				t.Helper()
				return &GrpcTransport{cfg: &Config{}}, nil, &models.GrpcProtoSource{Type: "unknown"}
			},
			want: func(t *testing.T, _ *GrpcTransport, src interface{}, err error) {
				require.Error(t, err)
				assert.Nil(t, src)
				assert.Contains(t, err.Error(), "unknown proto_source type")
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport, conn, protoSource := tc.setup(t)

			src, err := transport.buildDescriptorSource(context.Background(), conn, protoSource)
			tc.want(t, transport, src, err)
		})
	}
}

// ---------------------------------------------------------------------------
// TestGrpcTransport_Execute
// ---------------------------------------------------------------------------

func TestGrpcTransport_Execute(t *testing.T) {
	t.Parallel()

	// Capture metadata so both buildTransport and want closures can reference it.
	var receivedMD metadata.MD

	tests := map[string]struct {
		buildTransport func(t *testing.T) *GrpcTransport
		buildTest      func(t *testing.T) *yaml_file.Test
		want           func(*testing.T, *models.Result, error)
	}{
		"happy_path": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				mockSvc.EXPECT().EmptyCall(mock.Anything, mock.Anything).
					Return(&grpc_testing.Empty{}, nil).Once()
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				return newGrpcTransportWithConn(dialBufconn(t, lis))
			},
			buildTest: func(_ *testing.T) *yaml_file.Test {
				return &yaml_file.Test{
					TestDefinition: yaml_file.TestDefinition{
						Transport:  "grpc",
						RequestURL: "grpc.testing.TestService/EmptyCall",
					},
					Request: "{}",
				}
			},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, 0, result.GrpcStatusCode)
				// EmptyCall returns an empty proto; grpcurl encodes it as "{}"
				assert.Equal(t, "{}", result.ResponseBody)
			},
		},
		"return_notfound_as_result": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				mockSvc.EXPECT().EmptyCall(mock.Anything, mock.Anything).
					Return(nil, status.Error(codes.NotFound, "item not found")).Once()
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				return newGrpcTransportWithConn(dialBufconn(t, lis))
			},
			buildTest: func(_ *testing.T) *yaml_file.Test {
				return &yaml_file.Test{
					TestDefinition: yaml_file.TestDefinition{
						Transport:  "grpc",
						RequestURL: "grpc.testing.TestService/EmptyCall",
					},
					Request: "{}",
				}
			},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, int(codes.NotFound), result.GrpcStatusCode)
				assert.NotEmpty(t, result.GrpcStatusMessage)
				assert.Contains(t, result.ResponseBody, `"message"`)
			},
		},
		"return_unavailable_status_for_unreachable_host": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				// No pre-injected conn; the host cannot be resolved or reached.
				// grpc.NewClient is lazy, so the error surfaces during the RPC
				// invocation as a non-OK gRPC status (Unavailable), not as a Go error.
				return &GrpcTransport{cfg: &Config{GrpcHost: "255.255.255.255:1"}}
			},
			buildTest: func(_ *testing.T) *yaml_file.Test {
				return &yaml_file.Test{
					TestDefinition: yaml_file.TestDefinition{
						Transport:  "grpc",
						RequestURL: "grpc.testing.TestService/EmptyCall",
					},
					Request: "{}",
				}
			},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, int(codes.Unavailable), result.GrpcStatusCode)
			},
		},
		"metadata_forwarded_to_server": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				mockSvc.EXPECT().EmptyCall(mock.Anything, mock.Anything).
					Run(func(ctx context.Context, _ *grpc_testing.Empty) {
						receivedMD, _ = metadata.FromIncomingContext(ctx)
					}).
					Return(&grpc_testing.Empty{}, nil).Once()
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				return newGrpcTransportWithConn(dialBufconn(t, lis))
			},
			buildTest: func(_ *testing.T) *yaml_file.Test {
				return &yaml_file.Test{
					TestDefinition: yaml_file.TestDefinition{
						Transport:  "grpc",
						RequestURL: "grpc.testing.TestService/EmptyCall",
						HeadersVal: map[string]string{
							"x-custom-header": "test-value",
						},
					},
					Request: "{}",
				}
			},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, 0, result.GrpcStatusCode)
				require.NotNil(t, receivedMD, "server should have captured incoming metadata")
				vals := receivedMD.Get("x-custom-header")
				require.NotEmpty(t, vals, "x-custom-header must be present in server-side metadata")
				assert.Equal(t, "test-value", vals[0])
			},
		},
		"grpc_trailers_populated": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				mockSvc.EXPECT().EmptyCall(mock.Anything, mock.Anything).
					Run(func(ctx context.Context, _ *grpc_testing.Empty) {
						grpc.SetTrailer(ctx, metadata.Pairs("x-test-trailer", "trailer-value"))
					}).
					Return(&grpc_testing.Empty{}, nil).Once()
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				return newGrpcTransportWithConn(dialBufconn(t, lis))
			},
			buildTest: func(_ *testing.T) *yaml_file.Test {
				return &yaml_file.Test{
					TestDefinition: yaml_file.TestDefinition{
						Transport:  "grpc",
						RequestURL: "grpc.testing.TestService/EmptyCall",
					},
					Request: "{}",
				}
			},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				require.NotNil(t, result.GrpcTrailers)
				assert.Equal(t, []string{"trailer-value"}, result.GrpcTrailers["x-test-trailer"])
			},
		},
		"protoset_source": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				mockSvc.EXPECT().EmptyCall(mock.Anything, mock.Anything).
					Return(&grpc_testing.Empty{}, nil).Once()
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				return newGrpcTransportWithConn(dialBufconn(t, lis))
			},
			buildTest: func(t *testing.T) *yaml_file.Test {
				protosetPath := writeTestProtoset(t)
				return &yaml_file.Test{
					TestDefinition: yaml_file.TestDefinition{
						Transport:  "grpc",
						RequestURL: "grpc.testing.TestService/EmptyCall",
						ProtoSource: &models.GrpcProtoSource{
							Type:         models.GrpcProtoSourceTypeProtoset,
							ProtosetFile: protosetPath,
						},
					},
					Request: "{}",
				}
			},
			want: func(t *testing.T, result *models.Result, err error) {
				require.NoError(t, err)
				require.NotNil(t, result)
				assert.Equal(t, 0, result.GrpcStatusCode)
				assert.Equal(t, "{}", result.ResponseBody)
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := tc.buildTransport(t)
			test := tc.buildTest(t)
			result, err := transport.Execute(context.Background(), test)
			tc.want(t, result, err)
		})
	}
}

func TestGrpcTransport_Close(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		buildTransport func(t *testing.T) *GrpcTransport
		want           func(*testing.T, *GrpcTransport, error)
	}{
		"happy_path": {
			buildTransport: func(_ *testing.T) *GrpcTransport {
				return &GrpcTransport{cfg: &Config{GrpcHost: "bufnet"}}
			},
			want: func(t *testing.T, transport *GrpcTransport, err error) {
				require.NoError(t, err)
				assert.Nil(t, transport.conn)
			},
		},
		"close_initialized_conn": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				conn := dialBufconn(t, lis)
				return newGrpcTransportWithConn(conn)
			},
			want: func(t *testing.T, transport *GrpcTransport, err error) {
				require.NoError(t, err)
				assert.Nil(t, transport.conn)
			},
		},
		"close_after_close_is_noop": {
			buildTransport: func(t *testing.T) *GrpcTransport {
				t.Helper()
				mockSvc := grpc_mocks.NewMockTestServiceServer(t)
				lis := newBufconnServer(t, func(s *grpc.Server) {
					grpc_testing.RegisterTestServiceServer(s, mockSvc)
				})
				conn := dialBufconn(t, lis)
				return newGrpcTransportWithConn(conn)
			},
			want: func(t *testing.T, transport *GrpcTransport, err error) {
				require.NoError(t, err)
				closingErr := transport.Close()
				require.NoError(t, closingErr)
				assert.Nil(t, transport.conn)
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := tc.buildTransport(t)
			err := transport.Close()
			tc.want(t, transport, err)
		})
	}
}

func TestGrpcResponseHandler_OnReceiveResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		formatter grpcurl.Formatter
		messages  []gogoproto.Message // sequence of messages to feed to the handler
		want      func(*testing.T, *grpcResponseHandler)
	}{
		"happy_path": {
			formatter: func(_ gogoproto.Message) (string, error) {
				return `{"result":"ok"}`, nil
			},
			messages: []gogoproto.Message{nil},
			want: func(t *testing.T, h *grpcResponseHandler) {
				require.NoError(t, h.formatErr)
				assert.Equal(t, `{"result":"ok"}`, h.out.String())
			},
		},
		"format_error_is_captured": {
			formatter: func(_ gogoproto.Message) (string, error) {
				return "", errors.New("format failed")
			},
			messages: []gogoproto.Message{nil},
			want: func(t *testing.T, h *grpcResponseHandler) {
				require.Error(t, h.formatErr)
				assert.Contains(t, h.formatErr.Error(), "format failed")
				assert.Empty(t, h.out.String())
			},
		},
		"second_format_error_does_not_overwrite_first": {
			formatter: func() grpcurl.Formatter {
				callCount := 0
				return func(_ gogoproto.Message) (string, error) {
					callCount++
					switch callCount {
					case 1:
						return "", errors.New("first error")
					default:
						return "", errors.New("second error")
					}
				}
			}(),
			messages: []gogoproto.Message{nil, nil},
			want: func(t *testing.T, h *grpcResponseHandler) {
				require.Error(t, h.formatErr)
				assert.Equal(t, "first error", h.formatErr.Error())
				assert.Empty(t, h.out.String())
			},
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var sb strings.Builder
			h := &grpcResponseHandler{
				out:       &sb,
				formatter: tc.formatter,
			}

			for _, msg := range tc.messages {
				h.OnReceiveResponse(msg)
			}

			tc.want(t, h)
		})
	}
}
