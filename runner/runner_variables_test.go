package runner

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lamoda/gonkey/models"
	"github.com/lamoda/gonkey/testloader/yaml_file"
	"github.com/lamoda/gonkey/variables"
)

func TestRunner_setVariablesFromResponse(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		test         *yaml_file.Test
		result       *models.Result
		wantVarName  string
		wantVarValue string
		wantErr      bool
	}{
		"happy_path": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{
					Transport: "grpc",
					VariablesToSet: map[int]map[string]string{
						0: {"userName": "user.name"},
					},
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{"user": {"name": "Alice"}}`,
			},
			wantVarName:  "userName",
			wantVarValue: "Alice",
		},
		"grpc_status_key_not_in_map": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{
					Transport: "grpc",
					VariablesToSet: map[int]map[string]string{
						5: {"errCode": "code"},
					},
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{"user": {"name": "Alice"}}`,
			},
		},
		"nil_variables_to_set": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{},
			},
			result: &models.Result{},
		},
		"http_uses_response_status_code_as_key": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{
					VariablesToSet: map[int]map[string]string{
						200: {"respField": "field"},
					},
				},
			},
			result: &models.Result{
				ResponseStatusCode:  200,
				ResponseContentType: "application/json",
				ResponseBody:        `{"field": "value"}`,
			},
			wantVarName:  "respField",
			wantVarValue: "value",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			vars := variables.New()
			r := &Runner{
				config: &Config{
					Variables: vars,
				},
			}

			err := r.setVariablesFromResponse(tc.test, tc.result)

			if tc.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.wantVarName != "" {
				assert.Equal(t, tc.wantVarValue, getVarValue(t, vars, tc.wantVarName))
			}
		})
	}
}

func getVarValue(t *testing.T, vars *variables.Variables, name string) string {
	t.Helper()

	result := vars.Apply(&yaml_file.Test{
		Request: "{{ $" + name + " }}",
	})

	return result.GetRequest()
}
