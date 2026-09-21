package validator

import (
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
)

// LengthValidator validates string length constraints.
type LengthValidator struct{}

// Name returns the validator type identifier.
func (v *LengthValidator) Name() string {
	return "length"
}

// Validate checks if the field value length is within the specified range.
func (v *LengthValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	// Get field value
	value, exists := ctx.GetFieldValue(rule.FieldName)
	if !exists || value == nil {
		result.Passed = false
		result.Message = rule.Message()
		result.ActualValue = nil
		result.ExpectedValue = "string value"
		return result, nil
	}

	// Convert to string
	fieldStr := fmt.Sprintf("%v", value)
	length := len(fieldStr)

	// Get min and max from parameters
	minInterface, hasMin := rule.Parameters["min"]
	maxInterface, hasMax := rule.Parameters["max"]

	if !hasMin && !hasMax {
		return nil, fmt.Errorf("%w: 'min' or 'max' parameter required for length validator", domain.ErrInvalidParameters)
	}

	// Check minimum length
	if hasMin {
		minFloat, ok := toFloat64(minInterface)
		if !ok {
			return nil, fmt.Errorf("%w: 'min' must be numeric", domain.ErrInvalidParameters)
		}
		minInt := int(minFloat)

		if length < minInt {
			result.Passed = false
			result.Message = rule.Message()
			result.ActualValue = fmt.Sprintf("length %d", length)
			result.ExpectedValue = fmt.Sprintf(">= %d characters", minInt)
			return result, nil
		}
	}

	// Check maximum length
	if hasMax {
		maxFloat, ok := toFloat64(maxInterface)
		if !ok {
			return nil, fmt.Errorf("%w: 'max' must be numeric", domain.ErrInvalidParameters)
		}
		maxInt := int(maxFloat)

		if length > maxInt {
			result.Passed = false
			result.Message = rule.Message()
			result.ActualValue = fmt.Sprintf("length %d", length)
			result.ExpectedValue = fmt.Sprintf("<= %d characters", maxInt)
			return result, nil
		}
	}

	// Length is valid
	result.Passed = true
	result.Message = "Length is valid"
	result.ActualValue = fmt.Sprintf("length %d", length)
	return result, nil
}

// CanAutoFix returns false (length cannot be auto-fixed safely).
func (v *LengthValidator) CanAutoFix() bool {
	return false
}

// AutoFix is not supported for length validator.
func (v *LengthValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
