package domain

import (
	"strings"
	"time"
)

// Enforcement expresses whether a rule violation blocks a write (hard) or is
// recorded as a warning that lets the write proceed (soft). It is a property
// of the rule, not the individual result.
type Enforcement string

const (
	EnforcementHard Enforcement = "hard"
	EnforcementSoft Enforcement = "soft"
)

// Severity classifies a violation for display and filtering. It is a property
// of the emitted result. A hard-enforcement rule always emits SeverityError
// when it fails; a soft-enforcement rule may emit warn, info, or debug.
type Severity string

const (
	SeverityError Severity = "error"
	SeverityWarn  Severity = "warn"
	SeverityInfo  Severity = "info"
	SeverityDebug Severity = "debug"
)

// ParseEnforcement normalises a string to an Enforcement value. Empty input
// or unrecognised values fall back to soft, so a rule missing the field is
// treated as non-blocking rather than silently blocking writes.
func ParseEnforcement(s string) Enforcement {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "hard":
		return EnforcementHard
	case "soft":
		return EnforcementSoft
	}
	return EnforcementSoft
}

// ParseSeverity normalises a string to a Severity value. Empty or unknown
// input falls back to warn.
func ParseSeverity(s string) Severity {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "error":
		return SeverityError
	case "warn", "warning":
		return SeverityWarn
	case "info":
		return SeverityInfo
	case "debug":
		return SeverityDebug
	}
	return SeverityWarn
}

// DefaultSeverityFor returns the severity a violation of the given
// enforcement produces when the rule does not specify one explicitly.
func DefaultSeverityFor(e Enforcement) Severity {
	if e == EnforcementHard {
		return SeverityError
	}
	return SeverityWarn
}

// NewResult builds a Result pre-populated with the fields every validator
// stamps identically: identifiers copied from the rule and context, plus
// the enforcement/severity/legacy validation_type triple derived from the
// rule. Individual validators still set Passed, Message, ActualValue, etc.
// on the returned pointer.
func NewResult(ctx *ValidationContext, rule *Rule) *Result {
	enf := rule.EffectiveEnforcement()
	sev := rule.EffectiveSeverity()
	return &Result{
		RuleID:         rule.ID,
		RuleName:       rule.Name,
		RecordID:       ctx.RecordID,
		TableName:      ctx.TableName,
		FieldName:      rule.FieldName,
		ValidatorType:  rule.ValidatorType,
		Enforcement:    enf,
		Severity:       sev,
		ValidationType: string(sev),
		Timestamp:      time.Now(),
	}
}
