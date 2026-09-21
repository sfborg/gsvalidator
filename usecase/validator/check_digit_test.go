package validator

import (
	"testing"

	"github.com/sfborg/gsvalidator/domain"
)

// Known-good identifiers and known-bad variants (transposed or
// off-by-one on the check digit) chosen so the algorithm catches
// realistic typos.
var checkDigitCases = []struct {
	name      string
	algorithm string
	value     string
	want      bool // true = valid check digit
}{
	// ORCID — https://orcid.org/0000-0002-1825-0097 is a real ORCID.
	{"orcid_valid_bare", "orcid", "0000-0002-1825-0097", true},
	{"orcid_valid_url", "orcid", "https://orcid.org/0000-0002-1825-0097", true},
	{"orcid_valid_no_hyphens", "orcid", "0000000218250097", true},
	{"orcid_transposed", "orcid", "0000-0002-1852-0097", false},
	{"orcid_off_by_one", "orcid", "0000-0002-1825-0098", false},
	// Verified ORCID that ends in X: 0000-0002-1694-233X (real).
	{"orcid_x_check", "orcid", "0000-0002-1694-233X", true},

	// ISSN — 2049-3630 (Nature), 0028-0836 (Nature classic).
	{"issn_valid_a", "issn", "2049-3630", true},
	{"issn_valid_b", "issn", "0028-0836", true},
	{"issn_transposed", "issn", "0028-0386", false}, // swap 83 <-> 38
	{"issn_off_by_one", "issn", "0028-0837", false},
	// 0378-5955 (Hearing Research) has an X check when constructed
	// from certain payloads — use 0317-8471 which validates.
	{"issn_valid_c", "issn", "0317-8471", true},

	// ISBN-10 — 0-306-40615-2 (well-known example).
	{"isbn10_valid", "isbn10", "0306406152", true},
	{"isbn10_valid_hyphens", "isbn10", "0-306-40615-2", true},
	{"isbn10_transposed", "isbn10", "0-306-46015-2", false},
	{"isbn10_off_by_one", "isbn10", "0306406153", false},
	// X-terminated ISBN-10 sample: 0-8044-2957-X (Ada 95 Rationale).
	{"isbn10_x_check", "isbn10", "080442957X", true},

	// ISBN-13 — 978-0-306-40615-7 (same book, ISBN-13 form).
	{"isbn13_valid", "isbn13", "9780306406157", true},
	{"isbn13_valid_hyphens", "isbn13", "978-0-306-40615-7", true},
	{"isbn13_off_by_one", "isbn13", "9780306406158", false},
	{"isbn13_transposed", "isbn13", "9780306046157", false}, // 40 <-> 04

	// Luhn — well-known test card 4111 1111 1111 1111.
	{"luhn_valid", "luhn", "4111 1111 1111 1111", true},
	{"luhn_off_by_one", "luhn", "4111 1111 1111 1112", false},

	// RORID — MIT is https://ror.org/05dxps055 per ROR's own docs.
	{"rorid_valid_mit_bare", "rorid", "05dxps055", true},
	{"rorid_valid_mit_url", "rorid", "https://ror.org/05dxps055", true},
	{"rorid_off_by_one", "rorid", "05dxps054", false},
	{"rorid_transposed_check", "rorid", "05dxps505", false},
	{"rorid_transposed_payload", "rorid", "05dxsp055", false},
	{"rorid_wrong_prefix_char", "rorid", "15dxps055", false},
	{"rorid_case_insensitive", "rorid", "05DXPS055", true},
}

func TestVerifyCheckDigit(t *testing.T) {
	for _, c := range checkDigitCases {
		t.Run(c.name, func(t *testing.T) {
			got, err := verifyCheckDigit(c.algorithm, c.value)
			if err != nil {
				if c.want {
					t.Errorf("expected %s valid; error: %v", c.algorithm, err)
				}
				return
			}
			if got != c.want {
				t.Errorf("verify(%s, %q) = %v, want %v", c.algorithm, c.value, got, c.want)
			}
		})
	}
}

func TestVerifyCheckDigit_UnknownAlgorithm(t *testing.T) {
	_, err := verifyCheckDigit("mystery_algo", "1234")
	if err == nil {
		t.Fatal("expected error for unknown algorithm")
	}
}

func TestVerifyCheckDigit_WrongLength(t *testing.T) {
	cases := []struct {
		algorithm, value string
	}{
		{"orcid", "0000-0002-1825"},
		{"issn", "2049"},
		{"isbn10", "030640615"},
		{"isbn13", "978030640615"},
	}
	for _, c := range cases {
		t.Run(c.algorithm, func(t *testing.T) {
			_, err := verifyCheckDigit(c.algorithm, c.value)
			if err == nil {
				t.Errorf("expected length error for %s %q", c.algorithm, c.value)
			}
		})
	}
}

// TestCheckDigit_SeparatorValidatesEachToken covers the separator
// parameter used by fields that carry multiple identifiers in one
// value (e.g. a journal's print + electronic ISSNs joined as
// "1175-5326, 1175-5334"). Each token must validate independently
// for the field to pass; one invalid token fails the rule.
func TestCheckDigit_SeparatorValidatesEachToken(t *testing.T) {
	v := NewCheckDigitValidator()
	rule := &domain.Rule{
		FieldName: "col__issn",
		Parameters: map[string]interface{}{
			"algorithm": "issn",
			"separator": ",",
		},
	}
	// Both tokens valid — passes.
	ctx := &domain.ValidationContext{Record: map[string]interface{}{
		"col__issn": "1175-5326, 1175-5334",
	}}
	res, err := v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Passed {
		t.Errorf("two valid ISSNs should pass; got failure %q", res.Message)
	}
	// One token invalid — whole field fails.
	ctx = &domain.ValidationContext{Record: map[string]interface{}{
		"col__issn": "1175-5326, 1175-5333",
	}}
	res, err = v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if res.Passed {
		t.Errorf("second ISSN has wrong check digit; field should fail")
	}
	// Trailing separator + whitespace — the empty token gets
	// dropped, still passes on the single valid ISSN.
	ctx = &domain.ValidationContext{Record: map[string]interface{}{
		"col__issn": "1175-5326,",
	}}
	res, err = v.Validate(ctx, rule)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if !res.Passed {
		t.Errorf("trailing-separator input should pass; got failure %q", res.Message)
	}
}

func TestStripSeparators(t *testing.T) {
	if got := stripSeparators("0000-0002 1825\t0097"); got != "0000000218250097" {
		t.Errorf("stripSeparators = %q", got)
	}
}

func TestStripORCIDPrefix(t *testing.T) {
	if got := stripORCIDPrefix("https://orcid.org/0000-0002-1825-0097"); got != "0000-0002-1825-0097" {
		t.Errorf("https prefix = %q", got)
	}
	if got := stripORCIDPrefix("http://orcid.org/0000-0002-1825-0097"); got != "0000-0002-1825-0097" {
		t.Errorf("http prefix = %q", got)
	}
	if got := stripORCIDPrefix("0000-0002-1825-0097"); got != "0000-0002-1825-0097" {
		t.Errorf("no prefix = %q", got)
	}
}
