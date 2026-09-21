package validator

import (
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// AncestorExistsValidator walks a self-referencing parent chain
// from the current record and passes when at least one ancestor
// row satisfies the `match` predicate. Fails when the walk
// terminates (nil/empty parent) or hits max_depth without any
// match.
//
// Handles the "must have an ancestor of rank X" family — for a
// subspecies looking for a genus ancestor, the walk visits
// species → subgenus → genus and matches on the third hop; for a
// root record with no parent, the chain is empty and the rule
// fires (which is what we want: orphaned taxa are the target).
// Condition-gate rules so they only fire on records where the
// check is meaningful (e.g. "only for species-group names").
//
// Parameters:
//
//	parent_column string (required) — column holding parent PK.
//	match         object (required) — {join?, field, operator, value}
//	                                   where operator is equals |
//	                                   not_equals | in | not_in.
//	                                   Without `join`, field is read
//	                                   from the ancestor row itself.
//	                                   With `join`, field is read
//	                                   from a joined row via one
//	                                   plain equijoin — used when the
//	                                   value to match lives on a
//	                                   related table (e.g. taxon
//	                                   ancestors carry their rank on
//	                                   the associated name row).
//	                                   Shape:
//	                                   {"from": "<ancestor_col>",
//	                                    "to":   "<table>.<pk_col>"}.
//	max_depth     int    (optional, default 100) — safety cap on
//	                                   walk length.
type AncestorExistsValidator struct {
	pkProvider joins.PrimaryKeyProvider
}

// NewAncestorExistsValidator builds a validator wired to a PK
// provider (usually the consumer's SchemaMapper). Panics on nil
// provider.
func NewAncestorExistsValidator(pkProvider joins.PrimaryKeyProvider) *AncestorExistsValidator {
	if pkProvider == nil {
		panic("validator.NewAncestorExistsValidator: pkProvider must not be nil")
	}
	return &AncestorExistsValidator{pkProvider: pkProvider}
}

func (v *AncestorExistsValidator) Name() string { return "ancestor_exists" }

func (v *AncestorExistsValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	params, err := parseAncestorParams(rule.Parameters)
	if err != nil {
		return nil, fmt.Errorf("ancestor_exists: %w", err)
	}
	pkCol := v.pkProvider.GetPrimaryKeyField(ctx.TableName)
	query, matchArgs, err := buildAncestorExistsQuery(params, ctx.TableName, pkCol)
	if err != nil {
		return nil, fmt.Errorf("ancestor_exists: %w", err)
	}

	args := make([]interface{}, 0, 2+len(matchArgs))
	args = append(args, ctx.RecordID, params.maxDepth)
	args = append(args, matchArgs...)

	var count int
	if err := ctx.DB.QueryRowContext(ctx.Ctx, query, args...).Scan(&count); err != nil {
		return nil, fmt.Errorf("ancestor_exists: query: %w", err)
	}
	if count > 0 {
		result.Passed = true
		result.Message = "Matching ancestor found"
		return result, nil
	}
	result.Passed = false
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = "No ancestor matches the required predicate"
	}
	return result, nil
}

func (v *AncestorExistsValidator) CanAutoFix() bool { return false }
func (v *AncestorExistsValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

type ancestorMatch struct {
	join     *ancestorJoin // optional
	field    string
	operator string
	value    interface{}
}

type ancestorJoin struct {
	fromColumn string // column on the ancestor row
	toTable    string // table to join into
	toColumn   string // column on the joined table (usually its PK)
}

type ancestorParams struct {
	parentColumn string
	match        ancestorMatch
	maxDepth     int
}

func parseAncestorParams(p map[string]interface{}) (*ancestorParams, error) {
	pc, _ := p["parent_column"].(string)
	if pc == "" {
		return nil, fmt.Errorf("'parent_column' required")
	}
	matchRaw, ok := p["match"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'match' must be an object")
	}
	field, _ := matchRaw["field"].(string)
	op, _ := matchRaw["operator"].(string)
	if field == "" || op == "" {
		return nil, fmt.Errorf("match requires 'field' and 'operator'")
	}
	val := matchRaw["value"]

	var join *ancestorJoin
	if joinRaw, ok := matchRaw["join"].(map[string]interface{}); ok {
		from, _ := joinRaw["from"].(string)
		to, _ := joinRaw["to"].(string)
		if from == "" || to == "" {
			return nil, fmt.Errorf("match.join requires 'from' and 'to'")
		}
		parts := strings.SplitN(to, ".", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("match.join.to must be 'table.column'")
		}
		join = &ancestorJoin{fromColumn: from, toTable: parts[0], toColumn: parts[1]}
	}

	maxDepth := 100
	if d, ok := p["max_depth"].(float64); ok && d > 0 {
		maxDepth = int(d)
	} else if d, ok := p["max_depth"].(int); ok && d > 0 {
		maxDepth = d
	}
	return &ancestorParams{
		parentColumn: pc,
		match:        ancestorMatch{join: join, field: field, operator: op, value: val},
		maxDepth:     maxDepth,
	}, nil
}

// buildAncestorExistsQuery renders the recursive-CTE + match-filter
// query. Only equals, not_equals, in, not_in are supported for the
// match operator in v1 — the rank-matching use cases from the
// surveys are all coverable within that set. Returns the SQL plus
// the ordered bind values for the match predicate.
//
// Query shape without a join (all identifiers safely quoted):
//
//	WITH RECURSIVE chain(id, depth) AS (
//	  SELECT parent_col, 1 FROM table WHERE pk_col = ?
//	  UNION
//	  SELECT table.parent_col, chain.depth + 1
//	    FROM table JOIN chain ON table.pk_col = chain.id
//	    WHERE chain.depth < ?
//	)
//	SELECT COUNT(*) FROM chain c
//	JOIN table t ON t.pk_col = c.id
//	WHERE <match predicate on t>
//
// With a join, one more JOIN is added and the match runs against
// alias `j`:
//
//	... JOIN <join.toTable> j ON j.<join.toColumn> = t.<join.fromColumn>
//	WHERE <match predicate on j>
//
// Bind order: [recordID, maxDepth, ...matchArgs].
func buildAncestorExistsQuery(params *ancestorParams, tableName, pkColumn string) (string, []interface{}, error) {
	if err := safeIdent(tableName); err != nil {
		return "", nil, fmt.Errorf("table: %w", err)
	}
	if err := safeIdent(pkColumn); err != nil {
		return "", nil, fmt.Errorf("pk column: %w", err)
	}
	if err := safeIdent(params.parentColumn); err != nil {
		return "", nil, fmt.Errorf("parent column: %w", err)
	}
	if err := safeIdent(params.match.field); err != nil {
		return "", nil, fmt.Errorf("match.field: %w", err)
	}
	matchAlias := "t"
	if params.match.join != nil {
		j := params.match.join
		if err := safeIdent(j.fromColumn); err != nil {
			return "", nil, fmt.Errorf("match.join.from: %w", err)
		}
		if err := safeIdent(j.toTable); err != nil {
			return "", nil, fmt.Errorf("match.join.to table: %w", err)
		}
		if err := safeIdent(j.toColumn); err != nil {
			return "", nil, fmt.Errorf("match.join.to column: %w", err)
		}
		matchAlias = "j"
	}

	matchSQL, matchArgs, err := renderAncestorMatch(params.match, matchAlias)
	if err != nil {
		return "", nil, err
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
	sb.WriteString("SELECT COUNT(*) FROM chain c JOIN ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" t ON t.")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" = c.id")
	if params.match.join != nil {
		j := params.match.join
		sb.WriteString(" JOIN ")
		sb.WriteString(quoteIdent(j.toTable))
		sb.WriteString(" j ON j.")
		sb.WriteString(quoteIdent(j.toColumn))
		sb.WriteString(" = t.")
		sb.WriteString(quoteIdent(j.fromColumn))
	}
	sb.WriteString(" WHERE ")
	sb.WriteString(matchSQL)
	return sb.String(), matchArgs, nil
}

// renderAncestorMatch turns a match predicate into a SQL fragment
// against the given alias plus the ordered bind values. Column
// name is already safeIdent-checked by the caller.
func renderAncestorMatch(m ancestorMatch, alias string) (string, []interface{}, error) {
	col := alias + "." + quoteIdent(m.field)
	switch m.operator {
	case "equals":
		return col + " = ?", []interface{}{m.value}, nil
	case "not_equals":
		return col + " != ?", []interface{}{m.value}, nil
	case "in", "not_in":
		list, ok := toInterfaceSlice(m.value)
		if !ok || len(list) == 0 {
			return "", nil, fmt.Errorf("operator %q requires a non-empty list value", m.operator)
		}
		placeholders := strings.Repeat("?,", len(list))
		placeholders = placeholders[:len(placeholders)-1]
		verb := "IN"
		if m.operator == "not_in" {
			verb = "NOT IN"
		}
		return col + " " + verb + " (" + placeholders + ")", list, nil
	}
	return "", nil, fmt.Errorf("unsupported match operator %q (expected equals|not_equals|in|not_in)", m.operator)
}

func toInterfaceSlice(v interface{}) ([]interface{}, bool) {
	switch s := v.(type) {
	case []interface{}:
		return s, true
	case []string:
		out := make([]interface{}, len(s))
		for i, x := range s {
			out[i] = x
		}
		return out, true
	}
	return nil, false
}
