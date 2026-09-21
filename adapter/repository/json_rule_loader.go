package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/sfborg/gsvalidator/domain"
)

// JSONRuleLoader loads validation rules from JSON files.
// This adapter implements the RuleLoader interface defined in the usecase layer.
type JSONRuleLoader struct {
	filePath string
	rules    []*domain.Rule
	ruleMap  map[string]*domain.Rule
}

// NewJSONRuleLoader creates a new JSON rule loader.
func NewJSONRuleLoader(filePath string) *JSONRuleLoader {
	return &JSONRuleLoader{
		filePath: filePath,
		ruleMap:  make(map[string]*domain.Rule),
	}
}

// LoadRules loads all rules from the JSON file.
func (l *JSONRuleLoader) LoadRules(ctx context.Context) ([]*domain.Rule, error) {
	// If already loaded, return cached rules
	if len(l.rules) > 0 {
		return l.rules, nil
	}

	// Read JSON file
	data, err := os.ReadFile(l.filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read rule file %s: %w", l.filePath, err)
	}

	// Parse JSON - supports both single rule and array of rules
	var rules []*domain.Rule

	// Try parsing as array first
	if err := json.Unmarshal(data, &rules); err != nil {
		// Try parsing as object with "rules" key
		var wrapper struct {
			Rules []*domain.Rule `json:"rules"`
		}
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return nil, fmt.Errorf("failed to parse rules from %s: %w", l.filePath, err)
		}
		rules = wrapper.Rules
	}

	// Build rule map for fast lookup
	for _, rule := range rules {
		l.ruleMap[rule.ID] = rule
	}

	// Cache rules
	l.rules = rules

	return rules, nil
}

// LoadRuleByID loads a specific rule by its ID.
func (l *JSONRuleLoader) LoadRuleByID(ctx context.Context, ruleID string) (*domain.Rule, error) {
	// Ensure rules are loaded
	if len(l.rules) == 0 {
		if _, err := l.LoadRules(ctx); err != nil {
			return nil, err
		}
	}

	// Lookup rule
	rule, exists := l.ruleMap[ruleID]
	if !exists {
		return nil, fmt.Errorf("%w: %s", domain.ErrRuleNotFound, ruleID)
	}

	return rule, nil
}

// LoadRulesForTable loads all rules that apply to a specific table.
func (l *JSONRuleLoader) LoadRulesForTable(ctx context.Context, tableName string) ([]*domain.Rule, error) {
	// Ensure rules are loaded
	if len(l.rules) == 0 {
		if _, err := l.LoadRules(ctx); err != nil {
			return nil, err
		}
	}

	// Filter rules for this table
	var tableRules []*domain.Rule
	for _, rule := range l.rules {
		if rule.TableName == tableName {
			tableRules = append(tableRules, rule)
		}
	}

	return tableRules, nil
}
