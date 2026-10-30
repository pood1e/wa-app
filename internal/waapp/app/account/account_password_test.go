package account

import "testing"

func TestAccountPasswordMatchesNativeRequirements(t *testing.T) {
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"012345", false},
		{"Abcdefg1", true},
		{"Abcdefghijklmnopqrs1", true},
		{"Abcdefghijklmnopqrst1", false},
		{"abcdefgh", false},
		{"12345678", false},
		{"密碼測試１２３４", true},
		{" Password1 ", true},
		{"😀😀😀😀😀😀😀😀😀a1", true},
		{"😀😀😀😀😀😀😀😀😀😀a1", false},
		{"𐐀1234567", false},
	} {
		if (requireAccountPassword(test.value) == nil) != test.valid {
			t.Fatal("password admission differs from native UTF-16 length or character checks")
		}
	}
}
