package validator

import (
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// NoSelfCycleValidator walks a self-referencing parent chain from
// the record being validated and flags the record when its own PK
// appears in the reachable set — i.e. R sits on a parent-chain
// cycle. Applies to trees encoded as (pk, parent_pk) on the same
// table (taxonomies, subject hierarchies, comment threads, …).
//
// Parameters:
//
//	parent_column string  (required) — column holding the parent PK.
//	                      Table is always the current record's table;
//	                      pk column comes from the SchemaMapper.
//
// SQL uses a recursive CTE with UNION (not UNION ALL), so cycles
// don't cause the query to loop forever — the reachable set
// converges and the outer SELECT asks "is R's PK in it?".
type NoSelfCycleValidator struct {
	pkProvider joins.PrimaryKeyProvider
}

// NewNoSelfCycleValidator builds a validator wired to a PK
// provider (usually the consumer's SchemaMapper). A nil provider
// is a programmer error: the validator panics rather than emit
// SQL with an empty column name.
func NewNoSelfCycleValidator(pkProvider joins.PrimaryKeyProvider) *NoSelfCycleValidator {
	if pkProvider == nil {
		panic("validator.NewNoSelfCycleValidator: pkProvider must not be nil")
	}
	return &NoSelfCycleValidator{pkProvider: pkProvider}
}

func (v *NoSelfCycleValidator) Name() string { return "no_self_cycle" }

func (v *NoSelfCycleValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	parentCol, _ := rule.Parameters["parent_column"].(string)
	if parentCol == "" {
		return nil, fmt.Errorf("no_self_cycle: 'parent_column' required")
	}
	pkCol := v.pkProvider.GetPrimaryKeyField(ctx.TableName)
	query, err := buildCycleQuery(ctx.TableName, parentCol, pkCol)
	if err != nil {
		return nil, fmt.Errorf("no_self_cycle: %w", err)
	}

	var count int
	if err := ctx.DB.QueryRowContext(ctx.Ctx, query, ctx.RecordID, ctx.RecordID).Scan(&count); err != nil {
		return nil, fmt.Errorf("no_self_cycle: query: %w", err)
	}
	if count == 0 {
		result.Passed = true
		result.Message = "Parent chain does not cycle back to this record"
		return result, nil
	}
	result.Passed = false
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf("Record %s participates in a parent-chain cycle", ctx.RecordID)
	}
	return result, nil
}

func (v *NoSelfCycleValidator) CanAutoFix() bool { return false }
func (v *NoSelfCycleValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

// buildCycleQuery renders the recursive-CTE cycle-detection query.
// Extracted for unit testing.
//
// Shape (pseudo-quoted identifiers):
//
//	WITH RECURSIVE chain(id) AS (
//	    SELECT parent_col FROM table WHERE pk_col = ?
//	    UNION
//	    SELECT table.parent_col FROM table JOIN chain ON table.pk_col = chain.id
//	    WHERE table.parent_col IS NOT NULL AND table.parent_col != ''
//	)
//	SELECT COUNT(*) FROM chain WHERE id = ?
//
// UNION (not UNION ALL) deduplicates so cycles terminate. Both
// parameters bind to the record id: the seed picks the starting
// row, the outer SELECT asks whether we visited ourselves.
func buildCycleQuery(tableName, parentCol, pkCol string) (string, error) {
	if err := safeIdent(tableName); err != nil {
		return "", fmt.Errorf("table: %w", err)
	}
	if err := safeIdent(parentCol); err != nil {
		return "", fmt.Errorf("parent column: %w", err)
	}
	if err := safeIdent(pkCol); err != nil {
		return "", fmt.Errorf("pk column: %w", err)
	}
	var sb strings.Builder
	sb.WriteString("WITH RECURSIVE chain(id) AS (")
	sb.WriteString("SELECT ")
	sb.WriteString(quoteIdent(parentCol))
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" WHERE ")
	sb.WriteString(quoteIdent(pkCol))
	sb.WriteString(" = ? UNION SELECT ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(parentCol))
	sb.WriteString(" FROM ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(" JOIN chain ON ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(pkCol))
	sb.WriteString(" = chain.id WHERE ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(parentCol))
	sb.WriteString(" IS NOT NULL AND ")
	sb.WriteString(quoteIdent(tableName))
	sb.WriteString(".")
	sb.WriteString(quoteIdent(parentCol))
	sb.WriteString(` != '') SELECT COUNT(*) FROM chain WHERE id = ?`)
	return sb.String(), nil
}
