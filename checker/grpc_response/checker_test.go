package grpc_response

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lamoda/gonkey/models"
	"github.com/lamoda/gonkey/testloader/yaml_file"
)

func TestGrpcResponseChecker_Check(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		test   *yaml_file.Test
		result *models.Result
		want   func(t *testing.T, errs []error, err error)
	}{
		"happy_path": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					0: `{"message": "Hello, World!"}`,
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{"message": "Hello, World!"}`,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				assert.Empty(t, errs)
			},
		},
		"skip_non_grpc_test": {
			test: &yaml_file.Test{},
			result: &models.Result{
				ResponseStatusCode: 200,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				assert.Nil(t, errs)
			},
		},
		"return_error_when_status_not_in_responses": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					0: `{"ok": true}`,
				},
			},
			result: &models.Result{
				GrpcStatusCode: 5,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				require.Len(t, errs, 1)
				assert.Contains(t, errs[0].Error(), "unexpected grpc status code")
			},
		},
		"no_body_check_when_expected_body_empty": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					0: "",
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{"anything": true}`,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				assert.Empty(t, errs)
			},
		},
		"return_system_error_when_expected_body_invalid": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					0: "not-json",
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{}`,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.Error(t, err)
				assert.Nil(t, errs)
			},
		},
		"return_test_error_when_actual_body_invalid": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					0: `{"ok": true}`,
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   "not-json",
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				require.Len(t, errs, 1)
				assert.Contains(t, errs[0].Error(), "grpc response body is not valid JSON")
			},
		},
		"return_error_when_body_mismatch": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					0: `{"key": "expected"}`,
				},
			},
			result: &models.Result{
				GrpcStatusCode: 0,
				ResponseBody:   `{"key": "actual"}`,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				assert.NotEmpty(t, errs)
			},
		},
		"status_in_responses_but_body_empty": {
			test: &yaml_file.Test{
				TestDefinition: yaml_file.TestDefinition{Transport: "grpc"},
				Responses: map[int]string{
					5: "",
				},
			},
			result: &models.Result{
				GrpcStatusCode: 5,
			},
			want: func(t *testing.T, errs []error, err error) {
				require.NoError(t, err)
				assert.Empty(t, errs)
			},
		},
	}

	checker := NewChecker()

	for name, tc := range tests {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			errs, err := checker.Check(tc.test, tc.result)
			tc.want(t, errs, err)
		})
	}
}
