package tracker

import (
	"testing"
)

func TestParseISO8601Duration(t *testing.T) {
	tests := []struct {
		name     string
		duration string
		want     float64
		wantErr  bool
	}{
		{"8 hours", "PT8H", 480, false},
		{"1 hour 30 minutes", "PT1H30M", 90, false},
		{"45 minutes", "PT45M", 45, false},
		{"30 minutes", "PT30M", 30, false},
		{"10 minutes", "PT10M", 10, false},
		{"3 weeks", "P3W", 3 * 7 * 24 * 60, false},
		{"Empty string", "", 0, true},
		{"Invalid format", "INVALID", 0, true},
		{"No P prefix", "T8H", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseISO8601Duration(tt.duration)

			if (err != nil) != tt.wantErr {
				t.Errorf("ParseISO8601Duration(%q) error = %v, wantErr %v",
					tt.duration, err, tt.wantErr)
				return
			}

			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseISO8601Duration(%q) = %v, want %v",
					tt.duration, got, tt.want)
			}
		})
	}
}

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name    string
		minutes float64
		want    string
	}{
		{"8 hours", 480, "PT8H"},
		{"1 hour 30 minutes", 90, "PT1H30M"},
		{"45 minutes", 45, "PT45M"},
		{"30 minutes", 30, "PT30M"},
		{"10 minutes", 10, "PT10M"},
		{"0 minutes", 0, "PT0M"},
		{"2 hours exactly", 120, "PT2H"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatDuration(tt.minutes)

			if got != tt.want {
				t.Errorf("FormatDuration(%v) = %v, want %v",
					tt.minutes, got, tt.want)
			}
		})
	}
}

func TestFormatDurationRoundTrip(t *testing.T) {
	// Test that format -> parse -> format gives same result
	tests := []float64{480, 90, 45, 30, 10, 120, 60}

	for _, minutes := range tests {
		formatted := FormatDuration(minutes)
		parsed, err := ParseISO8601Duration(formatted)

		if err != nil {
			t.Errorf("Round trip failed for %v minutes: parse error %v",
				minutes, err)
			continue
		}

		if parsed != minutes {
			t.Errorf("Round trip failed for %v minutes: got %v",
				minutes, parsed)
		}
	}
}
