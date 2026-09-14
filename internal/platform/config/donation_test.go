package config

import (
	"strings"
	"testing"
)

func TestDonationTokenConfigurationIsOptionalAndDistinct(t *testing.T) {
	const admin = "synthetic-administrator-key-2026-abcdef"
	for _, test := range []struct {
		value string
		valid bool
	}{
		{"", true}, {"synthetic-donation-integration-key-abcdef", true},
		{"short", false}, {admin, false}, {strings.Repeat("x", 257), false},
		{strings.Repeat("x", 32) + " ", false}, {strings.Repeat("x", 32) + "\n", false},
	} {
		if err := validateDonationIntegrationToken(test.value, admin); (err == nil) != test.valid {
			t.Errorf("configuration length %d: valid=%t error=%v", len(test.value), test.valid, err)
		}
	}
}
