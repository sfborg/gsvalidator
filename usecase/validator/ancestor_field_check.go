package validator

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// AncestorFieldCheckValidator walks a self-referencing parent chain
// from the current record, finds the first ancestor matching
// stop_when, then asserts a comparison between a field on the
// current record and a field on the matched ancestor. Handles the
// "compare self to ancestor at rank X" family — CoL's
// NOMINOTYPICAL_AUTHORSHIP_DIFFERS (autonym subspecies authorship
// must equal parent species authorship) and PUBLISHED_BEFORE_GENUS
// (species year must not predate parent genus year).
//
// Parameters:
//
//	parent_column string  (required) — column holding parent PK.
//	stop_when     object  (required) — predicate that identifies the
//	                                    ancestor to compare against.
//	                                    Same shape as ancestor_exists's
//	                                    match: {join?, field, operator,
//	                                    value}. Operator ∈ equals |
//	                                    not_equals | in | not_in.
//	assert        object  (required) — {self_join?, self_field,
//	                                    ancestor_join?, ancestor_field,
//	                                    operator}. Operator ∈ equals |
//	                                    not_equals | less_than |
//	                                    less_than_or_equals |
//	                                    greater_than |
//	                                    greater_than_or_equals.
//	                                    The rule PASSES when the
//	                                    comparison holds and FAILS
//	                                    otherwise.
//	require_ancestor      bool (optional, default false) — when false,
//	                                    a missing ancestor is treated
//	                                    as "rule not applicable" and
//	                                    passes silently; when true,
//	                                    missing ancestor fails.
//	skip_if_either_empty  bool (optional, default true) — when true,
//	                                    if either self or ancestor
//	                                    value is nil/empty the rule
//	                                    passes silently (matches the
//	                                    "compatible-if-known" pattern
//	                                    used by related_field_equals).
//	max_depth             int  (optional, default 100) — safety cap
//	                                    on walk length.
type AncestorFieldCheckValidator struct {
	pkProvider joins.PrimaryKeyProvider
}

// NewAncestorFieldCheckValidator builds a validator wired to a PK
// provider. Panics on nil provider.
func NewAncestorFieldCheckValidator(pkProvider joins.PrimaryKeyProvider) *AncestorFieldCheckValidator {
	if pkProvider == nil {
		panic("validator.NewAncestorFieldCheckValidator: pkProvider must not be nil")
	}
	return &AncestorFieldCheckValidator{pkProvider: pkProvider}
}

func (v *AncestorFieldCheckValidator) Name() string { return "ancestor_field_check" }

func (v *AncestorFieldCheckValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	params, err := parseAncestorFieldCheckParams(rule.Parameters)
	if err != nil {
		return nil, fmt.Errorf("ancestor_field_check: %w", err)
	}
	pkCol := v.pkProvider.GetPrimaryKeyField(ctx.TableName)

	selfQuery, err := buildSelfValueQuery(ctx.TableName, pkCol, params.assert.selfJoin, params.assert.selfField)
	if err != nil {
		return nil, fmt.Errorf("ancestor_field_check: self query: %w", err)
	}
	var selfVal interface{}
	if err := ctx.DB.QueryRowContext(ctx.Ctx, selfQuery, ctx.RecordID).Scan(&selfVal); err != nil {
		if err == sql.ErrNoRows {
			// Current record vanished mid-flight (extremely unlikely
			// inside a validation pass) — nothing to assert, pass.
			result.Passed = true
			result.Message = "Self record not found; skipping"
			return result, nil
		}
		return nil, fmt.Errorf("ancestor_field_check: load self: %w", err)
	}

	ancestorQuery, ancestorArgs, err := buildAncestorValueQuery(params, ctx.TableName, pkCol)
	if err != nil {
		return nil, fmt.Errorf("ancestor_field_check: ancestor query: %w", err)
	}
	// Bind: recordID, maxDepth, then stop_when args.
	args := make([]interface{}, 0, 2+len(ancestorArgs))
	args = append(args, ctx.RecordID, params.maxDepth)
	args = append(args, ancestorArgs...)

	var ancestorVal interface{}
	row := ctx.DB.QueryRowContext(ctx.Ctx, ancestorQuery, args...)
	if err := row.Scan(&ancestorVal); err != nil {
		if err == sql.ErrNoRows {
			// No matching ancestor. Skip unless the rule opted into
			// require_ancestor, in which case the missing ancestor is
			// itself the failure.
			if params.requireAncestor {
				result.Passed = false
				if rule.WarningMessage != "" {
					result.Message = rule.WarningMessage
				} else {
					result.Message = "No matching ancestor to compare against"
				}
				return result, nil
			}
			result.Passed = true
			result.Message = "No matching ancestor; rule not applicable"
			return result, nil
		}
		return nil, fmt.Errorf("ancestor_field_check: load ancestor: %w", err)
	}
	selfVal = coerceScannedValue(selfVal)
	ancestorVal = coerceScannedValue(ancestorVal)

	if params.skipIfEitherEmpty && (isEmpty(selfVal) || isEmpty(ancestorVal)) {
		result.Passed = true
		result.Message = "Self or ancestor value empty; rule not applicable"
		return result, nil
	}

	ok, err := compareFieldValues(selfVal, ancestorVal, params.assert.operator)
	if err != nil {
		return nil, fmt.Errorf("ancestor_field_check: compare: %w", err)
	}
	result.ActualValue = selfVal
	result.ExpectedValue = ancestorVal
	if ok {
		result.Passed = true
		result.Message = "Assertion holds"
		return result, nil
	}
	result.Passed = false
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf("Self value %v does not satisfy %s vs ancestor value %v",
			selfVal, params.assert.operator, ancestorVal)
	}
	return result, nil
}

func (v *AncestorFieldCheckValidator) CanAutoFix() bool { return false }
func (v *AncestorFieldCheckValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

type ancestorAssert struct {
	selfJoin      *ancestorJoin // optional
	selfField     string
	ancestorJoin  *ancestorJoin // optional
	ancestorField string
	operator      string
}

type ancestorFieldCheckParams struct {
	parentColumn      string
	stopWhen          ancestorMatch
	assert            ancestorAssert
	requireAncestor   bool
	skipIfEitherEmpty bool
	maxDepth          int
}

func parseAncestorFieldCheckParams(p map[string]interface{}) (*ancestorFieldCheckParams, error) {
	pc, _ := p["parent_column"].(string)
	if pc == "" {
		return nil, fmt.Errorf("'parent_column' required")
	}

	stopRaw, ok := p["stop_when"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'stop_when' must be an object")
	}
	stopField, _ := stopRaw["field"].(string)
	stopOp, _ := stopRaw["operator"].(string)
	if stopField == "" || stopOp == "" {
		return nil, fmt.Errorf("stop_when requires 'field' and 'operator'")
	}
	stopVal := stopRaw["value"]
	var stopJoin *ancestorJoin
	if joinRaw, ok := stopRaw["join"].(map[string]interface{}); ok {
		j, err := parseAncestorJoin(joinRaw)
		if err != nil {
			return nil, fmt.Errorf("stop_when.join: %w", err)
		}
		stopJoin = j
	}

	assertRaw, ok := p["assert"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'assert' must be an object")
	}
	selfField, _ := assertRaw["self_field"].(string)
	ancestorField, _ := assertRaw["ancestor_field"].(string)
	assertOp, _ := assertRaw["operator"].(string)
	if selfField == "" || ancestorField == "" || assertOp == "" {
		return nil, fmt.Errorf("assert requires 'self_field', 'ancestor_field', and 'operator'")
	}
	var selfJoin, ancJoin *ancestorJoin
	if joinRaw, ok := assertRaw["self_join"].(map[string]interface{}); ok {
		j, err := parseAncestorJoin(joinRaw)
		if err != nil {
			return nil, fmt.Errorf("assert.self_join: %w", err)
		}
		selfJoin = j
	}
	if joinRaw, ok := assertRaw["ancestor_join"].(map[string]interface{}); ok {
		j, err := parseAncestorJoin(joinRaw)
		if err != nil {
			return nil, fmt.Errorf("assert.ancestor_join: %w", err)
		}
		ancJoin = j
	}

	requireAncestor, _ := p["require_ancestor"].(bool)
	skipIfEitherEmpty := true
	if b, ok := p["skip_if_either_empty"].(bool); ok {
		skipIfEitherEmpty = b
	}
	maxDepth := 100
	if d, ok := p["max_depth"].(float64); ok && d > 0 {
		maxDepth = int(d)
	} else if d, ok := p["max_depth"].(int); ok && d > 0 {
		maxDepth = d
	}

	return &ancestorFieldCheckParams{
		parentColumn: pc,
		stopWhen: ancestorMatch{
			join: stopJoin, field: stopField, operator: stopOp, value: stopVal,
		},
		assert: ancestorAssert{
			selfJoin: selfJoin, selfField: selfField,
			ancestorJoin: ancJoin, ancestorField: ancestorField,
			operator: assertOp,
		},
		requireAncestor:   requireAncestor,
		skipIfEitherEmpty: skipIfEitherEmpty,
		maxDepth:          maxDepth,
	}, nil
}

func parseAncestorJoin(m map[string]interface{}) (*ancestorJoin, error) {
	from, _ := m["from"].(string)
	to, _ := m["to"].(string)
	if from == "" || to == "" {
		return nil, fmt.Errorf("join requires 'from' and 'to'")
	}
	parts := strings.SplitN(to, ".", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("join.to must be 'table.column'")
	}
	return &ancestorJoin{fromColumn: from, toTable: parts[0], toColumn: parts[1]}, nil
}

// buildSelfValueQuery loads the self-side value for the assertion.
// Simple SELECT with an optional single equijoin.
func buildSelfValueQuery(tableName, pkColumn string, selfJoin *ancestorJoin, selfField string) (string, error) {
	if err := safeIdent(tableName); err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	if err := safeIdent(pkColumn); err != nil {
		return "", fmt.Errorf("pk column: %w", err)
	}
	if err := safeIdent(selfField); err != nil {
		return "", fmt.Errorf("self_field: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("SELECT ")
	if selfJoin != nil {
		if err := safeIdent(selfJoin.fromColumn); err != nil {
			return "", fmt.Errorf("self_join.from: %w", err)
		}
		if err := safeIdent(selfJoin.toTable); err != nil {
			return "", fmt.Errorf("self_join.to table: %w", err)
		}
		if err := safeIdent(selfJoin.toColumn); err != nil {
			return "", fmt.Errorf("self_join.to column: %w", err)
		}
		sb.WriteString(`sj.`)
		sb.WriteString(quoteIdent(selfField))
		sb.WriteString(" FROM ")
		sb.WriteString(quoteIdent(tableName))
		sb.WriteString(" t JOIN ")
		sb.WriteString(quoteIdent(selfJoin.toTable))
		sb.WriteString(" sj ON sj.")
		sb.WriteString(quoteIdent(selfJoin.toColumn))
		sb.WriteString(" = t.")
		sb.WriteString(quoteIdent(selfJoin.fromColumn))
	} else {
		sb.WriteString("t.")
		sb.WriteString(quoteIdent(selfField))
		sb.WriteString(" FROM ")
		sb.WriteString(quoteIdent(tableName))
		sb.WriteString(" t")
	}
	sb.WriteString(" WHERE t.")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" = ?")
	return sb.String(), nil
}

// buildAncestorValueQuery loads the ancestor-side value for the
// assertion, restricted to the first ancestor matching stop_when
// (ordered by walk depth). Returns SQL + the stop_when bind values.
func buildAncestorValueQuery(params *ancestorFieldCheckParams, tableName, pkColumn string) (string, []interface{}, error) {
	if err := safeIdent(tableName); err != nil {
		return "", nil, fmt.Errorf("table: %w", err)
	}
	if err := safeIdent(pkColumn); err != nil {
		return "", nil, fmt.Errorf("pk column: %w", err)
	}
	if err := safeIdent(params.parentColumn); err != nil {
		return "", nil, fmt.Errorf("parent column: %w", err)
	}
	if err := safeIdent(params.stopWhen.field); err != nil {
		return "", nil, fmt.Errorf("stop_when.field: %w", err)
	}
	if err := safeIdent(params.assert.ancestorField); err != nil {
		return "", nil, fmt.Errorf("assert.ancestor_field: %w", err)
	}

	// Stop-when predicate rendered against `sj` when stop_when.join is set,
	// otherwise against `a`.
	stopAlias := "a"
	if params.stopWhen.join != nil {
		j := params.stopWhen.join
		if err := safeIdent(j.fromColumn); err != nil {
			return "", nil, fmt.Errorf("stop_when.join.from: %w", err)
		}
		if err := safeIdent(j.toTable); err != nil {
			return "", nil, fmt.Errorf("stop_when.join.to table: %w", err)
		}
		if err := safeIdent(j.toColumn); err != nil {
			return "", nil, fmt.Errorf("stop_when.join.to column: %w", err)
		}
		stopAlias = "sj"
	}
	stopSQL, stopArgs, err := renderAncestorMatch(params.stopWhen, stopAlias)
	if err != nil {
		return "", nil, err
	}

	// SELECT column: ancestor-join alias `aj` when assert.ancestor_join
	// is set, otherwise the ancestor row alias `a`.
	selectAlias := "a"
	if params.assert.ancestorJoin != nil {
		j := params.assert.ancestorJoin
		if err := safeIdent(j.fromColumn); err != nil {
			return "", nil, fmt.Errorf("assert.ancestor_join.from: %w", err)
		}
		if err := safeIdent(j.toTable); err != nil {
			return "", nil, fmt.Errorf("assert.ancestor_join.to table: %w", err)
		}
		if err := safeIdent(j.toColumn); err != nil {
			return "", nil, fmt.Errorf("assert.ancestor_join.to column: %w", err)
		}
		selectAlias = "aj"
	}

	var sb strings.Builder
	sb.WriteString("WITH RECURSIVE chain(id, depth) AS (")
	sb.WriteString("SELECT ")
	sb.WriteString(quoteIdent(params.parentColumn))
	sb.WriteString(", 1 FROM ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" WHERE ")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" = ? UNION SELECT ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(params.parentColumn))
	sb.WriteString(", chain.depth + 1 FROM ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" JOIN chain ON ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" = chain.id WHERE chain.depth < ?) ")
	sb.WriteString("SELECT ")
	sb.WriteString(selectAlias)
	sb.WriteString(".")
	sb.WriteString(quoteIdent(params.assert.ancestorField))
	sb.WriteString(" FROM chain c JOIN ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" a ON a.")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" = c.id")
	if params.stopWhen.join != nil {
		j := params.stopWhen.join
		sb.WriteString(" JOIN ")
		sb.WriteString(quoteIdent(j.toTable))
		sb.WriteString(" sj ON sj.")
		sb.WriteString(quoteIdent(j.toColumn))
		sb.WriteString(" = a.")
		sb.WriteString(quoteIdent(j.fromColumn))
	}
	if params.assert.ancestorJoin != nil {
		j := params.assert.ancestorJoin
		sb.WriteString(" JOIN ")
		sb.WriteString(quoteIdent(j.toTable))
		sb.WriteString(" aj ON aj.")
		sb.WriteString(quoteIdent(j.toColumn))
		sb.WriteString(" = a.")
		sb.WriteString(quoteIdent(j.fromColumn))
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(stopSQL)
	sb.WriteString(" ORDER BY c.depth LIMIT 1")
	return sb.String(), stopArgs, nil
}

// compareFieldValues dispatches on the assertion operator. Numeric
// operators try to coerce both sides to float64; equality operators
// fall back to string comparison when the raw values don't match
// directly (SQLite returns TEXT for many types).
func compareFieldValues(a, b interface{}, op string) (bool, error) {
	switch op {
	case "equals":
		return fieldsEqual(a, b), nil
	case "not_equals":
		return !fieldsEqual(a, b), nil
	case "less_than", "less_than_or_equals", "greater_than", "greater_than_or_equals":
		af, aok := coerceFloat(a)
		bf, bok := coerceFloat(b)
		if !aok || !bok {
			return false, fmt.Errorf("operator %q requires numeric operands (got %T, %T)", op, a, b)
		}
		switch op {
		case "less_than":
			return af < bf, nil
		case "less_than_or_equals":
			return af <= bf, nil
		case "greater_than":
			return af > bf, nil
		case "greater_than_or_equals":
			return af >= bf, nil
		}
	}
	return false, fmt.Errorf("unsupported assert operator %q", op)
}

func fieldsEqual(a, b interface{}) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a == b {
		return true
	}
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

func coerceFloat(v interface{}) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		var f float64
		if _, err := fmt.Sscanf(n, "%f", &f); err == nil {
			return f, true
		}
	case []byte:
		var f float64
		if _, err := fmt.Sscanf(string(n), "%f", &f); err == nil {
			return f, true
		}
	}
	return 0, false
}

// coerceScannedValue normalises the result of a naked interface{}
// scan — SQLite's driver returns []byte for TEXT columns; converting
// to string here keeps downstream comparisons predictable.
func coerceScannedValue(v interface{}) interface{} {
	if b, ok := v.([]byte); ok {
		return string(b)
	}
	return v
}
