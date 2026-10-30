package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestOTPRequiresFreshMatchingPrivateInput(t *testing.T) {
	now := time.Date(2026, 10, 18, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name           string
		received       time.Time
		fresh, matches bool
		mode           os.FileMode
		want           bool
	}{
		{"fresh match", now, true, true, 0600, true},
		{"stale message", now.Add(-time.Minute), true, true, 0600, false},
		{"unmatched number", now, true, false, 0600, false},
		{"old baseline", now, false, true, 0600, false},
		{"public file", now, true, true, 0644, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "otp.json")
			data, err := json.Marshal(struct {
				Code          string    `json:"code"`
				ReceivedAt    time.Time `json:"received_at"`
				Fresh         bool      `json:"fresh"`
				NumberMatches bool      `json:"number_matches"`
			}{"123456", test.received, test.fresh, test.matches})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, test.mode); err != nil {
				t.Fatal(err)
			}
			code, err := readOTP(path, now)
			if (err == nil) != test.want || (test.want && code != "123456") {
				t.Fatal("OTP freshness, number binding or private file check failed")
			}
		})
	}
}
