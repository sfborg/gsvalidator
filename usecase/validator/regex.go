package validator

import (
	"fmt"
	"regexp"
	"sync"

	"github.com/sfborg/gsvalidator/domain"
)

// RegexValidator validates field values against regular expression patterns.
type RegexValidator struct {
	compiledPatterns map[string]*regexp.Regexp
	mu               sync.RWMutex
}

// NewRegexValidator creates a new regex validator with pattern caching.
func NewRegexValidator() *RegexValidator {
	return &RegexValidator{
		compiledPatterns: make(map[string]*regexp.Regexp),
	}
}

// Name returns the validator type identifier.
func (v *RegexValidator) Name() string {
	return "regex"
}

// Validate checks if the field value matches the regex pattern.
func (v *RegexValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	// Get pattern from rule parameters
	patternInterface, ok := rule.Parameters["pattern"]
	if !ok {
		return nil, fmt.Errorf("%w: 'pattern' parameter required for regex validator", domain.ErrInvalidParameters)
	}

	pattern, ok := patternInterface.(string)
	if !ok {
		return nil, fmt.Errorf("%w: 'pattern' must be a string", domain.ErrInvalidParameters)
	}

	// Get field value
	value, exists := ctx.GetFieldValue(rule.FieldName)
	if !exists || value == nil {
		result.Passed = false
		result.Message = rule.Message()
		result.ActualValue = nil
		result.ExpectedValue = fmt.Sprintf("matches pattern: %s", pattern)
		return result, nil
	}

	// Convert to string
	fieldStr := fmt.Sprintf("%v", value)

	// Get compiled regex (cached)
	re, err := v.getCompiledPattern(pattern)
	if err != nil {
		return nil, fmt.Errorf("failed to compile regex pattern '%s': %w", pattern, err)
	}

	// Test pattern match
	if re.MatchString(fieldStr) {
		result.Passed = true
		result.Message = "Field matches pattern"
		result.ActualValue = fieldStr
		return result, nil
	}

	// Pattern does not match
	result.Passed = false
	result.Message = rule.Message()
	result.ActualValue = fieldStr
	result.ExpectedValue = fmt.Sprintf("matches pattern: %s", pattern)
	return result, nil
}

// getCompiledPattern retrieves a compiled regex from cache or compiles it.
func (v *RegexValidator) getCompiledPattern(pattern string) (*regexp.Regexp, error) {
	// Check cache first (read lock)
	v.mu.RLock()
	re, exists := v.compiledPatterns[pattern]
	v.mu.RUnlock()

	if exists {
		return re, nil
	}

	// Compile pattern (write lock)
	v.mu.Lock()
	defer v.mu.Unlock()

	// Double-check (another goroutine might have compiled it)
	re, exists = v.compiledPatterns[pattern]
	if exists {
		return re, nil
	}

	// Compile and cache
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}

	v.compiledPatterns[pattern] = re
	return re, nil
}

// CanAutoFix returns false (regex cannot be auto-fixed).
func (v *RegexValidator) CanAutoFix() bool {
	return false
}

// AutoFix is not supported for regex validator.
func (v *RegexValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}
