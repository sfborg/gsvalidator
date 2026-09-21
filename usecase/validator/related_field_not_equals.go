package validator

import (
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
	"github.com/sfborg/gsvalidator/usecase/joins"
)

// RelatedFieldNotEqualsValidator is the negative counterpart of
// RelatedFieldEqualsValidator: it flags records whose field
// equals the same-or-different field on a related row, and
// passes when the two values differ.
//
// Parameters:
//
//	relation:              string  (required) — relation name in the catalog
//	field:                 string  (required) — field on the record to read
//	related_field:         string  (optional) — field on the related row (defaults to field)
//	skip_if_either_empty:  bool    (default true) — pass silently when
//	                                either side is empty; otherwise treat
//	                                empty vs non-empty as a difference
//
// Typical use is "an X should not reuse a Y drawn from a related
// record" — for example a subsequent-combination name whose
// nomenclatural reference must not be the original combination's
// reference.
type RelatedFieldNotEqualsValidator struct {
	resolver *joins.RelationResolver
}

// NewRelatedFieldNotEqualsValidator builds a validator wired to
// the caller's resolver.
func NewRelatedFieldNotEqualsValidator(resolver *joins.RelationResolver) *RelatedFieldNotEqualsValidator {
	return &RelatedFieldNotEqualsValidator{resolver: resolver}
}

func (v *RelatedFieldNotEqualsValidator) Name() string { return "related_field_not_equals" }

func (v *RelatedFieldNotEqualsValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	relation, _ := rule.Parameters["relation"].(string)
	field, _ := rule.Parameters["field"].(string)
	if relation == "" || field == "" {
		return nil, fmt.Errorf("%w: related_field_not_equals requires 'relation' and 'field'", domain.ErrInvalidParameters)
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
		return nil, fmt.Errorf("related_field_not_equals: %w", err)
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
		return nil, fmt.Errorf("related_field_not_equals: %w", err)
	}
	if !matched {
		result.Passed = true
		result.Message = "values differ"
		return result, nil
	}
	result.Passed = false
	result.ActualValue = selfVal
	result.ExpectedValue = relatedVal
	if rule.WarningMessage != "" {
		result.Message = rule.WarningMessage
	} else {
		result.Message = fmt.Sprintf(
			"Field %s = %v matches the related record via %q; a distinct value was expected",
			field, selfVal, relation,
		)
	}
	return result, nil
}

func (v *RelatedFieldNotEqualsValidator) CanAutoFix() bool { return false }
func (v *RelatedFieldNotEqualsValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
