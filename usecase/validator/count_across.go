package validator

import (
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// CountAcrossValidator counts rows in a target table whose columns
// match named fields on the record being validated, then range-checks
// the count against an expected {min, max} envelope. Generic
// mechanism — no schema-specific knowledge in the Go code; the rule's
// parameters carry the target table, the match predicates, and the
// expected bounds.
//
// Parameters:
//
//	table                  string  (required) — target table name
//	match                  list    (required) — list of {target, self}
//	                                 pairs. `target` is a column on the
//	                                 target table; `self` is a field on
//	                                 the record being validated to
//	                                 supply the compared value.
//	exclude_self           bool    (default false) — when target table
//	                                 equals the current table, exclude
//	                                 the current row from the count via
//	                                 its primary key. Ignored for
//	                                 cross-table lookups.
//	expected               object  (required) — {min?: int, max?: int}
//	                                 bounds. At least one of min or max
//	                                 must be set. Both are inclusive.
//	skip_if_any_self_empty bool    (default true) — pass silently when
//	                                 any self field is empty/nil. Turn
//	                                 off to make emptiness a match
//	                                 predicate.
//
// Common uses:
//
//   - uniqueness: `expected: {max: 0}` with `exclude_self: true`
//   - required-related: `expected: {min: 1}` for "at least one row exists"
//   - exactly-one: `expected: {min: 1, max: 1}`
type CountAcrossValidator struct {
	pkProvider joins.PrimaryKeyProvider
}

// NewCountAcrossValidator builds a validator wired to a PK provider
// (usually the consumer's SchemaMapper). The provider is only
// consulted when a rule opts into exclude_self; a nil provider still
// works for cross-table rules but panics if exclude_self is used.
func NewCountAcrossValidator(pkProvider joins.PrimaryKeyProvider) *CountAcrossValidator {
	return &CountAcrossValidator{pkProvider: pkProvider}
}

func (v *CountAcrossValidator) Name() string { return "count_across" }

func (v *CountAcrossValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	params, err := parseCountParams(rule.Parameters)
	if err != nil {
		return nil, fmt.Errorf("count_across: %w", err)
	}

	skipIfSelfEmpty := true
	if b, ok := rule.Parameters["skip_if_any_self_empty"].(bool); ok {
		skipIfSelfEmpty = b
	}

	args := make([]interface{}, 0, len(params.matches)+1)
	for _, m := range params.matches {
		val, exists := ctx.Record[m.self]
		if !exists {
			val = nil
		}
		if skipIfSelfEmpty && isEmpty(val) {
			result.Passed = true
			result.Message = fmt.Sprintf("self field %q empty; skipped per skip_if_any_self_empty", m.self)
			return result, nil
		}
		args = append(args, val)
	}

	sameTable := params.table == ctx.TableName
	applyExcludeSelf := params.excludeSelf && sameTable
	pkColumn := ""
	if applyExcludeSelf {
		if v.pkProvider == nil {
			return nil, fmt.Errorf("count_across: exclude_self requires a PrimaryKeyProvider on the validator")
		}
		pkColumn = v.pkProvider.GetPrimaryKeyField(params.table)
	}

	query, err := buildCountQuery(params, applyExcludeSelf, pkColumn)
	if err != nil {
		return nil, fmt.Errorf("count_across: %w", err)
	}
	if applyExcludeSelf {
		args = append(args, ctx.RecordID)
	}

	var count int
	if err := ctx.DB.QueryRowContext(ctx.Ctx, query, args...).Scan(&count); err != nil {
		return nil, fmt.Errorf("count_across: query: %w", err)
	}

	within := true
	if params.expected.hasMin && count < params.expected.min {
		within = false
	}
	if params.expected.hasMax && count > params.expected.max {
		within = false
	}
	result.ActualValue = count
	result.ExpectedValue = formatExpected(params.expected)
	if within {
		result.Passed = true
		result.Message = fmt.Sprintf("count %d within expected %s", count, result.ExpectedValue)
		return result, nil
	}
	result.Passed = false
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf("expected count %s, got %d", result.ExpectedValue, count)
	}
	return result, nil
}

func (v *CountAcrossValidator) CanAutoFix() bool { return false }
func (v *CountAcrossValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

// Neighborhood implements NeighborhoodProvider. Returns records in
// the target table that share this rule's match predicates with the
// current record. Same-table rules exclude the current record by PK.
// Returns nil when the rule would skip on this record (any self
// field empty and skip_if_any_self_empty is true).
func (v *CountAcrossValidator) Neighborhood(ctx *domain.ValidationContext, rule *domain.Rule) ([]NeighborRef, error) {
	params, err := parseCountParams(rule.Parameters)
	if err != nil {
		return nil, fmt.Errorf("count_across neighborhood: %w", err)
	}

	skipIfSelfEmpty := true
	if b, ok := rule.Parameters["skip_if_any_self_empty"].(bool); ok {
		skipIfSelfEmpty = b
	}

	args := make([]interface{}, 0, len(params.matches)+1)
	for _, m := range params.matches {
		val, exists := ctx.Record[m.self]
		if !exists {
			val = nil
		}
		if skipIfSelfEmpty && isEmpty(val) {
			return nil, nil
		}
		args = append(args, val)
	}

	sameTable := params.table == ctx.TableName
	applyExcludeSelf := params.excludeSelf && sameTable

	// Neighborhood queries always need the target's PK column (to
	// SELECT the id), regardless of exclude_self.
	if v.pkProvider == nil {
		return nil, fmt.Errorf("count_across neighborhood: pkProvider required")
	}
	pkColumn := v.pkProvider.GetPrimaryKeyField(params.table)
	if pkColumn == "" {
		return nil, fmt.Errorf("count_across neighborhood: empty pk column for table %q", params.table)
	}

	query, err := buildNeighborhoodQuery(params, applyExcludeSelf, pkColumn)
	if err != nil {
		return nil, fmt.Errorf("count_across neighborhood: %w", err)
	}
	if applyExcludeSelf {
		args = append(args, ctx.RecordID)
	}

	rows, err := ctx.DB.QueryContext(ctx.Ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("count_across neighborhood: query: %w", err)
	}
	defer rows.Close()

	var out []NeighborRef
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("count_across neighborhood: scan: %w", err)
		}
		out = append(out, NeighborRef{TableName: params.table, RecordID: id})
	}
	return out, rows.Err()
}

// buildNeighborhoodQuery renders the "which other rows match" query
// used by Neighborhood. Structure mirrors buildCountQuery except
// SELECT returns the target's PK column, always. Identifier safety
// re-uses safeIdent.
func buildNeighborhoodQuery(params *countParams, applyExcludeSelf bool, pkColumn string) (string, error) {
	if err := safeIdent(params.table); err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	if err := safeIdent(pkColumn); err != nil {
		return "", fmt.Errorf("pk column: %w", err)
	}
	for i, m := range params.matches {
		if err := safeIdent(m.target); err != nil {
			return "", fmt.Errorf("match[%d].target: %w", i, err)
		}
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdent(params.table))
	sb.WriteString(" WHERE ")
	for i, m := range params.matches {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(quoteIdent(m.target))
		sb.WriteString(" = ?")
	}
	if applyExcludeSelf {
		sb.WriteString(" AND ")
		sb.WriteString(quoteIdent(pkColumn))
		sb.WriteString(" != ?")
	}
	return sb.String(), nil
}

type matchPair struct {
	target string // column on the target table
	self   string // field name on the record being validated
}

type countBounds struct {
	min    int
	max    int
	hasMin bool
	hasMax bool
}

type countParams struct {
	table       string
	matches     []matchPair
	excludeSelf bool
	expected    countBounds
}

func parseCountParams(p map[string]interface{}) (*countParams, error) {
	table, _ := p["table"].(string)
	if table == "" {
		return nil, fmt.Errorf("'table' required")
	}

	matchRaw, ok := p["match"].([]interface{})
	if !ok || len(matchRaw) == 0 {
		return nil, fmt.Errorf("'match' must be a non-empty list")
	}
	matches := make([]matchPair, 0, len(matchRaw))
	for i, m := range matchRaw {
		mMap, ok := m.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("match[%d] must be an object", i)
		}
		tgt, _ := mMap["target"].(string)
		slf, _ := mMap["self"].(string)
		if tgt == "" || slf == "" {
			return nil, fmt.Errorf("match[%d] requires non-empty 'target' and 'self'", i)
		}
		matches = append(matches, matchPair{target: tgt, self: slf})
	}

	excludeSelf, _ := p["exclude_self"].(bool)

	exp, _ := p["expected"].(map[string]interface{})
	if exp == nil {
		return nil, fmt.Errorf("'expected' required (with min and/or max)")
	}
	var bounds countBounds
	if v, ok := exp["min"]; ok && v != nil {
		n, ok := toCountInt(v)
		if !ok {
			return nil, fmt.Errorf("expected.min must be an integer")
		}
		bounds.min = n
		bounds.hasMin = true
	}
	if v, ok := exp["max"]; ok && v != nil {
		n, ok := toCountInt(v)
		if !ok {
			return nil, fmt.Errorf("expected.max must be an integer")
		}
		bounds.max = n
		bounds.hasMax = true
	}
	if !bounds.hasMin && !bounds.hasMax {
		return nil, fmt.Errorf("'expected' must set min and/or max")
	}
	return &countParams{
		table:       table,
		matches:     matches,
		excludeSelf: excludeSelf,
		expected:    bounds,
	}, nil
}

// buildCountQuery renders parameters + resolved metadata into a
// parameterized COUNT(*) query. Only column/table identifiers land in
// the SQL string; every value is bound as a parameter. Identifier
// safety uses the same allow-list as joins/resolver.go (duplicated to
// keep the packages decoupled — a shared helper pkg is worth adding
// once a third caller wants these).
func buildCountQuery(params *countParams, applyExcludeSelf bool, pkColumn string) (string, error) {
	if err := safeIdent(params.table); err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	for i, m := range params.matches {
		if err := safeIdent(m.target); err != nil {
			return "", fmt.Errorf("match[%d].target: %w", i, err)
		}
	}
	if applyExcludeSelf {
		if pkColumn == "" {
			return "", fmt.Errorf("exclude_self requires a non-empty primary-key column for table %q", params.table)
		}
		if err := safeIdent(pkColumn); err != nil {
			return "", fmt.Errorf("pk column: %w", err)
		}
	}

	var sb strings.Builder
	sb.WriteString("SELECT COUNT(*) FROM ")
	sb.WriteString(quoteIdent(params.table))
	sb.WriteString(" WHERE ")
	for i, m := range params.matches {
		if i > 0 {
			sb.WriteString(" AND ")
		}
		sb.WriteString(quoteIdent(m.target))
		sb.WriteString(" = ?")
	}
	if applyExcludeSelf {
		sb.WriteString(" AND ")
		sb.WriteString(quoteIdent(pkColumn))
		sb.WriteString(" != ?")
	}
	return sb.String(), nil
}

func formatExpected(b countBounds) string {
	switch {
	case b.hasMin && b.hasMax && b.min == b.max:
		return fmt.Sprintf("exactly %d", b.min)
	case b.hasMin && b.hasMax:
		return fmt.Sprintf("between %d and %d", b.min, b.max)
	case b.hasMin:
		return fmt.Sprintf("at least %d", b.min)
	case b.hasMax:
		return fmt.Sprintf("at most %d", b.max)
	}
	return "unspecified"
}

func toCountInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int32:
		return int(n), true
	case int64:
		return int(n), true
	case float32:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// safeIdent + quoteIdent mirror the helpers in joins/resolver.go.
// Duplicated here to avoid growing the joins package's public surface
// (or forcing an internal shared pkg) for two callers. Extract if a
// third caller wants them.
func safeIdent(s string) error {
	if s == "" {
		return fmt.Errorf("identifier empty")
	}
	for _, r := range s {
		if r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			continue
		}
		return fmt.Errorf("identifier %q contains unsafe character %q", s, r)
	}
	return nil
}

func quoteIdent(s string) string {
	return `"` + s + `"`
}
