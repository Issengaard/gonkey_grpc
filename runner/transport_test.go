package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lamoda/gonkey/testloader/yaml_file"
)

func TestNewTransportExecutor(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		transport string
		wantErr   bool
		wantType  interface{}
	}{
		"happy_path_empty_transport":         {transport: "", wantErr: false, wantType: &HttpTransport{}},
		"happy_path_http_transport":          {transport: "http", wantErr: false, wantType: &HttpTransport{}},
		"happy_path_grpc_transport":          {transport: "grpc", wantErr: false, wantType: &GrpcTransport{}},
		"return_error_for_unknown_transport": {transport: "unknown", wantErr: true},
	}

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test := &yaml_file.Test{TestDefinition: yaml_file.TestDefinition{Transport: tc.transport}}
			executor, err := newTransportExecutor(test, &Config{})

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, executor)
			} else {
				require.NoError(t, err)
				assert.IsType(t, tc.wantType, executor)
			}
		})
	}
}
