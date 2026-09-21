package validator

import (
	"fmt"
	"strings"

	"github.com/sfborg/gsvalidator/domain"
)

// CheckDigitValidator verifies a field's check digit against a
// well-known identifier algorithm. Regex covers the format; this
// covers the mathematical relationship the standard defines
// between an identifier's payload digits and its trailing check
// digit, which catches the two typo modes check digits are
// designed for (single-digit substitution, adjacent-digit
// transposition).
//
// Supported algorithms:
//
//	orcid  — ISO 27729 MOD 11-2; 16 chars, last is 0-9 or X.
//	issn   — ISO 3297; 8 chars, last is 0-9 or X.
//	isbn10 — mod-11 with weights 10..2; last char 0-9 or X.
//	isbn13 — mod-10 with alternating 1,3 weights (EAN-13); 13 digits.
//	rorid  — ROR identifier; 9 chars starting with '0', 6 base32-
//	         Crockford payload, 2-digit ISO 7064 MOD 97-10 check.
//	luhn   — generic mod-10 with doubling (credit-card style).
//
// Parameters:
//
//	algorithm     string (required) — one of the above.
//	skip_if_empty bool   (default true) — empty field passes silently.
//	separator     string (optional) — if set, the field is split on
//	              this separator, each token is trimmed and validated
//	              independently, and the field passes only when every
//	              token passes. Use for fields that carry multiple
//	              identifiers (e.g. a serial's print + electronic
//	              ISSNs comma-joined into one col__issn value).
type CheckDigitValidator struct{}

// NewCheckDigitValidator constructs the validator. Stateless; no
// dependencies to inject.
func NewCheckDigitValidator() *CheckDigitValidator {
	return &CheckDigitValidator{}
}

func (v *CheckDigitValidator) Name() string { return "check_digit" }

func (v *CheckDigitValidator) Validate(ctx *domain.ValidationContext, rule *domain.Rule) (*domain.Result, error) {
	result := domain.NewResult(ctx, rule)

	algorithm, _ := rule.Parameters["algorithm"].(string)
	if algorithm == "" {
		return nil, fmt.Errorf("check_digit: 'algorithm' required")
	}
	skipIfEmpty := true
	if b, ok := rule.Parameters["skip_if_empty"].(bool); ok {
		skipIfEmpty = b
	}

	fieldName := rule.FieldName
	raw, exists := ctx.GetFieldValue(fieldName)
	if !exists || raw == nil {
		if skipIfEmpty {
			result.Passed = true
			result.Message = fmt.Sprintf("field %q empty; skipped", fieldName)
			return result, nil
		}
		result.Passed = false
		result.Message = fmt.Sprintf("field %q required for check_digit", fieldName)
		return result, nil
	}
	raw = coerceScannedValue(raw)
	str, ok := raw.(string)
	if !ok {
		return nil, fmt.Errorf("check_digit: field %q value is not a string (got %T)", fieldName, raw)
	}
	if str == "" {
		if skipIfEmpty {
			result.Passed = true
			result.Message = fmt.Sprintf("field %q empty; skipped", fieldName)
			return result, nil
		}
		result.Passed = false
		result.Message = fmt.Sprintf("field %q required for check_digit", fieldName)
		return result, nil
	}

	result.ActualValue = str
	separator, _ := rule.Parameters["separator"].(string)
	// Build the list of tokens to validate: one (the whole value)
	// by default, or N tokens when separator is set. Empty tokens
	// after trimming are skipped so a trailing separator doesn't
	// fail the rule.
	var tokens []string
	if separator != "" {
		for _, t := range strings.Split(str, separator) {
			t = strings.TrimSpace(t)
			if t != "" {
				tokens = append(tokens, t)
			}
		}
	} else {
		tokens = []string{str}
	}
	for _, tok := range tokens {
		valid, err := verifyCheckDigit(algorithm, tok)
		if err != nil {
			result.Passed = false
			result.Message = err.Error()
			return result, nil
		}
		if !valid {
			result.Passed = false
			if rule.WarningMessage != "" {
				result.Message = rule.WarningMessage
			} else {
				result.Message = fmt.Sprintf("check digit invalid for %s: %q", algorithm, tok)
			}
			return result, nil
		}
	}
	result.Passed = true
	result.Message = fmt.Sprintf("check digit valid (%s)", algorithm)
	return result, nil
}

func (v *CheckDigitValidator) CanAutoFix() bool { return false }
func (v *CheckDigitValidator) AutoFix(ctx *domain.ValidationContext, rule *domain.Rule, result *domain.Result) error {
	return domain.ErrAutoFixFailed
}

// verifyCheckDigit dispatches to the algorithm-specific verifier.
// Normalisation (strip hyphens/spaces, uppercase X) happens per
// algorithm since acceptable payload characters vary.
func verifyCheckDigit(algorithm, raw string) (bool, error) {
	switch strings.ToLower(algorithm) {
	case "orcid":
		return verifyOrcid(raw)
	case "issn":
		return verifyISSN(raw)
	case "isbn10":
		return verifyISBN10(raw)
	case "isbn13":
		return verifyISBN13(raw)
	case "rorid":
		return verifyRORID(raw)
	case "luhn":
		return verifyLuhn(raw)
	default:
		return false, fmt.Errorf("unknown algorithm %q (expected orcid|issn|isbn10|isbn13|rorid|luhn)", algorithm)
	}
}

// verifyOrcid implements ISO 27729 MOD 11-2. The identifier is 16
// characters after normalising away hyphens and a common
// "https://orcid.org/" prefix; the last character is the check
// digit (0-9 or X).
func verifyOrcid(raw string) (bool, error) {
	s := stripORCIDPrefix(raw)
	s = stripSeparators(s)
	s = strings.ToUpper(s)
	if len(s) != 16 {
		return false, fmt.Errorf("orcid must be 16 digits (with optional hyphens); got %d chars in %q", len(s), s)
	}
	total := 0
	for i := 0; i < 15; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false, fmt.Errorf("orcid digit at position %d is not 0-9: %q", i, c)
		}
		total = (total + int(c-'0')) * 2
	}
	remainder := total % 11
	expected := (12 - remainder) % 11
	got := s[15]
	if !isORCIDLastChar(got) {
		return false, fmt.Errorf("orcid last char must be 0-9 or X: %q", got)
	}
	var gotVal int
	if got == 'X' {
		gotVal = 10
	} else {
		gotVal = int(got - '0')
	}
	return expected == gotVal, nil
}

// verifyISSN checks the mod-11 check digit on 8-char ISSN payloads
// (last char may be X for value 10). Hyphen between positions 4
// and 5 is optional.
func verifyISSN(raw string) (bool, error) {
	s := stripSeparators(raw)
	s = strings.ToUpper(s)
	if len(s) != 8 {
		return false, fmt.Errorf("issn must be 8 digits; got %d in %q", len(s), s)
	}
	sum := 0
	for i := 0; i < 7; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false, fmt.Errorf("issn digit at position %d is not 0-9: %q", i, c)
		}
		sum += int(c-'0') * (8 - i)
	}
	remainder := sum % 11
	expected := (11 - remainder) % 11
	got := s[7]
	if !isISSNLastChar(got) {
		return false, fmt.Errorf("issn last char must be 0-9 or X: %q", got)
	}
	var gotVal int
	if got == 'X' {
		gotVal = 10
	} else {
		gotVal = int(got - '0')
	}
	return expected == gotVal, nil
}

// verifyISBN10 checks mod-11 with descending weights 10..2. Last
// char may be X for value 10.
func verifyISBN10(raw string) (bool, error) {
	s := stripSeparators(raw)
	s = strings.ToUpper(s)
	if len(s) != 10 {
		return false, fmt.Errorf("isbn10 must be 10 digits; got %d in %q", len(s), s)
	}
	sum := 0
	for i := 0; i < 9; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false, fmt.Errorf("isbn10 digit at position %d is not 0-9: %q", i, c)
		}
		sum += int(c-'0') * (10 - i)
	}
	got := s[9]
	if !isISSNLastChar(got) {
		return false, fmt.Errorf("isbn10 last char must be 0-9 or X: %q", got)
	}
	var gotVal int
	if got == 'X' {
		gotVal = 10
	} else {
		gotVal = int(got - '0')
	}
	sum += gotVal
	return sum%11 == 0, nil
}

// verifyISBN13 checks the EAN-13 style mod-10 with alternating
// weights 1,3.
func verifyISBN13(raw string) (bool, error) {
	s := stripSeparators(raw)
	if len(s) != 13 {
		return false, fmt.Errorf("isbn13 must be 13 digits; got %d in %q", len(s), s)
	}
	sum := 0
	for i := 0; i < 13; i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return false, fmt.Errorf("isbn13 digit at position %d is not 0-9: %q", i, c)
		}
		w := 1
		if i%2 == 1 {
			w = 3
		}
		sum += int(c-'0') * w
	}
	return sum%10 == 0, nil
}

// verifyRORID checks a Research Organization Registry identifier.
// Structure per https://ror.readme.io/: 9 chars, leading '0', then
// 6 characters of base32-Crockford payload, then 2-digit ISO 7064
// MOD 97-10 checksum. The checksum is computed from the DECODED
// integer, not the payload string:
//
//	check = 98 - (n * 100) mod 97
//
// where n is the payload's Crockford-base32 value. Accepts the
// "https://ror.org/" prefix and is case-insensitive; strict on the
// alphabet (I/L/O/U are not aliased since a curator typo containing
// them is more likely a mistake than an intended alternate form).
func verifyRORID(raw string) (bool, error) {
	s := stripRORIDPrefix(raw)
	s = strings.ToLower(s)
	if len(s) != 9 {
		return false, fmt.Errorf("rorid must be 9 chars; got %d in %q", len(s), s)
	}
	if s[0] != '0' {
		return false, fmt.Errorf("rorid must start with '0'; got %q", s[0])
	}
	var n int64
	for i := 1; i <= 6; i++ {
		d, ok := crockfordValue(s[i])
		if !ok {
			return false, fmt.Errorf("rorid char at position %d not in base32-crockford alphabet: %q", i, s[i])
		}
		n = n*32 + d
	}
	if s[7] < '0' || s[7] > '9' || s[8] < '0' || s[8] > '9' {
		return false, fmt.Errorf("rorid check digits must be 0-9: %q", s[7:9])
	}
	got := int64(s[7]-'0')*10 + int64(s[8]-'0')
	expected := 98 - (n*100)%97
	return expected == got, nil
}

// crockfordValue returns the Crockford base32 value of c (lowercase).
// Only the canonical alphabet is accepted — I/L/O/U aliases are
// rejected so a curator ORCID-vs-ROR mix-up doesn't silently pass.
func crockfordValue(c byte) (int64, bool) {
	switch {
	case c >= '0' && c <= '9':
		return int64(c - '0'), true
	case c == 'a':
		return 10, true
	case c == 'b':
		return 11, true
	case c == 'c':
		return 12, true
	case c == 'd':
		return 13, true
	case c == 'e':
		return 14, true
	case c == 'f':
		return 15, true
	case c == 'g':
		return 16, true
	case c == 'h':
		return 17, true
	case c == 'j':
		return 18, true
	case c == 'k':
		return 19, true
	case c == 'm':
		return 20, true
	case c == 'n':
		return 21, true
	case c == 'p':
		return 22, true
	case c == 'q':
		return 23, true
	case c == 'r':
		return 24, true
	case c == 's':
		return 25, true
	case c == 't':
		return 26, true
	case c == 'v':
		return 27, true
	case c == 'w':
		return 28, true
	case c == 'x':
		return 29, true
	case c == 'y':
		return 30, true
	case c == 'z':
		return 31, true
	}
	return 0, false
}

// stripRORIDPrefix drops the "https://ror.org/" or "http://ror.org/"
// prefix so the checker accepts URLs as well as bare IDs.
func stripRORIDPrefix(s string) string {
	s = strings.TrimPrefix(s, "https://ror.org/")
	s = strings.TrimPrefix(s, "http://ror.org/")
	return s
}

// verifyLuhn is the mod-10 doubling algorithm used by credit
// cards, IMEI, and other consumer-facing identifiers.
func verifyLuhn(raw string) (bool, error) {
	s := stripSeparators(raw)
	if len(s) < 2 {
		return false, fmt.Errorf("luhn payload must be at least 2 digits; got %d in %q", len(s), s)
	}
	sum := 0
	double := false
	// Walk right-to-left; double every second digit starting from
	// the second-to-last.
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if c < '0' || c > '9' {
			return false, fmt.Errorf("luhn digit at position %d is not 0-9: %q", i, c)
		}
		d := int(c - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0, nil
}

// stripSeparators removes hyphens and whitespace commonly used to
// group identifier segments for readability.
func stripSeparators(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '-' || r == ' ' || r == '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// stripORCIDPrefix removes the "https://orcid.org/" or
// "http://orcid.org/" prefix that ORCID identifiers often carry in
// linked-data contexts.
func stripORCIDPrefix(s string) string {
	s = strings.TrimPrefix(s, "https://orcid.org/")
	s = strings.TrimPrefix(s, "http://orcid.org/")
	return s
}

func isORCIDLastChar(c byte) bool { return (c >= '0' && c <= '9') || c == 'X' }
func isISSNLastChar(c byte) bool  { return (c >= '0' && c <= '9') || c == 'X' }
