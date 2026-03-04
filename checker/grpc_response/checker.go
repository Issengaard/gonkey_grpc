package grpc_response

import (
	"encoding/json"
	"fmt"

	"github.com/lamoda/gonkey/checker"
	"github.com/lamoda/gonkey/compare"
	"github.com/lamoda/gonkey/models"
)

type GrpcResponseChecker struct{}

func NewChecker() checker.CheckerInterface {
	return &GrpcResponseChecker{}
}

func (c *GrpcResponseChecker) Check(test models.TestInterface, result *models.Result) ([]error, error) {
	if test.GetTransport() != "grpc" {
		return nil, nil
	}

	var errs []error

	expectedBody, ok := test.GetResponses()[result.GrpcStatusCode]
	if !ok {
		errs = append(errs, fmt.Errorf("unexpected grpc status code %d: not in expected responses", result.GrpcStatusCode))

		return errs, nil
	}

	if expectedBody != "" {
		var expectedJSON, actualJSON interface{}

		if err := json.Unmarshal([]byte(expectedBody), &expectedJSON); err != nil {
			return nil, fmt.Errorf("grpc expected body is not valid JSON in test %s: %w", test.GetName(), err)
		}

		if err := json.Unmarshal([]byte(result.ResponseBody), &actualJSON); err != nil {
			errs = append(errs, fmt.Errorf("grpc response body is not valid JSON: %s", result.ResponseBody))

			return errs, nil
		}

		params := compare.Params{
			IgnoreValues:         !test.NeedsCheckingValues(),
			IgnoreArraysOrdering: test.IgnoreArraysOrdering(),
			DisallowExtraFields:  test.DisallowExtraFields(),
		}

		errs = append(errs, compare.Compare(expectedJSON, actualJSON, params)...)
	}

	return errs, nil
}
