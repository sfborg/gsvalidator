package domain

import "time"

// Trigger describes when a rule should be evaluated. Individual
// consumer applications decide how to honor each trigger — gsvalidator
// itself does not schedule re-evaluations; it just publishes the
// intent so callers can wire it in.
type Trigger string

const (
	// TriggerOnWrite (default) — evaluate immediately after any write
	// that touches a matching record. Same behavior existing consumers
	// already implemented; keeping it unnamed by leaving the field
	// empty is also treated as OnWrite for back-compat.
	TriggerOnWrite Trigger = "on_write"
	// TriggerTimeBased — re-evaluate periodically regardless of writes.
	// Suits rules whose truth changes with the passage of time (stale
	// data, freshness reminders, expiring references). RecheckDays on
	// the rule controls the cadence; a value of 0 falls back to a
	// consumer-defined default.
	TriggerTimeBased Trigger = "time_based"
)

// Rule defines a validation rule with conditions and parameters.
// This is a core domain entity with no external dependencies.
type Rule struct {
	ID             string                 `json:"rule_id"`
	Name           string                 `json:"rule_name"`
	Description    string                 `json:"description"`
	TableName      string                 `json:"table_name"`
	FieldName      string                 `json:"field_name,omitempty"`
	ValidatorType  string                 `json:"validator_type"`
	Enforcement    Enforcement            `json:"enforcement,omitempty"`
	Severity       Severity               `json:"severity,omitempty"`
	Trigger        Trigger                `json:"trigger,omitempty"`         // default: on_write
	RecheckDays    int                    `json:"recheck_days,omitempty"`    // for trigger=time_based
	ValidationType string                 `json:"validation_type,omitempty"` // legacy: "hard"/"soft" or "error"/"warn"/"info"/"debug"
	Conditions     []Condition            `json:"conditions,omitempty"`
	Parameters     map[string]interface{} `json:"parameters"`
	ErrorMessage   string                 `json:"error_message"`
	WarningMessage string                 `json:"warning_message,omitempty"`
	AutoFix        *AutoFixConfig         `json:"auto_fix,omitempty"`
	IsActive       bool                   `json:"is_active"`
	Priority       int                    `json:"priority"` // Execution order (lower = higher priority)
	CreatedAt      time.Time              `json:"created_at,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at,omitempty"`
}

// EffectiveTrigger returns TriggerOnWrite when the rule doesn't set one,
// matching the pre-Trigger behavior for existing rules.
func (r *Rule) EffectiveTrigger() Trigger {
	switch r.Trigger {
	case TriggerOnWrite, TriggerTimeBased:
		return r.Trigger
	}
	return TriggerOnWrite
}

// EvalConditions checks whether the rule applies to the record
// named by vctx. Conditions may reference fields on the record
// itself or, when Relation is set, on a record reached via the
// named relation from the bundle's Relations catalog. Returns
// (false, nil) when the rule is inactive or any condition is
// unmet; (true, nil) when all conditions pass (or there are
// none). Returns an error when a relation-scoped condition can't
// be evaluated (missing resolver, SQL failure, etc.).
func (r *Rule) EvalConditions(vctx *ValidationContext) (bool, error) {
	if !r.IsActive {
		return false, nil
	}
	if len(r.Conditions) == 0 {
		return true, nil
	}
	for i := range r.Conditions {
		ok, err := r.Conditions[i].Eval(vctx)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

// EffectiveEnforcement returns the rule's enforcement, falling back to
// legacy ValidationType parsing for rules loaded from older sources.
// "hard" and "error" both mean blocking; anything else is treated as soft.
func (r *Rule) EffectiveEnforcement() Enforcement {
	if r.Enforcement == EnforcementHard || r.Enforcement == EnforcementSoft {
		return r.Enforcement
	}
	switch r.ValidationType {
	case "hard", "error":
		return EnforcementHard
	case "soft", "warn", "warning", "info", "debug":
		return EnforcementSoft
	}
	return EnforcementSoft
}

// EffectiveSeverity returns the severity a failing result of this rule
// should carry, falling back to legacy ValidationType parsing and finally
// to the default severity for the effective enforcement.
func (r *Rule) EffectiveSeverity() Severity {
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
	}
	return DefaultSeverityFor(r.EffectiveEnforcement())
}

// Message returns the appropriate message based on severity: warning-level
// results prefer WarningMessage when available; other severities use
// ErrorMessage.
func (r *Rule) Message() string {
	sev := r.EffectiveSeverity()
	if sev != SeverityError && r.WarningMessage != "" {
		return r.WarningMessage
	}
	return r.ErrorMessage
}
