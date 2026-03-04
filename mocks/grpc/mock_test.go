package grpcmock

import (
	"context"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

func newTestServer(t *testing.T) (*GrpcMock, *bufconn.Listener) {
	t.Helper()

	lis := bufconn.Listen(bufSize)
	m := New()
	go m.server.Serve(lis)
	t.Cleanup(m.Stop)

	return m, lis
}

func dialBufconn(t *testing.T, lis *bufconn.Listener) *grpc.ClientConn {
	t.Helper()

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) {
			return lis.DialContext(ctx)
		}),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(rawBytesCodec{})),
	)
	require.NoError(t, err)

	t.Cleanup(func() { conn.Close() })

	return conn
}

func TestGrpcMock_UnknownServiceHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		definitions []*GrpcDefinition
		callMethod  string
		callPayload []byte
		want        func(t *testing.T, resp []byte, err error, recorded []*RecordedRequest)
	}{
		{
			name: "happy_path",
			definitions: []*GrpcDefinition{
				{
					Service:        "TestService",
					Method:         "TestMethod",
					Response:       []byte("response-data"),
					ResponseStatus: codes.OK,
				},
			},
			callMethod:  "/TestService/TestMethod",
			callPayload: []byte("request-data"),
			want: func(t *testing.T, resp []byte, err error, recorded []*RecordedRequest) {
				require.NoError(t, err)
				assert.Equal(t, []byte("response-data"), resp)
				require.Len(t, recorded, 1)
				assert.Equal(t, "/TestService/TestMethod", recorded[0].Method)
				assert.Equal(t, []byte("request-data"), recorded[0].Payload)
			},
		},
		{
			name:        "unregistered_method",
			definitions: nil,
			callMethod:  "/Unknown/Method",
			callPayload: []byte("data"),
			want: func(t *testing.T, _ []byte, err error, recorded []*RecordedRequest) {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.Unimplemented, st.Code())
				assert.Empty(t, recorded)
			},
		},
		{
			name: "error_status",
			definitions: []*GrpcDefinition{
				{
					Service:        "TestService",
					Method:         "NotFoundMethod",
					Response:       []byte("not-found-body"),
					ResponseStatus: codes.NotFound,
				},
			},
			callMethod:  "/TestService/NotFoundMethod",
			callPayload: []byte("request"),
			want: func(t *testing.T, _ []byte, err error, recorded []*RecordedRequest) {
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.NotFound, st.Code())
				require.Len(t, recorded, 1)
				assert.Equal(t, "/TestService/NotFoundMethod", recorded[0].Method)
				assert.Equal(t, []byte("request"), recorded[0].Payload)
			},
		},
	}

	for _, tt := range tests {
		tt := tt // UT-0041: capture for t.Parallel()
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mock, lis := newTestServer(t)
			conn := dialBufconn(t, lis)

			for _, def := range tt.definitions {
				mock.SetDefinition(def)
			}

			var resp []byte
			err := conn.Invoke(context.Background(), tt.callMethod, tt.callPayload, &resp)

			tt.want(t, resp, err, mock.GetRecordedRequests())
		})
	}
}

func TestGrpcMock_ResetDefinitions(t *testing.T) {
	t.Parallel()

	mock, lis := newTestServer(t)
	conn := dialBufconn(t, lis)

	mock.SetDefinition(&GrpcDefinition{
		Service:        "Svc",
		Method:         "Meth",
		Response:       []byte("resp"),
		ResponseStatus: codes.OK,
	})

	var resp []byte
	err := conn.Invoke(context.Background(), "/Svc/Meth", []byte("req"), &resp)
	require.NoError(t, err)
	assert.Len(t, mock.GetRecordedRequests(), 1)

	mock.ResetDefinitions()

	assert.Empty(t, mock.GetRecordedRequests())
	assert.Empty(t, mock.EndRunningContext())

	err = conn.Invoke(context.Background(), "/Svc/Meth", []byte("req"), &resp)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unimplemented, st.Code())
}

func TestGrpcMock_RecordedRequests(t *testing.T) {
	t.Parallel()

	mock, lis := newTestServer(t)
	conn := dialBufconn(t, lis)

	mock.SetDefinition(&GrpcDefinition{
		Service:        "Svc",
		Method:         "A",
		Response:       []byte("a"),
		ResponseStatus: codes.OK,
	})
	mock.SetDefinition(&GrpcDefinition{
		Service:        "Svc",
		Method:         "B",
		Response:       []byte("b"),
		ResponseStatus: codes.OK,
	})

	var resp []byte
	require.NoError(t, conn.Invoke(context.Background(), "/Svc/A", []byte("req-a"), &resp))
	require.NoError(t, conn.Invoke(context.Background(), "/Svc/B", []byte("req-b"), &resp))

	recorded := mock.GetRecordedRequests()
	require.Len(t, recorded, 2)
	assert.Equal(t, "/Svc/A", recorded[0].Method)
	assert.Equal(t, []byte("req-a"), recorded[0].Payload)
	assert.Equal(t, "/Svc/B", recorded[1].Method)
	assert.Equal(t, []byte("req-b"), recorded[1].Payload)
}

func TestGrpcMock_ConcurrentRequests(t *testing.T) {
	t.Parallel()

	mock, lis := newTestServer(t)
	conn := dialBufconn(t, lis)

	mock.SetDefinition(&GrpcDefinition{
		Service:        "Svc",
		Method:         "Conc",
		Response:       []byte("ok"),
		ResponseStatus: codes.OK,
	})

	const goroutines = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()

			var resp []byte
			_ = conn.Invoke(context.Background(), "/Svc/Conc", []byte("data"), &resp)
		}()
	}

	wg.Wait()

	recorded := mock.GetRecordedRequests()
	assert.Len(t, recorded, goroutines)
}

func TestGrpcMock_ExpectedRequestMismatch(t *testing.T) {
	t.Parallel()

	mock, lis := newTestServer(t)
	conn := dialBufconn(t, lis)

	mock.SetDefinition(&GrpcDefinition{
		Service:         "Svc",
		Method:          "Verify",
		ExpectedRequest: []byte("expected-payload"),
		Response:        []byte("resp"),
		ResponseStatus:  codes.OK,
	})

	var resp []byte
	err := conn.Invoke(context.Background(), "/Svc/Verify", []byte("wrong-payload"), &resp)
	require.NoError(t, err)

	errs := mock.EndRunningContext()
	require.Len(t, errs, 1)
	assert.Contains(t, errs[0].Error(), "request mismatch")
}
