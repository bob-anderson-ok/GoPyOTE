package main

import (
	"archive/zip"
	"bufio"
	"bytes"
	"encoding/xml"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"
	"github.com/xuri/excelize/v2"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/vg"
	"gonum.org/v1/plot/vg/draw"
	"gonum.org/v1/plot/vg/vgimg"
)

// VizieRTab holds all the widgets for the VizieR export tab
type VizieRTab struct {
	// Date fields
	DateYearEntry  *FocusLossEntry
	DateMonthEntry *FocusLossEntry
	DateDayEntry   *FocusLossEntry

	// Star catalog fields
	StarUCAC4Entry     *FocusLossEntry
	StarTycho2Entry    *FocusLossEntry
	StarHipparcosEntry *FocusLossEntry

	// Site location fields
	SiteLongDegEntry  *FocusLossEntry
	SiteLongMinEntry  *FocusLossEntry
	SiteLongSecsEntry *FocusLossEntry
	SiteLatDegEntry   *FocusLossEntry
	SiteLatMinEntry   *FocusLossEntry
	SiteLatSecsEntry  *FocusLossEntry
	SiteAltitudeEntry *FocusLossEntry

	// Observer and asteroid fields
	ObserverNameEntry   *FocusLossEntry
	AsteroidNumberEntry *FocusLossEntry
	AsteroidNameEntry   *FocusLossEntry

	// Output folder path
	OutputFolderEntry *FocusLossEntry

	// Status label
	StatusLabel *widget.Label

	// The tab item itself
	TabItem *container.TabItem

	// Generate button (exposed so the callback can be set from main)
	GenerateBtn *widget.Button

	// Preview button (exposed so the callback can be set from main)
	PreviewBtn *widget.Button

	// Zip button for zipping .dat files
	ZipBtn *widget.Button

	// Load from the NA spreadsheet button
	LoadXlsxBtn *widget.Button

	// Load from the SODIS form button
	LoadSodisBtn *widget.Button

	// Tab background rectangle (exposed for dark mode toggling)
	TabBg *canvas.Rectangle
}

// NewVizieRTab creates a new VizieR export tab with all its widgets
func NewVizieRTab() *VizieRTab {
	vt := &VizieRTab{}

	// Background
	tabBg := canvas.NewRectangle(color.RGBA{R: 210, G: 220, B: 210, A: 255})
	vt.TabBg = tabBg

	// Date fields
	vt.DateYearEntry = NewFocusLossEntry()
	vt.DateYearEntry.SetPlaceHolder("YYYY")
	vt.DateMonthEntry = NewFocusLossEntry()
	vt.DateMonthEntry.SetPlaceHolder("MM")
	vt.DateDayEntry = NewFocusLossEntry()
	vt.DateDayEntry.SetPlaceHolder("DD")
	dateYearContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), vt.DateYearEntry)
	dateMonthContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(50, 36)), vt.DateMonthEntry)
	dateDayContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(50, 36)), vt.DateDayEntry)

	// Star catalog fields
	vt.StarUCAC4Entry = NewFocusLossEntry()
	vt.StarUCAC4Entry.SetPlaceHolder("xxx-xxxxxx")
	vt.StarTycho2Entry = NewFocusLossEntry()
	vt.StarTycho2Entry.SetPlaceHolder("xxxx-xxxxx-x")
	vt.StarHipparcosEntry = NewFocusLossEntry()
	vt.StarHipparcosEntry.SetPlaceHolder("number")
	starUCAC4Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), vt.StarUCAC4Entry)
	starTycho2Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(130, 36)), vt.StarTycho2Entry)
	starHipparcosContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(100, 36)), vt.StarHipparcosEntry)

	// Site location fields - Longitude
	vt.SiteLongDegEntry = NewFocusLossEntry()
	vt.SiteLongDegEntry.SetPlaceHolder("+/-deg")
	vt.SiteLongMinEntry = NewFocusLossEntry()
	vt.SiteLongMinEntry.SetPlaceHolder("min")
	vt.SiteLongSecsEntry = NewFocusLossEntry()
	vt.SiteLongSecsEntry.SetPlaceHolder("sec")
	siteLongDegContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), vt.SiteLongDegEntry)
	siteLongMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), vt.SiteLongMinEntry)
	siteLongSecsContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), vt.SiteLongSecsEntry)

	// Site location fields - Latitude
	vt.SiteLatDegEntry = NewFocusLossEntry()
	vt.SiteLatDegEntry.SetPlaceHolder("+/-deg")
	vt.SiteLatMinEntry = NewFocusLossEntry()
	vt.SiteLatMinEntry.SetPlaceHolder("min")
	vt.SiteLatSecsEntry = NewFocusLossEntry()
	vt.SiteLatSecsEntry.SetPlaceHolder("sec")
	siteLatDegContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), vt.SiteLatDegEntry)
	siteLatMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), vt.SiteLatMinEntry)
	siteLatSecsContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), vt.SiteLatSecsEntry)

	// Altitude
	vt.SiteAltitudeEntry = NewFocusLossEntry()
	vt.SiteAltitudeEntry.SetPlaceHolder("meters")
	siteAltitudeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 36)), vt.SiteAltitudeEntry)

	// Observer name
	vt.ObserverNameEntry = NewFocusLossEntry()
	vt.ObserverNameEntry.SetPlaceHolder("Observer name")
	observerNameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), vt.ObserverNameEntry)

	// Asteroid fields
	vt.AsteroidNumberEntry = NewFocusLossEntry()
	vt.AsteroidNumberEntry.SetPlaceHolder("number")
	vt.AsteroidNameEntry = NewFocusLossEntry()
	vt.AsteroidNameEntry.SetPlaceHolder("name")
	asteroidNumberContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 36)), vt.AsteroidNumberEntry)
	asteroidNameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(150, 36)), vt.AsteroidNameEntry)

	// Output folder path with default
	vt.OutputFolderEntry = NewFocusLossEntry()
	defaultOutputFolder := ""
	if homeDir, err := os.UserHomeDir(); err == nil {
		defaultOutputFolder = filepath.Join(homeDir, "Documents", "VizieR")
	}
	vt.OutputFolderEntry.SetText(defaultOutputFolder)
	vt.OutputFolderEntry.SetPlaceHolder("path to output folder")
	outputFolderContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(350, 36)), vt.OutputFolderEntry)

	// Status label
	vt.StatusLabel = widget.NewLabel("Enter observation data for VizieR export")
	vt.StatusLabel.Wrapping = fyne.TextWrapWord

	// Generate button (callback set later via SetGenerateCallback)
	vt.GenerateBtn = widget.NewButton("Generate VizieR .dat file", func() {})
	vt.GenerateBtn.Disable()

	// Preview button (callback set later from main)
	vt.PreviewBtn = widget.NewButton("Preview submission", func() {})
	vt.PreviewBtn.Importance = widget.HighImportance

	// Clear inputs button
	clearBtn := widget.NewButton("Clear inputs", func() {
		vt.ClearInputs()
	})

	// Zip button (callback set below, needs access to vt)
	vt.ZipBtn = widget.NewButton("Zip *.dat files for sending", func() {})

	// Load from the NA spreadsheet button (callback set below, needs access to window)
	vt.LoadXlsxBtn = widget.NewButton("Load from NA spreadsheet", func() {})

	// Load from the SODIS form button (callback set below, needs access to window)
	vt.LoadSodisBtn = widget.NewButton("Copy from SODIS-REPORT.txt", func() {})

	// Build the tab content
	tabContent := container.NewStack(tabBg, container.NewPadded(container.NewVBox(
		widget.NewLabel("VizieR Export"),
		widget.NewSeparator(),
		container.NewHBox(widget.NewLabel("Date:"), widget.NewLabel("Year"), dateYearContainer, widget.NewLabel("Month"), dateMonthContainer, widget.NewLabel("Day"), dateDayContainer),
		widget.NewSeparator(),
		widget.NewLabel("Star (enter ONE catalog ID):"),
		container.NewHBox(widget.NewLabel("UCAC4:"), starUCAC4Container, widget.NewLabel("Tycho2:"), starTycho2Container, widget.NewLabel("Hipparcos:"), starHipparcosContainer),
		widget.NewSeparator(),
		widget.NewLabel("Site Location:"),
		container.NewHBox(widget.NewLabel("Longitude:"), siteLongDegContainer, widget.NewLabel("°"), siteLongMinContainer, widget.NewLabel("'"), siteLongSecsContainer, widget.NewLabel("\"")),
		container.NewHBox(widget.NewLabel("Latitude:"), siteLatDegContainer, widget.NewLabel("°"), siteLatMinContainer, widget.NewLabel("'"), siteLatSecsContainer, widget.NewLabel("\"")),
		container.NewHBox(widget.NewLabel("Altitude (m):"), siteAltitudeContainer),
		widget.NewSeparator(),
		container.NewHBox(widget.NewLabel("Observer:"), observerNameContainer),
		widget.NewSeparator(),
		widget.NewLabel("Asteroid:"),
		container.NewHBox(widget.NewLabel("Number:"), asteroidNumberContainer, widget.NewLabel("Name:"), asteroidNameContainer),
		widget.NewSeparator(),
		container.NewHBox(widget.NewLabel("Output folder:"), outputFolderContainer),
		widget.NewSeparator(),
		container.NewHBox(vt.PreviewBtn, vt.GenerateBtn, vt.ZipBtn),
		container.NewHBox(clearBtn, vt.LoadXlsxBtn, vt.LoadSodisBtn),
		widget.NewSeparator(),
		vt.StatusLabel,
	)))

	vt.TabItem = container.NewTabItem("VizieR", tabContent)
	return vt
}

// ClearInputs clears all input fields
func (vt *VizieRTab) ClearInputs() {
	vt.DateYearEntry.SetText("")
	vt.DateMonthEntry.SetText("")
	vt.DateDayEntry.SetText("")
	vt.StarUCAC4Entry.SetText("")
	vt.StarTycho2Entry.SetText("")
	vt.StarHipparcosEntry.SetText("")
	vt.SiteLongDegEntry.SetText("")
	vt.SiteLongMinEntry.SetText("")
	vt.SiteLongSecsEntry.SetText("")
	vt.SiteLatDegEntry.SetText("")
	vt.SiteLatMinEntry.SetText("")
	vt.SiteLatSecsEntry.SetText("")
	vt.SiteAltitudeEntry.SetText("")
	vt.ObserverNameEntry.SetText("")
	vt.AsteroidNumberEntry.SetText("")
	vt.AsteroidNameEntry.SetText("")
}

// FillStarFromOccelmntXml parses the first <Event>'s <Star> element from the
// supplied occelmnt XML. If the first comma-separated field starts with "UCAC4"
// (case-insensitive), the UCAC4 identifier that follows is written to StarUCAC4Entry.
func (vt *VizieRTab) FillStarFromOccelmntXml(xmlText string) {
	if xmlText == "" {
		return
	}
	var occ Occultations
	if err := xml.Unmarshal([]byte(xmlText), &occ); err != nil || len(occ.Events) == 0 {
		return
	}
	star := strings.TrimSpace(strings.SplitN(occ.Events[0].Star, ",", 2)[0])
	if strings.HasPrefix(strings.ToUpper(star), "UCAC4") {
		fields := strings.Fields(star)
		if len(fields) >= 2 {
			vt.StarUCAC4Entry.SetText(fields[1])
		}
	}
}

// ValidateInputs validates all input fields and returns an error if any are invalid
func (vt *VizieRTab) ValidateInputs(w fyne.Window) (year, month, day int, err error) {
	// Validate required fields
	if vt.ObserverNameEntry.Text == "" {
		return 0, 0, 0, fmt.Errorf("please enter an observer name")
	}
	if vt.AsteroidNumberEntry.Text == "" {
		return 0, 0, 0, fmt.Errorf("please enter an asteroid number")
	}
	if vt.AsteroidNameEntry.Text == "" {
		return 0, 0, 0, fmt.Errorf("please enter an asteroid name")
	}
	if len(vt.AsteroidNumberEntry.Text) > 6 {
		return 0, 0, 0, fmt.Errorf("asteroid number is restricted to a max of 6 digits")
	}

	// Validate date fields
	year, err = strconv.Atoi(vt.DateYearEntry.Text)
	if err != nil || year < 1900 || year > 2100 {
		return 0, 0, 0, fmt.Errorf("please enter a valid year (1900-2100)")
	}
	month, err = strconv.Atoi(vt.DateMonthEntry.Text)
	if err != nil || month < 1 || month > 12 {
		return 0, 0, 0, fmt.Errorf("please enter a valid month (1-12)")
	}
	day, err = strconv.Atoi(vt.DateDayEntry.Text)
	if err != nil || day < 1 || day > 31 {
		return 0, 0, 0, fmt.Errorf("please enter a valid day (1-31)")
	}

	// Check star catalog entries - need exactly one or all empty
	emptyStarFields := 0
	if vt.StarUCAC4Entry.Text == "" {
		emptyStarFields++
	}
	if vt.StarTycho2Entry.Text == "" {
		emptyStarFields++
	}
	if vt.StarHipparcosEntry.Text == "" {
		emptyStarFields++
	}

	if emptyStarFields == 3 {
		dialog.ShowInformation("No Star ID",
			"You have not entered a star catalog number. This is acceptable IF intentional.\n\n"+
				"It may be intentional because the involved star does not have a supported catalog number type.\n\n"+
				"VizieR accepts a no-star entry, so it is not a problem to leave all star fields empty.\n\n"+
				"Best practice is to use the star designation from the Occult4 prediction whenever possible.", w)
	}
	if emptyStarFields == 1 || emptyStarFields == 0 {
		return 0, 0, 0, fmt.Errorf("please use a single star number.\n\nBest practice is to use the star designation from the Occult4 prediction whenever possible")
	}

	// Validate UCAC4 format if provided
	if vt.StarUCAC4Entry.Text != "" {
		parts := strings.Split(vt.StarUCAC4Entry.Text, "-")
		if len(parts) != 2 {
			return 0, 0, 0, fmt.Errorf("UCAC4 star designation has incorrect format.\n\nThe correct form is: xxx-xxxxxx")
		}
		if len(parts[0]) > 3 || len(parts[1]) > 6 {
			return 0, 0, 0, fmt.Errorf("UCAC4 star designation has incorrect format.\n\nThe correct form is: xxx-xxxxxx\n\nThere are too many digits in one of the fields")
		}
	}

	// Validate the Tycho2 format if provided
	if vt.StarTycho2Entry.Text != "" {
		parts := strings.Split(vt.StarTycho2Entry.Text, "-")
		if len(parts) != 3 {
			return 0, 0, 0, fmt.Errorf("Tycho2 star designation has incorrect format.\n\nThe correct form is: xxxx-xxxxx-x")
		}
		if len(parts[0]) > 4 || len(parts[1]) > 5 || len(parts[2]) != 1 {
			return 0, 0, 0, fmt.Errorf("Tycho2 star designation has incorrect format.\n\nThe correct form is: xxxx-xxxxx-x\n\nThere are too many digits in one of the fields")
		}
	}

	// Validate location fields
	if vt.SiteLongDegEntry.Text == "" || vt.SiteLongMinEntry.Text == "" || vt.SiteLongSecsEntry.Text == "" {
		return 0, 0, 0, fmt.Errorf("please enter complete longitude (deg, min, sec)")
	}
	if vt.SiteLatDegEntry.Text == "" || vt.SiteLatMinEntry.Text == "" || vt.SiteLatSecsEntry.Text == "" {
		return 0, 0, 0, fmt.Errorf("please enter complete latitude (deg, min, sec)")
	}
	if vt.SiteAltitudeEntry.Text == "" {
		return 0, 0, 0, fmt.Errorf("please enter site altitude")
	}

	return year, month, day, nil
}

// GetFormattedStarIDs returns the star catalog IDs with defaults applied
func (vt *VizieRTab) GetFormattedStarIDs() (hipparcos, tycho2, ucac4 string) {
	hipparcos = vt.StarHipparcosEntry.Text
	if hipparcos == "" {
		hipparcos = "0"
	}
	tycho2 = vt.StarTycho2Entry.Text
	if tycho2 == "" {
		tycho2 = "0-0-1"
	}
	ucac4 = vt.StarUCAC4Entry.Text
	if ucac4 == "" {
		ucac4 = "0-0"
	}
	return
}

// GetFormattedLocation returns the location fields with sign prefixes
func (vt *VizieRTab) GetFormattedLocation() (longDeg, longMin, longSecs, latDeg, latMin, latSecs, altitude string) {
	longDeg = vt.SiteLongDegEntry.Text
	if !strings.HasPrefix(longDeg, "-") && !strings.HasPrefix(longDeg, "+") {
		longDeg = "+" + longDeg
	}
	latDeg = vt.SiteLatDegEntry.Text
	if !strings.HasPrefix(latDeg, "-") && !strings.HasPrefix(latDeg, "+") {
		latDeg = "+" + latDeg
	}
	return longDeg, vt.SiteLongMinEntry.Text, vt.SiteLongSecsEntry.Text,
		latDeg, vt.SiteLatMinEntry.Text, vt.SiteLatSecsEntry.Text,
		vt.SiteAltitudeEntry.Text
}

// generateVizieRFile creates a VizieR format file from the light curve data
func generateVizieRFile(w fyne.Window, data *LightCurveData, year, month, day int,
	hipparcos, tycho2, ucac4 string,
	longDeg, longMin, longSecs string,
	latDeg, latMin, latSecs string,
	altitude, observer string,
	asteroidNumber, asteroidName string,
	rangeStart, rangeEnd int,
	outputFolder string,
	statusLabel *widget.Label) {

	if data == nil || len(data.Columns) == 0 {
		dialog.ShowError(fmt.Errorf("no light curve data available"), w)
		return
	}

	// Find the indices corresponding to the frame range
	startIdx := 0
	endIdx := len(data.FrameNumbers) - 1
	foundStart := false
	for i, frameNum := range data.FrameNumbers {
		if int(frameNum) >= rangeStart && !foundStart {
			startIdx = i
			foundStart = true
		}
		if int(frameNum) <= rangeEnd {
			endIdx = i
		}
	}

	// Get timestamps for the range
	initialTimestamp := data.TimeValues[startIdx]
	finalTimestamp := data.TimeValues[endIdx]
	deltaTime := finalTimestamp - initialTimestamp
	numReadings := endIdx - startIdx + 1

	// Format the initial timestamp as hh:mm:ss.ss
	vizierTimestamp := formatSecondsAsTimestamp(initialTimestamp)
	// Remove leading/trailing characters if needed and truncate to hh:mm:ss.ss format
	if len(vizierTimestamp) > 11 {
		vizierTimestamp = vizierTimestamp[:11]
	}

	// Build the Date line
	dateText := fmt.Sprintf("Date: %d-%d-%d %s: %.2f: %d", year, month, day, vizierTimestamp, deltaTime, numReadings)

	// Build the Star line (SAO, XZ80Q, Kepler2 are legacy and set to 0)
	starText := fmt.Sprintf("Star: %s: 0: 0: 0: %s: %s", hipparcos, tycho2, ucac4)

	// Build the Observer line
	locationText := fmt.Sprintf("Observer: %s:%s:%s: %s:%s:%s: %s: %s",
		longDeg, longMin, longSecs, latDeg, latMin, latSecs, altitude, observer)

	// Build the Object line
	objectText := fmt.Sprintf("Object: Asteroid: %s: %s", asteroidNumber, asteroidName)

	// Build the Values line
	// Use the first signal column (or first available column) for the values
	var valueColumn []float64
	for _, col := range data.Columns {
		if strings.HasPrefix(col.Name, "signal") {
			valueColumn = col.Values
			break
		}
	}
	// Fallback to the first column if no signal column found
	if valueColumn == nil && len(data.Columns) > 0 {
		valueColumn = data.Columns[0].Values
	}

	if valueColumn == nil {
		dialog.ShowError(fmt.Errorf("no data columns available for export"), w)
		return
	}

	// Find max value for scaling (ignoring dropped/interpolated frames)
	maxValue := 0.0
	for i := startIdx; i <= endIdx; i++ {
		if !isInterpolatedIndex(i) && valueColumn[i] > maxValue {
			maxValue = valueColumn[i]
		}
	}

	// Compute scale factor to normalize to 0-9524 range
	scaleFactor := 9524.0 / maxValue
	if maxValue == 0 {
		scaleFactor = 1.0
	}

	// Build values string
	valuesText := "Values"
	for i := startIdx; i <= endIdx; i++ {
		// Check for dropped/interpolated frame
		if isInterpolatedIndex(i) {
			valuesText += ": "
		} else {
			scaledValue := int(valueColumn[i] * scaleFactor)
			valuesText += fmt.Sprintf(":%d", scaledValue)
		}
	}

	// Compose file name: (asteroidNumber)_YYYYMMdd_hhmmss_ss.dat
	timestampParts := strings.Split(vizierTimestamp, ":")
	var hh, mm, sss string
	if len(timestampParts) >= 3 {
		hh = timestampParts[0]
		mm = timestampParts[1]
		secParts := strings.Split(timestampParts[2], ".")
		if len(secParts) >= 2 {
			sss = fmt.Sprintf("%s_%s", secParts[0], secParts[1])
		} else {
			sss = secParts[0] + "_00"
		}
	} else {
		hh = "00"
		mm = "00"
		sss = "00_00"
	}

	filename := fmt.Sprintf("(%s)_%d%02d%02d_%s%s%s.dat", asteroidNumber, year, month, day, hh, mm, sss)

	// Determine the output directory
	destDir := outputFolder
	if destDir == "" {
		// Default to Documents/VizieR if no folder specified
		homeDir, err := os.UserHomeDir()
		if err != nil {
			dialog.ShowError(fmt.Errorf("could not determine home directory: %v", err), w)
			return
		}
		destDir = filepath.Join(homeDir, "Documents", "VizieR")
	}

	// Create a directory if it doesn't exist
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		if err := os.MkdirAll(destDir, 0755); err != nil {
			dialog.ShowError(fmt.Errorf("could not create output directory: %v", err), w)
			return
		}
	}

	vizierFilePath := filepath.Join(destDir, filename)

	// Write the file with CRLF line endings
	CRLF := "\r\n"

	file, err := os.Create(vizierFilePath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not create file: %v", err), w)
		return
	}

	// Use a flag to track if we need to close the file on error
	success := false
	defer func() {
		if !success {
			_ = file.Close() // Ignore close error when we already have a writing error
		}
	}()

	if _, err := file.WriteString(dateText + CRLF); err != nil {
		dialog.ShowError(fmt.Errorf("error writing date to file: %v", err), w)
		return
	}

	if _, err := file.WriteString(starText + CRLF); err != nil {
		dialog.ShowError(fmt.Errorf("error writing star data to file: %v", err), w)
		return
	}

	if _, err := file.WriteString(locationText + CRLF); err != nil {
		dialog.ShowError(fmt.Errorf("error writing location to file: %v", err), w)
		return
	}

	if _, err := file.WriteString(objectText + CRLF); err != nil {
		dialog.ShowError(fmt.Errorf("error writing object data to file: %v", err), w)
		return
	}

	if _, err := file.WriteString(valuesText + CRLF); err != nil {
		dialog.ShowError(fmt.Errorf("error writing values to file: %v", err), w)
		return
	}

	// Mark success so defer doesn't close, then close explicitly with error check
	success = true
	if err := file.Close(); err != nil {
		dialog.ShowError(fmt.Errorf("error closing file: %v", err), w)
		return
	}

	// Copy to the results folder if available
	resultsCopyMsg := ""
	if resultsFolder != "" {
		copyPath := filepath.Join(resultsFolder, filename)
		if copyData, err := os.ReadFile(vizierFilePath); err == nil {
			if err := os.WriteFile(copyPath, copyData, 0644); err != nil {
				fmt.Printf("Warning: could not copy VizieR file to results folder: %v\n", err)
			} else {
				resultsCopyMsg = fmt.Sprintf("\nCopy written to:\n%s", copyPath)
			}
		}
	}

	statusLabel.SetText(fmt.Sprintf("VizieR file written to:\n%s%s", vizierFilePath, resultsCopyMsg))
	dialog.ShowInformation("VizieR Export Complete",
		fmt.Sprintf("Your VizieR lightcurve file was written to:\n\n%s%s", vizierFilePath, resultsCopyMsg), w)
	vizierDatWrittenThisSession = true

	// Log VizieR page entries
	logAction(fmt.Sprintf("Generated VizieR file: %s", vizierFilePath))
	logAction(fmt.Sprintf("  VizieR file written to: %s", vizierFilePath))
	if resultsCopyMsg != "" {
		logAction(fmt.Sprintf("  Copy written to results folder: %s", filepath.Join(resultsFolder, filename)))
	}
	logAction(fmt.Sprintf("  Date: %d-%02d-%02d", year, month, day))
	logAction(fmt.Sprintf("  Timestamp: %s, Delta: %.2f sec, Readings: %d", vizierTimestamp, deltaTime, numReadings))
	logAction(fmt.Sprintf("  Frame range: %d to %d", rangeStart, rangeEnd))
	if hipparcos != "" {
		logAction(fmt.Sprintf("  Hipparcos: %s", hipparcos))
	}
	if tycho2 != "" {
		logAction(fmt.Sprintf("  Tycho-2: %s", tycho2))
	}
	if ucac4 != "" {
		logAction(fmt.Sprintf("  UCAC4: %s", ucac4))
	}
	logAction(fmt.Sprintf("  Longitude: %s° %s' %s\"", longDeg, longMin, longSecs))
	logAction(fmt.Sprintf("  Latitude: %s° %s' %s\"", latDeg, latMin, latSecs))
	logAction(fmt.Sprintf("  Altitude: %s m", altitude))
	logAction(fmt.Sprintf("  Observer: %s", observer))
	logAction(fmt.Sprintf("  Asteroid: (%s) %s", asteroidNumber, asteroidName))
}

// zipDatFiles zips all .dat files in the specified directory
func zipDatFiles(w fyne.Window, outputFolder string, statusLabel *widget.Label) {
	// Determine the directory to zip
	destDir := outputFolder
	if destDir == "" {
		homeDir, err := os.UserHomeDir()
		if err != nil {
			dialog.ShowError(fmt.Errorf("could not determine home directory: %v", err), w)
			return
		}
		destDir = filepath.Join(homeDir, "Documents", "VizieR")
	}

	// Check if a directory exists
	if _, err := os.Stat(destDir); os.IsNotExist(err) {
		dialog.ShowError(fmt.Errorf("output directory does not exist: %s", destDir), w)
		return
	}

	// Find all .dat files in the directory
	pattern := filepath.Join(destDir, "*.dat")
	datFiles, err := filepath.Glob(pattern)
	if err != nil {
		dialog.ShowError(fmt.Errorf("error searching for .dat files: %v", err), w)
		return
	}

	if len(datFiles) == 0 {
		dialog.ShowInformation("No Files Found",
			fmt.Sprintf("No .dat files found in:\n%s", destDir), w)
		return
	}

	// Create a zip file name with timestamp
	timestamp := time.Now().Format("20060102_150405")
	zipFileName := fmt.Sprintf("VizieR_dat_files_%s.zip", timestamp)
	zipFilePath := filepath.Join(destDir, zipFileName)

	// Create the zip file
	zipFile, err := os.Create(zipFilePath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not create zip file: %v", err), w)
		return
	}

	// Use the success flag to handle deferred closes properly
	success := false
	defer func() {
		if !success {
			_ = zipFile.Close()
		}
	}()

	zipWriter := zip.NewWriter(zipFile)
	defer func() {
		if !success {
			_ = zipWriter.Close()
		}
	}()

	// Add each .dat file to the zip
	filesAdded := 0
	for _, datFilePath := range datFiles {
		err := addFileToZip(zipWriter, datFilePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("error adding %s to zip: %v", filepath.Base(datFilePath), err), w)
			return
		}
		filesAdded++
	}

	// Close the zip writer to flush all data
	if err := zipWriter.Close(); err != nil {
		dialog.ShowError(fmt.Errorf("error finalizing zip file: %v", err), w)
		return
	}

	// Close the underlying file
	if err := zipFile.Close(); err != nil {
		dialog.ShowError(fmt.Errorf("error closing zip file: %v", err), w)
		return
	}

	// Mark success so defers don't close again
	success = true

	// Delete the .dat files after successful zip
	filesDeleted := 0
	for _, datFilePath := range datFiles {
		if err := os.Remove(datFilePath); err != nil {
			dialog.ShowError(fmt.Errorf("error deleting %s: %v", filepath.Base(datFilePath), err), w)
		} else {
			filesDeleted++
		}
	}

	statusLabel.SetText(fmt.Sprintf("Zipped %d .dat files to:\n%s\n(%d files deleted)", filesAdded, zipFilePath, filesDeleted))
	dialog.ShowInformation("Zip Complete",
		fmt.Sprintf("Successfully zipped %d .dat file(s) to:\n\n%s\n\n%d .dat file(s) deleted.", filesAdded, zipFilePath, filesDeleted), w)
	logAction(fmt.Sprintf("Zipped %d .dat files to: %s, deleted %d files", filesAdded, zipFilePath, filesDeleted))

	// Reminder to email the zip file
	dialog.ShowInformation("Email Reminder",
		fmt.Sprintf("Please email the zip file to:\n\nHeraldDR@bigpond.com\n\nFile: %s", zipFileName), w)
}

// addFileToZip adds a single file to a zip archive
func addFileToZip(zipWriter *zip.Writer, filePath string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}

	// Use the success flag to handle deferred close properly
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()

	// Get file info for the header
	info, err := file.Stat()
	if err != nil {
		return err
	}

	// Create zip header
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return err
	}

	// Use only the base name (not the full path)
	header.Name = filepath.Base(filePath)
	header.Method = zip.Deflate

	// Create a writer for this file in the zip
	writer, err := zipWriter.CreateHeader(header)
	if err != nil {
		return err
	}

	// Copy file contents to zip
	_, err = io.Copy(writer, file)
	if err != nil {
		return err
	}

	// Explicitly close and check for error
	if err := file.Close(); err != nil {
		return err
	}

	success = true
	return nil
}

// decimalToDMS converts a decimal degree value to degrees, minutes, seconds strings
func decimalToDMS(decimal float64) (deg, minutes, sec string) {
	negative := decimal < 0
	if negative {
		decimal = -decimal
	}

	degrees := int(decimal)
	remainder := (decimal - float64(degrees)) * 60
	mins := int(remainder)
	seconds := (remainder - float64(mins)) * 60

	if negative {
		deg = fmt.Sprintf("-%d", degrees)
	} else {
		deg = fmt.Sprintf("%d", degrees)
	}
	minutes = fmt.Sprintf("%d", mins)
	sec = fmt.Sprintf("%.2f", seconds)

	return deg, minutes, sec
}

// isRavfSource checks if the CSV headers indicate the source was a RAVF file
// by looking for the "#Instrument: Astrid" header line (case-insensitive)
func isRavfSource(headers []string) bool {
	for _, header := range headers {
		upperHeader := strings.ToUpper(header)
		if strings.HasPrefix(upperHeader, "#INSTRUMENT:") {
			if strings.Contains(upperHeader, "ASTRID") {
				return true
			}
		}
	}
	return false
}

// FillFromRavfHeaders populates VizieR tab fields from RAVF CSV headers
func (vt *VizieRTab) FillFromRavfHeaders(headers []string) {
	if !isRavfSource(headers) {
		return
	}

	for _, header := range headers {
		// Date at frame
		if strings.HasPrefix(header, "# date at frame") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				dateParts := strings.Split(strings.TrimSpace(parts[1]), "-")
				if len(dateParts) >= 3 {
					vt.DateYearEntry.SetText(strings.TrimSpace(dateParts[0]))
					vt.DateMonthEntry.SetText(strings.TrimSpace(dateParts[1]))
					vt.DateDayEntry.SetText(strings.TrimSpace(dateParts[2]))
				}
			}
			continue
		}

		// Observer
		if strings.HasPrefix(header, "#OBSERVER:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				vt.ObserverNameEntry.SetText(strings.TrimSpace(parts[1]))
			}
			continue
		}

		// Latitude
		if strings.HasPrefix(header, "#LATITUDE:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				latVal, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					deg, minutes, sec := decimalToDMS(latVal)
					vt.SiteLatDegEntry.SetText(deg)
					vt.SiteLatMinEntry.SetText(minutes)
					vt.SiteLatSecsEntry.SetText(sec)
				}
			}
			continue
		}

		// Longitude
		if strings.HasPrefix(header, "#LONGITUDE:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				lonVal, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					deg, minutes, sec := decimalToDMS(lonVal)
					vt.SiteLongDegEntry.SetText(deg)
					vt.SiteLongMinEntry.SetText(minutes)
					vt.SiteLongSecsEntry.SetText(sec)
				}
			}
			continue
		}

		// Altitude
		if strings.HasPrefix(header, "#ALTITUDE:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				altVal, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					vt.SiteAltitudeEntry.SetText(fmt.Sprintf("%.0f", altVal))
				}
			}
			continue
		}

		// Asteroid number
		if strings.HasPrefix(header, "#OCCULTATION-OBJECT-NUMBER:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				vt.AsteroidNumberEntry.SetText(strings.TrimSpace(parts[1]))
			}
			continue
		}

		// Asteroid name
		if strings.HasPrefix(header, "#OCCULTATION-OBJECT-NAME:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				vt.AsteroidNameEntry.SetText(strings.TrimSpace(parts[1]))
			}
			continue
		}

		// Star catalog
		if strings.HasPrefix(header, "#OCCULTATION-STAR") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				starParts := strings.Fields(strings.TrimSpace(parts[1]))
				if len(starParts) >= 2 {
					catalog := strings.ToUpper(starParts[0])
					starID := starParts[1]
					if strings.HasPrefix(catalog, "U") {
						vt.StarUCAC4Entry.SetText(starID)
					} else if strings.HasPrefix(catalog, "T") {
						vt.StarTycho2Entry.SetText(starID)
					} else if strings.HasPrefix(catalog, "H") {
						vt.StarHipparcosEntry.SetText(starID)
					}
				}
			}
			continue
		}
	}

	// Log the fields that were filled from RAVF headers
	logAction("VizieR fields filled from RAVF headers:")
	if vt.DateYearEntry.Text != "" {
		logAction(fmt.Sprintf("  Date: %s-%s-%s", vt.DateYearEntry.Text, vt.DateMonthEntry.Text, vt.DateDayEntry.Text))
	}
	if vt.ObserverNameEntry.Text != "" {
		logAction(fmt.Sprintf("  Observer: %s", vt.ObserverNameEntry.Text))
	}
	if vt.SiteLatDegEntry.Text != "" {
		logAction(fmt.Sprintf("  Latitude: %s° %s' %s\"", vt.SiteLatDegEntry.Text, vt.SiteLatMinEntry.Text, vt.SiteLatSecsEntry.Text))
	}
	if vt.SiteLongDegEntry.Text != "" {
		logAction(fmt.Sprintf("  Longitude: %s° %s' %s\"", vt.SiteLongDegEntry.Text, vt.SiteLongMinEntry.Text, vt.SiteLongSecsEntry.Text))
	}
	if vt.SiteAltitudeEntry.Text != "" {
		logAction(fmt.Sprintf("  Altitude: %s m", vt.SiteAltitudeEntry.Text))
	}
	if vt.AsteroidNumberEntry.Text != "" || vt.AsteroidNameEntry.Text != "" {
		logAction(fmt.Sprintf("  Asteroid: (%s) %s", vt.AsteroidNumberEntry.Text, vt.AsteroidNameEntry.Text))
	}
	if vt.StarUCAC4Entry.Text != "" {
		logAction(fmt.Sprintf("  UCAC4: %s", vt.StarUCAC4Entry.Text))
	}
	if vt.StarTycho2Entry.Text != "" {
		logAction(fmt.Sprintf("  Tycho-2: %s", vt.StarTycho2Entry.Text))
	}
	if vt.StarHipparcosEntry.Text != "" {
		logAction(fmt.Sprintf("  Hipparcos: %s", vt.StarHipparcosEntry.Text))
	}
}

// isAdvSource checks if the CSV headers indicate the source was an ADV file
// by looking for "ADV-VERSION" in a header line (case-insensitive)
func isAdvSource(headers []string) bool {
	for _, header := range headers {
		upperHeader := strings.ToUpper(header)
		if strings.Contains(upperHeader, "ADV-VERSION") {
			return true
		}
	}
	return false
}

// FillFromAdvHeaders populates VizieR tab fields from ADV CSV headers
func (vt *VizieRTab) FillFromAdvHeaders(headers []string) {
	if !isAdvSource(headers) {
		return
	}

	for _, header := range headers {
		// Date at frame
		if strings.HasPrefix(header, "# date at frame") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				dateParts := strings.Split(strings.TrimSpace(parts[1]), "-")
				if len(dateParts) >= 3 {
					vt.DateYearEntry.SetText(strings.TrimSpace(dateParts[0]))
					vt.DateMonthEntry.SetText(strings.TrimSpace(dateParts[1]))
					vt.DateDayEntry.SetText(strings.TrimSpace(dateParts[2]))
				}
			}
			continue
		}

		// Observer
		if strings.HasPrefix(header, "#OBSERVER:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				vt.ObserverNameEntry.SetText(strings.TrimSpace(parts[1]))
			}
			continue
		}

		// Latitude
		if strings.HasPrefix(header, "#LATITUDE:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				latVal, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					deg, minutes, sec := decimalToDMS(latVal)
					vt.SiteLatDegEntry.SetText(deg)
					vt.SiteLatMinEntry.SetText(minutes)
					vt.SiteLatSecsEntry.SetText(sec)
				}
			}
			continue
		}

		// Longitude
		if strings.HasPrefix(header, "#LONGITUDE:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				lonVal, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					deg, minutes, sec := decimalToDMS(lonVal)
					vt.SiteLongDegEntry.SetText(deg)
					vt.SiteLongMinEntry.SetText(minutes)
					vt.SiteLongSecsEntry.SetText(sec)
				}
			}
			continue
		}

		// Altitude
		if strings.HasPrefix(header, "#ALTITUDE:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				altVal, err := strconv.ParseFloat(strings.TrimSpace(parts[1]), 64)
				if err == nil {
					vt.SiteAltitudeEntry.SetText(fmt.Sprintf("%.0f", altVal))
				}
			}
			continue
		}

		// OBJNAME contains asteroid info and star catalog
		// Format: "(123) AsteroidName occ. U 123-456789"
		if strings.HasPrefix(header, "#OBJNAME:") {
			parts := strings.SplitN(header, ":", 2)
			if len(parts) >= 2 {
				objName := strings.TrimSpace(parts[1])
				// Split by "occ." to separate asteroid info from star info
				occParts := strings.Split(objName, "occ.")
				if len(occParts) >= 1 {
					// Parse asteroid number and name from "(123) AsteroidName"
					asteroidPart := strings.TrimSpace(occParts[0])
					// Find closing parenthesis to extract a number
					if idx := strings.Index(asteroidPart, ")"); idx > 0 {
						// Extract number between "(" and ")"
						if startIdx := strings.Index(asteroidPart, "("); startIdx >= 0 && startIdx < idx {
							asteroidNum := strings.TrimSpace(asteroidPart[startIdx+1 : idx])
							vt.AsteroidNumberEntry.SetText(asteroidNum)
						}
						// Extract name after ")"
						asteroidName := strings.TrimSpace(asteroidPart[idx+1:])
						vt.AsteroidNameEntry.SetText(asteroidName)
					}
				}
				if len(occParts) >= 2 {
					// Parse star catalog from "U 123-456789" or similar
					starPart := strings.TrimSpace(occParts[1])
					starFields := strings.Fields(starPart)
					if len(starFields) >= 2 {
						catalog := strings.ToUpper(starFields[0])
						starID := starFields[1]
						if strings.HasPrefix(catalog, "U") {
							vt.StarUCAC4Entry.SetText(starID)
						} else if strings.HasPrefix(catalog, "T") {
							vt.StarTycho2Entry.SetText(starID)
						} else if strings.HasPrefix(catalog, "H") {
							vt.StarHipparcosEntry.SetText(starID)
						}
					}
				}
			}
			continue
		}
	}

	// Log the fields that were filled from ADV headers
	logAction("VizieR fields filled from ADV headers:")
	if vt.DateYearEntry.Text != "" {
		logAction(fmt.Sprintf("  Date: %s-%s-%s", vt.DateYearEntry.Text, vt.DateMonthEntry.Text, vt.DateDayEntry.Text))
	}
	if vt.ObserverNameEntry.Text != "" {
		logAction(fmt.Sprintf("  Observer: %s", vt.ObserverNameEntry.Text))
	}
	if vt.SiteLatDegEntry.Text != "" {
		logAction(fmt.Sprintf("  Latitude: %s° %s' %s\"", vt.SiteLatDegEntry.Text, vt.SiteLatMinEntry.Text, vt.SiteLatSecsEntry.Text))
	}
	if vt.SiteLongDegEntry.Text != "" {
		logAction(fmt.Sprintf("  Longitude: %s° %s' %s\"", vt.SiteLongDegEntry.Text, vt.SiteLongMinEntry.Text, vt.SiteLongSecsEntry.Text))
	}
	if vt.SiteAltitudeEntry.Text != "" {
		logAction(fmt.Sprintf("  Altitude: %s m", vt.SiteAltitudeEntry.Text))
	}
	if vt.AsteroidNumberEntry.Text != "" || vt.AsteroidNameEntry.Text != "" {
		logAction(fmt.Sprintf("  Asteroid: (%s) %s", vt.AsteroidNumberEntry.Text, vt.AsteroidNameEntry.Text))
	}
	if vt.StarUCAC4Entry.Text != "" {
		logAction(fmt.Sprintf("  UCAC4: %s", vt.StarUCAC4Entry.Text))
	}
	if vt.StarTycho2Entry.Text != "" {
		logAction(fmt.Sprintf("  Tycho-2: %s", vt.StarTycho2Entry.Text))
	}
	if vt.StarHipparcosEntry.Text != "" {
		logAction(fmt.Sprintf("  Hipparcos: %s", vt.StarHipparcosEntry.Text))
	}
}

// FillFromNASpreadsheet opens a file dialog to select an NA Asteroid Occultation Report Form
// and populates the VizieR tab fields from the spreadsheet
func (vt *VizieRTab) FillFromNASpreadsheet(w fyne.Window) {
	fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
		if err != nil {
			dialog.ShowError(err, w)
			return
		}
		if reader == nil {
			return // User cancelled
		}
		selectedURI := reader.URI()
		filePath := selectedURI.Path()
		if cerr := reader.Close(); cerr != nil {
			dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
		}

		// Save the parent directory URI for next time
		if parentURI, perr := storage.Parent(selectedURI); perr == nil {
			fyne.CurrentApp().Preferences().SetString("lastNASpreadsheetDir", parentURI.String())
		}

		// Open the Excel file
		f, err := excelize.OpenFile(filePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to open xlsx file: %w", err), w)
			return
		}
		success := false
		defer func() {
			if !success {
				_ = f.Close()
			}
		}()

		// Validate that this is an Asteroid Occultation Report Form
		headerVal, err := f.GetCellValue("DATA", "G1")
		if err != nil || headerVal != "Asteroid Occultation Report Form" {
			dialog.ShowError(fmt.Errorf("the xlsx file selected does not appear to be an Asteroid Occultation Report Form"), w)
			return
		}

		// Clear existing fields first
		vt.ClearInputs()

		// Read Year (D5)
		if year, err := f.GetCellValue("DATA", "D5"); err == nil && year != "" {
			vt.DateYearEntry.SetText(year)
		}

		// Read Month (K5) - need to convert month name to number
		if monthStr, err := f.GetCellValue("DATA", "K5"); err == nil && monthStr != "" {
			months := []string{"January", "February", "March", "April", "May", "June",
				"July", "August", "September", "October", "November", "December"}
			for i, m := range months {
				if monthStr == m {
					vt.DateMonthEntry.SetText(fmt.Sprintf("%d", i+1))
					break
				}
			}
		}

		// Read Day (P5)
		if day, err := f.GetCellValue("DATA", "P5"); err == nil && day != "" {
			vt.DateDayEntry.SetText(day)
		}

		// Read Longitude (N18) and direction (R18)
		if longStr, err := f.GetCellValue("DATA", "N18"); err == nil && longStr != "" {
			longEW, _ := f.GetCellValue("DATA", "R18")
			longParts := strings.Split(longStr, " ")
			if len(longParts) >= 3 {
				if longEW == "W" {
					vt.SiteLongDegEntry.SetText("-" + longParts[0])
				} else {
					vt.SiteLongDegEntry.SetText("+" + longParts[0])
				}
				vt.SiteLongMinEntry.SetText(longParts[1])
				vt.SiteLongSecsEntry.SetText(longParts[2])
			}
		}

		// Read Latitude (E18) and direction (J18)
		if latStr, err := f.GetCellValue("DATA", "E18"); err == nil && latStr != "" {
			latNS, _ := f.GetCellValue("DATA", "J18")
			latParts := strings.Split(latStr, " ")
			if len(latParts) >= 3 {
				if latNS == "S" {
					vt.SiteLatDegEntry.SetText("-" + latParts[0])
				} else {
					vt.SiteLatDegEntry.SetText("+" + latParts[0])
				}
				vt.SiteLatMinEntry.SetText(latParts[1])
				vt.SiteLatSecsEntry.SetText(latParts[2])
			}
		}

		// Read Altitude (V18) and units (W18)
		if altStr, err := f.GetCellValue("DATA", "V18"); err == nil && altStr != "" {
			altUnits, _ := f.GetCellValue("DATA", "W18")
			if altVal, err := strconv.ParseFloat(altStr, 64); err == nil {
				if altUnits == "m" {
					vt.SiteAltitudeEntry.SetText(fmt.Sprintf("%.0f", altVal))
				} else {
					// Convert feet to meters
					vt.SiteAltitudeEntry.SetText(fmt.Sprintf("%.0f", math.Round(altVal*0.3048)))
				}
			}
		}

		// Read Observer (D9)
		if observer, err := f.GetCellValue("DATA", "D9"); err == nil && observer != "" {
			vt.ObserverNameEntry.SetText(observer)
		}

		// Read Star Type (S7) and Number (X7)
		if starType, err := f.GetCellValue("DATA", "S7"); err == nil && starType != "" {
			starNumber, _ := f.GetCellValue("DATA", "X7")
			if starNumber != "" {
				if strings.HasPrefix(starType, "TYC") {
					vt.StarTycho2Entry.SetText(starNumber)
				} else if strings.HasPrefix(starType, "HIP") {
					vt.StarHipparcosEntry.SetText(starNumber)
				} else if strings.HasPrefix(starType, "UCAC4") {
					vt.StarUCAC4Entry.SetText(starNumber)
				}
			}
		}

		// Read Asteroid Number (E7) and Name (K7)
		if asteroidNumber, err := f.GetCellValue("DATA", "E7"); err == nil && asteroidNumber != "" {
			vt.AsteroidNumberEntry.SetText(asteroidNumber)
		}
		if asteroidName, err := f.GetCellValue("DATA", "K7"); err == nil && asteroidName != "" {
			vt.AsteroidNameEntry.SetText(asteroidName)
		}

		if err := f.Close(); err != nil {
			dialog.ShowError(fmt.Errorf("error closing xlsx file: %v", err), w)
			return
		}
		success = true

		// Log the fields that were filled from the NA spreadsheet
		logAction(fmt.Sprintf("VizieR fields filled from NA spreadsheet: %s", filePath))
		if vt.DateYearEntry.Text != "" {
			logAction(fmt.Sprintf("  Date: %s-%s-%s", vt.DateYearEntry.Text, vt.DateMonthEntry.Text, vt.DateDayEntry.Text))
		}
		if vt.ObserverNameEntry.Text != "" {
			logAction(fmt.Sprintf("  Observer: %s", vt.ObserverNameEntry.Text))
		}
		if vt.SiteLatDegEntry.Text != "" {
			logAction(fmt.Sprintf("  Latitude: %s° %s' %s\"", vt.SiteLatDegEntry.Text, vt.SiteLatMinEntry.Text, vt.SiteLatSecsEntry.Text))
		}
		if vt.SiteLongDegEntry.Text != "" {
			logAction(fmt.Sprintf("  Longitude: %s° %s' %s\"", vt.SiteLongDegEntry.Text, vt.SiteLongMinEntry.Text, vt.SiteLongSecsEntry.Text))
		}
		if vt.SiteAltitudeEntry.Text != "" {
			logAction(fmt.Sprintf("  Altitude: %s m", vt.SiteAltitudeEntry.Text))
		}
		if vt.AsteroidNumberEntry.Text != "" || vt.AsteroidNameEntry.Text != "" {
			logAction(fmt.Sprintf("  Asteroid: (%s) %s", vt.AsteroidNumberEntry.Text, vt.AsteroidNameEntry.Text))
		}
		if vt.StarUCAC4Entry.Text != "" {
			logAction(fmt.Sprintf("  UCAC4: %s", vt.StarUCAC4Entry.Text))
		}
		if vt.StarTycho2Entry.Text != "" {
			logAction(fmt.Sprintf("  Tycho-2: %s", vt.StarTycho2Entry.Text))
		}
		if vt.StarHipparcosEntry.Text != "" {
			logAction(fmt.Sprintf("  Hipparcos: %s", vt.StarHipparcosEntry.Text))
		}

		vt.StatusLabel.SetText("NA spreadsheet data loaded successfully")
		dialog.ShowInformation("Success", "Excel spreadsheet Asteroid Report Form entries extracted successfully.", w)

	}, w)

	fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".xlsx"}))
	fileDialog.Resize(fyne.NewSize(800, 600))

	// Default to the last directory used by the NA spreadsheet dialog
	if lastDir := fyne.CurrentApp().Preferences().String("lastNASpreadsheetDir"); lastDir != "" {
		if parsed, err := storage.ParseURI(lastDir); err == nil {
			if listable, err := storage.ListerForURI(parsed); err == nil {
				fileDialog.SetLocation(listable)
			}
		}
	}

	fileDialog.Show()
}

// FillFromSodisForm opens a file dialog to select a SODIS form
// and populates the VizieR tab fields from the spreadsheet
func (vt *VizieRTab) FillFromSodisForm(w fyne.Window) {
	// Only accept a SODIS-REPORT.txt that was saved during this session.
	if sodisReportSavedThisSession && resultsFolder != "" {
		sodisPath := filepath.Join(resultsFolder, "SODIS-REPORT.txt")
		if _, err := os.Stat(sodisPath); err == nil {
			if perr := vt.parseSodisFile(sodisPath, w); perr != nil {
				dialog.ShowError(fmt.Errorf("error reading SODIS form: %w", perr), w)
				return
			}
			logAction(fmt.Sprintf("SODIS form loaded: %s", sodisPath))
			return
		}
	}

	// No SODIS report saved this session — inform the user.
	dialog.ShowInformation("SODIS Report Not Found",
		"A SODIS-REPORT.txt has not yet been generated.\n\n"+
			"Please use the Fill SODIS Report button on the Fit tab to create one first.", w)
}

// parseSodisFile reads a SODIS form text file and fills VizieR tab fields
func (vt *VizieRTab) parseSodisFile(filePath string, w fyne.Window) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := file.Close(); cerr != nil {
			dialog.ShowError(fmt.Errorf("error closing SODIS form file: %w", cerr), w)
		}
	}()

	vt.ClearInputs()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()

		// #DATE: day month year (e.g. "15 January 2025")
		if strings.HasPrefix(line, "#DATE:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#DATE:"))
			parts := strings.Fields(value)
			if len(parts) >= 3 {
				vt.DateDayEntry.SetText(parts[0])
				// Convert month name to number
				months := map[string]string{
					"january": "1", "february": "2", "march": "3", "april": "4",
					"may": "5", "june": "6", "july": "7", "august": "8",
					"september": "9", "october": "10", "november": "11", "december": "12",
				}
				if num, ok := months[strings.ToLower(parts[1])]; ok {
					vt.DateMonthEntry.SetText(num)
				}
				vt.DateYearEntry.SetText(parts[2])
			}
			continue
		}

		// #Star: / #STAR: designation (e.g. "TYC 1234-5678-1") — leave blank if "unknown"
		if strings.HasPrefix(strings.ToUpper(line), "#STAR:") {
			value := strings.TrimSpace(line[len("#STAR:"):])
			if strings.EqualFold(value, "unknown") || value == "" {
				continue
			}
			starFields := strings.Fields(value)
			if len(starFields) >= 2 {
				catalog := strings.ToUpper(starFields[0])
				starID := starFields[1]
				if strings.HasPrefix(catalog, "U") {
					vt.StarUCAC4Entry.SetText(starID)
				} else if strings.HasPrefix(catalog, "T") {
					vt.StarTycho2Entry.SetText(starID)
				} else if strings.HasPrefix(catalog, "H") {
					vt.StarHipparcosEntry.SetText(starID)
				}
			}
			continue
		}

		// #Longitude: +/-DDD MM SS.S
		if strings.HasPrefix(line, "#Longitude:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#Longitude:"))
			parts := strings.Fields(value)
			if len(parts) >= 3 {
				vt.SiteLongDegEntry.SetText(parts[0])
				vt.SiteLongMinEntry.SetText(parts[1])
				vt.SiteLongSecsEntry.SetText(parts[2])
			}
			continue
		}

		// #Latitude: +/-DD MM SS.S
		if strings.HasPrefix(line, "#Latitude:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#Latitude:"))
			parts := strings.Fields(value)
			if len(parts) >= 3 {
				vt.SiteLatDegEntry.SetText(parts[0])
				vt.SiteLatMinEntry.SetText(parts[1])
				vt.SiteLatSecsEntry.SetText(parts[2])
			}
			continue
		}

		// #Altitude: meters
		if strings.HasPrefix(line, "#Altitude:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#Altitude:"))
			if altVal, err := strconv.ParseFloat(value, 64); err == nil {
				vt.SiteAltitudeEntry.SetText(fmt.Sprintf("%.0f", altVal))
			}
			continue
		}

		// #Observer1: observer name
		if strings.HasPrefix(line, "#Observer1:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#Observer1:"))
			vt.ObserverNameEntry.SetText(value)
			continue
		}

		// #ASTEROID: asteroid name
		if strings.HasPrefix(line, "#ASTEROID:") {
			value := strings.TrimSpace(strings.TrimPrefix(line, "#ASTEROID:"))
			vt.AsteroidNameEntry.SetText(value)
			continue
		}

		// #Nr.: asteroid number (handles "#Nr.:", "#Nr.  :", "#Nr:" etc.)
		if strings.HasPrefix(line, "#Nr") {
			// Strip the "#Nr" prefix, then trim punctuation and whitespace
			value := strings.TrimPrefix(line, "#Nr")
			value = strings.TrimLeft(value, ".: \t")
			value = strings.TrimSpace(value)
			if value != "" {
				vt.AsteroidNumberEntry.SetText(value)
			}
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return err
	}

	// Log the fields that were filled
	logAction(fmt.Sprintf("VizieR fields filled from SODIS form: %s", filePath))
	if vt.DateYearEntry.Text != "" {
		logAction(fmt.Sprintf("  Date: %s-%s-%s", vt.DateYearEntry.Text, vt.DateMonthEntry.Text, vt.DateDayEntry.Text))
	}
	if vt.ObserverNameEntry.Text != "" {
		logAction(fmt.Sprintf("  Observer: %s", vt.ObserverNameEntry.Text))
	}
	if vt.SiteLatDegEntry.Text != "" {
		logAction(fmt.Sprintf("  Latitude: %s° %s' %s\"", vt.SiteLatDegEntry.Text, vt.SiteLatMinEntry.Text, vt.SiteLatSecsEntry.Text))
	}
	if vt.SiteLongDegEntry.Text != "" {
		logAction(fmt.Sprintf("  Longitude: %s° %s' %s\"", vt.SiteLongDegEntry.Text, vt.SiteLongMinEntry.Text, vt.SiteLongSecsEntry.Text))
	}
	if vt.SiteAltitudeEntry.Text != "" {
		logAction(fmt.Sprintf("  Altitude: %s m", vt.SiteAltitudeEntry.Text))
	}
	if vt.AsteroidNumberEntry.Text != "" || vt.AsteroidNameEntry.Text != "" {
		logAction(fmt.Sprintf("  Asteroid: (%s) %s", vt.AsteroidNumberEntry.Text, vt.AsteroidNameEntry.Text))
	}
	if vt.StarUCAC4Entry.Text != "" {
		logAction(fmt.Sprintf("  UCAC4: %s", vt.StarUCAC4Entry.Text))
	}
	if vt.StarTycho2Entry.Text != "" {
		logAction(fmt.Sprintf("  Tycho-2: %s", vt.StarTycho2Entry.Text))
	}
	if vt.StarHipparcosEntry.Text != "" {
		logAction(fmt.Sprintf("  Hipparcos: %s", vt.StarHipparcosEntry.Text))
	}

	vt.StatusLabel.SetText("SODIS form data loaded successfully")
	dialog.ShowInformation("Success", "SODIS form entries extracted successfully.", w)
	return nil
}

// ==================== SODIS Report Dialog ====================

// sodisPreFill carries the current fit/MC/lightcurve/site data used to
// pre-populate the SODIS report dialog. All fields are optional (zero = not available).
type sodisPreFill struct {
	fitResult          *fitResult
	mcResult           *mcTrialsResult
	fitParams          *OccultationParameters
	lcData             *LightCurveData
	occTitle           string     // e.g. "(2731) Cucula" — used for #ASTEROID and #Nr
	sitePath           string     // path to the last-loaded .site file
	occelmntXml        string     // raw occelmnt XML text — first <Star> CSV field used for #STAR
	noiseSigma         float64    // baseline noise sigma — used for Signal/Noise (1/sigma)
	csvExposureSecs    float64    // CSV-measured median exposure time — used for Exp_Time
	observerT0         time.Time  // observer-corrected event time (zero = not available; use geocentric)
	detailsEventTimeUT string     // "Event Time (UT)" from the details file, e.g. "26 Feb 2026 20:27:55"; overrides calculated time when non-empty
	vt                 *VizieRTab // VizieR tab — used to propagate the observer name when a site file loads
}

// formatSecondsForSODIS formats total seconds as HH:MM:SS.sss (3 decimal places),
// matching the precision expected by the SODIS report form.
func formatSecondsForSODIS(totalSeconds float64) string {
	totalSeconds = math.Mod(totalSeconds, 86400)
	h := int(totalSeconds / 3600)
	totalSeconds -= float64(h) * 3600
	m := int(totalSeconds / 60)
	totalSeconds -= float64(m) * 60
	return fmt.Sprintf("%02d:%02d:%06.3f", h, m, totalSeconds)
}

// decimalDegToSODIS converts a decimal-degree coordinate to the SODIS DMS notation
// "+/-DD MM SS.S" (degrees zero-padded to at least 2 digits, seconds 1 decimal place).
func decimalDegToSODIS(deg float64) string {
	sign := "+"
	if deg < 0 {
		sign = "-"
		deg = -deg
	}
	d := int(deg)
	rem := (deg - float64(d)) * 60.0
	m := int(rem)
	s := (rem - float64(m)) * 60.0
	return fmt.Sprintf("%s%02d %02d %04.1f", sign, d, m, s)
}

// parseSiteFileToMap reads a .site file and returns a map of key→value pairs.
func parseSiteFileToMap(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	result := make(map[string]string)
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimRight(raw, "\r")
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" {
				result[key] = val
			}
		}
	}
	return result
}

// sodisTemplateItem is a parsed item from a SODIS form template.
type sodisTemplateItem struct {
	isSection bool   // true = section header line (e.g., #OBSERVER)
	name      string // field name (e.g. "Observer1") or section label (e.g. "OBSERVER")
	hint      string // preceding description/hint line, if any (for fields only)
	value     string // default value from template (for fields only)
}

// parseSodisTemplateLines walks a slice of SODIS template file lines and returns
// a list of structured items (sections and fields with optional hints).
//
// A line is a field if it matches: #<identifier>: [value]
// where the identifier contains only [A-Za-z0-9_/-] with no spaces.
// A line is a section header if the text after '#' contains no spaces or
// special characters (+-/=.,*).
// All other '#' lines are treated as hint/description text for the next field.
func parseSodisTemplateLines(lines []string) []sodisTemplateItem {
	var items []sodisTemplateItem
	var pendingHint string

	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t\r")
		if !strings.HasPrefix(line, "#") {
			continue
		}
		rest := line[1:] // everything after the leading '#'

		// --- field line check ---
		colonIdx := strings.IndexByte(rest, ':')
		if colonIdx > 0 {
			word := rest[:colonIdx]
			if !strings.ContainsAny(word, " \t") && isSodisFieldName(word) {
				value := strings.TrimSpace(rest[colonIdx+1:])
				items = append(items, sodisTemplateItem{
					name:  word,
					hint:  pendingHint,
					value: value,
				})
				pendingHint = ""
				continue
			}
		}

		// --- section header check: only letters, digits, underscores; no spaces or specials ---
		if !strings.ContainsAny(rest, " \t+-/=.,*") && len(rest) > 0 {
			pendingHint = ""
			items = append(items, sodisTemplateItem{
				isSection: true,
				name:      rest,
			})
			continue
		}

		// --- hint/description line ---
		if trimmed := strings.TrimSpace(rest); trimmed != "" {
			pendingHint = trimmed
		}
	}
	return items
}

// isSodisFieldName reports whether s is a valid SODIS field identifier
// (letters, digits, underscores, forward-slashes, or hyphens only).
func isSodisFieldName(s string) bool {
	if s == "" {
		return false
	}
	for _, c := range s {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '/' || c == '-') {
			return false
		}
	}
	return true
}

// buildSodisReportText reconstructs the SODIS report text from the original
// template lines, substituting filled-in entry values for each field.
func buildSodisReportText(templateLines []string, entries map[string]*widget.Entry) string {
	var sb strings.Builder
	for _, raw := range templateLines {
		line := strings.TrimRight(raw, " \t\r")
		if strings.HasPrefix(line, "#") {
			rest := line[1:]
			colonIdx := strings.IndexByte(rest, ':')
			if colonIdx > 0 {
				word := rest[:colonIdx]
				if !strings.ContainsAny(word, " \t") && isSodisFieldName(word) {
					if e, ok := entries[word]; ok {
						val := strings.TrimSpace(e.Text)
						if word == "Comments" {
							sb.WriteString("#Comments:\n")
							for _, commentLine := range strings.Split(val, "\n") {
								if commentLine != "" {
									sb.WriteString("  ")
									sb.WriteString(commentLine)
								}
								sb.WriteString("\n")
							}
						} else {
							sb.WriteString("#")
							sb.WriteString(word)
							sb.WriteString(":  ")
							if val != "" {
								sb.WriteString(val)
							}
							sb.WriteString("\n")
						}
						continue
					}
				}
			}
		}
		// Preserve non-field lines exactly (section headers, hints, blank lines)
		sb.WriteString(raw)
		sb.WriteString("\n")
	}
	return sb.String()
}

// showSodisReportDialog opens a scrollable dialog with an entry box for every
// SODIS form field (any line of the form "#FieldName: value" in the blank template).
// Fields for which current data is available (fit edges, MC uncertainty, observation
// times, exposure time) are pre-filled automatically.  All other fields start empty.
// Use "Load SODIS template" to pre-fill further from a saved template file.
func showSodisReportDialog(w fyne.Window, fill *sodisPreFill, onSave func()) {
	// Read the blank SODIS template to drive the form structure
	templatePath := filepath.Join(appDir, "SODIS-FOLDER", "1 BLANK SODIS TEMPLATE.txt")
	data, err := os.ReadFile(templatePath)
	if err != nil {
		dialog.ShowError(fmt.Errorf("could not read SODIS template:\n%s\n\n%w", templatePath, err), w)
		return
	}
	rawLines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	items := parseSodisTemplateLines(rawLines)

	// Build the form UI: entries indexed by field name, grouped by section.
	// All entries start empty.
	entries := make(map[string]*widget.Entry)
	var occultationSelect *widget.Select

	telescopeOpts := []string{"unstated", "1=Refractor", "2=Newtonian", "3=SCT", "4=Dobsonian", "5=Binoculars", "6=Other", "7=None", "8=eVscope"}
	telescopeOptToVal := map[string]string{
		"unstated": "", "1=Refractor": "1", "2=Newtonian": "2", "3=SCT": "3",
		"4=Dobsonian": "4", "5=Binoculars": "5", "6=Other": "6", "7=None": "7", "8=eVscope": "8",
	}
	telescopeValToOpt := map[string]string{
		"": "unstated", "1": "1=Refractor", "2": "2=Newtonian", "3": "3=SCT",
		"4": "4=Dobsonian", "5": "5=Binoculars", "6": "6=Other", "7": "7=None", "8": "8=eVscope",
	}
	var telescopeSelect *widget.Select

	observingMethodOpts := []string{"unspecified", "a=Analogue & digital video", "b=Digital SLR-camera video", "c=Photometer", "d=Sequential images", "e=Drift scan", "f=Visual", "g=Other"}
	observingMethodOptToVal := map[string]string{
		"unspecified": "", "a=Analogue & digital video": "a", "b=Digital SLR-camera video": "b",
		"c=Photometer": "c", "d=Sequential images": "d", "e=Drift scan": "e", "f=Visual": "f", "g=Other": "g",
	}
	observingMethodValToOpt := map[string]string{
		"": "unspecified", "a": "a=Analogue & digital video", "b": "b=Digital SLR-camera video",
		"c": "c=Photometer", "d": "d=Sequential images", "e": "e=Drift scan", "f": "f=Visual", "g": "g=Other",
	}
	var observingMethodSelect *widget.Select

	timesourceOpts := []string{"unspecified", "a=GPS", "b=NTP", "c=Telephone (fixed or mobile)", "d=Radio time signal", "e=Internal clock of recorder", "f=Stopwatch", "g=Other"}
	timesourceOptToVal := map[string]string{
		"unspecified": "", "a=GPS": "a", "b=NTP": "b", "c=Telephone (fixed or mobile)": "c",
		"d=Radio time signal": "d", "e=Internal clock of recorder": "e", "f=Stopwatch": "f", "g=Other": "g",
	}
	timesourceValToOpt := map[string]string{
		"": "unspecified", "a": "a=GPS", "b": "b=NTP", "c": "c=Telephone (fixed or mobile)",
		"d": "d=Radio time signal", "e": "e=Internal clock of recorder", "f": "f=Stopwatch", "g": "g=Other",
	}
	var timesourceSelect *widget.Select

	stabilityOpts := []string{"unstated", "1=Steady", "2=Slight flickering", "3=Strong flickering"}
	stabilityOptToVal := map[string]string{
		"unstated": "", "1=Steady": "1", "2=Slight flickering": "2", "3=Strong flickering": "3",
	}
	stabilityValToOpt := map[string]string{
		"": "unstated", "1": "1=Steady", "2": "2=Slight flickering", "3": "3=Strong flickering",
	}
	var stabilitySelect *widget.Select

	windOpts := []string{
		"0=Calm (0-1 kts)", "1=Light Air (1-3 kts)", "2=Light Breeze (4-6 kts)", "3=Gentle Breeze (7-10 kts)",
		"4=Moderate Breeze (11-16 kts)", "5=Fresh Breeze (17-21 kts)", "6=Strong Breeze (22-27 kts)",
		"7=Near Gale (28-33 kts)", "8=Gale (34-40 kts)", "9=Severe Gale (41-47 kts)",
		"10=Storm (48-55 kts)", "11=Violent Storm (56-63 kts)", "12=Hurricane (64-71 kts)",
	}
	windOptToVal := map[string]string{
		"0=Calm (0-1 kts)": "0", "1=Light Air (1-3 kts)": "1", "2=Light Breeze (4-6 kts)": "2", "3=Gentle Breeze (7-10 kts)": "3",
		"4=Moderate Breeze (11-16 kts)": "4", "5=Fresh Breeze (17-21 kts)": "5", "6=Strong Breeze (22-27 kts)": "6",
		"7=Near Gale (28-33 kts)": "7", "8=Gale (34-40 kts)": "8", "9=Severe Gale (41-47 kts)": "9",
		"10=Storm (48-55 kts)": "10", "11=Violent Storm (56-63 kts)": "11", "12=Hurricane (64-71 kts)": "12",
	}
	windValToOpt := map[string]string{
		"0": "0=Calm (0-1 kts)", "1": "1=Light Air (1-3 kts)", "2": "2=Light Breeze (4-6 kts)", "3": "3=Gentle Breeze (7-10 kts)",
		"4": "4=Moderate Breeze (11-16 kts)", "5": "5=Fresh Breeze (17-21 kts)", "6": "6=Strong Breeze (22-27 kts)",
		"7": "7=Near Gale (28-33 kts)", "8": "8=Gale (34-40 kts)", "9": "9=Severe Gale (41-47 kts)",
		"10": "10=Storm (48-55 kts)", "11": "11=Violent Storm (56-63 kts)", "12": "12=Hurricane (64-71 kts)",
	}
	var windSelect *widget.Select

	transparencyOpts := []string{
		"1=Clear", "2=Fog", "3=Thin cloud < 2 [mag loss < 2mag]",
		"4=Thick cloud > 2 [mag loss > 2mag]", "5=Broken opaque cloud [that is, observed thru gaps in the cloud]",
		"6=Star faint", "7=By averted vision",
	}
	transparencyOptToVal := map[string]string{
		"1=Clear": "1", "2=Fog": "2", "3=Thin cloud < 2 [mag loss < 2mag]": "3",
		"4=Thick cloud > 2 [mag loss > 2mag]": "4", "5=Broken opaque cloud [that is, observed thru gaps in the cloud]": "5",
		"6=Star faint": "6", "7=By averted vision": "7",
	}
	transparencyValToOpt := map[string]string{
		"1": "1=Clear", "2": "2=Fog", "3": "3=Thin cloud < 2 [mag loss < 2mag]",
		"4": "4=Thick cloud > 2 [mag loss > 2mag]", "5": "5=Broken opaque cloud [that is, observed thru gaps in the cloud]",
		"6": "6=Star faint", "7": "7=By averted vision",
	}
	var transparencySelect *widget.Select

	var vboxContent []fyne.CanvasObject
	var currentFormItems []*widget.FormItem

	flushForm := func() {
		if len(currentFormItems) > 0 {
			vboxContent = append(vboxContent, widget.NewForm(currentFormItems...))
			currentFormItems = nil
		}
	}

	for _, item := range items {
		if item.isSection {
			flushForm()
			lbl := widget.NewLabel(item.name)
			lbl.TextStyle = fyne.TextStyle{Bold: true}
			vboxContent = append(vboxContent, lbl, widget.NewSeparator())
		} else if item.name == "Occultation" {
			// Use a dropdown instead of a free-text entry
			backingEntry := widget.NewEntry()
			entries["Occultation"] = backingEntry
			sel := widget.NewSelect([]string{"POSITIVE", "NEGATIVE"}, func(val string) {
				backingEntry.SetText(val)
			})
			occultationSelect = sel
			fi := widget.NewFormItem("Occultation:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else if item.name == "Telescope" {
			backingEntry := widget.NewEntry()
			entries["Telescope"] = backingEntry
			sel := widget.NewSelect(telescopeOpts, func(opt string) {
				backingEntry.SetText(telescopeOptToVal[opt])
			})
			telescopeSelect = sel
			fi := widget.NewFormItem("Telescope:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else if item.name == "ObservingMethod" {
			backingEntry := widget.NewEntry()
			entries["ObservingMethod"] = backingEntry
			sel := widget.NewSelect(observingMethodOpts, func(opt string) {
				backingEntry.SetText(observingMethodOptToVal[opt])
			})
			observingMethodSelect = sel
			fi := widget.NewFormItem("ObservingMethod:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else if item.name == "Timesource" {
			backingEntry := widget.NewEntry()
			entries["Timesource"] = backingEntry
			sel := widget.NewSelect(timesourceOpts, func(opt string) {
				backingEntry.SetText(timesourceOptToVal[opt])
			})
			timesourceSelect = sel
			fi := widget.NewFormItem("Timesource:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else if item.name == "Stability" {
			backingEntry := widget.NewEntry()
			entries["Stability"] = backingEntry
			sel := widget.NewSelect(stabilityOpts, func(opt string) {
				backingEntry.SetText(stabilityOptToVal[opt])
			})
			stabilitySelect = sel
			fi := widget.NewFormItem("Stability:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else if item.name == "Wind" {
			backingEntry := widget.NewEntry()
			entries["Wind"] = backingEntry
			sel := widget.NewSelect(windOpts, func(opt string) {
				backingEntry.SetText(windOptToVal[opt])
			})
			windSelect = sel
			fi := widget.NewFormItem("Wind:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else if item.name == "Transparency" {
			backingEntry := widget.NewEntry()
			entries["Transparency"] = backingEntry
			sel := widget.NewSelect(transparencyOpts, func(opt string) {
				backingEntry.SetText(transparencyOptToVal[opt])
			})
			transparencySelect = sel
			fi := widget.NewFormItem("Transparency:", sel)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		} else {
			var e *widget.Entry
			if item.name == "Comments" {
				e = widget.NewMultiLineEntry()
				e.SetMinRowsVisible(3)
			} else {
				e = widget.NewEntry()
			}
			entries[item.name] = e
			fi := widget.NewFormItem(item.name+":", e)
			if item.hint != "" {
				fi.HintText = item.hint
			}
			currentFormItems = append(currentFormItems, fi)
		}
	}
	flushForm()

	// Pre-fill entries from currently available data.
	if fill != nil {
		setEntry := func(name, value string) {
			if e, ok := entries[name]; ok && value != "" {
				e.SetText(value)
			}
		}

		// D, R, and Duration from the fit result
		if fill.fitResult != nil && len(fill.fitResult.edgeTimes) == 2 {
			t0 := fill.fitResult.edgeTimes[0] + fill.fitResult.bestShift
			t1 := fill.fitResult.edgeTimes[1] + fill.fitResult.bestShift
			// Ensure chronological order (D before R)
			dIdx, rIdx := 0, 1
			if t0 > t1 {
				t0, t1 = t1, t0
				dIdx, rIdx = 1, 0
			}
			setEntry("D", "D"+formatSecondsForSODIS(t0))
			setEntry("R", "R"+formatSecondsForSODIS(t1))
			setEntry("Duration", fmt.Sprintf("%.3f", t1-t0))

			// Acc_D and Acc_R from the most recent Monte Carlo run (3-sigma values)
			if fill.mcResult != nil && fill.mcResult.numEdges == 2 &&
				len(fill.mcResult.edgeStds) == 2 {
				setEntry("Acc_D", fmt.Sprintf("%.3f", 3*fill.mcResult.edgeStds[dIdx]))
				setEntry("Acc_R", fmt.Sprintf("%.3f", 3*fill.mcResult.edgeStds[rIdx]))
			}
		}

		// StartObs and EndObs from the loaded light curve (only if timestamps are present).
		// Rounded to the nearest integer second, formatted as HH:MM:SS.
		hhmmss := func(totalSeconds float64) string {
			s := int(math.Round(totalSeconds)) % 86400
			return fmt.Sprintf("%02d:%02d:%02d", s/3600, (s%3600)/60, s%60)
		}
		if fill.lcData != nil && len(fill.lcData.TimeValues) > 1 &&
			fill.lcData.TimeValues[0] > 0 {
			setEntry("StartObs", hhmmss(fill.lcData.TimeValues[0]))
			setEntry("EndObs", hhmmss(fill.lcData.TimeValues[len(fill.lcData.TimeValues)-1]))
		}

		// Exp_Time: prefer the CSV-measured median cadence; fall back to the occultation parameters
		if fill.csvExposureSecs > 0 {
			setEntry("Exp_Time", fmt.Sprintf("%.4f", fill.csvExposureSecs))
		} else if fill.fitParams != nil && fill.fitParams.ExposureTimeSecs > 0 {
			setEntry("Exp_Time", fmt.Sprintf("%.4f", fill.fitParams.ExposureTimeSecs))
		}

		// Signal/Noise: 1/noiseSigma (baseline noise sigma from the Fit tab)
		if fill.noiseSigma > 0 {
			setEntry("Signal/Noise", fmt.Sprintf("%.2f", 1.0/fill.noiseSigma))
		}

		// ASTEROID and Nr from the occultation title, e.g. "(2731) Cucula"
		if fill.occTitle != "" {
			title := fill.occTitle
			if strings.HasPrefix(title, "(") {
				if end := strings.Index(title, ")"); end > 0 {
					setEntry("Nr", strings.TrimSpace(title[1:end]))
					setEntry("ASTEROID", strings.TrimSpace(title[end+1:]))
				}
			} else {
				setEntry("ASTEROID", title)
			}
		}

		// Occultation: default to POSITIVE
		if occultationSelect != nil {
			occultationSelect.SetSelected("POSITIVE")
		}

		// Fields from occelmnt XML: STAR, DATE, PREDICTTIME
		if fill.occelmntXml != "" {
			var occ Occultations
			if xmlErr := xml.Unmarshal([]byte(fill.occelmntXml), &occ); xmlErr == nil && len(occ.Events) > 0 {
				ev := occ.Events[0]

				// STAR: first CSV field of the <Star> element
				if ev.Star != "" {
					starParts := strings.SplitN(ev.Star, ",", 2)
					if starName := strings.TrimSpace(starParts[0]); starName != "" {
						setEntry("STAR", starName)
					}
				}

				// DATE and PREDICTTIME: from <Elements> fields (0-indexed: 2=year, 3=month, 4=day, 5=UT hours)
				if ev.Elements != "" {
					elParts := splitCSVPreserveEmpty(strings.TrimSpace(ev.Elements))
					if len(elParts) >= 6 {
						year, yearErr := strconv.Atoi(strings.TrimSpace(elParts[2]))
						month, monthErr := strconv.Atoi(strings.TrimSpace(elParts[3]))
						day, dayErr := strconv.Atoi(strings.TrimSpace(elParts[4]))
						utHours, utErr := strconv.ParseFloat(strings.TrimSpace(elParts[5]), 64)
						if yearErr == nil && monthErr == nil && dayErr == nil && month >= 1 && month <= 12 {
							monthNames := [13]string{"", "January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"}
							monthAbbrevs := [13]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
							// DATE: "D MonthName YYYY"
							setEntry("DATE", fmt.Sprintf("%d %s %d", day, monthNames[month], year))
							// PREDICTTIME: "DD Mon; HH:MM:SS UT"
							// Use observer-corrected t0 when available, else fall back to geocentric.
							fmt.Printf("[SODIS PREDICTTIME] fill.observerT0=%v  IsZero=%v\n", fill.observerT0, fill.observerT0.IsZero())
							if !fill.observerT0.IsZero() {
								t := fill.observerT0.UTC()
								val := fmt.Sprintf("%02d %s; %02d:%02d:%02d UT",
									t.Day(), monthAbbrevs[t.Month()], t.Hour(), t.Minute(), t.Second())
								fmt.Printf("[SODIS PREDICTTIME] using observerT0 -> %q\n", val)
								setEntry("PREDICTTIME", val)
							} else if utErr == nil {
								totalSec := int(math.Round(utHours * 3600))
								h := totalSec / 3600
								m := (totalSec % 3600) / 60
								s := totalSec % 60
								val := fmt.Sprintf("%02d %s; %02d:%02d:%02d UT", day, monthAbbrevs[month], h, m, s)
								fmt.Printf("[SODIS PREDICTTIME] using geocentric -> %q\n", val)
								setEntry("PREDICTTIME", val)
							} else {
								fmt.Printf("[SODIS PREDICTTIME] skipped: observerT0 is zero AND utErr=%v\n", utErr)
							}
						}
					}
				}
			}
		}

		// Details file Event Time (UT) overrides any calculated PREDICTTIME.
		if fill.detailsEventTimeUT != "" {
			monthAbbrevs := [13]string{"", "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"}
			if t, perr := time.Parse("02 Jan 2006 15:04:05", fill.detailsEventTimeUT); perr == nil {
				val := fmt.Sprintf("%02d %s; %02d:%02d:%02d UT",
					t.Day(), monthAbbrevs[t.Month()], t.Hour(), t.Minute(), t.Second())
				setEntry("PREDICTTIME", val)
			} else {
				setEntry("PREDICTTIME", fill.detailsEventTimeUT)
			}
		}

		// Site file fields: observer, location, equipment
		if fill.sitePath != "" {
			site := parseSiteFileToMap(fill.sitePath)
			setEntry("Observer1", site["observer1"])
			if fill.vt != nil && site["observer1"] != "" {
				fill.vt.ObserverNameEntry.SetText(site["observer1"])
			}
			setEntry("Observer2", site["observer2"])
			setEntry("Observatory", site["observatory"])
			setEntry("E-mail", site["email"])
			setEntry("Address", site["address"])
			setEntry("NearestCity", site["nearest_city"])
			setEntry("Countrycode", site["country_code"])
			setEntry("Telescope", site["telescope"])
			if telescopeSelect != nil {
				if e, ok := entries["Telescope"]; ok {
					if opt, ok2 := telescopeValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						telescopeSelect.SetSelected(opt)
					}
				}
			}
			setEntry("Aperture", site["aperture"])
			setEntry("FocalLength", site["focal_length"])
			setEntry("ObservingMethod", site["observing_method"])
			if observingMethodSelect != nil {
				if e, ok := entries["ObservingMethod"]; ok {
					if opt, ok2 := observingMethodValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						observingMethodSelect.SetSelected(opt)
					}
				}
			}
			setEntry("Timesource", site["time_source"])
			if timesourceSelect != nil {
				if e, ok := entries["Timesource"]; ok {
					if opt, ok2 := timesourceValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						timesourceSelect.SetSelected(opt)
					}
				}
			}
			setEntry("Camera", site["camera"])
			// Convert decimal lat/lon to SODIS DMS notation
			if latStr := site["latitude_decimal"]; latStr != "" {
				if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
					setEntry("Latitude", decimalDegToSODIS(lat))
				}
			}
			if lonStr := site["longitude_decimal"]; lonStr != "" {
				if lon, err := strconv.ParseFloat(lonStr, 64); err == nil {
					setEntry("Longitude", decimalDegToSODIS(lon))
				}
			}
			setEntry("Altitude", site["altitude"])
			if fill.vt != nil && site["altitude"] != "" {
				fill.vt.SiteAltitudeEntry.SetText(site["altitude"])
			}
		}
	}

	// Default Transparency to 1=Clear, Stability (Seeing) to 1=Steady, and Wind to 0=Calm.
	if transparencySelect != nil && transparencySelect.Selected == "" {
		transparencySelect.SetSelected("1=Clear")
	}
	if stabilitySelect != nil && stabilitySelect.Selected == "" {
		stabilitySelect.SetSelected("1=Steady")
	}
	if windSelect != nil && windSelect.Selected == "" {
		windSelect.SetSelected("0=Calm (0-1 kts)")
	}

	scroll := container.NewVScroll(container.NewVBox(vboxContent...))
	scroll.SetMinSize(fyne.NewSize(740, 500))

	var dlg dialog.Dialog

	// loadTemplateBtn opens a .txt file from SODIS-FOLDER and fills all entries.
	// It deliberately does NOT add the folder to the CSV recent-folders list.
	loadTemplateBtn := widget.NewButton("Load SODIS template", func() {
		sodisDir := filepath.Join(appDir, "SODIS-FOLDER")
		_ = os.MkdirAll(sodisDir, 0755)
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, ferr error) {
			if ferr != nil {
				dialog.ShowError(ferr, w)
				return
			}
			if reader == nil {
				return // user cancelled
			}
			filePath := reader.URI().Path()
			if cerr := reader.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("error closing file: %w", cerr), w)
				return
			}
			tplData, rerr := os.ReadFile(filePath)
			if rerr != nil {
				dialog.ShowError(fmt.Errorf("error reading template: %w", rerr), w)
				return
			}
			// Clear all entries then fill from the loaded template
			for _, e := range entries {
				e.SetText("")
			}
			tplLines := strings.Split(strings.ReplaceAll(string(tplData), "\r\n", "\n"), "\n")
			for _, tplItem := range parseSodisTemplateLines(tplLines) {
				if !tplItem.isSection {
					if e, ok := entries[tplItem.name]; ok {
						e.SetText(tplItem.value)
					}
				}
			}
			// Sync Occultation select from the backing entry
			if occultationSelect != nil {
				if e, ok := entries["Occultation"]; ok {
					occultationSelect.SetSelected(e.Text)
				}
			}
			// Sync Telescope select from a backing entry
			if telescopeSelect != nil {
				if e, ok := entries["Telescope"]; ok {
					if opt, ok2 := telescopeValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						telescopeSelect.SetSelected(opt)
					}
				}
			}
			// Sync ObservingMethod select from the backing entry
			if observingMethodSelect != nil {
				if e, ok := entries["ObservingMethod"]; ok {
					if opt, ok2 := observingMethodValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						observingMethodSelect.SetSelected(opt)
					}
				}
			}
			// Sync Timesource select from the backing entry
			if timesourceSelect != nil {
				if e, ok := entries["Timesource"]; ok {
					if opt, ok2 := timesourceValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						timesourceSelect.SetSelected(opt)
					}
				}
			}
			// Sync Stability select from backing entry
			if stabilitySelect != nil {
				if e, ok := entries["Stability"]; ok {
					if opt, ok2 := stabilityValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						stabilitySelect.SetSelected(opt)
					}
				}
			}
			// Sync Transparency select from a backing entry
			if transparencySelect != nil {
				if e, ok := entries["Transparency"]; ok {
					if opt, ok2 := transparencyValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						transparencySelect.SetSelected(opt)
					}
				}
			}
			// Sync Wind select from the backing entry
			if windSelect != nil {
				if e, ok := entries["Wind"]; ok {
					if opt, ok2 := windValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						windSelect.SetSelected(opt)
					}
				}
			}
			logAction(fmt.Sprintf("SODIS template loaded: %s", filePath))
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
		if listable, lerr := storage.ListerForURI(storage.NewFileURI(sodisDir)); lerr == nil {
			fileDialog.SetLocation(listable)
		}
		fileDialog.Resize(fyne.NewSize(800, 600))
		fileDialog.Show()
	})

	saveBtn := widget.NewButton("Save", func() {
		reportText := buildSodisReportText(rawLines, entries)
		if resultsFolder != "" {
			savePath := filepath.Join(resultsFolder, "SODIS-REPORT.txt")
			if werr := os.WriteFile(savePath, []byte(reportText), 0644); werr != nil {
				dialog.ShowError(fmt.Errorf("error writing SODIS report: %w", werr), w)
				return
			}
			sodisReportSavedThisSession = true
			logAction(fmt.Sprintf("SODIS report saved: %s", savePath))
			onSave()
			dlg.Hide()
			dialog.ShowInformation("Saved", "SODIS report saved successfully.", w)
			return
		}
		// No results folder yet — fall back to a file dialog in SODIS-FOLDER
		saveDir := filepath.Join(appDir, "SODIS-FOLDER")
		_ = os.MkdirAll(saveDir, 0755)
		fileSave := dialog.NewFileSave(func(wr fyne.URIWriteCloser, ferr error) {
			if ferr != nil {
				dialog.ShowError(ferr, w)
				return
			}
			if wr == nil {
				return // user cancelled
			}
			defer func() {
				if cerr := wr.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("error closing file: %w", cerr), w)
				}
			}()
			if _, werr := wr.Write([]byte(reportText)); werr != nil {
				dialog.ShowError(fmt.Errorf("error writing SODIS report: %w", werr), w)
				return
			}
			sodisReportSavedThisSession = true
			logAction(fmt.Sprintf("SODIS report saved: %s", wr.URI().Path()))
			onSave()
			dlg.Hide()
			dialog.ShowInformation("Saved", "SODIS report saved successfully.", w)
		}, w)
		fileSave.SetFilter(storage.NewExtensionFileFilter([]string{".txt"}))
		if listable, lerr := storage.ListerForURI(storage.NewFileURI(saveDir)); lerr == nil {
			fileSave.SetLocation(listable)
		}
		fileSave.SetFileName("SODIS-REPORT.txt")
		fileSave.Resize(fyne.NewSize(800, 600))
		fileSave.Show()
	})
	saveBtn.Importance = widget.HighImportance

	// changeSiteBtn opens SITE-FILES and fills site-related entries from the chosen file
	// without clearing the rest of the form first.
	changeSiteBtn := widget.NewButton("Add/Change Site info", func() {
		siteDir := filepath.Join(appDir, "SITE-FILES")
		_ = os.MkdirAll(siteDir, 0755)
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, ferr error) {
			if ferr != nil {
				dialog.ShowError(ferr, w)
				return
			}
			if reader == nil {
				return // user cancelled
			}
			filePath := reader.URI().Path()
			if cerr := reader.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("error closing file: %w", cerr), w)
				return
			}
			site := parseSiteFileToMap(filePath)
			if site == nil {
				dialog.ShowError(fmt.Errorf("could not read site file: %s", filePath), w)
				return
			}
			setIfPresent := func(name, val string) {
				if val != "" {
					if e, ok := entries[name]; ok {
						e.SetText(val)
					}
				}
			}
			setIfPresent("Observer1", site["observer1"])
			if fill.vt != nil && site["observer1"] != "" {
				fill.vt.ObserverNameEntry.SetText(site["observer1"])
			}
			setIfPresent("Observer2", site["observer2"])
			setIfPresent("Observatory", site["observatory"])
			setIfPresent("E-mail", site["email"])
			setIfPresent("Address", site["address"])
			setIfPresent("NearestCity", site["nearest_city"])
			setIfPresent("Countrycode", site["country_code"])
			setIfPresent("Telescope", site["telescope"])
			if telescopeSelect != nil {
				if e, ok := entries["Telescope"]; ok {
					if opt, ok2 := telescopeValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						telescopeSelect.SetSelected(opt)
					}
				}
			}
			setIfPresent("Aperture", site["aperture"])
			setIfPresent("FocalLength", site["focal_length"])
			setIfPresent("ObservingMethod", site["observing_method"])
			if observingMethodSelect != nil {
				if e, ok := entries["ObservingMethod"]; ok {
					if opt, ok2 := observingMethodValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						observingMethodSelect.SetSelected(opt)
					}
				}
			}
			setIfPresent("Timesource", site["time_source"])
			if timesourceSelect != nil {
				if e, ok := entries["Timesource"]; ok {
					if opt, ok2 := timesourceValToOpt[strings.TrimSpace(e.Text)]; ok2 {
						timesourceSelect.SetSelected(opt)
					}
				}
			}
			setIfPresent("Camera", site["camera"])
			if latStr := site["latitude_decimal"]; latStr != "" {
				if lat, err := strconv.ParseFloat(latStr, 64); err == nil {
					setIfPresent("Latitude", decimalDegToSODIS(lat))
				}
			}
			if lonStr := site["longitude_decimal"]; lonStr != "" {
				if lon, err := strconv.ParseFloat(lonStr, 64); err == nil {
					setIfPresent("Longitude", decimalDegToSODIS(lon))
				}
			}
			setIfPresent("Altitude", site["altitude"])
			if fill.vt != nil && site["altitude"] != "" {
				fill.vt.SiteAltitudeEntry.SetText(site["altitude"])
			}
			logAction(fmt.Sprintf("SODIS site info updated from: %s", filePath))
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".site"}))
		if listable, lerr := storage.ListerForURI(storage.NewFileURI(siteDir)); lerr == nil {
			fileDialog.SetLocation(listable)
		}
		fileDialog.Resize(fyne.NewSize(800, 600))
		fileDialog.Show()
	})

	cancelBtn := widget.NewButton("Cancel", func() {
		dlg.Hide()
	})

	buttons := container.NewHBox(loadTemplateBtn, changeSiteBtn, layout.NewSpacer(), cancelBtn, saveBtn)
	content := container.NewBorder(nil, buttons, nil, nil, scroll)
	dlg = dialog.NewCustomWithoutButtons("Fill SODIS Report", content, w)
	dlg.Resize(fyne.NewSize(800, 650))
	dlg.Show()
}

// createVizieRPreviewPlotImage creates a plot image showing the light curve
// as it will appear when submitted to VizieR (scaled to 0–9524 range).
// timestamps and scaledValues are parallel slices for the submission range.
// dropped[i] == true means that the frame is interpolated/missing and should be
// rendered as a gap in the line.
func createVizieRPreviewPlotImage(
	timestamps []float64, scaledValues []int, dropped []bool,
	title string, plotWidth, plotHeight int,
) (image.Image, error) {
	plt := plot.New()
	if grayPlotBackground {
		plt.BackgroundColor = plotBackgroundGray
	}

	plt.Title.TextStyle.Font.Typeface = "Liberation"
	plt.Title.TextStyle.Font.Variant = "Sans"
	plt.Title.TextStyle.Font.Size = vg.Points(14)
	plt.Title.TextStyle.Font.Weight = 2

	plt.X.Label.TextStyle.Font.Typeface = "Liberation"
	plt.X.Label.TextStyle.Font.Variant = "Sans"
	plt.X.Label.TextStyle.Font.Size = vg.Points(11)

	plt.Y.Label.TextStyle.Font.Typeface = "Liberation"
	plt.Y.Label.TextStyle.Font.Variant = "Sans"
	plt.Y.Label.TextStyle.Font.Size = vg.Points(11)

	plt.X.Tick.Label.Font.Typeface = "Liberation"
	plt.X.Tick.Label.Font.Variant = "Sans"
	plt.X.Tick.Label.Font.Size = vg.Points(9)

	plt.Y.Tick.Label.Font.Typeface = "Liberation"
	plt.Y.Tick.Label.Font.Variant = "Sans"
	plt.Y.Tick.Label.Font.Size = vg.Points(9)

	plt.Title.Text = title
	plt.X.Label.Text = "Time"
	plt.Y.Label.Text = "VizieR Scaled Intensity (0\u20139524)"
	plt.X.Tick.Marker = timestampTicker{}
	plt.Y.Min = 0
	plt.Y.Max = 10000

	// Extend the X axis 3 frame-intervals beyond the last point for right-side breathing room.
	if len(timestamps) >= 2 {
		dt := (timestamps[len(timestamps)-1] - timestamps[0]) / float64(len(timestamps)-1)
		plt.X.Max = timestamps[len(timestamps)-1] + 6*dt
	}

	// Dark gray horizontal reference line at y=0.
	if len(timestamps) > 0 {
		xMax := plt.X.Max
		if xMax == 0 {
			xMax = timestamps[len(timestamps)-1]
		}
		zeroLine, err := plotter.NewLine(plotter.XYs{
			{X: timestamps[0], Y: 0},
			{X: xMax, Y: 0},
		})
		if err == nil {
			zeroLine.Color = color.RGBA{R: 80, G: 80, B: 80, A: 255}
			zeroLine.Width = vg.Points(1)
			plt.Add(zeroLine)
		}
	}

	// Draw red vertical marker lines at each dropped frame location.
	gapColor := color.RGBA{R: 220, G: 0, B: 0, A: 255}
	for i, ts := range timestamps {
		if dropped[i] {
			gapLine, err := plotter.NewLine(plotter.XYs{
				{X: ts, Y: plt.Y.Min},
				{X: ts, Y: plt.Y.Max},
			})
			if err == nil {
				gapLine.Color = gapColor
				gapLine.Width = vg.Points(1.5)
				plt.Add(gapLine)
			}
		}
	}

	// Build connected line segments with blue dot markers, breaking at dropped frames.
	// Lines are thin black; dots are small blue circles.
	dotColor := color.RGBA{R: 0, G: 100, B: 200, A: 255}
	var seg plotter.XYs
	flushSeg := func() {
		if len(seg) >= 2 {
			line, pts, err := plotter.NewLinePoints(seg)
			if err == nil {
				line.Color = color.RGBA{R: 0, G: 0, B: 0, A: 255}
				line.Width = vg.Points(0.5)
				pts.GlyphStyle.Color = dotColor
				pts.GlyphStyle.Radius = vg.Points(2)
				pts.GlyphStyle.Shape = draw.CircleGlyph{}
				plt.Add(line, pts)
			}
		} else if len(seg) == 1 {
			sc, err := plotter.NewScatter(seg)
			if err == nil {
				sc.GlyphStyle.Color = dotColor
				sc.GlyphStyle.Radius = vg.Points(2)
				sc.GlyphStyle.Shape = draw.CircleGlyph{}
				plt.Add(sc)
			}
		}
		seg = nil
	}
	for i, ts := range timestamps {
		if dropped[i] {
			flushSeg()
		} else {
			seg = append(seg, plotter.XY{X: ts, Y: float64(scaledValues[i])})
		}
	}
	flushSeg()

	// Render to an in-memory PNG.
	w := vg.Length(plotWidth) * vg.Inch / 96
	h := vg.Length(plotHeight) * vg.Inch / 96
	img := vgimg.New(w, h)
	dc := draw.New(img)
	plt.Draw(dc)

	var buf bytes.Buffer
	if err := png.Encode(&buf, img.Image()); err != nil {
		return nil, fmt.Errorf("failed to encode VizieR preview PNG: %w", err)
	}
	goImg, err := png.Decode(bytes.NewReader(buf.Bytes()))
	if err != nil {
		return nil, fmt.Errorf("failed to decode VizieR preview PNG: %w", err)
	}
	return goImg, nil
}
