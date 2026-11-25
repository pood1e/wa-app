package wire

import modelv1 "github.com/pood1e/wa-app/gen/go/byte/v/forge/waapp/model/v1"

func AndroidPackageName(kind modelv1.WAAccountType) string {
	if kind == modelv1.WAAccountType_WA_ACCOUNT_TYPE_BUSINESS {
		return "com.whatsapp.w4b"
	}
	return "com.whatsapp"
}
