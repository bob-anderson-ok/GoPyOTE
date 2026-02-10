package main

import (
	"archive/zip"
	"fmt"
	"image/color"
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

	// Zip button for zipping .dat files
	ZipBtn *widget.Button

	// Load from the NA spreadsheet button
	LoadXlsxBtn *widget.Button

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

	// Clear inputs button
	clearBtn := widget.NewButton("Clear inputs", func() {
		vt.ClearInputs()
	})

	// Zip button (callback set below, needs access to vt)
	vt.ZipBtn = widget.NewButton("Zip *.dat files for sending", func() {})

	// Load from the NA spreadsheet button (callback set below, needs access to window)
	vt.LoadXlsxBtn = widget.NewButton("Load from NA spreadsheet", func() {})

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
		container.NewHBox(vt.GenerateBtn, vt.ZipBtn),
		container.NewHBox(clearBtn, vt.LoadXlsxBtn),
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
	vt.StatusLabel.SetText("Inputs cleared")
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
		filePath := reader.URI().Path()
		if cerr := reader.Close(); cerr != nil {
			dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
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
	fileDialog.Show()
}
