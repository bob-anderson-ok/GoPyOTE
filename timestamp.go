package main

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// decodeTimestamp converts a timestamp string like "[03:58:34.6796]" to float64 seconds
// It handles passage through midnight by detecting large backward jumps
func decodeTimestamp(timeStr string, prevTime float64) float64 {
	// Remove brackets if present
	timeStr = strings.Trim(timeStr, "[]")

	// Parse HH:MM:SS.mmmm format
	parts := strings.Split(timeStr, ":")
	if len(parts) != 3 {
		return prevTime
	}

	hours, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return prevTime
	}

	minutes, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return prevTime
	}

	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return prevTime
	}

	// Convert to total seconds
	totalSeconds := hours*3600 + minutes*60 + seconds

	// Handle midnight passage: if time suddenly drops significantly, add 24 hours
	if prevTime > 0 && totalSeconds < prevTime-43200 { // 43200 = 12 hours
		totalSeconds += 86400 // Add 24 hours
	}

	return totalSeconds
}

// formatSecondsAsTimestamp converts float64 seconds to timestamp format [hh:mm:ss.sss]
func formatSecondsAsTimestamp(totalSeconds float64) string {
	// Handle negative values (should not happen but be safe)
	if totalSeconds < 0 {
		totalSeconds = 0
	}

	// Handle values that have wrapped past midnight (> 24 hours)
	totalSeconds = math.Mod(totalSeconds, 86400)

	hours := int(totalSeconds / 3600)
	totalSeconds -= float64(hours) * 3600
	minutes := int(totalSeconds / 60)
	totalSeconds -= float64(minutes) * 60
	seconds := totalSeconds

	return fmt.Sprintf("%02d:%02d:%07.4f", hours, minutes, seconds)
}

// parseTimestampInput parses a timestamp string (hh:mm:ss.sss or hh:mm:ss) to float64 seconds
// Returns the value and true if successful, or 0 and false if parsing fails
func parseTimestampInput(input string) (float64, bool) {
	// Remove any surrounding whitespace and brackets
	input = strings.TrimSpace(input)
	input = strings.Trim(input, "[]")

	// Try to parse as hh:mm:ss.sss format
	parts := strings.Split(input, ":")
	if len(parts) != 3 {
		return 0, false
	}

	hours, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, false
	}

	minutes, err := strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, false
	}

	seconds, err := strconv.ParseFloat(parts[2], 64)
	if err != nil {
		return 0, false
	}

	totalSeconds := hours*3600 + minutes*60 + seconds
	return totalSeconds, true
}
