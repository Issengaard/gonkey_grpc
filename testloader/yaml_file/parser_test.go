package yaml_file

import (
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/lamoda/gonkey/models"
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

func TestParseTestsWithCases(t *testing.T) {
	tmpfile, err := os.CreateTemp("../..", "tmpfile_")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpfile.Name())

	if _, err := fmt.Fprint(tmpfile, testsYAMLData); err != nil {
		t.Fatal(err)
	}

	tests, err := parseTestDefinitionFile(tmpfile.Name())
	if err != nil {
		t.Error(err)
	}
	if len(tests) != 2 {
		t.Errorf("wait len(tests) == 2, got len(tests) == %d", len(tests))
	}
}

func TestParseTestsWithFixtures(t *testing.T) {
	tests, err := parseTestDefinitionFile("./testdata/with-fixtures.yaml")
	if err != nil {
		t.Error(err)
	}

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
	tests, err := parseTestDefinitionFile("./testdata/with-db-checks.yaml")
	if err != nil {
		t.Error(err)
	}

	assert.Equal(t, 2, len(tests))
	assert.Equal(t, "", tests[0].GetDatabaseChecks()[0].DbNameString())
	assert.Equal(t, "connection_name", tests[1].GetDatabaseChecks()[0].DbNameString())
}

func TestParseGrpcFields(t *testing.T) {
	tests, err := parseTestDefinitionFile("./testdata/grpc-test.yaml")
	require.NoError(t, err)
	require.Len(t, tests, 2)

	// first test: reflection
	assert.Equal(t, "grpc", tests[0].GetTransport())
	require.NotNil(t, tests[0].GetProtoSource())
	assert.Equal(t, models.GrpcProtoSourceTypeReflection, tests[0].GetProtoSource().Type)
	assert.Equal(t, "", tests[0].GetProtoSource().ProtosetFile)

	// second test: protoset
	assert.Equal(t, "grpc", tests[1].GetTransport())
	require.NotNil(t, tests[1].GetProtoSource())
	assert.Equal(t, models.GrpcProtoSourceTypeProtoset, tests[1].GetProtoSource().Type)
	assert.Equal(t, "testdata/service.protoset", tests[1].GetProtoSource().ProtosetFile)
}
