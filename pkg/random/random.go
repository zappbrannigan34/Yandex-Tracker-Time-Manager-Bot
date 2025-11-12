package random

import (
	"math"
	"math/rand"
	"time"
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Randomize applies ±percent randomization to value
// Example: Randomize(100, 1.0) returns value in range [99, 101]
func Randomize(value float64, percent float64) float64 {
	if percent <= 0 {
		return value
	}

	// Calculate variance
	variance := value * (percent / 100.0)

	// Generate random offset in range [-variance, +variance]
	offset := (rand.Float64()*2 - 1) * variance

	// Apply offset and round to reasonable precision
	result := value + offset
	return math.Round(result*100) / 100
}

// RandomizeInt applies ±percent randomization to int value
func RandomizeInt(value int, percent float64) int {
	result := Randomize(float64(value), percent)
	return int(math.Round(result))
}

// SelectRandomDays selects n random days from Monday to Friday
// Returns slice of weekday indices (0=Monday, 1=Tuesday, ..., 4=Friday)
func SelectRandomDays(n int) []int {
	if n <= 0 || n > 5 {
		return []int{}
	}

	// Create slice [0, 1, 2, 3, 4] for Mon-Fri
	days := []int{0, 1, 2, 3, 4}

	// Shuffle using Fisher-Yates algorithm
	for i := len(days) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		days[i], days[j] = days[j], days[i]
	}

	// Return first n days
	return days[:n]
}

// SelectRandomWeekdayDates selects n random weekday dates from the given week
// week: time.Time representing any day in the week
// n: number of random days to select
// Returns slice of dates (Monday-Friday only)
func SelectRandomWeekdayDates(week time.Time, n int) []time.Time {
	if n <= 0 || n > 5 {
		return []time.Time{}
	}

	// Get Monday of the week
	weekday := int(week.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday = 7
	}
	daysFromMonday := weekday - 1
	monday := week.AddDate(0, 0, -daysFromMonday)

	// Select random day indices
	selectedIndices := SelectRandomDays(n)

	// Convert to dates
	dates := make([]time.Time, n)
	for i, dayIndex := range selectedIndices {
		dates[i] = monday.AddDate(0, 0, dayIndex)
	}

	return dates
}

// SelectRandomItems selects n random items from slice
// Returns indices of selected items
func SelectRandomItems(totalCount, n int) []int {
	if n <= 0 || totalCount <= 0 {
		return []int{}
	}

	if n >= totalCount {
		// Return all indices
		indices := make([]int, totalCount)
		for i := range indices {
			indices[i] = i
		}
		return indices
	}

	// Create slice of all indices
	allIndices := make([]int, totalCount)
	for i := range allIndices {
		allIndices[i] = i
	}

	// Shuffle using Fisher-Yates algorithm
	for i := len(allIndices) - 1; i > 0; i-- {
		j := rand.Intn(i + 1)
		allIndices[i], allIndices[j] = allIndices[j], allIndices[i]
	}

	// Return first n indices
	return allIndices[:n]
}

// DistributeWithRandomization distributes total value across n items with randomization
// Each item gets approximately total/n with ±randomizationPercent variance
// Returns slice of n values that sum to approximately total
func DistributeWithRandomization(total float64, n int, randomizationPercent float64) []float64 {
	if n <= 0 {
		return []float64{}
	}

	if n == 1 {
		return []float64{total}
	}

	// Base value per item
	baseValue := total / float64(n)

	// Generate randomized values
	values := make([]float64, n)
	sum := 0.0
	for i := 0; i < n; i++ {
		values[i] = Randomize(baseValue, randomizationPercent)
		sum += values[i]
	}

	// Adjust to match total exactly (distribute the difference proportionally)
	if sum > 0 {
		factor := total / sum
		for i := range values {
			values[i] *= factor
			values[i] = math.Round(values[i]*100) / 100 // Round to 2 decimal places
		}
	}

	return values
}
