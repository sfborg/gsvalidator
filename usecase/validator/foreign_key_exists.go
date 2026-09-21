package validator

import (
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// ForeignKeyExistsValidator asserts that a FK column on the record
// resolves to an existing row via a declared relation. Passes when
// the FK is empty (nothing to resolve) or when the relation returns
// a row; fails only when the FK is populated but the walk yields
// no target.
//
// Ergonomic sugar over expressing the same check with
// related_field_equals + a presence assertion on the joined id —
// reads naturally at the rule call site and matches SQL semantics.
//
// Parameters:
//
//	field    string (required) — FK column on the current record. Read
//	                              once to decide whether the rule
//	                              applies (skip on empty) and to report
//	                              the offending value on failure.
//	relation string (required) — name of the relation to walk. The
//	                              relation must be declared in the
//	                              bundle's relations block and be
//	                              one-cardinality.
type ForeignKeyExistsValidator struct {
	resolver *joins.RelationResolver
}

// NewForeignKeyExistsValidator builds a validator over the caller's
// resolver. Same construction pattern as related_field_equals.
func NewForeignKeyExistsValidator(resolver *joins.RelationResolver) *ForeignKeyExistsValidator {
	return &ForeignKeyExistsValidator{resolver: resolver}
}

func (v *ForeignKeyExistsValidator) Name() string { return "foreign_key_exists" }

func (v *ForeignKeyExistsValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	field, _ := rule.Parameters["field"].(string)
	relation, _ := rule.Parameters["relation"].(string)
	if field == "" || relation == "" {
		return nil, fmt.Errorf("foreign_key_exists: 'field' and 'relation' required")
	}
	result.FieldName = field

	val, exists := ctx.Record[field]
	if !exists || isEmpty(val) {
		result.Passed = true
		result.Message = fmt.Sprintf("field %q empty; nothing to resolve", field)
		return result, nil
	}

	if v.resolver == nil {
		return nil, fmt.Errorf("foreign_key_exists: resolver required for relation %q", relation)
	}
	row, err := v.resolver.ResolveOne(ctx.Ctx, ctx.DB, ctx.TableName, ctx.RecordID, relation)
	if err != nil {
		return nil, fmt.Errorf("foreign_key_exists: %w", err)
	}
	if row != nil {
		result.Passed = true
		result.Message = "Foreign key resolves"
		result.ActualValue = val
		return result, nil
	}
	result.Passed = false
	result.ActualValue = val
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf("Foreign key %v does not resolve via relation %q", val, relation)
	}
	return result, nil
}

func (v *ForeignKeyExistsValidator) CanAutoFix() bool { return false }
func (v *ForeignKeyExistsValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
