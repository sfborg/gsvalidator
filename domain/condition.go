package domain

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"strings"
)

// Condition gates a rule's applicability. A rule with N conditions
// applies only when all N are satisfied. Empty condition list means
// the rule always applies (to every record of its target table).
//
// A condition normally checks a field on the record being
// validated. When Relation is set, the condition instead checks a
// field on the record reached via that named relation from the
// bundle's Relations catalog. This lets a rule scoped on one table
// gate on fields belonging to a related table (e.g. a
// taxon-scoped rule that only fires when the owning name has a
// specific code).
type Condition struct {
	Field    string      `json:"field"`
	Operator string      `json:"operator"` // equals, not_equals, in, not_in, exists, not_exists, greater_than, less_than, contains, matches
	Value    interface{} `json:"value"`
	Relation string      `json:"relation,omitempty"` // optional named relation; empty = check the record's own field
}

// ConditionResolver looks up a related record by name. Supplied by
// the consumer (typically wrapping joins.RelationResolver) so
// condition.go stays free of any cross-table SQL specifics.
type ConditionResolver interface {
	LookupRelated(ctx context.Context, db *sql.DB, sourceTable, sourceID, relation string) (map[string]interface{}, error)
}

// Eval evaluates the condition against the current
// ValidationContext. When c.Relation is empty, reads the field
// from vctx.Record. Otherwise it resolves the relation via
// vctx.RelationResolver and reads the field from the related
// record. Missing resolver on a relation-scoped condition is an
// error; missing related row is treated as field-not-present.
//
// c.Value may be a literal (string, number, list, …) or a
// same-record self-reference in the shape {"field": "other_col"}.
// Self-references always resolve against vctx.Record — even when
// c.Relation is set — so rules can compare a related row's field
// against a field on the current record (or against a fixed value).
func (c *Condition) Eval(vctx *ValidationContext) (bool, error) {
	comparisonValue := resolveConditionValue(c.Value, vctx.Record)
	if c.Relation == "" {
		fieldValue, exists := vctx.Record[c.Field]
		return c.evaluate(fieldValue, exists, comparisonValue), nil
	}
	if vctx.RelationResolver == nil {
		return false, fmt.Errorf("condition on relation %q requires a resolver on the ValidationContext", c.Relation)
	}
	related, err := vctx.RelationResolver.LookupRelated(vctx.Ctx, vctx.DB, vctx.TableName, vctx.RecordID, c.Relation)
	if err != nil {
		return false, fmt.Errorf("condition relation %q: %w", c.Relation, err)
	}
	if related == nil {
		// No related row — treat as field-not-present for this
		// condition's evaluation.
		return c.evaluate(nil, false, comparisonValue), nil
	}
	fieldValue, exists := related[c.Field]
	return c.evaluate(fieldValue, exists, comparisonValue), nil
}

// resolveConditionValue expands a same-record self-reference of
// the shape {"field": "col_name"} into the record's value for
// that column. A missing column resolves to nil (comparators
// handle nil per their normal rules). Any other shape — literal
// scalar, list, or a map without a single "field" key — passes
// through unchanged so authored rules that happen to carry a
// map-valued literal aren't misinterpreted.
func resolveConditionValue(v interface{}, record map[string]interface{}) interface{} {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) != 1 {
		return v
	}
	name, ok := m["field"].(string)
	if !ok || name == "" {
		return v
	}
	return record[name]
}

// evaluate is the operator dispatch. Takes a resolved
// (fieldValue, exists) pair plus the comparison value (which may
// itself have been resolved from a self-reference sigil) —
// decouples operator handling from where the operands came from.
func (c *Condition) evaluate(fieldValue interface{}, exists bool, comparisonValue interface{}) bool {
	switch c.Operator {
	case "exists":
		return exists && fieldValue != nil

	case "not_exists":
		return !exists || fieldValue == nil

	case "equals":
		if !exists {
			return false
		}
		return c.compareValues(fieldValue, comparisonValue, "==")

	case "not_equals":
		if !exists {
			return true
		}
		return c.compareValues(fieldValue, comparisonValue, "!=")

	case "in":
		if !exists {
			return false
		}
		return c.inSlice(fieldValue, comparisonValue)

	case "not_in":
		if !exists {
			return true
		}
		return !c.inSlice(fieldValue, comparisonValue)

	case "greater_than":
		if !exists {
			return false
		}
		return c.compareNumeric(fieldValue, comparisonValue, ">")

	case "less_than":
		if !exists {
			return false
		}
		return c.compareNumeric(fieldValue, comparisonValue, "<")

	case "contains":
		if !exists {
			return false
		}
		return c.containsString(fieldValue, comparisonValue)

	case "matches":
		if !exists {
			return false
		}
		return c.matchesRegex(fieldValue, comparisonValue)

	default:
		return false
	}
}

// compareValues compares two values for equality or inequality.
// Numeric operands are compared by numeric value even across type
// boundaries (int64 vs float64), because JSON-authored rule
// literals arrive as float64 while SQLite scans INTEGER columns
// as int64 — a strict reflect.DeepEqual would falsely reject
// numerically-equal cross-type pairs. Non-numeric comparisons fall
// back to DeepEqual so string, list, and map equality stay strict.
func (c *Condition) compareValues(a, b interface{}, op string) bool {
	if a == nil && b == nil {
		return op == "=="
	}
	if a == nil || b == nil {
		return op == "!="
	}
	if aF, aOk := toFloat64(a); aOk {
		if bF, bOk := toFloat64(b); bOk {
			if op == "==" {
				return aF == bF
			}
			return aF != bF
		}
	}
	if op == "==" {
		return reflect.DeepEqual(a, b)
	}
	return !reflect.DeepEqual(a, b)
}

// inSlice checks if value is in the slice. Uses the same
// numeric-coercing equality as compareValues for the same reason
// (JSON int literals in the rule vs SQLite-scanned int64s).
func (c *Condition) inSlice(value, slice interface{}) bool {
	sliceVal := reflect.ValueOf(slice)
	if sliceVal.Kind() != reflect.Slice && sliceVal.Kind() != reflect.Array {
		return false
	}
	for i := 0; i < sliceVal.Len(); i++ {
		item := sliceVal.Index(i).Interface()
		if aF, aOk := toFloat64(value); aOk {
			if bF, bOk := toFloat64(item); bOk {
				if aF == bF {
					return true
				}
				continue
			}
		}
		if reflect.DeepEqual(value, item) {
			return true
		}
	}
	return false
}

// compareNumeric compares two numeric values.
func (c *Condition) compareNumeric(a, b interface{}, op string) bool {
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)

	if !aOk || !bOk {
		return false
	}

	if op == ">" {
		return aFloat > bFloat
	}
	return aFloat < bFloat
}

// containsString checks if field value contains the substring.
func (c *Condition) containsString(fieldValue, substring interface{}) bool {
	fieldStr := fmt.Sprintf("%v", fieldValue)
	subStr := fmt.Sprintf("%v", substring)
	return strings.Contains(fieldStr, subStr)
}

// matchesRegex checks if field value matches the regex pattern.
func (c *Condition) matchesRegex(fieldValue, pattern interface{}) bool {
	fieldStr := fmt.Sprintf("%v", fieldValue)
	patternStr := fmt.Sprintf("%v", pattern)

	re, err := regexp.Compile(patternStr)
	if err != nil {
		return false
	}

	return re.MatchString(fieldStr)
}

// toFloat64 converts various numeric types (and numeric-looking
// strings) to float64. Returns (0, false) when the value isn't
// convertible.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case float32:
		return float64(val), true
	case float64:
		return val, true
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
