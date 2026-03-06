package yaml_file

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Issengaard/gonkey_grpc/models"
)

var testsYAMLData = `
- method: POST
  path: /jsonrpc/v2/orders.nr
  request:
    '{
      "jsonrpc": "2.0",
      "id": "550e8400-e29b-41d4-a716-446655440000",
      "method": "orders.nr",
      "params": [
        {
          "amount": 1,
          "prefix": "ru"
        }
      ]
    }'
  response:
    200:
      '{
         "result": [
           {
             "nr": "number",
             "prefix": "ru",
             "vc": "vc"
           }
         ],
         "id": "550e8400-e29b-41d4-a716-446655440000",
         "jsonrpc": "2.0"
       }'
  cases:
    - requestArgs:
        foo: 'Hello world'
        bar: 42
      responseArgs:
        200:
          foo: 'Hello world'
          bar: 42
    - requestArgs:
        foo: 'Hello world'
        bar: 42
      responseArgs:
        200:
          foo: 'Hello world'
          bar: 42
      variables:
        newVar: some_value
`

func writeTempYAML(t *testing.T, content string) string {
	t.Helper()

	f, err := os.CreateTemp(t.TempDir(), "gonkey-test-*.yaml")
	require.NoError(t, err)

	_, err = fmt.Fprint(f, content)
	require.NoError(t, err)

	return f.Name()
}

func TestParseTestsWithCases(t *testing.T) {
	t.Parallel()

	path := writeTempYAML(t, testsYAMLData)

	tests, err := parseTestDefinitionFile(path)
	require.NoError(t, err)
	if len(tests) != 2 {
		t.Errorf("wait len(tests) == 2, got len(tests) == %d", len(tests))
	}
}

func TestParseTestsWithFixtures(t *testing.T) {
	t.Parallel()

	tests, err := parseTestDefinitionFile("./testdata/with-fixtures.yaml")
	require.NoError(t, err)

	assert.Equal(t, 2, len(tests))

	expectedSimple := []string{"path/fixture1.yaml", "path/fixture2.yaml"}
	assert.Equal(t, expectedSimple, tests[0].Fixtures())

	expectedMultiDb := models.FixturesMultiDb([]models.Fixture{
		{DbName: "conn1", Files: []string{"path/fixture3.yaml"}},
		{DbName: "conn2", Files: []string{"path/fixture4.yaml"}},
	})
	assert.Equal(t, expectedMultiDb, tests[1].FixturesMultiDb())
}

func TestParseTestsWithDbChecks(t *testing.T) {
	t.Parallel()

	tests, err := parseTestDefinitionFile("./testdata/with-db-checks.yaml")
	require.NoError(t, err)

	assert.Equal(t, 2, len(tests))
	assert.Equal(t, "", tests[0].GetDatabaseChecks()[0].DbNameString())
	assert.Equal(t, "connection_name", tests[1].GetDatabaseChecks()[0].DbNameString())
}

func TestParser_ParseGrpcTest(t *testing.T) {
	t.Parallel()

	parsedTests, err := parseTestDefinitionFile("testdata/grpc-test.yaml")
	require.NoError(t, err)
	require.Len(t, parsedTests, 2)

	cases := map[string]struct {
		testIdx int
		verify  func(t *testing.T, test *Test)
	}{
		"happy_path": {
			testIdx: 0,
			verify: func(t *testing.T, test *Test) {
				assert.Equal(t, "grpc", test.GetTransport())
				assert.Equal(t, "helloworld.Greeter/SayHello", test.Path())
				assert.Equal(t, `{"name": "World"}`, test.GetRequest())
				require.NotNil(t, test.GetProtoSource())
				assert.Equal(t, models.GrpcProtoSourceTypeReflection, test.GetProtoSource().Type)
				assert.Equal(t, "Bearer token123", test.Headers()["authorization"])
				body, ok := test.GetResponse(0)
				require.True(t, ok)
				assert.Equal(t, `{"message": "Hello, World!"}`, body)
			},
		},
		"protoset_with_file": {
			testIdx: 1,
			verify: func(t *testing.T, test *Test) {
				assert.Equal(t, "grpc", test.GetTransport())
				require.NotNil(t, test.GetProtoSource())
				assert.Equal(t, models.GrpcProtoSourceTypeProtoset, test.GetProtoSource().Type)
				assert.Equal(t, "testdata/service.protoset", test.GetProtoSource().ProtosetFile)
				body, ok := test.GetResponse(5)
				require.True(t, ok)
				assert.Equal(t, "", body)
				assert.Empty(t, test.Headers())
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			tc.verify(t, &parsedTests[tc.testIdx])
		})
	}
}

func TestParser_ParseGrpcTest_Errors(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		yamlContent string
		filePath    string
		wantErr     string
	}{
		"empty_protoset_file": {
			yamlContent: "- name: test\n  transport: grpc\n  path: svc/Method\n  proto_source:\n    type: protoset\n  response:\n    0: \"{}\"\n",
			wantErr:     "protoset_file is not set",
		},
		"missing_protoset_file": {
			filePath: "testdata/grpc-test-missing-protoset.yaml",
			wantErr:  "nonexistent.protoset",
		},
		"unknown_proto_source_type": {
			yamlContent: "- name: test\n  transport: grpc\n  path: svc/Method\n  proto_source:\n    type: unknown_type\n  response:\n    0: \"{}\"\n",
			wantErr:     "unknown proto_source type",
		},
	}

	for name, tc := range cases {
		tc := tc

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var path string
			if tc.yamlContent != "" {
				path = writeTempYAML(t, tc.yamlContent)
			} else {
				path = tc.filePath
			}

			_, err := parseTestDefinitionFile(path)
			require.Error(t, err)
			assert.ErrorContains(t, err, tc.wantErr)
		})
	}
}

func TestParser_ParseGrpcTest_Clone(t *testing.T) {
	t.Parallel()

	parsedTests, err := parseTestDefinitionFile("testdata/grpc-test.yaml")
	require.NoError(t, err)
	require.Len(t, parsedTests, 2)

	original := &parsedTests[0]
	cloned := original.Clone().(*Test)

	require.NotNil(t, cloned.GetProtoSource())
	assert.NotSame(t, original.GetProtoSource(), cloned.GetProtoSource())
	assert.Equal(t, *original.GetProtoSource(), *cloned.GetProtoSource())
}

func TestParser_ParseHttpTest(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		file   string
		verify func(t *testing.T, tests []Test)
	}{
		"happy_path": {
			file: "testdata/with-fixtures.yaml",
			verify: func(t *testing.T, tests []Test) {
				require.NotEmpty(t, tests)
				assert.Equal(t, "", tests[0].GetTransport())
				assert.Nil(t, tests[0].GetProtoSource())
			},
		},
	}

	for name, tc := range cases {
		tc := tc
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			parsedTests, err := parseTestDefinitionFile(tc.file)
			require.NoError(t, err)
			tc.verify(t, parsedTests)
		})
	}
}
