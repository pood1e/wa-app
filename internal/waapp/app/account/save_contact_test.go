package account

import "testing"

func TestContactPhoneNumberRequiresInternationalNumber(t *testing.T) {
	for _, value := range []string{"+1 (202) 555-0124", "12025550124", "  +1 202-555-0124  "} {
		if got, err := contactPhoneNumber(value); err != nil || got != "12025550124" {
			t.Fatalf("contactPhoneNumber(%q) = %q, %v", value, got, err)
		}
	}
	for _, value := range []string{"", "2025550124", "+123", "+1 202-555-0124 ext 12", "+1 202-555-0124@lid", "abc12025550124", "+1+2025550124"} {
		if got, err := contactPhoneNumber(value); err == nil || got != "" {
			t.Fatalf("contactPhoneNumber(%q) accepted an invalid contact reference", value)
		}
	}
}
