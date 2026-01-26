package main

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/KevinWang15/go-json5"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// Version information
const Version = "1.0.20"

// Track the last loaded parameters file path for use by Run IOTAdiffraction
var lastLoadedParamsPath string

// Windows API
var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	procGetForegroundWindow = user32.NewProc("GetForegroundWindow")
	procGetWindowRect       = user32.NewProc("GetWindowRect")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

const swpNoZOrder = 0x0004

func getForegroundWindow() uintptr {
	hwnd, _, _ := procGetForegroundWindow.Call()
	return hwnd
}

func getWindowRect(hwnd uintptr) (x, y, w, h int32, ok bool) {
	var rect winRect
	ret, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		return 0, 0, 0, 0, false
	}
	return rect.Left, rect.Top, rect.Right - rect.Left, rect.Bottom - rect.Top, true
}

func setWindowPos(hwnd uintptr, x, y, w, h int32) bool {
	ret, _, _ := procSetWindowPos.Call(hwnd, 0, uintptr(x), uintptr(y), uintptr(w), uintptr(h), swpNoZOrder)
	return ret != 0
}

// EllipseParams represents ellipse parameters for the main body or satellite
type EllipseParams struct {
	XCenterKm          float64 `json:"x_center_km"`
	YCenterKm          float64 `json:"y_center_km"`
	MajorAxisKm        float64 `json:"major_axis_km"`
	MinorAxisKm        float64 `json:"minor_axis_km"`
	MajorAxisPaDegrees float64 `json:"major_axis_pa_degrees"`
}

// OccultationParameters holds all parameters for occultation calculations
type OccultationParameters struct {
	WindowSizePixels               int           `json:"window_size_pixels"`
	Title                          string        `json:"title"`
	FundamentalPlaneWidthKm        float64       `json:"fundamental_plane_width_km"`
	FundamentalPlaneWidthNumPoints int           `json:"fundamental_plane_width_num_points"`
	ParallaxArcsec                 float64       `json:"parallax_arcsec"`
	DistanceAu                     float64       `json:"distance_au"`
	PathToQeTableFile              string        `json:"path_to_qe_table_file"`
	ObservationWavelengthNm        int           `json:"observation_wavelength_nm"`
	DXKmPerSec                     float64       `json:"dX_km_per_sec"`
	DYKmPerSec                     float64       `json:"dY_km_per_sec"`
	PathPerpendicularOffsetKm      float64       `json:"path_perpendicular_offset_from_center_km"`
	PercentMagDrop                 int           `json:"percent_mag_drop"`
	StarDiamOnPlaneMas             float64       `json:"star_diam_on_plane_mas"`
	LimbDarkeningCoeff             float64       `json:"limb_darkening_coeff"`
	StarClass                      string        `json:"star_class"`
	MainBody                       EllipseParams `json:"main_body"`
	Satellite                      EllipseParams `json:"satellite"`
	PathToExternalImage            string        `json:"path_to_external_image"`
}

// parseOccultationParameters parses a JSON5 parameters file
func parseOccultationParameters(reader io.Reader) (*OccultationParameters, error) {
	content, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var params OccultationParameters
	if err := json5.Unmarshal(content, &params); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	return &params, nil
}

// LightCurveColumn represents a single light curve column from a CSV file
type LightCurveColumn struct {
	Name   string
	Values []float64
}

// LightCurveData holds all parsed data from a light curve CSV file
type LightCurveData struct {
	TimeValues     []float64          // Decoded timestamps as float64 seconds
	FrameNumbers   []float64          // Frame numbers from the first column (used when timestamps empty)
	Columns        []LightCurveColumn // All data columns (excluding index and time)
	SkippedLines   []string           // Comment and blank lines preserved for writing output
	HeaderLine     string             // Original header line for writing output
	SourceFilePath string             // Path to the original CSV file
}

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

	// Read lines, accumulating header lines until we find a line starting with "FrameNum,"
	foundHeader := false
	for scanner.Scan() {
		line := scanner.Text()
		if !foundHeader {
			if strings.HasPrefix(line, "FrameNum,") {
				headerLine = line
				foundHeader = true
			} else {
				skippedLines = append(skippedLines, line)
			}
		} else {
			dataLines = append(dataLines, line)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading file: %w", err)
	}

	if headerLine == "" {
		return nil, fmt.Errorf("no header line starting with 'FrameNum,' found")
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
// Only rows within the frame range [startFrame, endFrame] are written
func writeSelectedLightCurves(data *LightCurveData, selectedColumns map[int]bool, startFrame, endFrame float64) (string, error) {
	if data == nil {
		return "", fmt.Errorf("no light curve data loaded")
	}
	if len(selectedColumns) == 0 {
		return "", fmt.Errorf("no light curves selected")
	}

	// Build output file path: insert "_GoPyOTE" before .csv
	dir := filepath.Dir(data.SourceFilePath)
	base := filepath.Base(data.SourceFilePath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	outputPath := filepath.Join(dir, nameWithoutExt+"_GoPyOTE"+ext)

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

// Global variable to hold loaded light curve data
var loadedLightCurveData *LightCurveData

// Global variables for action logging
var (
	actionLogFile   *os.File
	actionLogWriter *bufio.Writer
)

// createActionLog creates a new log file based on the CSV file path
func createActionLog(csvFilePath string) error {
	// Close any existing log file
	closeActionLog()

	// Build log file path: same name as CSV but with .log extension
	dir := filepath.Dir(csvFilePath)
	base := filepath.Base(csvFilePath)
	ext := filepath.Ext(base)
	nameWithoutExt := strings.TrimSuffix(base, ext)
	logPath := filepath.Join(dir, nameWithoutExt+".log")

	// Create/open the log file (append mode)
	file, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to create log file: %w", err)
	}

	actionLogFile = file
	actionLogWriter = bufio.NewWriter(file)

	// Write session start marker
	logAction("=== New Session Started ===")
	logAction("CSV file: " + csvFilePath)

	return nil
}

// logAction writes a timestamped action to the log file
func logAction(action string) {
	if actionLogWriter == nil {
		return
	}

	timestamp := time.Now().Format("2006-Jan-02 15:04")
	line := fmt.Sprintf("[%s] %s\n", timestamp, action)

	if _, err := actionLogWriter.WriteString(line); err != nil {
		fmt.Printf("Warning: failed to write to log: %v\n", err)
		return
	}
	if err := actionLogWriter.Flush(); err != nil {
		fmt.Printf("Warning: failed to flush log: %v\n", err)
	}
}

// closeActionLog closes the current log file
func closeActionLog() {
	if actionLogWriter != nil {
		logAction("=== Session Ended ===")
		if err := actionLogWriter.Flush(); err != nil {
			fmt.Printf("Warning: failed to flush log on close: %v\n", err)
		}
		actionLogWriter = nil
	}
	if actionLogFile != nil {
		if err := actionLogFile.Close(); err != nil {
			fmt.Printf("Warning: failed to close log file: %v\n", err)
		}
		actionLogFile = nil
	}
}

// showOccultationParametersDialog displays a form dialog for editing occultation parameters
func showOccultationParametersDialog(w fyne.Window) {
	// Create entry fields for all parameters
	windowSizeEntry := widget.NewEntry()
	titleEntry := widget.NewEntry()
	fundamentalPlaneWidthKmEntry := widget.NewEntry()
	fundamentalPlaneWidthNumPointsEntry := widget.NewEntry()
	parallaxArcsecEntry := widget.NewEntry()
	distanceAuEntry := widget.NewEntry()
	pathToQeTableFileEntry := widget.NewEntry()
	observationWavelengthNmEntry := widget.NewEntry()
	dXKmPerSecEntry := widget.NewEntry()
	dYKmPerSecEntry := widget.NewEntry()
	pathPerpendicularOffsetKmEntry := widget.NewEntry()
	percentMagDropEntry := widget.NewEntry()
	starDiamOnPlaneMasEntry := widget.NewEntry()
	limbDarkeningCoeffEntry := widget.NewEntry()
	starClassEntry := widget.NewEntry()

	// Main body ellipse parameters
	mainBodyXCenterEntry := widget.NewEntry()
	mainBodyYCenterEntry := widget.NewEntry()
	mainBodyMajorAxisEntry := widget.NewEntry()
	mainBodyMinorAxisEntry := widget.NewEntry()
	mainBodyPaDegreesEntry := widget.NewEntry()

	// Satellite ellipse parameters
	satelliteXCenterEntry := widget.NewEntry()
	satelliteYCenterEntry := widget.NewEntry()
	satelliteMajorAxisEntry := widget.NewEntry()
	satelliteMinorAxisEntry := widget.NewEntry()
	satellitePaDegreesEntry := widget.NewEntry()

	pathToExternalImageEntry := widget.NewEntry()

	// Track the currently loaded file name for save dialog default
	var loadedFileName string

	// Auto-load previously opened parameters file if available
	if lastLoadedParamsPath != "" {
		file, err := os.Open(lastLoadedParamsPath)
		if err == nil {
			params, parseErr := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", closeErr), w)
			}
			if parseErr == nil {
				windowSizeEntry.SetText(strconv.Itoa(params.WindowSizePixels))
				titleEntry.SetText(params.Title)
				fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(params.FundamentalPlaneWidthKm, 'f', -1, 64))
				fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(params.FundamentalPlaneWidthNumPoints))
				parallaxArcsecEntry.SetText(strconv.FormatFloat(params.ParallaxArcsec, 'f', -1, 64))
				distanceAuEntry.SetText(strconv.FormatFloat(params.DistanceAu, 'f', -1, 64))
				pathToQeTableFileEntry.SetText(params.PathToQeTableFile)
				observationWavelengthNmEntry.SetText(strconv.Itoa(params.ObservationWavelengthNm))
				dXKmPerSecEntry.SetText(strconv.FormatFloat(params.DXKmPerSec, 'f', -1, 64))
				dYKmPerSecEntry.SetText(strconv.FormatFloat(params.DYKmPerSec, 'f', -1, 64))
				pathPerpendicularOffsetKmEntry.SetText(strconv.FormatFloat(params.PathPerpendicularOffsetKm, 'f', -1, 64))
				percentMagDropEntry.SetText(strconv.Itoa(params.PercentMagDrop))
				starDiamOnPlaneMasEntry.SetText(strconv.FormatFloat(params.StarDiamOnPlaneMas, 'f', -1, 64))
				limbDarkeningCoeffEntry.SetText(strconv.FormatFloat(params.LimbDarkeningCoeff, 'f', -1, 64))
				starClassEntry.SetText(params.StarClass)
				mainBodyXCenterEntry.SetText(strconv.FormatFloat(params.MainBody.XCenterKm, 'f', -1, 64))
				mainBodyYCenterEntry.SetText(strconv.FormatFloat(params.MainBody.YCenterKm, 'f', -1, 64))
				mainBodyMajorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisKm, 'f', -1, 64))
				mainBodyMinorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MinorAxisKm, 'f', -1, 64))
				mainBodyPaDegreesEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisPaDegrees, 'f', -1, 64))
				satelliteXCenterEntry.SetText(strconv.FormatFloat(params.Satellite.XCenterKm, 'f', -1, 64))
				satelliteYCenterEntry.SetText(strconv.FormatFloat(params.Satellite.YCenterKm, 'f', -1, 64))
				satelliteMajorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisKm, 'f', -1, 64))
				satelliteMinorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MinorAxisKm, 'f', -1, 64))
				satellitePaDegreesEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisPaDegrees, 'f', -1, 64))
				pathToExternalImageEntry.SetText(params.PathToExternalImage)
				loadedFileName = filepath.Base(lastLoadedParamsPath)
			}
		}
	}

	// Helper to wrap entry in a fixed-width container
	entryWidth := float32(280)
	wrapEntry := func(e *widget.Entry) *fyne.Container {
		return container.New(layout.NewGridWrapLayout(fyne.NewSize(entryWidth, 36)), e)
	}

	// Create a left column form
	leftForm := widget.NewForm(
		&widget.FormItem{Text: "Window Size (pixels)", Widget: wrapEntry(windowSizeEntry)},
		&widget.FormItem{Text: "Title", Widget: wrapEntry(titleEntry)},
		&widget.FormItem{Text: "Fund. Plane Width (km)", Widget: wrapEntry(fundamentalPlaneWidthKmEntry)},
		&widget.FormItem{Text: "Fund. Plane Width (pts)", Widget: wrapEntry(fundamentalPlaneWidthNumPointsEntry)},
		&widget.FormItem{Text: "Parallax (arcsec)", Widget: wrapEntry(parallaxArcsecEntry)},
		&widget.FormItem{Text: "Distance (AU)", Widget: wrapEntry(distanceAuEntry)},
		&widget.FormItem{Text: "Path to QE Table File", Widget: wrapEntry(pathToQeTableFileEntry)},
		&widget.FormItem{Text: "Obs. Wavelength (nm)", Widget: wrapEntry(observationWavelengthNmEntry)},
		&widget.FormItem{Text: "dX (km/sec)", Widget: wrapEntry(dXKmPerSecEntry)},
		&widget.FormItem{Text: "dY (km/sec)", Widget: wrapEntry(dYKmPerSecEntry)},
		&widget.FormItem{Text: "Path Perp. Offset (km)", Widget: wrapEntry(pathPerpendicularOffsetKmEntry)},
		&widget.FormItem{Text: "Percent Mag Drop", Widget: wrapEntry(percentMagDropEntry)},
		&widget.FormItem{Text: "Star Diam. (mas)", Widget: wrapEntry(starDiamOnPlaneMasEntry)},
	)

	// Create a right column form
	rightForm := widget.NewForm(
		&widget.FormItem{Text: "Limb Darkening Coeff", Widget: wrapEntry(limbDarkeningCoeffEntry)},
		&widget.FormItem{Text: "Star Class", Widget: wrapEntry(starClassEntry)},
		&widget.FormItem{Text: "Main Body X (km)", Widget: wrapEntry(mainBodyXCenterEntry)},
		&widget.FormItem{Text: "Main Body Y (km)", Widget: wrapEntry(mainBodyYCenterEntry)},
		&widget.FormItem{Text: "Main Body Major (km)", Widget: wrapEntry(mainBodyMajorAxisEntry)},
		&widget.FormItem{Text: "Main Body Minor (km)", Widget: wrapEntry(mainBodyMinorAxisEntry)},
		&widget.FormItem{Text: "Main Body PA (deg)", Widget: wrapEntry(mainBodyPaDegreesEntry)},
		&widget.FormItem{Text: "Satellite X (km)", Widget: wrapEntry(satelliteXCenterEntry)},
		&widget.FormItem{Text: "Satellite Y (km)", Widget: wrapEntry(satelliteYCenterEntry)},
		&widget.FormItem{Text: "Satellite Major (km)", Widget: wrapEntry(satelliteMajorAxisEntry)},
		&widget.FormItem{Text: "Satellite Minor (km)", Widget: wrapEntry(satelliteMinorAxisEntry)},
		&widget.FormItem{Text: "Satellite PA (deg)", Widget: wrapEntry(satellitePaDegreesEntry)},
		&widget.FormItem{Text: "External Image Path", Widget: wrapEntry(pathToExternalImageEntry)},
	)

	// Create a two-column layout
	twoColumns := container.NewHBox(
		container.NewPadded(leftForm),
		container.NewPadded(rightForm),
	)

	scrollContent := container.NewVScroll(twoColumns)
	scrollContent.SetMinSize(fyne.NewSize(770, 650))

	// Create a custom dialog with OK/Cancel buttons
	var customDialog *dialog.CustomDialog
	okBtn := widget.NewButton("OK", func() {
		// TODO: Process the form data
		customDialog.Hide()
	})
	okBtn.Importance = widget.HighImportance
	cancelBtn := widget.NewButton("Cancel", func() {
		customDialog.Hide()
	})

	// File open button
	loadBtn := widget.NewButton("Load...", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // User cancelled
			}
			defer func() {
				if cerr := reader.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
				}
			}()

			params, err := parseOccultationParameters(reader)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load parameters: %w", err), w)
				return
			}

			// Populate entry fields with loaded values
			windowSizeEntry.SetText(strconv.Itoa(params.WindowSizePixels))
			titleEntry.SetText(params.Title)
			fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(params.FundamentalPlaneWidthKm, 'f', -1, 64))
			fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(params.FundamentalPlaneWidthNumPoints))
			parallaxArcsecEntry.SetText(strconv.FormatFloat(params.ParallaxArcsec, 'f', -1, 64))
			distanceAuEntry.SetText(strconv.FormatFloat(params.DistanceAu, 'f', -1, 64))
			pathToQeTableFileEntry.SetText(params.PathToQeTableFile)
			observationWavelengthNmEntry.SetText(strconv.Itoa(params.ObservationWavelengthNm))
			dXKmPerSecEntry.SetText(strconv.FormatFloat(params.DXKmPerSec, 'f', -1, 64))
			dYKmPerSecEntry.SetText(strconv.FormatFloat(params.DYKmPerSec, 'f', -1, 64))
			pathPerpendicularOffsetKmEntry.SetText(strconv.FormatFloat(params.PathPerpendicularOffsetKm, 'f', -1, 64))
			percentMagDropEntry.SetText(strconv.Itoa(params.PercentMagDrop))
			starDiamOnPlaneMasEntry.SetText(strconv.FormatFloat(params.StarDiamOnPlaneMas, 'f', -1, 64))
			limbDarkeningCoeffEntry.SetText(strconv.FormatFloat(params.LimbDarkeningCoeff, 'f', -1, 64))
			starClassEntry.SetText(params.StarClass)
			mainBodyXCenterEntry.SetText(strconv.FormatFloat(params.MainBody.XCenterKm, 'f', -1, 64))
			mainBodyYCenterEntry.SetText(strconv.FormatFloat(params.MainBody.YCenterKm, 'f', -1, 64))
			mainBodyMajorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisKm, 'f', -1, 64))
			mainBodyMinorAxisEntry.SetText(strconv.FormatFloat(params.MainBody.MinorAxisKm, 'f', -1, 64))
			mainBodyPaDegreesEntry.SetText(strconv.FormatFloat(params.MainBody.MajorAxisPaDegrees, 'f', -1, 64))
			satelliteXCenterEntry.SetText(strconv.FormatFloat(params.Satellite.XCenterKm, 'f', -1, 64))
			satelliteYCenterEntry.SetText(strconv.FormatFloat(params.Satellite.YCenterKm, 'f', -1, 64))
			satelliteMajorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisKm, 'f', -1, 64))
			satelliteMinorAxisEntry.SetText(strconv.FormatFloat(params.Satellite.MinorAxisKm, 'f', -1, 64))
			satellitePaDegreesEntry.SetText(strconv.FormatFloat(params.Satellite.MajorAxisPaDegrees, 'f', -1, 64))
			pathToExternalImageEntry.SetText(params.PathToExternalImage)

			// Store the loaded file name for use as default in the save dialog
			loadedFileName = reader.URI().Name()
			// Store the full path for use by Run IOTAdiffraction
			lastLoadedParamsPath = reader.URI().Path()
		}, w)
		fileDialog.SetFilter(nil) // Allow all files or set a specific filter
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})

	// Helper functions to parse entry values
	parseFloat := func(s string) float64 {
		v, _ := strconv.ParseFloat(s, 64)
		return v
	}
	parseInt := func(s string) int {
		v, _ := strconv.Atoi(s)
		return v
	}

	// File save button
	saveBtn := widget.NewButton("Save...", func() {
		fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if writer == nil {
				return // User cancelled
			}
			defer func() {
				if cerr := writer.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
				}
			}()

			// Build parameters struct from entry fields
			params := OccultationParameters{
				WindowSizePixels:               parseInt(windowSizeEntry.Text),
				Title:                          titleEntry.Text,
				FundamentalPlaneWidthKm:        parseFloat(fundamentalPlaneWidthKmEntry.Text),
				FundamentalPlaneWidthNumPoints: parseInt(fundamentalPlaneWidthNumPointsEntry.Text),
				ParallaxArcsec:                 parseFloat(parallaxArcsecEntry.Text),
				DistanceAu:                     parseFloat(distanceAuEntry.Text),
				PathToQeTableFile:              pathToQeTableFileEntry.Text,
				ObservationWavelengthNm:        parseInt(observationWavelengthNmEntry.Text),
				DXKmPerSec:                     parseFloat(dXKmPerSecEntry.Text),
				DYKmPerSec:                     parseFloat(dYKmPerSecEntry.Text),
				PathPerpendicularOffsetKm:      parseFloat(pathPerpendicularOffsetKmEntry.Text),
				PercentMagDrop:                 parseInt(percentMagDropEntry.Text),
				StarDiamOnPlaneMas:             parseFloat(starDiamOnPlaneMasEntry.Text),
				LimbDarkeningCoeff:             parseFloat(limbDarkeningCoeffEntry.Text),
				StarClass:                      starClassEntry.Text,
				MainBody: EllipseParams{
					XCenterKm:          parseFloat(mainBodyXCenterEntry.Text),
					YCenterKm:          parseFloat(mainBodyYCenterEntry.Text),
					MajorAxisKm:        parseFloat(mainBodyMajorAxisEntry.Text),
					MinorAxisKm:        parseFloat(mainBodyMinorAxisEntry.Text),
					MajorAxisPaDegrees: parseFloat(mainBodyPaDegreesEntry.Text),
				},
				Satellite: EllipseParams{
					XCenterKm:          parseFloat(satelliteXCenterEntry.Text),
					YCenterKm:          parseFloat(satelliteYCenterEntry.Text),
					MajorAxisKm:        parseFloat(satelliteMajorAxisEntry.Text),
					MinorAxisKm:        parseFloat(satelliteMinorAxisEntry.Text),
					MajorAxisPaDegrees: parseFloat(satellitePaDegreesEntry.Text),
				},
				PathToExternalImage: pathToExternalImageEntry.Text,
			}

			// Marshal to JSON5
			data, err := json5.Marshal(params)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to encode parameters: %w", err), w)
				return
			}

			// Indent the JSON5 output
			var indented []byte
			if err := json5.Indent(&indented, data, "", "  "); err != nil {
				dialog.ShowError(fmt.Errorf("failed to format parameters: %w", err), w)
				return
			}
			data = indented

			// Write to the file
			if _, err := writer.Write(data); err != nil {
				dialog.ShowError(fmt.Errorf("failed to write file: %w", err), w)
				return
			}

			// Close the parameters dialog after a successful save
			customDialog.Hide()
		}, w)
		fileDialog.SetFilter(nil) // Allow all files or set a specific filter
		if loadedFileName != "" {
			fileDialog.SetFileName(loadedFileName)
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})

	buttons := container.NewHBox(loadBtn, saveBtn, layout.NewSpacer(), cancelBtn, okBtn)
	content := container.NewBorder(nil, buttons, nil, nil, scrollContent)

	customDialog = dialog.NewCustomWithoutButtons("Occultation Parameters", content, w)
	customDialog.Resize(fyne.NewSize(840, 750))
	customDialog.Show()
}

// FocusLossEntry is a custom Entry widget that triggers OnSubmitted when focus is lost
type FocusLossEntry struct {
	widget.Entry
}

// NewFocusLossEntry creates a new FocusLossEntry widget
func NewFocusLossEntry() *FocusLossEntry {
	e := &FocusLossEntry{}
	e.ExtendBaseWidget(e)
	return e
}

// FocusLost is called when the entry loses focus - triggers OnSubmitted
func (e *FocusLossEntry) FocusLost() {
	e.Entry.FocusLost()
	if e.OnSubmitted != nil {
		e.OnSubmitted(e.Text)
	}
}

// HoverableCheck is a checkbox with tooltip support on hover
type HoverableCheck struct {
	widget.Check
	tooltip   string
	popUp     *widget.PopUp
	parentWin fyne.Window
}

// NewHoverableCheck creates a new HoverableCheck widget with a tooltip
func NewHoverableCheck(label string, changed func(bool), tooltip string, win fyne.Window) *HoverableCheck {
	h := &HoverableCheck{
		tooltip:   tooltip,
		parentWin: win,
	}
	h.Text = label
	h.OnChanged = changed
	h.ExtendBaseWidget(h)
	return h
}

// MouseIn is called when the mouse enters the widget - shows tooltip
func (h *HoverableCheck) MouseIn(e *desktop.MouseEvent) {
	if h.tooltip == "" || h.parentWin == nil {
		return
	}

	tooltipLabel := widget.NewLabel(h.tooltip)
	tooltipLabel.Wrapping = fyne.TextWrapOff
	tooltipContent := container.NewPadded(tooltipLabel)

	h.popUp = widget.NewPopUp(tooltipContent, h.parentWin.Canvas())
	h.popUp.ShowAtPosition(fyne.NewPos(e.AbsolutePosition.X+10, e.AbsolutePosition.Y+20))
}

// MouseMoved is called when the mouse moves within the widget
func (h *HoverableCheck) MouseMoved(_ *desktop.MouseEvent) {
	// Required to implement desktop.Hoverable interface
}

// MouseOut is called when the mouse leaves the widget - hides tooltip
func (h *HoverableCheck) MouseOut() {
	if h.popUp != nil {
		h.popUp.Hide()
		h.popUp = nil
	}
}

// PlotPoint represents a data point in the light curve
type PlotPoint struct {
	X      float64 // Time or frame number
	Y      float64 // Brightness/magnitude
	Index  int     // Point index for identification
	Series int     // Which series this point belongs to
}

// PlotSeries represents a single light curve series
type PlotSeries struct {
	Points []PlotPoint
	Color  color.RGBA
	Name   string
}

// LightCurvePlot is a custom widget for displaying interactive light curve plots using gonum/plot
type LightCurvePlot struct {
	widget.BaseWidget
	series            []PlotSeries
	pointRadius       float32
	onPointClicked    func(point PlotPoint)
	onScroll          func(position fyne.Position, scrollDelta float32) // Callback for scroll events
	selectedSeries    int
	selectedIndex     int
	xAxisLabel        string
	useTimestampTicks bool // When true, format X axis ticks as timestamps

	// Stable identifiers for preserving selection across rebuilds
	selectedPointDataIndex int    // Original data index of a selected point
	selectedSeriesName     string // Name of the series containing the selected point

	// Plot bounds for coordinate conversion
	minX, maxX, minY, maxY float64
	// Plot area margins (in pixels) - approximate values for gonum/plot
	marginLeft, marginRight, marginTop, marginBottom float32
}

// NewLightCurvePlot creates a new light curve plot widget
func NewLightCurvePlot(series []PlotSeries, onPointClicked func(PlotPoint)) *LightCurvePlot {
	p := &LightCurvePlot{
		series:                 series,
		pointRadius:            5,
		onPointClicked:         onPointClicked,
		selectedSeries:         -1,
		selectedIndex:          -1,
		selectedPointDataIndex: -1,
		selectedSeriesName:     "",
		xAxisLabel:             "Time",
		marginLeft:             60,
		marginRight:            20,
		marginTop:              20,
		marginBottom:           40,
	}
	p.calculateBounds()
	p.ExtendBaseWidget(p)
	return p
}

// SetXAxisLabel sets the label for the X axis
func (p *LightCurvePlot) SetXAxisLabel(label string) {
	p.xAxisLabel = label
	p.Refresh()
}

// SetXBounds sets the X axis min and max values
func (p *LightCurvePlot) SetXBounds(minX, maxX float64) {
	if maxX > minX {
		p.minX = minX
		p.maxX = maxX
		p.Refresh()
	}
}

// GetXBounds returns the current X axis min and max values
func (p *LightCurvePlot) GetXBounds() (float64, float64) {
	return p.minX, p.maxX
}

// SetYBounds sets the Y axis min and max values
func (p *LightCurvePlot) SetYBounds(minY, maxY float64) {
	if maxY > minY {
		p.minY = minY
		p.maxY = maxY
		p.Refresh()
	}
}

// GetYBounds returns the current Y axis min and max values
func (p *LightCurvePlot) GetYBounds() (float64, float64) {
	return p.minY, p.maxY
}

// SetUseTimestampTicks sets whether X axis ticks should be formatted as timestamps
func (p *LightCurvePlot) SetUseTimestampTicks(use bool) {
	p.useTimestampTicks = use
	p.Refresh()
}

// GetUseTimestampTicks returns whether X axis ticks are formatted as timestamps
func (p *LightCurvePlot) GetUseTimestampTicks() bool {
	return p.useTimestampTicks
}

// SetOnScroll sets the callback for scroll events
func (p *LightCurvePlot) SetOnScroll(callback func(position fyne.Position, scrollDelta float32)) {
	p.onScroll = callback
}

// Scrolled handles scroll wheel events
func (p *LightCurvePlot) Scrolled(ev *fyne.ScrollEvent) {
	if p.onScroll != nil {
		p.onScroll(ev.Position, ev.Scrolled.DY)
	}
}

// timestampTicker is a custom tick marker that formats values as timestamps
type timestampTicker struct{}

// Ticks returns tick marks for the given axis range, formatted as timestamps
func (t timestampTicker) Ticks(min, max float64) []plot.Tick {
	// Use the default ticker to get reasonable tick positions
	defaultTicker := plot.DefaultTicks{}
	ticks := defaultTicker.Ticks(min, max)

	// Replace labels with the timestamp format
	for i := range ticks {
		if ticks[i].Label != "" {
			ticks[i].Label = formatSecondsAsTimestamp(ticks[i].Value)
		}
	}
	return ticks
}

// calculateBounds computes the data bounds across all series
func (p *LightCurvePlot) calculateBounds() {
	if len(p.series) == 0 {
		return
	}
	first := true
	for _, s := range p.series {
		for _, pt := range s.Points {
			if first {
				p.minX, p.maxX = pt.X, pt.X
				p.minY, p.maxY = pt.Y, pt.Y
				first = false
			} else {
				if pt.X < p.minX {
					p.minX = pt.X
				}
				if pt.X > p.maxX {
					p.maxX = pt.X
				}
				if pt.Y < p.minY {
					p.minY = pt.Y
				}
				if pt.Y > p.maxY {
					p.maxY = pt.Y
				}
			}
		}
	}
	// Add padding
	rangeX := p.maxX - p.minX
	rangeY := p.maxY - p.minY
	if rangeX == 0 {
		rangeX = 1
	}
	if rangeY == 0 {
		rangeY = 1
	}
	p.minX -= rangeX * 0.05
	p.maxX += rangeX * 0.05
	p.minY -= rangeY * 0.05
	p.maxY += rangeY * 0.05
}

// SetSeries updates the plot data
func (p *LightCurvePlot) SetSeries(series []PlotSeries) {
	p.series = series
	p.selectedSeries = -1
	p.selectedIndex = -1

	// Try to restore the selection based on saved stable identifiers
	if p.selectedPointDataIndex >= 0 && p.selectedSeriesName != "" {
		for s, ser := range series {
			if ser.Name == p.selectedSeriesName {
				for i, pt := range ser.Points {
					if pt.Index == p.selectedPointDataIndex {
						p.selectedSeries = s
						p.selectedIndex = i
						break
					}
				}
				break
			}
		}
	}

	p.calculateBounds()
	p.Refresh()
}

// MinSize returns the minimum size
func (p *LightCurvePlot) MinSize() fyne.Size {
	return fyne.NewSize(200, 150)
}

// CreateRenderer creates the plot renderer
func (p *LightCurvePlot) CreateRenderer() fyne.WidgetRenderer {
	return &lightCurvePlotRenderer{plot: p}
}

// Tapped handles tap/click events
func (p *LightCurvePlot) Tapped(ev *fyne.PointEvent) {
	p.handleClick(ev.Position)
}

// MouseDown handles mouse down events for desktop
func (p *LightCurvePlot) MouseDown(ev *desktop.MouseEvent) {
	p.handleClick(ev.Position)
}

// screenToData converts screen coordinates to data coordinates
func (p *LightCurvePlot) screenToData(pos fyne.Position, size fyne.Size) (float64, float64) {
	plotWidth := size.Width - p.marginLeft - p.marginRight
	plotHeight := size.Height - p.marginTop - p.marginBottom

	// Convert screen position to data coordinates
	dataX := p.minX + float64((pos.X-p.marginLeft)/plotWidth)*(p.maxX-p.minX)
	dataY := p.maxY - float64((pos.Y-p.marginTop)/plotHeight)*(p.maxY-p.minY)

	return dataX, dataY
}

func (p *LightCurvePlot) handleClick(pos fyne.Position) {
	if len(p.series) == 0 {
		return
	}

	size := p.Size()
	clickX, clickY := p.screenToData(pos, size)

	// Find the closest point in data space
	clickRadius := (p.maxX - p.minX) * 0.03 // 3% of data range
	var closestSeries = -1
	var closestIdx = -1
	var closestDist = clickRadius * 2

	for s, series := range p.series {
		for i, pt := range series.Points {
			dx := pt.X - clickX
			dy := (pt.Y - clickY) * (p.maxX - p.minX) / (p.maxY - p.minY) // Normalize
			dist := math.Sqrt(dx*dx + dy*dy)
			if dist < clickRadius && dist < closestDist {
				closestDist = dist
				closestSeries = s
				closestIdx = i
			}
		}
	}

	if closestSeries >= 0 && closestIdx >= 0 {
		p.selectedSeries = closestSeries
		p.selectedIndex = closestIdx
		// Save stable identifiers for preserving selection across rebuilds
		p.selectedPointDataIndex = p.series[closestSeries].Points[closestIdx].Index
		p.selectedSeriesName = p.series[closestSeries].Name
		p.Refresh()
		if p.onPointClicked != nil {
			p.onPointClicked(p.series[closestSeries].Points[closestIdx])
		}
	}
}

// lightCurvePlotRenderer renders the plot using gonum/plot
type lightCurvePlotRenderer struct {
	plot    *LightCurvePlot
	image   *canvas.Image
	objects []fyne.CanvasObject
}

func (r *lightCurvePlotRenderer) Destroy() {}

func (r *lightCurvePlotRenderer) Layout(size fyne.Size) {
	if r.image != nil {
		r.image.Resize(size)
	}
}

func (r *lightCurvePlotRenderer) MinSize() fyne.Size {
	return r.plot.MinSize()
}

func (r *lightCurvePlotRenderer) Objects() []fyne.CanvasObject {
	return r.objects
}

func (r *lightCurvePlotRenderer) Refresh() {
	p := r.plot
	size := p.Size()

	if size.Width < 10 || size.Height < 10 {
		return
	}

	// Create gonum plot
	plt := plot.New()
	// Modify the font fields directly on existing styles
	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(12)

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(12)

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(12)

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(10)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(10)

	// If no series, show an empty plot
	if len(p.series) == 0 {
		plt.Title.Text = "Light Curve"
		plt.X.Label.Text = p.xAxisLabel
		plt.Y.Label.Text = "Brightness"

		// Render empty plot
		width := vg.Length(size.Width) * vg.Inch / 96
		height := vg.Length(size.Height) * vg.Inch / 96
		img := vgimg.New(width, height)
		dc := draw.New(img)
		plt.Draw(dc)

		var buf bytes.Buffer
		if err := png.Encode(&buf, img.Image()); err != nil {
			fmt.Printf("Error encoding plot image: %v\n", err)
			return
		}
		goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
		if err != nil {
			fmt.Printf("Error decoding plot image: %v\n", err)
			return
		}

		if r.image == nil {
			r.image = canvas.NewImageFromImage(goImg)
			r.image.FillMode = canvas.ImageFillStretch
			r.objects = []fyne.CanvasObject{r.image}
		} else {
			r.image.Image = goImg
			r.image.Refresh()
		}
		r.image.Resize(size)
		return
	}
	plt.Title.Text = "Light Curve"
	plt.Title.TextStyle.Font.Weight = 2 // Bold
	plt.X.Label.Text = p.xAxisLabel
	plt.X.Label.TextStyle.Font.Weight = 2
	plt.Y.Label.Text = "Brightness"
	plt.Y.Label.TextStyle.Font.Weight = 2

	// Add grid
	plt.Add(plotter.NewGrid())

	// Add each series
	for s, series := range p.series {
		// Create XY data for line and scatter
		pts := make(plotter.XYs, len(series.Points))
		for i, pt := range series.Points {
			pts[i].X = pt.X
			pts[i].Y = pt.Y
		}

		// Create line
		line, err := plotter.NewLine(pts)
		if err != nil {
			fmt.Printf("Error creating line plot: %v\n", err)
			continue
		}
		line.Color = series.Color
		line.Width = vg.Points(2)
		plt.Add(line)

		// Create scatter points
		scatter, err := plotter.NewScatter(pts)
		if err != nil {
			fmt.Printf("Error creating scatter plot: %v\n", err)
			continue
		}
		scatter.Color = series.Color
		scatter.GlyphStyle.Shape = draw.CircleGlyph{}
		scatter.GlyphStyle.Radius = vg.Points(4)

		// Highlight the selected point
		if s == p.selectedSeries && p.selectedIndex >= 0 && p.selectedIndex < len(series.Points) {
			// Draw regular points first
			plt.Add(scatter)

			// Draw the selected point larger and in red
			selectedPt := make(plotter.XYs, 1)
			selectedPt[0].X = series.Points[p.selectedIndex].X
			selectedPt[0].Y = series.Points[p.selectedIndex].Y
			selectedScatter, err := plotter.NewScatter(selectedPt)
			if err != nil {
				fmt.Printf("Error creating selected point scatter: %v\n", err)
			} else {
				selectedScatter.Color = color.RGBA{R: 255, G: 50, B: 50, A: 255}
				selectedScatter.GlyphStyle.Shape = draw.CircleGlyph{}
				selectedScatter.GlyphStyle.Radius = vg.Points(7)
				plt.Add(selectedScatter)
			}
		} else {
			plt.Add(scatter)
		}

		// Add to legend
		plt.Legend.Add(series.Name, line, scatter)
	}

	// Set axis ranges
	plt.X.Min = p.minX
	plt.X.Max = p.maxX
	plt.Y.Min = p.minY
	plt.Y.Max = p.maxY

	// Use timestamp tick labels if enabled
	if p.useTimestampTicks {
		plt.X.Tick.Marker = timestampTicker{}
	}

	plt.Legend.Top = true
	plt.Legend.Left = true

	// Render to image
	width := vg.Length(size.Width) * vg.Inch / 96 // Convert pixels to inches at 96 DPI
	height := vg.Length(size.Height) * vg.Inch / 96

	img := vgimg.New(width, height)
	dc := draw.New(img)
	plt.Draw(dc)

	// Convert to Go image
	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		fmt.Printf("Error encoding plot image: %v\n", err)
		return
	}
	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		fmt.Printf("Error decoding plot image: %v\n", err)
		return
	}

	// Create or update Fyne image
	if r.image == nil {
		r.image = canvas.NewImageFromImage(goImg)
		r.image.FillMode = canvas.ImageFillStretch
		r.objects = []fyne.CanvasObject{r.image}
	} else {
		r.image.Image = goImg
		r.image.Refresh()
	}
	r.image.Resize(size)
}

// generateRandomLightCurve creates sample light curve data
func generateRandomLightCurve(numPoints int, seriesIndex int, baseY float64, dipCenter float64) []PlotPoint {
	points := make([]PlotPoint, numPoints)

	for i := 0; i < numPoints; i++ {
		x := float64(i)
		y := baseY + rand.Float64()*0.1 - 0.05
		dipPos := int(float64(numPoints) * dipCenter)
		if i > dipPos-numPoints/6 && i < dipPos+numPoints/6 {
			dip := 0.3 * math.Exp(-math.Pow(float64(i-dipPos)/5.0, 2))
			y -= dip
		}
		points[i] = PlotPoint{X: x, Y: y, Index: i, Series: seriesIndex}
	}
	return points
}

func main() {
	a := app.NewWithID("com.gopyote.app")
	w := a.NewWindow("GoPyOTE Version: " + Version)

	// Load saved window geometry
	prefs := a.Preferences()

	// Initialize preferences on the first run to avoid EOF errors
	if prefs.Int("initialized") == 0 {
		prefs.SetInt("initialized", 1)
		prefs.SetInt("windowX", -1)
		prefs.SetInt("windowY", -1)
		prefs.SetInt("windowW", 1000)
		prefs.SetInt("windowH", 600)
		prefs.SetFloat("splitOffset", 0.6)
	}

	savedX := int32(prefs.IntWithFallback("windowX", -1))
	savedY := int32(prefs.IntWithFallback("windowY", -1))
	savedW := int32(prefs.IntWithFallback("windowW", 1000))
	savedH := int32(prefs.IntWithFallback("windowH", 600))
	firstRun := savedX == -1 && savedY == -1

	// Create a menu
	fileMenu := fyne.NewMenu("File",
		fyne.NewMenuItem("New", func() {}),
		fyne.NewMenuItem("Open", func() {}),
		fyne.NewMenuItem("Save", func() {}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("Quit", func() { a.Quit() }),
	)
	editMenu := fyne.NewMenu("Edit",
		fyne.NewMenuItem("Cut", func() {}),
		fyne.NewMenuItem("Copy", func() {}),
		fyne.NewMenuItem("Paste", func() {}),
	)
	helpMenu := fyne.NewMenu("Help",
		fyne.NewMenuItem("About", func() {}),
	)
	mainMenu := fyne.NewMainMenu(fileMenu, editMenu, helpMenu)
	w.SetMainMenu(mainMenu)

	// Tab 2: Settings
	// Track which light curve prefixes to include when loading CSV
	lightCurvePrefixes := map[string]bool{
		"signal":     true,
		"appsum":     false,
		"avgbkg":     false,
		"stdbkg":     false,
		"nmaskpx":    false,
		"maxpx":      false,
		"xcentroid":  false,
		"ycentroid":  false,
		"hit-defect": false,
	}
	acceptAnyName := false

	// Function variable for refreshing the light curve list when filter changes (set later)
	var refreshLightCurveFilter func()

	// Create checkboxes for light curve prefixes
	signalCheck := widget.NewCheck("signal", func(checked bool) {
		lightCurvePrefixes["signal"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	signalCheck.SetChecked(true)
	appsumCheck := widget.NewCheck("appsum", func(checked bool) {
		lightCurvePrefixes["appsum"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	avgbkgCheck := widget.NewCheck("avgbkg", func(checked bool) {
		lightCurvePrefixes["avgbkg"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	stdbkgCheck := widget.NewCheck("stdbkg", func(checked bool) {
		lightCurvePrefixes["stdbkg"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	nmaskpxCheck := widget.NewCheck("nmaskpx", func(checked bool) {
		lightCurvePrefixes["nmaskpx"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	maxpxCheck := widget.NewCheck("maxpx", func(checked bool) {
		lightCurvePrefixes["maxpx"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	xcentroidCheck := widget.NewCheck("xcentroid", func(checked bool) {
		lightCurvePrefixes["xcentroid"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	ycentroidCheck := widget.NewCheck("ycentroid", func(checked bool) {
		lightCurvePrefixes["ycentroid"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	hitDefectCheck := widget.NewCheck("hit-defect", func(checked bool) {
		lightCurvePrefixes["hit-defect"] = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})
	anyNameCheck := widget.NewCheck("any name", func(checked bool) {
		acceptAnyName = checked
		if refreshLightCurveFilter != nil {
			refreshLightCurveFilter()
		}
	})

	prefixCheckboxes := container.NewVBox(
		widget.NewLabel("Light curve prefixes to include:"),
		anyNameCheck,
		signalCheck,
		appsumCheck,
		avgbkgCheck,
		stdbkgCheck,
		nmaskpxCheck,
		maxpxCheck,
		xcentroidCheck,
		ycentroidCheck,
		hitDefectCheck,
	)

	tab2Bg := canvas.NewRectangle(color.RGBA{R: 200, G: 200, B: 230, A: 255})
	tab2Content := container.NewStack(tab2Bg, container.NewPadded(prefixCheckboxes))
	tab2 := container.NewTabItem("Settings", tab2Content)

	// Create the plot area with an interactive light curve (before Tab 3 so it can be referenced)
	plotStatusLabel := widget.NewLabel("Click on a point to see details")
	plotStatusLabel.Wrapping = fyne.TextWrapWord

	// Generate two light curve series with different colors
	series1Color := color.RGBA{R: 70, G: 130, B: 180, A: 255} // Steel blue
	series2Color := color.RGBA{R: 220, G: 120, B: 50, A: 255} // Orange

	lightCurveSeries := []PlotSeries{
		{
			Points: generateRandomLightCurve(50, 0, 1.0, 0.4),
			Color:  series1Color,
			Name:   "Star A",
		},
		{
			Points: generateRandomLightCurve(50, 1, 0.8, 0.6),
			Color:  series2Color,
			Name:   "Star B",
		},
	}

	// Track the current x-axis label for click callback
	currentXAxisLabel := "Time"

	// Create the plot with a click callback
	var lightCurvePlot *LightCurvePlot
	lightCurvePlot = NewLightCurvePlot(lightCurveSeries, func(point PlotPoint) {
		if point.Series < 0 || point.Series >= len(lightCurvePlot.series) {
			return
		}
		seriesName := lightCurvePlot.series[point.Series].Name

		// Get frame number from loaded data
		frameNum := point.Index // fallback to index
		if loadedLightCurveData != nil && point.Index < len(loadedLightCurveData.FrameNumbers) {
			frameNum = int(loadedLightCurveData.FrameNumbers[point.Index])
		}

		// Format X value based on timestamp ticks setting
		var xValueStr string
		if lightCurvePlot.GetUseTimestampTicks() {
			xValueStr = formatSecondsAsTimestamp(point.X)
		} else {
			xValueStr = fmt.Sprintf("%.4f", point.X)
		}

		plotStatusLabel.SetText(fmt.Sprintf("%s - Frame %d\n%s: %s\nValue: %.4f",
			seriesName, frameNum, currentXAxisLabel, xValueStr, point.Y))
		logAction(fmt.Sprintf("Clicked point: %s Frame %d, %s=%s, Value=%.4f",
			seriesName, frameNum, currentXAxisLabel, xValueStr, point.Y))
	})

	// Create X and Y axis range spinners (start empty, filled when the first curve selected)
	xMinEntry := NewFocusLossEntry()
	xMaxEntry := NewFocusLossEntry()
	yMinEntry := NewFocusLossEntry()
	yMaxEntry := NewFocusLossEntry()
	xMinEntry.SetPlaceHolder("X Min")
	xMaxEntry.SetPlaceHolder("X Max")
	yMinEntry.SetPlaceHolder("Y Min")
	yMaxEntry.SetPlaceHolder("Y Max")

	// Track if the user has manually set any bounds (don't reset on curve toggle)
	userSetBounds := false

	// Update entries when plot bounds change
	updateRangeEntries := func() {
		minX, maxX := lightCurvePlot.GetXBounds()
		minY, maxY := lightCurvePlot.GetYBounds()
		// Format X entries as timestamps if timestamp ticks are enabled
		if lightCurvePlot.GetUseTimestampTicks() {
			xMinEntry.SetText(formatSecondsAsTimestamp(minX))
			xMaxEntry.SetText(formatSecondsAsTimestamp(maxX))
		} else {
			xMinEntry.SetText(fmt.Sprintf("%.4f", minX))
			xMaxEntry.SetText(fmt.Sprintf("%.4f", maxX))
		}
		yMinEntry.SetText(fmt.Sprintf("%.4f", minY))
		yMaxEntry.SetText(fmt.Sprintf("%.4f", maxY))
	}
	// Don't call updateRangeEntries() here - wait until the first curve is selected

	// Handle X Min entry changes
	xMinEntry.OnSubmitted = func(text string) {
		var val float64
		var ok bool
		// Try the timestamp format first if timestamp ticks are enabled
		if lightCurvePlot.GetUseTimestampTicks() {
			val, ok = parseTimestampInput(text)
		}
		// Fall back to float parsing
		if !ok {
			var err error
			val, err = strconv.ParseFloat(text, 64)
			ok = err == nil
		}
		if ok {
			_, maxX := lightCurvePlot.GetXBounds()
			lightCurvePlot.SetXBounds(val, maxX)
			userSetBounds = true
			logAction(fmt.Sprintf("Set X Min to %.4f", val))
		}
		updateRangeEntries()
	}

	// Handle X Max entry changes
	xMaxEntry.OnSubmitted = func(text string) {
		var val float64
		var ok bool
		// Try the timestamp format first if timestamp ticks are enabled
		if lightCurvePlot.GetUseTimestampTicks() {
			val, ok = parseTimestampInput(text)
		}
		// Fall back to float parsing
		if !ok {
			var err error
			val, err = strconv.ParseFloat(text, 64)
			ok = err == nil
		}
		if ok {
			minX, _ := lightCurvePlot.GetXBounds()
			lightCurvePlot.SetXBounds(minX, val)
			userSetBounds = true
			logAction(fmt.Sprintf("Set X Max to %.4f", val))
		}
		updateRangeEntries()
	}

	// Handle Y Min entry changes
	yMinEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err == nil {
			_, maxY := lightCurvePlot.GetYBounds()
			lightCurvePlot.SetYBounds(val, maxY)
			userSetBounds = true
			logAction(fmt.Sprintf("Set Y Min to %.4f", val))
		}
		updateRangeEntries()
	}

	// Handle Y Max entry changes
	yMaxEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err == nil {
			minY, _ := lightCurvePlot.GetYBounds()
			lightCurvePlot.SetYBounds(minY, val)
			userSetBounds = true
			logAction(fmt.Sprintf("Set Y Max to %.4f", val))
		}
		updateRangeEntries()
	}

	// Wrap entries in containers (150px width)
	xMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), xMinEntry)
	xMaxContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), xMaxEntry)
	yMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), yMinEntry)
	yMaxContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), yMaxEntry)

	// Checkbox for timestamp tick format
	timestampTicksCheck := widget.NewCheck("Change seconds to timestamp", func(checked bool) {
		lightCurvePlot.SetUseTimestampTicks(checked)
		// Update the entry box format to match the new mode
		if len(lightCurvePlot.series) > 0 {
			updateRangeEntries()
		}
		if checked {
			logAction("Enabled timestamp tick format")
		} else {
			logAction("Disabled timestamp tick format")
		}
	})

	// Create a toolbar with X and Y range controls
	rangeControls := container.NewHBox(
		widget.NewLabel("X Min:"),
		xMinContainer,
		widget.NewLabel("X Max:"),
		xMaxContainer,
		widget.NewLabel("Y Min:"),
		yMinContainer,
		widget.NewLabel("Y Max:"),
		yMaxContainer,
		widget.NewButton("Reset", func() {
			userSetBounds = false
			lightCurvePlot.calculateBounds()
			lightCurvePlot.Refresh()
			// Clear entries if no curves selected, otherwise update with calculated bounds
			if len(lightCurvePlot.series) == 0 {
				xMinEntry.SetText("")
				xMaxEntry.SetText("")
				yMinEntry.SetText("")
				yMaxEntry.SetText("")
			} else {
				updateRangeEntries()
			}
			logAction("Reset axis bounds to default")
		}),
		timestampTicksCheck,
	)

	plotArea := container.NewBorder(
		rangeControls,   // top
		plotStatusLabel, // bottom
		nil,             // left
		nil,             // right
		lightCurvePlot,  // center
	)

	// Tab 3: Data - Light curve list with click to toggle on/off plot
	tab3Bg := canvas.NewRectangle(color.RGBA{R: 230, G: 220, B: 200, A: 255})

	// Create a list to display light curve column names
	var lightCurveListData []string       // Will be populated when CSV is loaded (filtered by prefixes)
	var listIndexToColumnIndex []int      // Maps list index to actual column index in data
	displayedCurves := make(map[int]bool) // Track which curves are currently displayed (uses actual column indices)
	checkedCurveIndex := -1               // Track which single curve is checked (-1 = none, uses list index)
	var lightCurveList *widget.List

	// Color palette for multiple light curves
	curveColors := []color.RGBA{
		{R: 70, G: 130, B: 180, A: 255},  // Steel blue
		{R: 220, G: 120, B: 50, A: 255},  // Orange
		{R: 50, G: 180, B: 80, A: 255},   // Green
		{R: 180, G: 50, B: 180, A: 255},  // Purple
		{R: 200, G: 50, B: 50, A: 255},   // Red
		{R: 50, G: 180, B: 180, A: 255},  // Cyan
		{R: 180, G: 180, B: 50, A: 255},  // Yellow
		{R: 100, G: 100, B: 100, A: 255}, // Gray
	}

	// Track the current frame range for filtering plot data
	var frameRangeStart, frameRangeEnd float64
	// Save min/max frame numbers from loaded CSV for validation
	var minFrameNum, maxFrameNum float64

	// Function to rebuild the plot with all currently displayed curves
	rebuildPlot := func() {
		if loadedLightCurveData == nil {
			return
		}

		// Check if timestamps are empty (all zeros) - use frame numbers instead
		useFrameNumbers := true
		for _, t := range loadedLightCurveData.TimeValues {
			if t != 0 {
				useFrameNumbers = false
				break
			}
		}

		var allSeries []PlotSeries
		colorIdx := 0
		var displayedNames []string

		for colIdx := range loadedLightCurveData.Columns {
			if !displayedCurves[colIdx] {
				continue
			}

			col := loadedLightCurveData.Columns[colIdx]
			var points []PlotPoint
			for i, val := range col.Values {
				// Filter by frame range if set
				frameNum := loadedLightCurveData.FrameNumbers[i]
				if frameRangeStart > 0 && frameNum < frameRangeStart {
					continue
				}
				if frameRangeEnd > 0 && frameNum > frameRangeEnd {
					continue
				}

				xVal := loadedLightCurveData.TimeValues[i]
				if useFrameNumbers {
					xVal = frameNum
				}
				points = append(points, PlotPoint{
					X:      xVal,
					Y:      val,
					Index:  i,
					Series: len(allSeries),
				})
			}

			if len(points) == 0 {
				continue // Skip series with no points in range
			}

			allSeries = append(allSeries, PlotSeries{
				Points: points,
				Color:  curveColors[colorIdx%len(curveColors)],
				Name:   col.Name,
			})
			displayedNames = append(displayedNames, col.Name)
			colorIdx++
		}

		// Set appropriate X axis label
		if useFrameNumbers {
			currentXAxisLabel = "Frame Number"
			lightCurvePlot.xAxisLabel = "Frame Number"
		} else {
			currentXAxisLabel = "Time"
			lightCurvePlot.xAxisLabel = "Time"
		}

		if len(allSeries) == 0 {
			// Clear the plot if no curves are selected
			lightCurvePlot.SetSeries(nil)
			plotStatusLabel.SetText("No light curves selected")
		} else {
			lightCurvePlot.SetSeries(allSeries)
			plotStatusLabel.SetText(fmt.Sprintf("Displaying: %s", strings.Join(displayedNames, ", ")))
		}
	}

	// Function to toggle a light curve on/off
	toggleLightCurve := func(columnIndex int) {
		if loadedLightCurveData == nil || columnIndex < 0 || columnIndex >= len(loadedLightCurveData.Columns) {
			return
		}

		curveName := loadedLightCurveData.Columns[columnIndex].Name
		if displayedCurves[columnIndex] {
			delete(displayedCurves, columnIndex)
			logAction(fmt.Sprintf("Hid light curve: %s", curveName))
		} else {
			displayedCurves[columnIndex] = true
			logAction(fmt.Sprintf("Displayed light curve: %s", curveName))
		}

		// Save bounds if the user has set them manually
		var savedMinX, savedMaxX, savedMinY, savedMaxY float64
		if userSetBounds {
			savedMinX, savedMaxX = lightCurvePlot.GetXBounds()
			savedMinY, savedMaxY = lightCurvePlot.GetYBounds()
		}

		rebuildPlot()

		// Restore bounds if the user had set them, otherwise set Y min to 0 and update entries
		if userSetBounds {
			lightCurvePlot.SetXBounds(savedMinX, savedMaxX)
			lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		} else {
			// Set Y min to 0.0 by default
			_, maxY := lightCurvePlot.GetYBounds()
			lightCurvePlot.SetYBounds(0.0, maxY)
			updateRangeEntries()
		}
		lightCurveList.Refresh() // Refresh to update visual indicators
	}

	lightCurveList = widget.NewList(
		func() int { return len(lightCurveListData) },
		func() fyne.CanvasObject {
			check := NewHoverableCheck("", nil, "Checking this box enables this light curve to be used as the normalization reference (used for treating cloud effects)", w)
			label := widget.NewLabel("Light Curve Name")
			return container.NewHBox(check, label)
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			box := obj.(*fyne.Container)
			check := box.Objects[0].(*HoverableCheck)
			label := box.Objects[1].(*widget.Label)

			name := lightCurveListData[id]
			label.SetText(name)

			// Set the checkbox state based on whether this is the checked curve
			check.SetChecked(id == checkedCurveIndex)

			// Handle checkbox changes - only one can be checked at a time
			check.OnChanged = func(checked bool) {
				if checked {
					checkedCurveIndex = id
					logAction(fmt.Sprintf("Checked normalization reference: %s", name))
				} else if checkedCurveIndex == id {
					checkedCurveIndex = -1
					logAction(fmt.Sprintf("Unchecked normalization reference: %s", name))
				}
				lightCurveList.Refresh() // Refresh to update other checkboxes
			}

			// Map list index to actual column index for checking display status
			colIdx := -1
			if id >= 0 && id < len(listIndexToColumnIndex) {
				colIdx = listIndexToColumnIndex[id]
			}
			if displayedCurves[colIdx] {
				label.TextStyle.Bold = true
			} else {
				label.TextStyle.Bold = false
			}
			label.Refresh()
		},
	)

	// Handle click on list items to toggle the display
	lightCurveList.OnSelected = func(id widget.ListItemID) {
		// Map list index to actual column index
		if id >= 0 && id < len(listIndexToColumnIndex) {
			toggleLightCurve(listIndexToColumnIndex[id])
		}
		lightCurveList.UnselectAll() // Unselect so clicking again works
	}

	// Set up the function to refresh the light curve list when filter checkboxes change
	refreshLightCurveFilter = func() {
		if loadedLightCurveData == nil {
			return
		}

		// Clear displayed curves
		for k := range displayedCurves {
			delete(displayedCurves, k)
		}

		// Re-filter the light curve list
		lightCurveListData = nil
		listIndexToColumnIndex = nil
		checkedCurveIndex = -1

		for i, col := range loadedLightCurveData.Columns {
			// If "any name" is checked, include all columns
			if acceptAnyName {
				lightCurveListData = append(lightCurveListData, col.Name)
				listIndexToColumnIndex = append(listIndexToColumnIndex, i)
				continue
			}
			// Check if the column name starts with any enabled prefix
			for prefix, enabled := range lightCurvePrefixes {
				if enabled && strings.HasPrefix(col.Name, prefix) {
					lightCurveListData = append(lightCurveListData, col.Name)
					listIndexToColumnIndex = append(listIndexToColumnIndex, i)
					break
				}
			}
		}
		lightCurveList.Refresh()

		// Automatically display the first light curve if available
		if len(listIndexToColumnIndex) > 0 {
			toggleLightCurve(listIndexToColumnIndex[0])
		} else {
			rebuildPlot() // Rebuild with an empty plot if no curves match
		}

		logAction("Filter settings changed, refreshed light curve list")
	}

	// Frame number range entry boxes (defined here so they can be initialized when CSV is loaded)
	startFrameEntry := NewFocusLossEntry()
	startFrameEntry.SetPlaceHolder("Start Frame")
	endFrameEntry := NewFocusLossEntry()
	endFrameEntry.SetPlaceHolder("End Frame")

	// Handle start frame entry changes
	startFrameEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err != nil {
			startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
			return
		}
		// Validate: not less than minFrameNum
		if val < minFrameNum {
			val = minFrameNum
			startFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		// Validate: end - start must be at least 3
		if frameRangeEnd-val < 3 {
			val = frameRangeEnd - 3
			if val < minFrameNum {
				val = minFrameNum
			}
			startFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		if val != frameRangeStart {
			frameRangeStart = val
			logAction(fmt.Sprintf("Set start frame to %.0f", val))
			rebuildPlot()
		}
	}

	// Handle end frame entry changes
	endFrameEntry.OnSubmitted = func(text string) {
		val, err := strconv.ParseFloat(text, 64)
		if err != nil {
			endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
			return
		}
		// Validate: not greater than maxFrameNum
		if val > maxFrameNum {
			val = maxFrameNum
			endFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		// Validate: end - start must be at least 3
		if val-frameRangeStart < 3 {
			val = frameRangeStart + 3
			if val > maxFrameNum {
				val = maxFrameNum
			}
			endFrameEntry.SetText(fmt.Sprintf("%.0f", val))
		}
		if val != frameRangeEnd {
			frameRangeEnd = val
			logAction(fmt.Sprintf("Set end frame to %.0f", val))
			rebuildPlot()
		}
	}

	// Set up scroll wheel zoom on the plot
	lightCurvePlot.SetOnScroll(func(position fyne.Position, scrollDelta float32) {
		if loadedLightCurveData == nil || maxFrameNum == minFrameNum {
			return
		}

		// Get plot size
		plotSize := lightCurvePlot.Size()
		if plotSize.Width <= 0 || plotSize.Height <= 0 {
			return
		}

		// Calculate the relative X position within the plot area (0 to 1)
		// Account for margins
		plotAreaWidth := plotSize.Width - lightCurvePlot.marginLeft - lightCurvePlot.marginRight
		relX := float64((position.X - lightCurvePlot.marginLeft) / plotAreaWidth)
		if relX < 0 {
			relX = 0
		}
		if relX > 1 {
			relX = 1
		}

		// Calculate the frame number under the cursor
		currentRange := frameRangeEnd - frameRangeStart
		frameUnderCursor := frameRangeStart + relX*currentRange

		// Determine zoom factor (scroll up = zoom in, scroll down = zoom out)
		zoomFactor := 1.0
		if scrollDelta > 0 {
			zoomFactor = 0.8 // Zoom in - reduce the range by 20%
		} else if scrollDelta < 0 {
			zoomFactor = 1.25 // Zoom out - increase range by 25%
		} else {
			return
		}

		// Calculate new range
		newRange := currentRange * zoomFactor

		// Ensure a minimum range of 3
		if newRange < 3 {
			newRange = 3
		}

		// Ensure we don't exceed the full data range
		fullRange := maxFrameNum - minFrameNum
		if newRange > fullRange {
			newRange = fullRange
		}

		// Calculate a new start and end, keeping frameUnderCursor at the same relative position
		newStart := frameUnderCursor - relX*newRange
		newEnd := frameUnderCursor + (1-relX)*newRange

		// Clamp to valid bounds
		if newStart < minFrameNum {
			newStart = minFrameNum
			newEnd = newStart + newRange
		}
		if newEnd > maxFrameNum {
			newEnd = maxFrameNum
			newStart = newEnd - newRange
			if newStart < minFrameNum {
				newStart = minFrameNum
			}
		}

		// Update frame range and UI
		if newStart != frameRangeStart || newEnd != frameRangeEnd {
			frameRangeStart = newStart
			frameRangeEnd = newEnd
			startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
			endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
			rebuildPlot()
		}
	})

	// Button to load a CSV file
	loadCSVBtn := widget.NewButton("Open browser to select csv file", func() {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // User cancelled
			}
			filePath := reader.URI().Path()
			if cerr := reader.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
			}

			// Parse the CSV file
			data, err := parseLightCurveCSV(filePath)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to parse CSV: %w", err), w)
				return
			}

			loadedLightCurveData = data

			// Create an action log file for this CSV
			if err := createActionLog(filePath); err != nil {
				fmt.Printf("Warning: could not create log file: %v\n", err)
			}
			logAction(fmt.Sprintf("Loaded CSV with %d columns and %d data points", len(data.Columns), len(data.TimeValues)))

			// Check if the timestamp column was empty (all zeros)
			timestampsEmpty := true
			for _, t := range data.TimeValues {
				if t != 0 {
					timestampsEmpty = false
					break
				}
			}
			if timestampsEmpty && len(data.TimeValues) > 0 {
				dialog.ShowInformation("Warning", "Manual timestamping is required.", w)
			}

			// Clear displayed curves and reset the plot
			for k := range displayedCurves {
				delete(displayedCurves, k)
			}
			lightCurvePlot.SetSeries(nil)

			// Clear range entries and reset the user bounds flag
			userSetBounds = false
			xMinEntry.SetText("")
			xMaxEntry.SetText("")
			yMinEntry.SetText("")
			yMaxEntry.SetText("")

			// Update the list with column names, filtered by selected prefixes
			lightCurveListData = nil
			listIndexToColumnIndex = nil
			checkedCurveIndex = -1 // Reset checked curve
			for i, col := range data.Columns {
				// If "any name" is checked, include all columns
				if acceptAnyName {
					lightCurveListData = append(lightCurveListData, col.Name)
					listIndexToColumnIndex = append(listIndexToColumnIndex, i)
					continue
				}
				// Check if the column name starts with any enabled prefix
				for prefix, enabled := range lightCurvePrefixes {
					if enabled && strings.HasPrefix(col.Name, prefix) {
						lightCurveListData = append(lightCurveListData, col.Name)
						listIndexToColumnIndex = append(listIndexToColumnIndex, i)
						break
					}
				}
			}
			lightCurveList.Refresh()

			// Automatically display the first light curve if available
			if len(listIndexToColumnIndex) > 0 {
				toggleLightCurve(listIndexToColumnIndex[0])
			}

			// Initialize frame number range entries and variables
			if len(data.FrameNumbers) > 0 {
				minFrameNum = data.FrameNumbers[0]
				maxFrameNum = data.FrameNumbers[len(data.FrameNumbers)-1]
				frameRangeStart = minFrameNum
				frameRangeEnd = maxFrameNum
				startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
				endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
			}

			plotStatusLabel.SetText(fmt.Sprintf("Loaded %d light curves (%d shown) with %d data points. Click to toggle display.",
				len(data.Columns), len(lightCurveListData), len(data.TimeValues)))
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})

	lightCurveListScroll := container.NewVScroll(lightCurveList)
	lightCurveListScroll.SetMinSize(fyne.NewSize(200, 300))

	// Button to export selected light curves to CSV
	exportCSVBtn := widget.NewButton("Export selected light curves", func() {
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}
		if len(displayedCurves) == 0 {
			dialog.ShowError(fmt.Errorf("no light curves selected"), w)
			return
		}

		outputPath, err := writeSelectedLightCurves(loadedLightCurveData, displayedCurves, frameRangeStart, frameRangeEnd)
		if err != nil {
			dialog.ShowError(err, w)
			logAction(fmt.Sprintf("Export failed: %v", err))
			return
		}

		logAction(fmt.Sprintf("Exported selected light curves to: %s", outputPath))
		dialog.ShowInformation("Export Complete",
			fmt.Sprintf("Selected light curves exported to:\n%s", outputPath), w)
	})

	// Buttons row for Data tab - wrap in containers with minimum size for full labels
	loadCSVBtnContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 36)), loadCSVBtn)
	exportCSVBtnContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 36)), exportCSVBtn)
	dataTabButtons := container.NewHBox(loadCSVBtnContainer, exportCSVBtnContainer)

	// Frame number range row (entries defined earlier so they can be initialized when CSV is loaded)
	startFrameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), startFrameEntry)
	endFrameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), endFrameEntry)
	frameRangeRow := container.NewHBox(
		widget.NewLabel("Start Frame:"),
		startFrameContainer,
		widget.NewLabel("End Frame:"),
		endFrameContainer,
	)

	tab3Content := container.NewStack(tab3Bg, container.NewPadded(container.NewBorder(
		dataTabButtons,       // top
		frameRangeRow,        // bottom
		nil,                  // left
		nil,                  // right
		lightCurveListScroll, // center
	)))
	tab3 := container.NewTabItem("csv file ops", tab3Content)

	// Tab 4: Reports
	tab4Bg := canvas.NewRectangle(color.RGBA{R: 230, G: 200, B: 220, A: 255})
	radioGroup := widget.NewRadioGroup([]string{
		"Option 1", "Option 2", "Option 3", "Option 4", "Option 5", "Option 6",
	}, func(selected string) {})
	radioGroup.SetSelected("Option 1")
	tab4Content := container.NewStack(tab4Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Reports page content"),
		radioGroup,
	)))
	tab4 := container.NewTabItem("Reports", tab4Content)
	tabs := container.NewAppTabs(tab2, tab3, tab4)

	// Helper function to run IOTAdiffraction with a given parameter file
	runIOTAdiffraction := func(paramFilePath string) {
		// Get the current working directory
		cwd, err := os.Getwd()
		if err != nil {
			dialog.ShowError(fmt.Errorf("could not determine current directory: %v", err), w)
			return
		}

		// Build the path to IOTAdiffraction.exe
		exePath := filepath.Join(cwd, "IOTAdiffraction.exe")

		// Check if the file exists
		if _, err := os.Stat(exePath); os.IsNotExist(err) {
			dialog.ShowInformation("File Not Found",
				"IOTAdiffraction.exe was not found in the current directory.\n\n"+
					"Please ensure the file is located at:\n"+exePath, w)
			return
		}

		// Create output display
		outputLabel := widget.NewLabel("")
		outputLabel.Wrapping = fyne.TextWrapWord

		scrollContainer := container.NewVScroll(container.NewPadded(outputLabel))
		scrollContainer.SetMinSize(fyne.NewSize(500, 300))

		// Create a custom dialog with the output
		outputDialog := dialog.NewCustom("IOTAdiffraction Output", "Close", scrollContainer, w)

		// Set up the command with pipes using the selected file as a parameter
		cmd := exec.Command(exePath, paramFilePath)
		cmd.Dir = cwd

		stdout, err := cmd.StdoutPipe()
		if err != nil {
			dialog.ShowError(fmt.Errorf("error creating stdout pipe: %v", err), w)
			return
		}

		stderr, err := cmd.StderrPipe()
		if err != nil {
			dialog.ShowError(fmt.Errorf("error creating stderr pipe: %v", err), w)
			return
		}

		// Start the command
		if err := cmd.Start(); err != nil {
			dialog.ShowError(fmt.Errorf("error starting IOTAdiffraction: %v", err), w)
			return
		}

		outputDialog.Show()

		// Mutex to protect output text updates
		var mu sync.Mutex
		var outputLines string

		appendOutput := func(line string) {
			mu.Lock()
			outputLines += line + "\n"
			text := outputLines
			mu.Unlock()
			fyne.Do(func() {
				outputLabel.SetText(text)
				scrollContainer.ScrollToBottom()
			})
		}

		// Read stdout in a goroutine
		go func() {
			scanner := bufio.NewScanner(stdout)
			for scanner.Scan() {
				appendOutput(scanner.Text())
			}
		}()

		// Read stderr in a goroutine
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				appendOutput("[stderr] " + scanner.Text())
			}
		}()

		// Wait for completion in a goroutine
		go func() {
			err := cmd.Wait()
			if err != nil {
				appendOutput(fmt.Sprintf("\n[Error: %v]", err))
			} else {
				appendOutput("\n[Process completed successfully]")
			}
		}()
	}

	btnIOTA := widget.NewButton("Run IOTAdiffraction", func() {
		// If a parameters file was previously loaded, use it directly
		if lastLoadedParamsPath != "" {
			runIOTAdiffraction(lastLoadedParamsPath)
			return
		}

		// Otherwise, open the file selection dialog to choose a parameter file
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if reader == nil {
				return // User cancelled
			}
			// Get the file path and close the reader (we don't need to read the content)
			paramFilePath := reader.URI().Path()
			if cerr := reader.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
			}

			runIOTAdiffraction(paramFilePath)
		}, w)
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})
	btnOccultParams := widget.NewButton("Occultation Parameters", func() {
		showOccultationParametersDialog(w)
	})
	buttons := container.NewHBox(btnIOTA, btnOccultParams)

	// Split tabs and plot area
	split := container.NewHSplit(tabs, plotArea)
	splitOffset := prefs.FloatWithFallback("splitOffset", 0.6)
	split.SetOffset(splitOffset)

	content := container.NewBorder(nil, buttons, nil, nil, split)

	w.SetContent(content)
	w.Resize(fyne.NewSize(float32(savedW), float32(savedH)))

	if firstRun {
		w.CenterOnScreen()
	}

	// Save window geometry and split position on close
	w.SetCloseIntercept(func() {
		prefs.SetFloat("splitOffset", split.Offset)
		if hwnd := getForegroundWindow(); hwnd != 0 {
			if x, y, width, height, ok := getWindowRect(hwnd); ok {
				prefs.SetInt("windowX", int(x))
				prefs.SetInt("windowY", int(y))
				prefs.SetInt("windowW", int(width))
				prefs.SetInt("windowH", int(height))
			}
		}
		closeActionLog() // Close the action log file
		w.Close()
	})

	w.Show()

	if !firstRun {
		go func() {
			time.Sleep(100 * time.Millisecond)
			if hwnd := getForegroundWindow(); hwnd != 0 {
				setWindowPos(hwnd, savedX, savedY, savedW, savedH)
			}
		}()
	}

	a.Run()
}
