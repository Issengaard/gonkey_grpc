package compare

import (
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/fatih/color"
)

type Params struct {
	IgnoreValues         bool `json:"ignoreValues" yaml:"ignoreValues"`
	IgnoreArraysOrdering bool `json:"ignoreArraysOrdering" yaml:"ignoreArraysOrdering"`
	DisallowExtraFields  bool `json:"disallowExtraFields" yaml:"disallowExtraFields"`
	IgnoreDbOrdering     bool `json:"IgnoreDbOrdering" yaml:"ignoreDbOrdering"`
	failFast             bool // End compare operation after first error
}

type leafsMatchType int

const (
	pure leafsMatchType = iota
	regex
	arrayLen
)

const (
	arrayType = "array"
	mapType   = "map"
)

var (
	regexExprRx    = regexp.MustCompile(`^\$matchRegexp\((.+)\)$`)
	arrayLenExprRx = regexp.MustCompile(`^\$matchArrayLen\(([^)]*)\)$`)
)

// Compare compares values as plain text
// It can be compared several ways:
//   - Pure values: should be equal
//   - Regex: try to compile 'expected' as regex and match 'actual' with it
//     It activates on following syntax: $matchRegexp(%EXPECTED_VALUE%)
func Compare(expected, actual interface{}, params Params) []error {
	return compareBranch("$", expected, actual, &params)
}

func compareBranch(path string, expected, actual interface{}, params *Params) []error {
	expectedType := getType(expected)
	actualType := getType(actual)
	var errors []error

	mt := leafMatchType(expected)

	// $matchArrayLen replaces the default array-length check and the
	// per-element traversal; bypass the type check the same way regex does.
	if mt == arrayLen {
		return compareArrayLen(path, expected.(string), actual)
	}

	// compare types
	if mt != regex && expectedType != actualType {
		errors = append(errors, makeError(path, "types do not match", expectedType, actualType))

		return errors
	}

	// compare scalars
	if isScalarType(actualType) && !params.IgnoreValues {
		return compareLeafs(path, expected, actual)
	}

	// compare arrays
	if actualType == arrayType {
		expectedArray := convertToArray(expected)
		actualArray := convertToArray(actual)

		if len(expectedArray) != len(actualArray) {
			errors = append(errors, makeError(path, "array lengths do not match", len(expectedArray), len(actualArray)))

			return errors
		}

		if params.IgnoreArraysOrdering {
			expectedArray, actualArray = getUnmatchedArrays(expectedArray, actualArray, params)
		}

		// iterate over children
		for i, item := range expectedArray {
			subPath := fmt.Sprintf("%s[%d]", path, i)
			res := compareBranch(subPath, item, actualArray[i], params)
			errors = append(errors, res...)
			if params.failFast && len(errors) != 0 {
				return errors
			}
		}
	}

	// compare maps
	if actualType == mapType {
		expectedRef := reflect.ValueOf(expected)
		actualRef := reflect.ValueOf(actual)

		if params.DisallowExtraFields && expectedRef.Len() != actualRef.Len() {
			errors = append(errors, makeError(path, "map lengths do not match", expectedRef.Len(), actualRef.Len()))

			return errors
		}

		for _, key := range expectedRef.MapKeys() {
			// check keys presence
			if ok := actualRef.MapIndex(key); !ok.IsValid() {
				errors = append(errors, makeError(path, "key is missing", key.String(), "<missing>"))
				if params.failFast {
					return errors
				}

				continue
			}

			// check values
			subPath := fmt.Sprintf("%s.%s", path, key.String())
			res := compareBranch(
				subPath,
				expectedRef.MapIndex(key).Interface(),
				actualRef.MapIndex(key).Interface(),
				params,
			)
			errors = append(errors, res...)
			if params.failFast && len(errors) != 0 {
				return errors
			}
		}
	}

	return errors
}

func getType(value interface{}) string {
	if value == nil {
		return "nil"
	}

	rt := reflect.TypeOf(value)
	switch {
	case rt.Kind() == reflect.Slice || rt.Kind() == reflect.Array:
		return "array"
	case rt.Kind() == reflect.Map:
		return "map"
	default:
		return rt.String()
	}
}

func isScalarType(t string) bool {
	return !(t == "array" || t == "map")
}

func compareLeafs(path string, expected, actual interface{}) []error {
	var errors []error

	switch leafMatchType(expected) {
	case pure:
		errors = append(errors, comparePure(path, expected, actual)...)

	case regex:
		errors = append(errors, compareRegex(path, expected, actual)...)

	default:
		panic("unknown compare type")
	}

	return errors
}

func comparePure(path string, expected, actual interface{}) (errors []error) {
	if expected != actual {
		errors = append(errors, makeError(path, "values do not match", expected, actual))
	}

	return errors
}

func compareRegex(path string, expected, actual interface{}) (errors []error) {
	regexExpr, ok := expected.(string)
	if !ok {
		errors = append(errors, makeError(path, "type mismatch", "string", reflect.TypeOf(expected)))

		return errors
	}

	value := fmt.Sprintf("%v", actual)

	rx, err := regexp.Compile(retrieveRegexStr(regexExpr))
	if err != nil {
		errors = append(errors, makeError(path, "can not compile regex", nil, "error"))

		return errors
	}

	if !rx.MatchString(value) {
		errors = append(errors, makeError(path, "value does not match regex", expected, actual))

		return errors
	}

	return nil
}

func retrieveRegexStr(expr string) string {
	if matches := regexExprRx.FindStringSubmatch(expr); matches != nil {
		return matches[1]
	}

	return ""
}

func leafMatchType(expected interface{}) leafsMatchType {
	val, ok := expected.(string)
	if !ok {
		return pure
	}

	if matches := regexExprRx.FindStringSubmatch(val); matches != nil {
		return regex
	}

	if matches := arrayLenExprRx.FindStringSubmatch(val); matches != nil {
		return arrayLen
	}

	return pure
}

// compareArrayLen validates the length of an actual array against the spec
// declared in $matchArrayLen(...). The array contents are not inspected.
func compareArrayLen(path, expected string, actual interface{}) []error {
	matches := arrayLenExprRx.FindStringSubmatch(expected)
	if matches == nil {
		return []error{makeError(path, "matchArrayLen invalid spec", expected, "could not parse")}
	}

	minN, maxN, err := parseArrayLenSpec(matches[1])
	if err != nil {
		return []error{makeError(path, "matchArrayLen invalid spec", expected, err.Error())}
	}

	if getType(actual) != arrayType {
		return []error{makeError(path, "matchArrayLen requires array", expected, fmt.Sprintf("<%s>", getType(actual)))}
	}

	got := reflect.ValueOf(actual).Len()
	if got < minN || got > maxN {
		return []error{makeError(
			path,
			"matchArrayLen length out of bounds",
			expected,
			fmt.Sprintf("array length %d", got),
		)}
	}

	return nil
}

// parseArrayLenSpec parses the body of $matchArrayLen(...).
// Supported forms: "N" (exact), "min=N", "max=M", "min=N,max=M".
// Whitespace around "=" and "," is trimmed. The returned [minN, maxN] is the
// inclusive allowed range; an unbounded side becomes 0 or math.MaxInt.
func parseArrayLenSpec(body string) (minN, maxN int, err error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return 0, 0, fmt.Errorf("empty spec")
	}

	// Exact form: a bare unsigned integer.
	if n, convErr := strconv.Atoi(body); convErr == nil {
		if n < 0 {
			return 0, 0, fmt.Errorf("negative length: %d", n)
		}

		return n, n, nil
	}

	const maxInt = int(^uint(0) >> 1)
	minN, maxN = 0, maxInt
	haveMin, haveMax := false, false

	for _, part := range strings.Split(body, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			return 0, 0, fmt.Errorf("expected key=value, got %q", strings.TrimSpace(part))
		}

		key := strings.TrimSpace(kv[0])
		val := strings.TrimSpace(kv[1])

		n, convErr := strconv.Atoi(val)
		if convErr != nil {
			return 0, 0, fmt.Errorf("%s: not an integer: %q", key, val)
		}
		if n < 0 {
			return 0, 0, fmt.Errorf("%s: negative length: %d", key, n)
		}

		switch key {
		case "min":
			if haveMin {
				return 0, 0, fmt.Errorf("duplicate key: min")
			}
			haveMin = true
			minN = n
		case "max":
			if haveMax {
				return 0, 0, fmt.Errorf("duplicate key: max")
			}
			haveMax = true
			maxN = n
		default:
			return 0, 0, fmt.Errorf("unknown key: %q", key)
		}
	}

	if !haveMin && !haveMax {
		return 0, 0, fmt.Errorf("no constraints specified")
	}

	if minN > maxN {
		return 0, 0, fmt.Errorf("min (%d) > max (%d)", minN, maxN)
	}

	return minN, maxN, nil
}

func makeError(path, msg string, expected, actual interface{}) error {
	return fmt.Errorf(
		"at path %s %s:\n     expected: %s\n       actual: %s",
		color.CyanString(path),
		msg,
		color.GreenString("%v", expected),
		color.RedString("%v", actual),
	)
}

func convertToArray(array interface{}) []interface{} {
	ref := reflect.ValueOf(array)

	interfaceSlice := make([]interface{}, 0)
	for i := 0; i < ref.Len(); i++ {
		interfaceSlice = append(interfaceSlice, ref.Index(i).Interface())
	}

	return interfaceSlice
}

// For every elem in "expected" try to find elem in "actual". Returns arrays without matching.
func getUnmatchedArrays(expected, actual []interface{}, params *Params) (expectedUnmatched, actualUnmatched []interface{}) {
	expectedError := make([]interface{}, 0)

	failfastParams := *params
	failfastParams.failFast = true

	for _, expectedElem := range expected {
		found := false
		for i, actualElem := range actual {
			if len(compareBranch("", expectedElem, actualElem, &failfastParams)) == 0 {
				// expectedElem match actualElem
				found = true
				// remove actualElem from  actual
				if len(actual) != 1 {
					actual[i] = actual[len(actual)-1]
				}
				actual = actual[:len(actual)-1]

				break
			}
		}
		if !found {
			expectedError = append(expectedError, expectedElem)
			if params.failFast {
				return expectedError, actual[0:1]
			}
		}
	}

	return expectedError, actual
}
