package yaml_file

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lamoda/gonkey/models"
)

func TestNewTestWithCases(t *testing.T) {
	t.Parallel()

	type wantResult struct {
		json     []byte
		filename string
	}

	cases := map[string]struct {
		filename    string
		definition  TestDefinition
		wantCount   int
		wantResults []wantResult
	}{
		"two_cases": {
			filename: "cases/example.yaml",
			definition: TestDefinition{
				RequestTmpl: `{"foo": "bar", "hello": {{ .hello }} }`,
				ResponseTmpls: map[int]string{
					200: `{"foo": "bar", "hello": {{ .hello }} }`,
					400: `{"foo": "bar", "hello": {{ .hello }} }`,
				},
				ResponseHeaders: map[int]map[string]string{
					200: {
						"hello": "world",
						"say":   "hello",
					},
					400: {
						"hello": "world",
						"foo":   "bar",
					},
				},
				Cases: []CaseData{
					{
						RequestArgs: map[string]interface{}{
							"hello": `"world"`,
						},
						ResponseArgs: map[int]map[string]interface{}{
							200: {"hello": "world"},
							400: {"hello": "world"},
						},
					},
					{
						RequestArgs: map[string]interface{}{
							"hello": `"world2"`,
						},
						ResponseArgs: map[int]map[string]interface{}{
							200: {"hello": "world2"},
							400: {"hello": "world2"},
						},
					},
				},
			},
			wantCount: 2,
			wantResults: []wantResult{
				{json: []byte(`{"foo": "bar", "hello": "world" }`), filename: "cases/example.yaml"},
				{json: []byte(`{"foo": "bar", "hello": "world2" }`), filename: "cases/example.yaml"},
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got, err := makeTestFromDefinition(tc.filename, tc.definition)
			require.NoError(t, err)
			require.Len(t, got, tc.wantCount)
			for i, want := range tc.wantResults {
				reqData, err := got[i].ToJSON()
				require.NoError(t, err)
				assert.Equal(t, want.json, reqData)
				assert.Equal(t, want.filename, got[i].GetFileName())
			}
		})
	}
}

func TestTest_GetTransport(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		transport string
		want      string
	}{
		"happy_path": {
			transport: "grpc",
			want:      "grpc",
		},
		"empty_for_http": {
			transport: "",
			want:      "",
		},
		"arbitrary_value": {
			transport: "custom-transport",
			want:      "custom-transport",
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			td := &Test{
				TestDefinition: TestDefinition{
					Transport: tc.transport,
				},
			}
			assert.Equal(t, tc.want, td.GetTransport())
		})
	}
}

func TestTest_GetProtoSource(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		source *models.GrpcProtoSource
		want   *models.GrpcProtoSource
	}{
		"happy_path": {
			source: &models.GrpcProtoSource{
				Type:         "reflection",
				ProtosetFile: "",
			},
			want: &models.GrpcProtoSource{
				Type:         "reflection",
				ProtosetFile: "",
			},
		},
		"nil_proto_source": {
			source: nil,
			want:   nil,
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			td := &Test{
				TestDefinition: TestDefinition{
					ProtoSource: tc.source,
				},
			}
			assert.Equal(t, tc.want, td.GetProtoSource())
		})
	}
}

func TestTest_Clone_ProtoSource(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		input  *Test
		verify func(t *testing.T, original, cloned models.TestInterface)
	}{
		"happy_path": {
			input: &Test{
				TestDefinition: TestDefinition{
					Name: "test-name",
				},
			},
			verify: func(t *testing.T, original, cloned models.TestInterface) {
				require.NotNil(t, cloned)
				origTest := original.(*Test)
				clonedTest := cloned.(*Test)
				assert.Equal(t, origTest.Name, clonedTest.Name)
				assert.NotSame(t, origTest, clonedTest)
			},
		},
		"proto_source_pointer_copied": {
			input: &Test{
				TestDefinition: TestDefinition{
					ProtoSource: &models.GrpcProtoSource{
						Type:         "reflection",
						ProtosetFile: "",
					},
				},
			},
			verify: func(t *testing.T, original, cloned models.TestInterface) {
				clonedTest := cloned.(*Test)
				origTest := original.(*Test)
				require.NotNil(t, clonedTest.ProtoSource)
				// разные указатели
				assert.NotSame(t, origTest.ProtoSource, clonedTest.ProtoSource)
				// одинаковые значения
				assert.Equal(t, *origTest.ProtoSource, *clonedTest.ProtoSource)
			},
		},
		"nil_proto_source_handled": {
			input: &Test{
				TestDefinition: TestDefinition{
					ProtoSource: nil,
				},
			},
			verify: func(t *testing.T, original, cloned models.TestInterface) {
				clonedTest := cloned.(*Test)
				assert.Nil(t, clonedTest.ProtoSource)
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cloned := tc.input.Clone()
			tc.verify(t, tc.input, cloned)
		})
	}
}
