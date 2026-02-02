package main

import (
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
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
}

// NewVizieRTab creates a new VizieR export tab with all its widgets
func NewVizieRTab() *VizieRTab {
	vt := &VizieRTab{}

	// Background
	tabBg := canvas.NewRectangle(color.RGBA{R: 210, G: 220, B: 210, A: 255})

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
	vt.GenerateBtn = widget.NewButton("Generate VizieR file", func() {})

	// Clear inputs button
	clearBtn := widget.NewButton("Clear inputs", func() {
		vt.ClearInputs()
	})

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
		container.NewHBox(vt.GenerateBtn, clearBtn),
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
	for i, frameNum := range data.FrameNumbers {
		if int(frameNum) >= rangeStart && startIdx == 0 {
			startIdx = i
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

	// Extract values in range
	vizierY := valueColumn[startIdx : endIdx+1]

	// Find max value for scaling (ignoring negative zeros which represent dropped frames)
	maxValue := 0.0
	for _, val := range vizierY {
		if val > maxValue {
			maxValue = val
		}
	}

	// Compute scale factor to normalize to 0-9524 range
	scaleFactor := 9524.0 / maxValue
	if maxValue == 0 {
		scaleFactor = 1.0
	}

	// Build values string
	valuesText := "Values"
	for _, value := range vizierY {
		// Check for negative zero (dropped reading marker)
		if value == 0 && 1.0/value < 0 {
			valuesText += ": "
		} else {
			scaledValue := int(value * scaleFactor)
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

	statusLabel.SetText(fmt.Sprintf("VizieR file written to:\n%s", vizierFilePath))
	dialog.ShowInformation("VizieR Export Complete",
		fmt.Sprintf("Your VizieR lightcurve file was written to:\n\n%s", vizierFilePath), w)
	logAction(fmt.Sprintf("Generated VizieR file: %s with %d readings", vizierFilePath, numReadings))
}
