package domain

import "errors"

var (
	// ErrRuleNotFound indicates a rule with the given ID was not found.
	ErrRuleNotFound = errors.New("validation rule not found")

	// ErrInvalidRule indicates a rule has invalid configuration.
	ErrInvalidRule = errors.New("invalid validation rule")

	// ErrValidatorNotFound indicates a validator with the given type was not registered.
	ErrValidatorNotFound = errors.New("validator not found")

	// ErrRecordNotFound indicates the record to validate was not found.
	ErrRecordNotFound = errors.New("record not found")

	// ErrTableNotFound indicates the table does not exist.
	ErrTableNotFound = errors.New("table not found")

	// ErrFieldNotFound indicates the field does not exist in the record.
	ErrFieldNotFound = errors.New("field not found")

	// ErrAutoFixFailed indicates automatic fix attempt failed.
	ErrAutoFixFailed = errors.New("auto-fix failed")

	// ErrHardValidationFailed indicates a hard validation failed (should block save).
	ErrHardValidationFailed = errors.New("hard validation failed")

	// ErrInvalidParameters indicates rule parameters are invalid for the validator type.
	ErrInvalidParameters = errors.New("invalid validation parameters")

	// ErrSchemaMapperNotSet indicates no schema mapper was configured.
	ErrSchemaMapperNotSet = errors.New("schema mapper not set")

	// ErrRuleLoaderNotSet indicates no rule loader was configured.
	ErrRuleLoaderNotSet = errors.New("rule loader not set")
)
