package domain

// AutoFixConfig defines how to automatically fix a validation error.
type AutoFixConfig struct {
	Strategy string                 `json:"strategy"` // copy_from_coordinate, set_default, capitalize, etc.
	Params   map[string]interface{} `json:"params"`
}

// IsEnabled returns true if auto-fix is configured.
func (a *AutoFixConfig) IsEnabled() bool {
	return a != nil && a.Strategy != ""
}
