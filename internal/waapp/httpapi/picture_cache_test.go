package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestProfilePicturesHonorBrowserValidators(t *testing.T) {
	for _, test := range []struct {
		name, validator string
		status          int
	}{
		{"fresh", "", http.StatusOK},
		{"unchanged", `"version"`, http.StatusNotModified},
		{"weak_validator", `W/"version"`, http.StatusNotModified},
		{"changed", `"old-version"`, http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/picture", nil)
			r.Header.Set("If-None-Match", test.validator)
			w := httptest.NewRecorder()
			writeProfilePicture(w, r, "image/jpeg", "version", "private, max-age=3600", []byte("fixture-image"))
			if w.Code != test.status || w.Header().Get("Cache-Control") != "private, max-age=3600" || w.Header().Get("ETag") != `"version"` {
				t.Fatal("picture response lost its cache policy or validator")
			}
			if test.status == http.StatusNotModified && w.Body.Len() != 0 {
				t.Fatal("unchanged picture was downloaded again")
			}
		})
	}
}
