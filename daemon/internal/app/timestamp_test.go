package app

import (
	"testing"
	"time"
)

func TestFormatControlUTCTimestamp(t *testing.T) {
	tests := []struct {
		name  string
		value time.Time
		want  string
	}{
		{
			name:  "truncates nanoseconds",
			value: time.Date(2026, 9, 13, 12, 34, 56, 987_654_321, time.UTC),
			want:  "2026-09-13T12:34:56.987654Z",
		},
		{
			name:  "normalizes non UTC location",
			value: time.Date(2026, 9, 13, 20, 34, 56, 1_200_999, time.FixedZone("UTC+08", 8*60*60)),
			want:  "2026-09-13T12:34:56.001200Z",
		},
		{
			name:  "pads trailing zeroes",
			value: time.Date(2026, 9, 13, 12, 34, 56, 1_200_000, time.UTC),
			want:  "2026-09-13T12:34:56.001200Z",
		},
		{
			name:  "pads zero fraction",
			value: time.Date(2026, 9, 13, 12, 34, 56, 0, time.UTC),
			want:  "2026-09-13T12:34:56.000000Z",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := formatControlUTCTimestamp(test.value); got != test.want {
				t.Fatalf("formatControlUTCTimestamp(%s) = %q, want %q", test.value, got, test.want)
			}
		})
	}
}
