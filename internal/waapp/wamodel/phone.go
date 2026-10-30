package wamodel

import (
	"strings"

	waappv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/v1"
)

func NormalizePhone(phone *waappv1.PhoneTarget) *waappv1.PhoneTarget {
	if phone == nil {
		return &waappv1.PhoneTarget{}
	}
	cc := strings.TrimPrefix(strings.TrimSpace(phone.GetCountryCallingCode()), "+")
	national := strings.TrimSpace(phone.GetNationalNumber())
	e164 := strings.TrimSpace(phone.GetE164Number())
	if e164 == "" && cc != "" && national != "" {
		e164 = "+" + cc + national
	}
	if e164 != "" && !strings.HasPrefix(e164, "+") {
		e164 = "+" + e164
	}
	return &waappv1.PhoneTarget{E164Number: e164, CountryCallingCode: cc, NationalNumber: national, CountryIso2: strings.ToUpper(strings.TrimSpace(phone.GetCountryIso2()))}
}
