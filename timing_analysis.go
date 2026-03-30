package main

import (
	"fmt"
	"math"
	"sort"
)

const (
	// droppedFrameRatioThreshold is the minimum ratio of observed/expected time step
	// that indicates one or more frames were dropped.
	droppedFrameRatioThreshold = 1.8

	// cadenceErrorUpperRatio and cadenceErrorLowerRatio define the band outside
	// which a time step is flagged as a cadence error.
	cadenceErrorUpperRatio = 1.3
	cadenceErrorLowerRatio = 0.7
)

// TimingError represents a timing anomaly in the light curve data
type TimingError struct {
	Index        int     // Index in the data where the error occurs (after the time step)
	ErrorType    string  // "cadence" or "dropped"
	TimeStep     float64 // Actual time step observed
	ExpectedStep float64 // Expected (median) time step
	Ratio        float64 // Ratio of actual to expected
	DroppedCount int     // Number of frames dropped (only for dropped frame errors)
}

// TimingAnalysisResult contains the results of timing analysis
type TimingAnalysisResult struct {
	MedianTimeStep      float64
	AverageTimeStep     float64 // Average of valid (non-dropped) time steps
	CadenceErrors       []TimingError
	DroppedFrameErrors  []TimingError
	NegativeDeltaErrors []TimingError // Negative time deltas (not midnight crossing)
	InterpolatedCount   int           // Number of points interpolated
	NegativeDeltaFixed  int           // Number of negative delta timestamps fixed
}

// analyzeTimingErrors examines time values and detects cadence and dropped frame errors
// Returns the analysis result with all errors found
func analyzeTimingErrors(timeValues []float64) *TimingAnalysisResult {
	result := &TimingAnalysisResult{}

	if len(timeValues) < 2 {
		return result
	}

	// Calculate all time steps
	timeSteps := make([]float64, len(timeValues)-1)
	for i := 1; i < len(timeValues); i++ {
		timeSteps[i-1] = timeValues[i] - timeValues[i-1]
	}

	// Calculate the median time step
	sortedSteps := make([]float64, len(timeSteps))
	copy(sortedSteps, timeSteps)
	sort.Float64s(sortedSteps)

	medianIdx := len(sortedSteps) / 2
	if len(sortedSteps)%2 == 0 {
		result.MedianTimeStep = (sortedSteps[medianIdx-1] + sortedSteps[medianIdx]) / 2
	} else {
		result.MedianTimeStep = sortedSteps[medianIdx]
	}

	// Skip analysis if the median is zero or negative (invalid data)
	if result.MedianTimeStep <= 0 {
		return result
	}

	// First pass: identify negative deltas
	droppedIndices := make(map[int]bool)
	negativeIndices := make(map[int]bool)
	for i, step := range timeSteps {
		if step < 0 {
			// Negative time delta (not a midnight crossing - those are handled in decodeTimestamp)
			ratio := step / result.MedianTimeStep
			result.NegativeDeltaErrors = append(result.NegativeDeltaErrors, TimingError{
				Index:        i + 1, // Index of the frame with the bad timestamp
				ErrorType:    "negative",
				TimeStep:     step,
				ExpectedStep: result.MedianTimeStep,
				Ratio:        ratio,
			})
			negativeIndices[i] = true
		}
	}

	// Second pass: identify dropped frames but skip steps that follow a negative delta
	// (those appear artificially large due to the bad timestamp)
	for i, step := range timeSteps {
		if negativeIndices[i] {
			continue // Skip negative deltas
		}
		// Skip if the previous step was a negative delta - the large gap is artificial
		if i > 0 && negativeIndices[i-1] {
			continue
		}
		ratio := step / result.MedianTimeStep
		if ratio >= droppedFrameRatioThreshold {
			// Dropped frame error (could be multiple dropped frames)
			droppedCount := int(math.Round(ratio)) - 1 // Number of frames that were dropped
			result.DroppedFrameErrors = append(result.DroppedFrameErrors, TimingError{
				Index:        i + 1, // Index after the gap
				ErrorType:    "dropped",
				TimeStep:     step,
				ExpectedStep: result.MedianTimeStep,
				Ratio:        ratio,
				DroppedCount: droppedCount,
			})
			droppedIndices[i] = true
		}
	}

	// Calculate an average time step using only valid frames
	// Exclude: dropped frames, negative deltas, and steps following a negative delta
	var validStepSum float64
	var validStepCount int
	for i, step := range timeSteps {
		if droppedIndices[i] || negativeIndices[i] {
			continue
		}
		// Also exclude the step following a negative delta (artificially large)
		if i > 0 && negativeIndices[i-1] {
			continue
		}
		if step > 0 {
			validStepSum += step
			validStepCount++
		}
	}
	if validStepCount > 0 {
		result.AverageTimeStep = validStepSum / float64(validStepCount)
	} else {
		result.AverageTimeStep = result.MedianTimeStep // Fallback to median if no valid steps
	}

	// Second pass: identify cadence errors using average as reference
	for i, step := range timeSteps {
		if droppedIndices[i] || negativeIndices[i] {
			continue // Already identified as a dropped frame or negative delta
		}
		ratio := step / result.AverageTimeStep

		if ratio >= cadenceErrorUpperRatio || ratio <= cadenceErrorLowerRatio {
			// Cadence error: ratio outside normal band
			result.CadenceErrors = append(result.CadenceErrors, TimingError{
				Index:        i + 1,
				ErrorType:    "cadence",
				TimeStep:     step,
				ExpectedStep: result.AverageTimeStep,
				Ratio:        ratio,
			})
		}
	}

	return result
}

// fixNegativeDeltaTimestamps fixes timestamps that have negative deltas by replacing
// them with: previous timestamp + average time step. Only the specific frame with the
// negative delta is fixed, not later frames. Returns the number of timestamps fixed.
func fixNegativeDeltaTimestamps(data *LightCurveData, negativeDeltaErrors []TimingError, averageTimeStep float64) int {
	if len(negativeDeltaErrors) == 0 || data == nil || len(data.TimeValues) < 2 {
		return 0
	}

	fixed := 0
	for _, err := range negativeDeltaErrors {
		idx := err.Index
		if idx < 1 || idx >= len(data.TimeValues) {
			continue
		}

		// Replace the timestamp with: previous timestamp + average time step
		data.TimeValues[idx] = data.TimeValues[idx-1] + averageTimeStep
		fixed++
	}

	return fixed
}

// interpolateDroppedFrames modifies the LightCurveData in place to insert interpolated
// points for dropped frames. Returns the number of points inserted.
func interpolateDroppedFrames(data *LightCurveData, droppedErrors []TimingError) int {
	if len(droppedErrors) == 0 || data == nil || len(data.TimeValues) < 2 {
		return 0
	}

	// Filter valid errors and sort by index in ascending order to calculate final indices
	var validErrors []TimingError
	for _, err := range droppedErrors {
		idxAfterGap := err.Index
		idxBeforeGap := err.Index - 1
		if idxBeforeGap >= 0 && idxAfterGap < len(data.TimeValues) && err.DroppedCount >= 1 {
			validErrors = append(validErrors, err)
		}
	}

	if len(validErrors) == 0 {
		return 0
	}

	// Sort by index ascending to calculate final indices correctly
	sort.Slice(validErrors, func(i, j int) bool {
		return validErrors[i].Index < validErrors[j].Index
	})

	// Calculate final indices for all interpolated points BEFORE any insertions
	// Each insertion at index i shifts all later indices by the number of points inserted
	cumulativeOffset := 0
	for _, err := range validErrors {
		idxAfterGap := err.Index
		numToInsert := err.DroppedCount

		// In the final array, the interpolated points for this gap will be at:
		// (idxAfterGap + cumulativeOffset) through (idxAfterGap + cumulativeOffset + numToInsert - 1)
		finalStartIdx := idxAfterGap + cumulativeOffset
		for i := 0; i < numToInsert; i++ {
			interpolatedIndices[finalStartIdx+i] = true
		}

		cumulativeOffset += numToInsert
	}

	// Now sort by index descending to do the actual insertions
	// (inserting from last to first keeps earlier indices valid)
	sort.Slice(validErrors, func(i, j int) bool {
		return validErrors[i].Index > validErrors[j].Index
	})

	totalInserted := 0

	for _, err := range validErrors {
		// err.Index is the index of the first point AFTER the gap
		idxAfterGap := err.Index
		idxBeforeGap := err.Index - 1
		numToInsert := err.DroppedCount

		// Get the boundary points: the last valid point BEFORE the gap
		// and the first valid point AFTER the gap
		timeBeforeGap := data.TimeValues[idxBeforeGap]
		timeAfterGap := data.TimeValues[idxAfterGap]
		frameBeforeGap := data.FrameNumbers[idxBeforeGap]
		frameAfterGap := data.FrameNumbers[idxAfterGap]

		// Linear interpolation: divide the gap into (numToInsert + 1) equal segments
		timeIncrement := (timeAfterGap - timeBeforeGap) / float64(numToInsert+1)
		frameIncrement := (frameAfterGap - frameBeforeGap) / float64(numToInsert+1)

		// Create slices for interpolated values
		newTimes := make([]float64, numToInsert)
		newFrames := make([]float64, numToInsert)
		newColumnValues := make([][]float64, len(data.Columns))

		for i := 0; i < numToInsert; i++ {
			newTimes[i] = timeBeforeGap + timeIncrement*float64(i+1)
			newFrames[i] = frameBeforeGap + frameIncrement*float64(i+1)
		}

		// Interpolate column values using linear interpolation between
		// the point before the gap and the point after the gap
		for colIdx, col := range data.Columns {
			newColumnValues[colIdx] = make([]float64, numToInsert)
			valBeforeGap := col.Values[idxBeforeGap]
			valAfterGap := col.Values[idxAfterGap]
			valIncrement := (valAfterGap - valBeforeGap) / float64(numToInsert+1)

			for i := 0; i < numToInsert; i++ {
				newColumnValues[colIdx][i] = valBeforeGap + valIncrement*float64(i+1)
			}
		}

		// Insert the interpolated values at idxAfterGap (between the two boundary points)
		data.TimeValues = insertFloat64Slice(data.TimeValues, idxAfterGap, newTimes)
		data.FrameNumbers = insertFloat64Slice(data.FrameNumbers, idxAfterGap, newFrames)
		for colIdx := range data.Columns {
			data.Columns[colIdx].Values = insertFloat64Slice(data.Columns[colIdx].Values, idxAfterGap, newColumnValues[colIdx])
		}

		totalInserted += numToInsert
	}

	return totalInserted
}

// insertFloat64Slice inserts multiple values into a slice at the given position
func insertFloat64Slice(slice []float64, pos int, values []float64) []float64 {
	if pos > len(slice) {
		pos = len(slice)
	}
	result := make([]float64, len(slice)+len(values))
	copy(result[:pos], slice[:pos])
	copy(result[pos:pos+len(values)], values)
	copy(result[pos+len(values):], slice[pos:])
	return result
}

// Global map to track interpolated indices
var interpolatedIndices map[int]bool

// resetInterpolatedIndices clears the interpolated indices map
func resetInterpolatedIndices() {
	interpolatedIndices = make(map[int]bool)
}

// isInterpolatedIndex checks if a given index was interpolated
func isInterpolatedIndex(idx int) bool {
	if interpolatedIndices == nil {
		return false
	}
	return interpolatedIndices[idx]
}

// Global map to track negative delta indices
var negativeDeltaIndices map[int]bool

// resetNegativeDeltaIndices clears the negative delta indices map
func resetNegativeDeltaIndices() {
	negativeDeltaIndices = make(map[int]bool)
}

// markNegativeDeltaIndex marks an index as having a negative delta
func markNegativeDeltaIndex(idx int) {
	if negativeDeltaIndices == nil {
		negativeDeltaIndices = make(map[int]bool)
	}
	negativeDeltaIndices[idx] = true
}

// isNegativeDeltaIndex checks if a given index had a negative delta
func isNegativeDeltaIndex(idx int) bool {
	if negativeDeltaIndices == nil {
		return false
	}
	return negativeDeltaIndices[idx]
}

// formatTimingReport creates a human-readable report of timing errors
func formatTimingReport(result *TimingAnalysisResult) string {
	if result == nil {
		return "No timing analysis performed."
	}

	var report string

	report += fmt.Sprintf("Median time step: %.4f seconds\n", result.MedianTimeStep)
	report += fmt.Sprintf("Average time step (valid frames): %.4f seconds\n\n", result.AverageTimeStep)

	if len(result.CadenceErrors) == 0 && len(result.DroppedFrameErrors) == 0 && len(result.NegativeDeltaErrors) == 0 {
		report += "No timing errors detected."
		return report
	}

	if len(result.NegativeDeltaErrors) > 0 {
		report += fmt.Sprintf("NEGATIVE DELTA ERRORS (%d found):\n", len(result.NegativeDeltaErrors))
		for i, err := range result.NegativeDeltaErrors {
			if i >= 10 {
				report += fmt.Sprintf("  ... and %d more\n", len(result.NegativeDeltaErrors)-10)
				break
			}
			report += fmt.Sprintf("  At index %d: step=%.4fs\n", err.Index, err.TimeStep)
		}
		if result.NegativeDeltaFixed > 0 {
			report += fmt.Sprintf("\n%d timestamps corrected.\n", result.NegativeDeltaFixed)
		}
		report += "\n"
	}

	if len(result.CadenceErrors) > 0 {
		report += fmt.Sprintf("CADENCE OR OCR ERRORS (%d found):\n", len(result.CadenceErrors))
		for i, err := range result.CadenceErrors {
			if i >= 10 {
				report += fmt.Sprintf("  ... and %d more\n", len(result.CadenceErrors)-10)
				break
			}
			report += fmt.Sprintf("  At index %d: step=%.4fs (%.2fx normal)\n",
				err.Index, err.TimeStep, err.Ratio)
		}
		report += "\n"
	}

	if len(result.DroppedFrameErrors) > 0 {
		report += fmt.Sprintf("DROPPED FRAME ERRORS (%d gaps found):\n", len(result.DroppedFrameErrors))
		totalDropped := 0
		for i, err := range result.DroppedFrameErrors {
			totalDropped += err.DroppedCount
			if i >= 10 {
				report += fmt.Sprintf("  ... and %d more gaps\n", len(result.DroppedFrameErrors)-10)
				break
			}
			report += fmt.Sprintf("  At index %d: %d frame(s) dropped (step=%.4fs, %.2fx normal)\n",
				err.Index, err.DroppedCount, err.TimeStep, err.Ratio)
		}
		if result.InterpolatedCount > 0 {
			report += fmt.Sprintf("\n%d interpolated points inserted.\n", result.InterpolatedCount)
		}
	}

	return report
}
