package validator

import (
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// RelatedFieldEqualsValidator compares a field on the target
// record against the same-or-different field on a related record
// reached via a named relation from the bundle's Relations
// catalog. Generic mechanism — no schema-specific knowledge in
// the Go code; the rule's parameters carry the relation name,
// the fields to compare, and how to treat empty values.
//
// Parameters:
//
//	relation:              string  (required) — relation name in the catalog
//	field:                 string  (required) — field on the record to read
//	related_field:         string  (optional) — field on the related row (defaults to field)
//	skip_if_either_empty:  bool    (default true) — pass silently when
//	                                either side is empty; otherwise treat
//	                                empty vs non-empty as mismatch
//
// Passes when the two values compare equal, or when either side
// is empty and skip_if_either_empty is true (the common
// "compatible-if-known" pattern), or when the relation has no
// target row (orphan / root record).
type RelatedFieldEqualsValidator struct {
	resolver *joins.RelationResolver
}

// NewRelatedFieldEqualsValidator builds a validator wired to the
// caller's resolver. Consumer applications construct one resolver
// from the loaded bundles' Relations blocks and pass it in.
func NewRelatedFieldEqualsValidator(resolver *joins.RelationResolver) *RelatedFieldEqualsValidator {
	return &RelatedFieldEqualsValidator{resolver: resolver}
}

func (v *RelatedFieldEqualsValidator) Name() string { return "related_field_equals" }

func (v *RelatedFieldEqualsValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	relation, _ := rule.Parameters["relation"].(string)
	field, _ := rule.Parameters["field"].(string)
	if relation == "" || field == "" {
		return nil, fmt.Errorf("%w: related_field_equals requires 'relation' and 'field'", domain.ErrInvalidParameters)
	}
	relatedField, _ := rule.Parameters["related_field"].(string)
	if relatedField == "" {
		relatedField = field
	}
	skipEmpty := true
	if b, ok := rule.Parameters["skip_if_either_empty"].(bool); ok {
		skipEmpty = b
	}
	result.FieldName = field

	selfVal, ok := ctx.GetFieldValue(field)
	if !ok {
		selfVal = nil
	}
	if skipEmpty && isEmpty(selfVal) {
		result.Passed = true
		result.Message = "self value empty; skipped per skip_if_either_empty"
		return result, nil
	}

	related, err := v.resolver.ResolveOne(ctx.Ctx, ctx.DB, ctx.TableName, ctx.RecordID, relation)
	if err != nil {
		return nil, fmt.Errorf("related_field_equals: %w", err)
	}
	if related == nil {
		result.Passed = true
		result.Message = fmt.Sprintf("no related record via %q; skipped", relation)
		return result, nil
	}
	relatedVal := related[relatedField]
	if skipEmpty && isEmpty(relatedVal) {
		result.Passed = true
		result.Message = "related value empty; skipped per skip_if_either_empty"
		return result, nil
	}

	matched, err := domain.Compare(domain.OpEquals, selfVal, relatedVal)
	if err != nil {
		return nil, fmt.Errorf("related_field_equals: %w", err)
	}
	if matched {
		result.Passed = true
		result.Message = "values match"
		return result, nil
	}
	result.Passed = false
	result.ActualValue = selfVal
	result.ExpectedValue = relatedVal
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf(
			"Field %s = %v, but related record via %q has %v",
			field, selfVal, relation, relatedVal,
		)
	}
	return result, nil
}

func (v *RelatedFieldEqualsValidator) CanAutoFix() bool { return false }
func (v *RelatedFieldEqualsValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

// isEmpty mirrors the domain.Operator helper for the case where
// this validator needs to check emptiness without going through
// the Compare dispatcher.
func isEmpty(v interface{}) bool {
	if v == nil {
		return true
	}
	if s, ok := v.(string); ok {
		return s == ""
	}
	return false
}
