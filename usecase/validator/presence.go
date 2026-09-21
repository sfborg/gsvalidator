package validator

import (
	"github.com/sfborg/gsvalidator/domain"
)

// PresenceValidator checks if a field has a non-nil, non-empty value.
type PresenceValidator struct{}

// Name returns the validator type identifier.
func (v *PresenceValidator) Name() string {
	return "presence"
}

// Validate checks if the field has a value.
func (v *PresenceValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	// Get field value
	value, exists := ctx.GetFieldValue(rule.FieldName)

	// Check if field exists and has non-nil value
	if !exists || value == nil {
		result.Passed = false
		result.Message = rule.Message()
		result.ActualValue = nil
		result.ExpectedValue = "non-empty value"
		return result, nil
	}

	// Check if value is empty string
	if strVal, ok := value.(string); ok && strVal == "" {
		result.Passed = false
		result.Message = rule.Message()
		result.ActualValue = ""
		result.ExpectedValue = "non-empty value"
		return result, nil
	}

	// Field has value
	result.Passed = true
	result.Message = "Field has value"
	result.ActualValue = value
	return result, nil
}

// CanAutoFix returns false (presence cannot be auto-fixed).
func (v *PresenceValidator) CanAutoFix() bool {
	return false
}

// AutoFix is not supported for presence validator.
func (v *PresenceValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
