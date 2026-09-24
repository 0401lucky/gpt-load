package health

import (
	"testing"
	"time"
)

func TestParseResetHintExtractsUpstreamRetryDelay(t *testing.T) {
	tests := []struct {
		name    string
		summary string
		want    time.Duration
	}{
		{
			name:    "production cline limit",
			summary: "Error 429: Daily free limit reached on model z-ai/glm-5.3-flash. Try again in 4h 17m",
			want:    4*time.Hour + 18*time.Minute,
		},
		{
			name:    "hours and minutes",
			summary: "Try again in 3h 18m",
			want:    3*time.Hour + 19*time.Minute,
		},
		{
			name:    "minutes only",
			summary: "Try again in 45m",
			want:    46 * time.Minute,
		},
		{
			name:    "seconds only",
			summary: "Try again in 30s",
			want:    time.Minute + 30*time.Second,
		},
		{
			name:    "seconds beyond a minute",
			summary: "Please try again in 90s",
			want:    2*time.Minute + 30*time.Second,
		},
		{
			name:    "day plus hours clamps to the ceiling",
			summary: "Try again in 1d 2h",
			want:    maxResetHintCooldown,
		},
		{
			name:    "single hour",
			summary: "Try again in 1h",
			want:    time.Hour + time.Minute,
		},
		{
			name:    "concatenated fragments",
			summary: "Try again in 1h30m",
			want:    time.Hour + 31*time.Minute,
		},
		{
			name:    "case insensitive",
			summary: "TRY AGAIN IN 3H 18M",
			want:    3*time.Hour + 19*time.Minute,
		},
		{
			name:    "trailing sentence text",
			summary: "Try again in 45m for this model.",
			want:    46 * time.Minute,
		},
		{
			name:    "weeks clamp to the ceiling",
			summary: "Try again in 2w",
			want:    maxResetHintCooldown,
		},
		{
			name:    "large minute amount clamps to the ceiling instead of shrinking",
			summary: "Try again in 1500m",
			want:    maxResetHintCooldown,
		},
		{
			name:    "large second amount keeps its real magnitude",
			summary: "Try again in 3600s",
			want:    time.Hour + time.Minute,
		},
		{
			name:    "overflowing amount clamps instead of failing",
			summary: "Try again in 99999999999999999999h",
			want:    maxResetHintCooldown,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, ok := ParseResetHint(test.summary)
			if !ok {
				t.Fatalf("ParseResetHint(%q) ok = false, want true", test.summary)
			}
			if got != test.want {
				t.Fatalf("ParseResetHint(%q) = %v, want %v", test.summary, got, test.want)
			}
		})
	}
}

func TestParseResetHintRejectsSummariesWithoutUsableDelay(t *testing.T) {
	for _, summary := range []string{
		"",
		"Error 429: Daily free limit reached on model z-ai/glm-5.3-flash.",
		"Try again in",
		"Try again in soon",
		"Try again in 5",
		"Try again in 0s",
		"Try again in -5m",
		"Try again later in 3h",
		"Retry after 3h 18m",
	} {
		got, ok := ParseResetHint(summary)
		if ok {
			t.Errorf("ParseResetHint(%q) = %v, true; want false", summary, got)
		}
	}
}
