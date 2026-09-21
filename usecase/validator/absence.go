package validator

import (
	"github.com/sfborg/gsvalidator/domain"
)

// AbsenceValidator is the inverse of PresenceValidator: passes when
// the named field is empty (nil or ""), fails when it has any
// non-empty value. Combined with the rule's conditions block, it
// expresses "field X must not be set when Y" — e.g. "infraspecific
// epithet must be empty on a species-rank name", "gender agreement
// flag must not be set on a name without a terminal epithet".
type AbsenceValidator struct{}

// Name returns the validator type identifier.
func (v *AbsenceValidator) Name() string {
	return "absence"
}

// Validate checks that the field is empty. A missing field, nil
// value, or empty string all count as absent (and pass); any other
// value fails.
func (v *AbsenceValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	value, exists := ctx.GetFieldValue(rule.FieldName)
	if !exists || value == nil {
		result.Passed = true
		result.Message = "Field is absent"
		return result, nil
	}
	if strVal, ok := value.(string); ok && strVal == "" {
		result.Passed = true
		result.Message = "Field is absent"
		return result, nil
	}

	result.Passed = false
	result.Message = rule.Message()
	result.ActualValue = value
	result.ExpectedValue = "empty value"
	return result, nil
}

// CanAutoFix returns false (absence cannot be auto-fixed — the
// safe action would be to clear the field, but that discards data
// the curator entered).
func (v *AbsenceValidator) CanAutoFix() bool {
	return false
}

// AutoFix is not supported for absence.
func (v *AbsenceValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
