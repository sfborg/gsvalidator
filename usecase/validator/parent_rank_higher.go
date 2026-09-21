package validator

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// ParentRankHigherValidator asserts that the record's immediate
// parent has a taxonomically-higher rank than the record itself,
// per an ordered-list rank vocabulary that varies by nomenclatural
// code. Single-hop is sufficient — if the invariant holds pairwise
// through the whole tree, the classification is monotonic by
// induction.
//
// rank_order is a per-code map from code id to an ordered list of
// rank ids. Position in the list is the ordinal; lower index means
// higher in the tree (kingdom is 0, subspecies is far down). Same
// rank id may appear at different indexes across codes — that's the
// whole reason the vocab is per-code.
//
// Parameters:
//
//	parent_column string (required) — column on self holding the
//	                                    parent PK.
//	rank_column   string (required) — column carrying the rank id.
//	                                    Read from self and from parent
//	                                    (optionally via `join`).
//	code_column   string (required) — column carrying the code id.
//	                                    Same join semantics as rank.
//	join          object (optional) — {"from": "<col_on_taxon>", "to":
//	                                    "<table>.<pk>"}. Used when the
//	                                    rank/code fields live on a
//	                                    related row (in sfga they live
//	                                    on the name row reached via
//	                                    col__name_id).
//	rank_order    object (required) — {code_id: [rank_id, rank_id,
//	                                    ...]}. Position = ordinal.
//
// Skip conditions (all return pass):
//   - self has no parent
//   - self or parent lacks rank / code
//   - self and parent have different codes (a cross-code check is a
//     different concern; skip here)
//   - either code isn't declared in rank_order
//   - either rank isn't in the code's rank list
type ParentRankHigherValidator struct {
	pkProvider joins.PrimaryKeyProvider
}

// NewParentRankHigherValidator builds a validator over a PK
// provider. Panics on nil provider.
func NewParentRankHigherValidator(pkProvider joins.PrimaryKeyProvider) *ParentRankHigherValidator {
	if pkProvider == nil {
		panic("validator.NewParentRankHigherValidator: pkProvider must not be nil")
	}
	return &ParentRankHigherValidator{pkProvider: pkProvider}
}

func (v *ParentRankHigherValidator) Name() string { return "parent_rank_higher" }

func (v *ParentRankHigherValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	params, err := parseParentRankHigherParams(rule.Parameters)
	if err != nil {
		return nil, fmt.Errorf("parent_rank_higher: %w", err)
	}
	pkCol := v.pkProvider.GetPrimaryKeyField(ctx.TableName)

	selfQuery, err := buildRankCodeLookup(ctx.TableName, pkCol, params.join,
		params.rankColumn, params.codeColumn, false, params.parentColumn)
	if err != nil {
		return nil, fmt.Errorf("parent_rank_higher: self lookup: %w", err)
	}
	var selfRank, selfCode sql.NullString
	if err := ctx.DB.QueryRowContext(ctx.Ctx, selfQuery, ctx.RecordID).Scan(&selfRank, &selfCode); err != nil {
		if err == sql.ErrNoRows {
			result.Passed = true
			result.Message = "Self record not found; skipping"
			return result, nil
		}
		return nil, fmt.Errorf("parent_rank_higher: load self: %w", err)
	}
	if !selfRank.Valid || selfRank.String == "" || !selfCode.Valid || selfCode.String == "" {
		result.Passed = true
		result.Message = "Self rank or code empty; rule not applicable"
		return result, nil
	}

	parentQuery, err := buildRankCodeLookup(ctx.TableName, pkCol, params.join,
		params.rankColumn, params.codeColumn, true, params.parentColumn)
	if err != nil {
		return nil, fmt.Errorf("parent_rank_higher: parent lookup: %w", err)
	}
	var parentRank, parentCode sql.NullString
	err = ctx.DB.QueryRowContext(ctx.Ctx, parentQuery, ctx.RecordID).Scan(&parentRank, &parentCode)
	if err == sql.ErrNoRows {
		result.Passed = true
		result.Message = "No parent; rule not applicable"
		return result, nil
	}
	if err != nil {
		return nil, fmt.Errorf("parent_rank_higher: load parent: %w", err)
	}
	if !parentRank.Valid || parentRank.String == "" || !parentCode.Valid || parentCode.String == "" {
		result.Passed = true
		result.Message = "Parent rank or code empty; rule not applicable"
		return result, nil
	}
	if selfCode.String != parentCode.String {
		result.Passed = true
		result.Message = "Self and parent have different codes; rule not applicable"
		return result, nil
	}

	list, ok := params.rankOrder[selfCode.String]
	if !ok {
		result.Passed = true
		result.Message = fmt.Sprintf("No rank_order entry for code %q; skipping", selfCode.String)
		return result, nil
	}
	selfOrd := indexOfString(list, selfRank.String)
	parentOrd := indexOfString(list, parentRank.String)
	if selfOrd < 0 || parentOrd < 0 {
		result.Passed = true
		result.Message = fmt.Sprintf("Rank not found in rank_order for code %q; skipping", selfCode.String)
		return result, nil
	}

	result.ActualValue = fmt.Sprintf("self=%s(%d) parent=%s(%d)",
		selfRank.String, selfOrd, parentRank.String, parentOrd)
	if parentOrd < selfOrd {
		result.Passed = true
		result.Message = "Parent rank is higher than self"
		return result, nil
	}
	result.Passed = false
	result.ExpectedValue = fmt.Sprintf("parent index < self index (%d)", selfOrd)
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf("Parent rank %q is not higher than self rank %q under code %q",
			parentRank.String, selfRank.String, selfCode.String)
	}
	return result, nil
}

func (v *ParentRankHigherValidator) CanAutoFix() bool { return false }
func (v *ParentRankHigherValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

type parentRankHigherParams struct {
	parentColumn string
	rankColumn   string
	codeColumn   string
	join         *ancestorJoin // optional; nil = fields read from self row directly
	rankOrder    map[string][]string
}

func parseParentRankHigherParams(p map[string]interface{}) (*parentRankHigherParams, error) {
	parentCol, _ := p["parent_column"].(string)
	rankCol, _ := p["rank_column"].(string)
	codeCol, _ := p["code_column"].(string)
	if parentCol == "" || rankCol == "" || codeCol == "" {
		return nil, fmt.Errorf("'parent_column', 'rank_column', and 'code_column' are required")
	}
	var join *ancestorJoin
	if joinRaw, ok := p["join"].(map[string]interface{}); ok {
		j, err := parseAncestorJoin(joinRaw)
		if err != nil {
			return nil, fmt.Errorf("join: %w", err)
		}
		join = j
	}
	rawOrder, ok := p["rank_order"].(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("'rank_order' must be a map of code → ordered list")
	}
	order := make(map[string][]string, len(rawOrder))
	for code, listRaw := range rawOrder {
		list, ok := toInterfaceSlice(listRaw)
		if !ok || len(list) == 0 {
			return nil, fmt.Errorf("rank_order[%q] must be a non-empty list", code)
		}
		strs := make([]string, len(list))
		for i, v := range list {
			s, ok := v.(string)
			if !ok || s == "" {
				return nil, fmt.Errorf("rank_order[%q][%d] must be a non-empty string", code, i)
			}
			strs[i] = s
		}
		order[code] = strs
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("'rank_order' must declare at least one code")
	}
	return &parentRankHigherParams{
		parentColumn: parentCol,
		rankColumn:   rankCol,
		codeColumn:   codeCol,
		join:         join,
		rankOrder:    order,
	}, nil
}

// buildRankCodeLookup renders a SELECT that yields (rank, code) for
// either the record itself (parent=false) or the record's immediate
// parent (parent=true). Optional join reaches a related row that
// carries the fields.
//
// Shapes:
//
//	self, no join:      SELECT t.rank, t.code FROM table t WHERE t.pk = ?
//	self, join:         SELECT j.rank, j.code FROM table t JOIN joined j ON ... WHERE t.pk = ?
//	parent, no join:    SELECT p.rank, p.code FROM table t JOIN table p ON p.pk = t.parent_col WHERE t.pk = ?
//	parent, join:       SELECT j.rank, j.code FROM table t JOIN table p ON p.pk = t.parent_col JOIN joined j ON j.to = p.from WHERE t.pk = ?
func buildRankCodeLookup(tableName, pkColumn string, join *ancestorJoin,
	rankColumn, codeColumn string, parent bool, parentColumn string) (string, error) {
	if err := safeIdent(tableName); err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	if err := safeIdent(pkColumn); err != nil {
		return "", fmt.Errorf("pk column: %w", err)
	}
	if err := safeIdent(rankColumn); err != nil {
		return "", fmt.Errorf("rank column: %w", err)
	}
	if err := safeIdent(codeColumn); err != nil {
		return "", fmt.Errorf("code column: %w", err)
	}
	if parent {
		if err := safeIdent(parentColumn); err != nil {
			return "", fmt.Errorf("parent column: %w", err)
		}
	}
	if join != nil {
		if err := safeIdent(join.fromColumn); err != nil {
			return "", fmt.Errorf("join.from: %w", err)
		}
		if err := safeIdent(join.toTable); err != nil {
			return "", fmt.Errorf("join.to table: %w", err)
		}
		if err := safeIdent(join.toColumn); err != nil {
			return "", fmt.Errorf("join.to column: %w", err)
		}
	}

	sourceAlias := "t"
	if parent {
		sourceAlias = "p"
	}
	selectAlias := sourceAlias
	if join != nil {
		selectAlias = "j"
	}

	var sb strings.Builder
	sb.WriteString("SELECT ")
	sb.WriteString(selectAlias)
	sb.WriteString(".")
	sb.WriteString(quoteIdent(rankColumn))
	sb.WriteString(", ")
	sb.WriteString(selectAlias)
	sb.WriteString(".")
	sb.WriteString(quoteIdent(codeColumn))
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" t")
	if parent {
		sb.WriteString(" JOIN ")
		sb.WriteString(quoteIdent(tableName))
		sb.WriteString(" p ON p.")
		sb.WriteString(quoteIdent(pkColumn))
		sb.WriteString(" = t.")
		sb.WriteString(quoteIdent(parentColumn))
	}
	if join != nil {
		sb.WriteString(" JOIN ")
		sb.WriteString(quoteIdent(join.toTable))
		sb.WriteString(" j ON j.")
		sb.WriteString(quoteIdent(join.toColumn))
		sb.WriteString(" = ")
		sb.WriteString(sourceAlias)
		sb.WriteString(".")
		sb.WriteString(quoteIdent(join.fromColumn))
	}
	sb.WriteString(" WHERE t.")
	sb.WriteString(quoteIdent(pkColumn))
	sb.WriteString(" = ?")
	return sb.String(), nil
}

func indexOfString(list []string, s string) int {
	for i, v := range list {
		if v == s {
			return i
		}
	}
	return -1
}
