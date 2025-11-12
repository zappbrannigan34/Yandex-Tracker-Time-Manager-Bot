package random

import (
	"math"
	"testing"
)

func TestRandomize(t *testing.T) {
	tests := []struct {
		name    string
		value   float64
		percent float64
		wantMin float64
		wantMax float64
	}{
		{
			name:    "1% randomization of 100",
			value:   100,
			percent: 1.0,
			wantMin: 99,
			wantMax: 101,
		},
		{
			name:    "5% randomization of 80",
			value:   80,
			percent: 5.0,
			wantMin: 76,
			wantMax: 84,
		},
		{
			name:    "0% randomization (no change)",
			value:   50,
			percent: 0,
			wantMin: 50,
			wantMax: 50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Run multiple times to check range
			for i := 0; i < 100; i++ {
				result := Randomize(tt.value, tt.percent)

				if result < tt.wantMin || result > tt.wantMax {
					t.Errorf("Randomize(%v, %v) = %v, want range [%v, %v]",
						tt.value, tt.percent, result, tt.wantMin, tt.wantMax)
				}
			}
		})
	}
}

func TestRandomizeInt(t *testing.T) {
	tests := []struct {
		name    string
		value   int
		percent float64
		wantMin int
		wantMax int
	}{
		{
			name:    "1% randomization of 100",
			value:   100,
			percent: 1.0,
			wantMin: 99,
			wantMax: 101,
		},
		{
			name:    "10% randomization of 50",
			value:   50,
			percent: 10.0,
			wantMin: 45,
			wantMax: 55,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i := 0; i < 100; i++ {
				result := RandomizeInt(tt.value, tt.percent)

				if result < tt.wantMin || result > tt.wantMax {
					t.Errorf("RandomizeInt(%v, %v) = %v, want range [%v, %v]",
						tt.value, tt.percent, result, tt.wantMin, tt.wantMax)
				}
			}
		})
	}
}

func TestSelectRandomDays(t *testing.T) {
	tests := []struct {
		name      string
		n         int
		wantCount int
	}{
		{"Select 2 days", 2, 2},
		{"Select 3 days", 3, 3},
		{"Select 5 days", 5, 5},
		{"Select 0 days", 0, 0},
		{"Select more than 5 days", 6, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SelectRandomDays(tt.n)

			if len(result) != tt.wantCount {
				t.Errorf("SelectRandomDays(%v) returned %v days, want %v",
					tt.n, len(result), tt.wantCount)
			}

			// Check all values are in range [0, 4] (Monday-Friday)
			for _, day := range result {
				if day < 0 || day > 4 {
					t.Errorf("SelectRandomDays(%v) returned day %v, want range [0, 4]",
						tt.n, day)
				}
			}

			// Check uniqueness
			seen := make(map[int]bool)
			for _, day := range result {
				if seen[day] {
					t.Errorf("SelectRandomDays(%v) returned duplicate day %v",
						tt.n, day)
				}
				seen[day] = true
			}
		})
	}
}

func TestSelectRandomDaysDistribution(t *testing.T) {
	// Test that random selection is actually random (statistical test)
	n := 2
	iterations := 1000
	counts := make(map[int]int)

	for i := 0; i < iterations; i++ {
		days := SelectRandomDays(n)
		for _, day := range days {
			counts[day]++
		}
	}

	// Each day should be selected approximately 40% of the time (2 out of 5 days)
	expectedCount := float64(iterations) * float64(n) / 5.0
	tolerance := expectedCount * 0.3 // 30% tolerance

	for day := 0; day < 5; day++ {
		count := counts[day]
		diff := math.Abs(float64(count) - expectedCount)

		if diff > tolerance {
			t.Logf("Day %d selected %d times (expected ~%.0f, tolerance ±%.0f)",
				day, count, expectedCount, tolerance)
		}
	}
}
