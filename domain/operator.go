package domain

import (
	"fmt"
	"regexp"
	"strings"
)

// Operator names the shared comparison vocabulary that validators
// share for value checks. Rule parameters carry an operator string
// (e.g. "equals", "ends_with"); the Compare helper below dispatches
// on it. Keeping the list centralized avoids per-validator drift
// where two mechanisms both accept "matches" but interpret it
// differently.
type Operator string

const (
	OpEquals      Operator = "equals"
	OpNotEquals   Operator = "not_equals"
	OpGreaterThan Operator = "greater_than"
	OpLessThan    Operator = "less_than"
	OpInSet       Operator = "in_set"
	OpNotInSet    Operator = "not_in_set"
	OpStartsWith  Operator = "starts_with"
	OpEndsWith    Operator = "ends_with"
	OpContains    Operator = "contains"
	OpMatches     Operator = "matches"    // regex
	OpIsEmpty     Operator = "is_empty"   // unary; expected ignored
	OpIsPresent   Operator = "is_present" // unary; expected ignored
)

// Compare applies op to actual, comparing against expected where
// applicable. Returns true when the condition holds, false when it
// doesn't. Unknown operators or type-incompatible arguments return
// an error rather than silently mis-matching.
//
// Type handling is deliberately loose to match how rule parameters
// arrive: JSON numbers become float64, strings stay strings,
// arrays become []interface{}. Callers don't pre-normalize.
func Compare(op Operator, actual, expected interface{}) (bool, error) {
	switch op {
	case OpIsEmpty:
		return isEmpty(actual), nil
	case OpIsPresent:
		return !isEmpty(actual), nil
	case OpEquals:
		return equalValues(actual, expected), nil
	case OpNotEquals:
		return !equalValues(actual, expected), nil
	case OpGreaterThan:
		return compareNumeric(actual, expected, func(a, b float64) bool { return a > b })
	case OpLessThan:
		return compareNumeric(actual, expected, func(a, b float64) bool { return a < b })
	case OpInSet:
		return inSet(actual, expected)
	case OpNotInSet:
		matched, err := inSet(actual, expected)
		return !matched, err
	case OpStartsWith:
		return stringOp(actual, expected, strings.HasPrefix), nil
	case OpEndsWith:
		return stringOp(actual, expected, strings.HasSuffix), nil
	case OpContains:
		return stringOp(actual, expected, strings.Contains), nil
	case OpMatches:
		pattern := toString(expected)
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Errorf("operator %q: bad regex %q: %w", op, pattern, err)
		}
		return re.MatchString(toString(actual)), nil
	}
	return false, fmt.Errorf("unknown operator %q", op)
}

// isEmpty treats nil, empty string, and empty slice as empty.
// Everything else is present.
func isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	switch t := v.(type) {
	case string:
		return t == ""
	case []interface{}:
		return len(t) == 0
	}
	return false
}

// equalValues compares two values via string coercion so a JSON
// number 4 and a database integer 4 count as equal. Sufficient
// for value comparisons rule parameters typically drive; callers
// needing strict type equality use Go's == directly.
func equalValues(a, b interface{}) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return toString(a) == toString(b)
}

// compareNumeric coerces both sides to float64 and applies cmp.
// Returns an error when either side isn't numeric.
func compareNumeric(a, b interface{}, cmp func(float64, float64) bool) (bool, error) {
	af, ok := toFloat64(a)
	if !ok {
		return false, fmt.Errorf("value %v not numeric", a)
	}
	bf, ok := toFloat64(b)
	if !ok {
		return false, fmt.Errorf("value %v not numeric", b)
	}
	return cmp(af, bf), nil
}

// inSet checks whether actual matches any element of expected,
// which must be a slice. String coercion so numeric elements
// match numeric actuals cleanly.
func inSet(actual, expected interface{}) (bool, error) {
	needle := toString(actual)
	switch t := expected.(type) {
	case []interface{}:
		for _, e := range t {
			if toString(e) == needle {
				return true, nil
			}
		}
		return false, nil
	case []string:
		for _, e := range t {
			if e == needle {
				return true, nil
			}
		}
		return false, nil
	}
	return false, fmt.Errorf("in_set expected an array, got %T", expected)
}

// stringOp coerces both sides to string and applies op.
func stringOp(a, b interface{}, op func(string, string) bool) bool {
	return op(toString(a), toString(b))
}

// toString coerces any value to a canonical string representation
// via fmt. Nil becomes "".
func toString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}
