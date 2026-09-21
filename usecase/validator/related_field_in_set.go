package validator

import (
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// RelatedFieldInSetValidator checks that a field on the related
// record reached via a named relation is one of a caller-supplied
// set of allowed values. Common pattern: "the parent taxon's rank
// must be one of GENUS, SUBGENUS, INFRAGENUS."
//
// Parameters:
//
//	relation:       string    (required) — relation name in the catalog
//	related_field:  string    (required) — field on the related row
//	allowed_values: []string  (required) — the set to test against
//	skip_if_empty:  bool      (default true) — pass silently when the
//	                          related row's field is empty (undeclared)
//
// Passes when the related row's value is in allowed_values, when
// the related row is missing (orphan / root record), or when the
// related field is empty and skip_if_empty is true.
type RelatedFieldInSetValidator struct {
	resolver *joins.RelationResolver
}

// NewRelatedFieldInSetValidator builds a validator wired to the
// caller's resolver.
func NewRelatedFieldInSetValidator(resolver *joins.RelationResolver) *RelatedFieldInSetValidator {
	return &RelatedFieldInSetValidator{resolver: resolver}
}

func (v *RelatedFieldInSetValidator) Name() string { return "related_field_in_set" }

func (v *RelatedFieldInSetValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	relation, _ := rule.Parameters["relation"].(string)
	relatedField, _ := rule.Parameters["related_field"].(string)
	allowedRaw, _ := rule.Parameters["allowed_values"]
	if relation == "" || relatedField == "" || allowedRaw == nil {
		return nil, fmt.Errorf("%w: related_field_in_set requires 'relation', 'related_field', and 'allowed_values'", domain.ErrInvalidParameters)
	}
	skipEmpty := true
	if b, ok := rule.Parameters["skip_if_empty"].(bool); ok {
		skipEmpty = b
	}
	result.FieldName = relatedField

	related, err := v.resolver.ResolveOne(ctx.Ctx, ctx.DB, ctx.TableName, ctx.RecordID, relation)
	if err != nil {
		return nil, fmt.Errorf("related_field_in_set: %w", err)
	}
	if related == nil {
		result.Passed = true
		result.Message = fmt.Sprintf("no related record via %q; skipped", relation)
		return result, nil
	}
	val := related[relatedField]
	if skipEmpty && isEmpty(val) {
		result.Passed = true
		result.Message = "related value empty; skipped per skip_if_empty"
		return result, nil
	}

	matched, err := domain.Compare(domain.OpInSet, val, allowedRaw)
	if err != nil {
		return nil, fmt.Errorf("related_field_in_set: %w", err)
	}
	if matched {
		result.Passed = true
		result.Message = "value in allowed set"
		return result, nil
	}
	result.Passed = false
	result.ActualValue = val
	result.ExpectedValue = allowedRaw
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf(
			"Related %s = %v not in allowed set %v",
			relatedField, val, allowedRaw,
		)
	}
	return result, nil
}

func (v *RelatedFieldInSetValidator) CanAutoFix() bool { return false }
func (v *RelatedFieldInSetValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
