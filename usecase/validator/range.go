package validator

import (
	"fmt"

	"github.com/sfborg/gsvalidator/domain"
)

// RangeValidator validates that numeric or date values fall within a specified range.
type RangeValidator struct{}

// Name returns the validator type identifier.
func (v *RangeValidator) Name() string {
	return "range"
}

// Validate checks if the field value is within the specified range.
func (v *RangeValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	// Get field value
	value, exists := ctx.GetFieldValue(rule.FieldName)
	if !exists || value == nil {
		result.Passed = false
		result.Message = rule.Message()
		result.ActualValue = nil
		result.ExpectedValue = "value within range"
		return result, nil
	}

	// Get min and max from parameters
	minInterface, hasMin := rule.Parameters["min"]
	maxInterface, hasMax := rule.Parameters["max"]

	if !hasMin && !hasMax {
		return nil, fmt.Errorf("%w: 'min' or 'max' parameter required for range validator", domain.ErrInvalidParameters)
	}

	// Convert value to float64
	valueFloat, ok := toFloat64(value)
	if !ok {
		result.Passed = false
		result.Message = rule.Message()
		result.ActualValue = value
		result.ExpectedValue = "numeric value"
		return result, nil
	}

	// Check minimum
	if hasMin {
		// Handle dynamic values like "current_year"
		minValue := v.resolveDynamicValue(minInterface, ctx)
		minFloat, ok := toFloat64(minValue)
		if ok && valueFloat < minFloat {
			result.Passed = false
			result.Message = rule.Message()
			result.ActualValue = valueFloat
			result.ExpectedValue = fmt.Sprintf(">= %v", minFloat)
			return result, nil
		}
	}

	// Check maximum
	if hasMax {
		// Handle dynamic values like "current_year + 5"
		maxValue := v.resolveDynamicValue(maxInterface, ctx)
		maxFloat, ok := toFloat64(maxValue)
		if ok && valueFloat > maxFloat {
			result.Passed = false
			result.Message = rule.Message()
			result.ActualValue = valueFloat
			result.ExpectedValue = fmt.Sprintf("<= %v", maxFloat)
			return result, nil
		}
	}

	// Value is within range
	result.Passed = true
	result.Message = "Value is within range"
	result.ActualValue = valueFloat
	return result, nil
}

// resolveDynamicValue resolves dynamic values like "current_year" or "current_year + 5".
func (v *RangeValidator) resolveDynamicValue(value interface{}, ctx *domain.ValidationContext) interface{} {
	strVal, ok := value.(string)
	if !ok {
		return value
	}

	switch strVal {
	case "current_year":
		return ctx.SystemContext.CurrentYear
	// Add more dynamic values as needed
	default:
		return value
	}
}

// CanAutoFix returns false (range cannot be auto-fixed).
func (v *RangeValidator) CanAutoFix() bool {
	return false
}

// AutoFix is not supported for range validator.
func (v *RangeValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

// toFloat64 converts various numeric types and strings to float64.
func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int8:
		return float64(val), true
	case int16:
		return float64(val), true
	case int32:
		return float64(val), true
	case int64:
		return float64(val), true
	case float32:
		return float64(val), true
	case float64:
		return val, true
	case string:
		// Parse string to float64
		var f float64
		_, err := fmt.Sscanf(val, "%f", &f)
		return f, err == nil
	default:
		return 0, false
	}
}
