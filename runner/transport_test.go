package runner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lamoda/gonkey/models"
	"github.com/lamoda/gonkey/testloader/yaml_file"
)

func TestRunner_NewTransportExecutor(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		transport string
		want      func(*testing.T, transportExecutor, error)
	}{
		"happy_path": {
			transport: "http",
			want: func(t *testing.T, ex transportExecutor, err error) {
				require.NoError(t, err)
				require.NotNil(t, ex)
				assert.IsType(t, &HttpTransport{}, ex)
			},
		},
		"empty_transport_defaults_to_http": {
			transport: "",
			want: func(t *testing.T, ex transportExecutor, err error) {
				require.NoError(t, err)
				assert.IsType(t, &HttpTransport{}, ex)
			},
		},
		"grpc_transport": {
			transport: "grpc",
			want: func(t *testing.T, ex transportExecutor, err error) {
				require.NoError(t, err)
				require.NotNil(t, ex)
				assert.IsType(t, &GrpcTransport{}, ex)
			},
		},
		"return_error_for_unknown_transport": {
			transport: "unknown",
			want: func(t *testing.T, ex transportExecutor, err error) {
				require.Error(t, err)
				assert.ErrorContains(t, err, "unsupported transport: unknown")
				assert.Nil(t, ex)
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test := &yaml_file.Test{TestDefinition: yaml_file.TestDefinition{Transport: tc.transport}}
			executor, err := newTransportExecutor(test, &Config{})
			tc.want(t, executor, err)
		})
	}
}

func TestGrpcTransport_Execute(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		host string
		want func(*testing.T, *models.Result, error)
	}{
		"empty_host_returns_error": {
			host: "",
			want: func(t *testing.T, result *models.Result, err error) {
				require.Error(t, err)
				assert.Nil(t, result)
			},
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			transport := &GrpcTransport{cfg: &Config{GrpcHost: tc.host}}
			test := &yaml_file.Test{TestDefinition: yaml_file.TestDefinition{Transport: "grpc"}}
			result, err := transport.Execute(context.Background(), test)
			tc.want(t, result, err)
		})
	}
}

func TestGrpcTransport_Close(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		setup func() *GrpcTransport
		want  func(*testing.T, error)
	}{
		"close_with_nil_conn_returns_no_error": {
			setup: func() *GrpcTransport { return newGrpcTransport(&Config{}) },
			want:  func(t *testing.T, err error) { require.NoError(t, err) },
		},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			err := tc.setup().Close()
			tc.want(t, err)
		})
	}
}
