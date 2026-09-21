package domain

import "time"

// Result represents the outcome of a single validation rule execution.
// Multiple results can exist for the same field (multiple validation issues).
type Result struct {
	RuleID           string      `json:"rule_id"`
	RuleName         string      `json:"rule_name"`
	RecordID         string      `json:"record_id"`
	TableName        string      `json:"table_name"`
	FieldName        string      `json:"field_name"` // Required to support multiple issues per field
	ValidatorType    string      `json:"validator_type"`
	Enforcement      Enforcement `json:"enforcement,omitempty"`     // hard = blocked write; soft = advisory
	Severity         Severity    `json:"severity,omitempty"`        // error/warn/info/debug for display
	ValidationType   string      `json:"validation_type,omitempty"` // legacy mirror of Severity for older readers
	Passed           bool        `json:"passed"`                    // False for failures, true for passes
	Message          string      `json:"message"`
	ActualValue      interface{} `json:"actual_value,omitempty"`
	ExpectedValue    interface{} `json:"expected_value,omitempty"`
	AutoFixAvailable bool        `json:"auto_fix_available"`
	AutoFixStrategy  string      `json:"auto_fix_strategy,omitempty"`
	Timestamp        time.Time   `json:"timestamp"`
}

// effectiveSeverity returns the result's Severity, falling back to legacy
// ValidationType parsing so results written by older validators still
// classify correctly.
func (r *Result) effectiveSeverity() Severity {
	switch r.Severity {
	case SeverityError, SeverityWarn, SeverityInfo, SeverityDebug:
		return r.Severity
	}
	switch r.ValidationType {
	case "error":
		return SeverityError
	case "warn", "warning":
		return SeverityWarn
	case "info":
		return SeverityInfo
	case "debug":
		return SeverityDebug
	case "hard":
		return SeverityError
	case "soft":
		return SeverityWarn
	}
	return SeverityWarn
}

// IsError returns true for a failed hard-enforcement rule.
func (r *Result) IsError() bool {
	return !r.Passed && r.effectiveSeverity() == SeverityError
}

// IsWarning returns true for a failed rule that carries warn severity.
func (r *Result) IsWarning() bool {
	return !r.Passed && r.effectiveSeverity() == SeverityWarn
}

// IsInfo returns true for an info-severity result.
func (r *Result) IsInfo() bool {
	return r.effectiveSeverity() == SeverityInfo
}

// IsDebug returns true for a debug-severity result.
func (r *Result) IsDebug() bool {
	return r.effectiveSeverity() == SeverityDebug
}

// IsHardFailure returns true when the result comes from a hard-enforcement
// rule that failed and should block the write.
func (r *Result) IsHardFailure() bool {
	if r.Passed {
		return false
	}
	if r.Enforcement == EnforcementHard {
		return true
	}
	if r.Enforcement == EnforcementSoft {
		return false
	}
	// Fall through to legacy interpretation when Enforcement is unset.
	return r.effectiveSeverity() == SeverityError
}

// IsFailure returns true if validation failed (regardless of hard/soft).
func (r *Result) IsFailure() bool {
	return !r.Passed
}
