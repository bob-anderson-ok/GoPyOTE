package main

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Global variable to hold loaded light curve data
var loadedLightCurveData *LightCurveData

// Flag to track if normalization has been applied (for filename generation)
var normalizationApplied bool

// Flag to track if the baseline has been scaled to unity via calcBaselineMeanBtn.
// Reset on a new CSV load; NOT cleared by cosmetic plot state changes.
var baselineScaledToUnity bool

// Flag to track if a trim has been performed (for the fit tab warning).
var trimPerformed bool

// parseLightCurveCSV reads a CSV file, skipping comments and blank lines,
// and extracts light curve data
func parseLightCurveCSV(filePath string) (*LightCurveData, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			fmt.Printf("Warning: failed to close file: %v\n", cerr)
		}
	}()

	scanner := bufio.NewScanner(file)
	var dataLines []string
	var headerLine string
	var skippedLines []string

	// Read lines, accumulating header lines until we find a line starting with "FrameNum," or "FrameNo"
	foundHeader := false
	for scanner.Scan() {
		line := scanner.Text()
		if !foundHeader {
			if strings.HasPrefix(line, "FrameNum,") || strings.HasPrefix(line, "FrameNo") {
				headerLine = line
				foundHeader = true
			} else {
				skippedLines = append(skippedLines, strings.TrimRight(line, ","))
			}
		} else {
			dataLines = append(dataLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	if headerLine == "" {
		return nil, fmt.Errorf("no header line starting with 'FrameNum,' or 'FrameNo' found")
	}

	// Parse header to get column names
	headerReader := csv.NewReader(strings.NewReader(headerLine))
	headers, err := headerReader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to parse header: %w", err)
	}

	if len(headers) < 3 {
		return nil, fmt.Errorf("CSV must have at least 3 columns (index, time, and data)")
	}

	// Initialize data structure
	data := &LightCurveData{
		TimeValues:     make([]float64, 0, len(dataLines)),
		FrameNumbers:   make([]float64, 0, len(dataLines)),
		Columns:        make([]LightCurveColumn, len(headers)-2), // Exclude index and time columns
		SkippedLines:   skippedLines,
		HeaderLine:     headerLine,
		SourceFilePath: filePath,
	}

	// Set column names (skip the first two: index and time)
	for i := 2; i < len(headers); i++ {
		data.Columns[i-2].Name = headers[i]
		data.Columns[i-2].Values = make([]float64, 0, len(dataLines))
	}

	// Parse data lines
	var prevTime float64
	for _, line := range dataLines {
		lineReader := csv.NewReader(strings.NewReader(line))
		record, err := lineReader.Read()
		if err != nil {
			continue // Skip malformed lines
		}

		if len(record) < len(headers) {
			continue // Skip incomplete lines
		}

		// Parse frame number (first column)
		frameNum, err := strconv.ParseFloat(record[0], 64)
		if err != nil {
			frameNum = float64(len(data.FrameNumbers)) // Use index as a fallback
		}
		data.FrameNumbers = append(data.FrameNumbers, frameNum)

		// Decode timestamp (second column)
		timeVal := decodeTimestamp(record[1], prevTime)
		data.TimeValues = append(data.TimeValues, timeVal)
		prevTime = timeVal

		// Parse data columns (skip the first two)
		for i := 2; i < len(headers); i++ {
			val, err := strconv.ParseFloat(record[i], 64)
			if err != nil {
				val = 0
			}
			data.Columns[i-2].Values = append(data.Columns[i-2].Values, val)
		}
	}

	return data, nil
}

// writeSelectedLightCurves writes the selected light curves to a CSV file
// The output file is named originalname + "_GoPyOTE.csv" in the same directory
// If normalization has been applied, "_NORMALIZED" is inserted in the filename.
// Only rows within the frame range [startFrame, endFrame] are written
func writeSelectedLightCurves(data *LightCurveData, selectedColumns map[int]bool, startFrame, endFrame float64) (string, error) {
	if data == nil {
		return "", fmt.Errorf("no light curve data loaded")
	}
	if len(selectedColumns) == 0 {
		return "", fmt.Errorf("no light curves selected")
	}

	// Build output file path: insert "_GoPyOTE" (and "_NORMALIZED" if applicable) before .csv
	dir := filepath.Dir(data.SourceFilePath)
	base := filepath.Base(data.SourceFilePath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	suffix := "_GoPyOTE"
	if normalizationApplied {
		suffix = "_NORMALIZED_GoPyOTE"
	}
	outputPath := filepath.Join(dir, nameWithoutExt+suffix+ext)

	// Create the output file
	file, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("failed to create output file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			fmt.Printf("Warning: failed to close output file: %v\n", cerr)
		}
	}()

	writer := bufio.NewWriter(file)

	// Write skipped lines (comments) first
	for _, line := range data.SkippedLines {
		if _, err := fmt.Fprintln(writer, line); err != nil {
			return "", fmt.Errorf("failed to write comment line: %w", err)
		}
	}

	// Build a new header with only selected columns
	// Parse the original header to get column names
	headerReader := csv.NewReader(strings.NewReader(data.HeaderLine))
	headers, err := headerReader.Read()
	if err != nil {
		return "", fmt.Errorf("failed to parse header: %w", err)
	}

	// Build selected header: Frame No., Timestamp, then selected columns
	var selectedHeaders []string
	selectedHeaders = append(selectedHeaders, headers[0], headers[1]) // Frame No. and Timestamp
	var selectedIndices []int
	for i := 0; i < len(data.Columns); i++ {
		if selectedColumns[i] {
			selectedHeaders = append(selectedHeaders, headers[i+2]) // +2 to skip Frame No. and Timestamp
			selectedIndices = append(selectedIndices, i)
		}
	}

	// Write header
	if _, err := fmt.Fprintln(writer, strings.Join(selectedHeaders, ",")); err != nil {
		return "", fmt.Errorf("failed to write header: %w", err)
	}

	// Write data rows (filtered by frame range)
	for rowIdx := 0; rowIdx < len(data.FrameNumbers); rowIdx++ {
		frameNum := data.FrameNumbers[rowIdx]

		// Filter by frame range
		if startFrame > 0 && frameNum < startFrame {
			continue
		}
		if endFrame > 0 && frameNum > endFrame {
			continue
		}

		var row []string

		// Frame number
		row = append(row, fmt.Sprintf("%.0f", frameNum))

		// Timestamp - format as [hh:mm:ss.ssss]
		row = append(row, "["+formatSecondsAsTimestamp(data.TimeValues[rowIdx])+"]")

		// Selected data columns
		for _, colIdx := range selectedIndices {
			row = append(row, fmt.Sprintf("%g", data.Columns[colIdx].Values[rowIdx]))
		}

		if _, err := fmt.Fprintln(writer, strings.Join(row, ",")); err != nil {
			return "", fmt.Errorf("failed to write data row: %w", err)
		}
	}

	if err := writer.Flush(); err != nil {
		return "", fmt.Errorf("failed to write output file: %w", err)
	}

	return outputPath, nil
}
