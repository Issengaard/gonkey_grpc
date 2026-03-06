package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Issengaard/gonkey_grpc/testloader/yaml_file"
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
