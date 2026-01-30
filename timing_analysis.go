package main

import (
	"fmt"
	"math"
	"sort"
)

// TimingError represents a timing anomaly in the light curve data
type TimingError struct {
	Index        int     // Index in the data where the error occurs (after the time step)
	ErrorType    string  // "cadence", "dropped", or "ocr"
	TimeStep     float64 // Actual time step observed
	ExpectedStep float64 // Expected (median) time step
	Ratio        float64 // Ratio of actual to expected
	DroppedCount int     // Number of frames dropped (only for dropped frame errors)
}

// TimingAnalysisResult contains the results of timing analysis
type TimingAnalysisResult struct {
	MedianTimeStep     float64
	CadenceErrors      []TimingError
	DroppedFrameErrors []TimingError
	OCRErrors          []TimingError // Negative time deltas not due to midnight (OCR misread)
	InterpolatedCount  int           // Number of points interpolated
	OCRFixedCount      int           // Number of timestamps fixed due to OCR errors
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

	// Analyze each time step for errors
	for i, step := range timeSteps {
		ratio := step / result.MedianTimeStep

		// OCR error: negative time delta that's not a midnight crossing,
		// which would be a large negative jump (handled in decodeTimestamp),
		// Small negative jumps indicate OCR misread of the timestamp
		if step < 0 {
			result.OCRErrors = append(result.OCRErrors, TimingError{
				Index:        i + 1, // Index of the point with the bad timestamp
				ErrorType:    "ocr",
				TimeStep:     step,
				ExpectedStep: result.MedianTimeStep,
				Ratio:        ratio,
			})
		} else if ratio >= 1.8 {
			// Dropped frame error: ratio >= 1.8 (could be multiple dropped frames)
			droppedCount := int(math.Round(ratio)) - 1 // Number of frames that were dropped
			result.DroppedFrameErrors = append(result.DroppedFrameErrors, TimingError{
				Index:        i + 1, // Index after the gap
				ErrorType:    "dropped",
				TimeStep:     step,
				ExpectedStep: result.MedianTimeStep,
				Ratio:        ratio,
				DroppedCount: droppedCount,
			})
		} else if ratio >= 1.3 || ratio <= 0.7 {
			// Cadence error: ratio is 1.3x or 0.7x normal
			result.CadenceErrors = append(result.CadenceErrors, TimingError{
				Index:        i + 1,
				ErrorType:    "cadence",
				TimeStep:     step,
				ExpectedStep: result.MedianTimeStep,
				Ratio:        ratio,
			})
		}
	}

	return result
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

// Global map to track OCR error indices
var ocrErrorIndices map[int]bool

// resetInterpolatedIndices clears the interpolated indices map
func resetInterpolatedIndices() {
	interpolatedIndices = make(map[int]bool)
}

// resetOCRErrorIndices clears the OCR error indices map
func resetOCRErrorIndices() {
	ocrErrorIndices = make(map[int]bool)
}

// isInterpolatedIndex checks if a given index was interpolated
func isInterpolatedIndex(idx int) bool {
	if interpolatedIndices == nil {
		return false
	}
	return interpolatedIndices[idx]
}

// isOCRErrorIndex checks if a given index had an OCR timestamp error
func isOCRErrorIndex(idx int) bool {
	if ocrErrorIndices == nil {
		return false
	}
	return ocrErrorIndices[idx]
}

// fixOCRTimestampErrors fixes timestamps that have OCR errors (negative time deltas)
// by substituting the expected timestamp (previous time + median step).
// The data values are kept as-is since only the timestamp was misread.
// Returns the number of timestamps fixed.
func fixOCRTimestampErrors(data *LightCurveData, ocrErrors []TimingError, medianTimeStep float64) int {
	if len(ocrErrors) == 0 || data == nil || len(data.TimeValues) < 2 {
		return 0
	}

	fixed := 0
	for _, err := range ocrErrors {
		idx := err.Index
		if idx < 1 || idx >= len(data.TimeValues) {
			continue
		}

		// Calculate expected timestamp: previous time + median step
		expectedTime := data.TimeValues[idx-1] + medianTimeStep

		// Fix the timestamp
		data.TimeValues[idx] = expectedTime

		// Mark this index as having an OCR error (for display purposes)
		ocrErrorIndices[idx] = true

		fixed++
	}

	return fixed
}

// formatTimingReport creates a human-readable report of timing errors
func formatTimingReport(result *TimingAnalysisResult) string {
	if result == nil {
		return "No timing analysis performed."
	}

	var report string

	report += fmt.Sprintf("Median time step: %.4f seconds\n\n", result.MedianTimeStep)

	if len(result.CadenceErrors) == 0 && len(result.DroppedFrameErrors) == 0 && len(result.OCRErrors) == 0 {
		report += "No timing errors detected."
		return report
	}

	if len(result.OCRErrors) > 0 {
		report += fmt.Sprintf("OCR TIMESTAMP ERRORS (%d found):\n", len(result.OCRErrors))
		for i, err := range result.OCRErrors {
			if i >= 10 {
				report += fmt.Sprintf("  ... and %d more\n", len(result.OCRErrors)-10)
				break
			}
			report += fmt.Sprintf("  At index %d: negative step=%.4fs\n",
				err.Index, err.TimeStep)
		}
		if result.OCRFixedCount > 0 {
			report += fmt.Sprintf("  %d timestamps corrected (marked with black circles).\n", result.OCRFixedCount)
		}
		report += "\n"
	}

	if len(result.CadenceErrors) > 0 {
		report += fmt.Sprintf("CADENCE ERRORS (%d found):\n", len(result.CadenceErrors))
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
