package main

import (
	"bufio"
	"bytes"
	"embed"
	"encoding/csv"
	"encoding/xml"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"GoPyOTE/lightcurve"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/KevinWang15/go-json5"
	"github.com/pconstantinou/savitzkygolay"
)

//go:embed help_markdown/smoothingAndNormalization.md help_images/SmoothingFig1.png help_images/SmoothingFig2.png help_images/SmoothingFig3.png help_images/SmoothingFig4.png
var smoothingMarkdown embed.FS

//go:embed help_markdown/timestampAnalysis.md help_images/droppedFrameDemoPlot.png help_images/consecutiveOCRerrorDemo.png
var timestampAnalysisMarkdown embed.FS

//go:embed help_markdown/blockIntegration.md
var blockIntegrationMarkdown embed.FS

//go:embed help_markdown/about.md
var aboutMarkdown embed.FS

//go:embed help_markdown/vizierMarkdown.md
var vizierExportMarkdown embed.FS

//go:embed help_markdown/fitMarkdown.md
var fitExplanationMarkdown embed.FS

//go:embed help_markdown/occelmntOWC.md
var occelmntButtonExplanation embed.FS

//go:embed help_markdown/editOccParams.md
var editOccParamsExplanation embed.FS

//go:embed help_markdown/runIOTAdiffraction.md
var runIOTAdiffractionExplanation embed.FS

//go:embed help_markdown/fresnelScaleResolution.md
var fresnelScaleResolutionMarkdown embed.FS

//go:embed help_markdown/edgeTimeSigmaExplanation.md
var monteCarloExplanation embed.FS

// Version information
const Version = "1.2.12"

// Track the last loaded parameters file path for use by Run IOTAdiffraction
var lastLoadedParamsPath string

// Track the last loaded site file path for use by the Fill SODIS Report dialog
var lastLoadedSitePath string

// Track the last loaded occelmnt XML text for use by the Fill SODIS Report dialog
var lastLoadedOccelmntXml string

// Track the CSV-measured median exposure time (seconds) for use by the Fill SODIS Report dialog
var lastCsvExposureSecs float64

// Track the observer GPS location from the last successful ObserverT0CorrectionFromOWC call.
// Persisted in preferences so the SODIS fill works across sessions.
var lastObserverLatDeg float64
var lastObserverLonDeg float64
var lastObserverAltMeters float64
var lastObserverLocationSet bool

// Track the parameters file used for the last IOTAdiffraction run (for startup display)
var lastDiffractionParamsPath string

// Title from the parameters file used for the last IOTAdiffraction run (for plot titles)
var lastDiffractionTitle string

// resultsFolder is the path to the -RESULTS folder created alongside the opened CSV file.
// Various outputs (fit plots, histograms, etc.) are written here.
var resultsFolder string

// sodisReportSavedThisSession is set to true when a SODIS report is saved
// during the current session, so that the VizieR "Copy from SODIS-REPORT.txt"
// button only accepts a report generated in this session.
var sodisReportSavedThisSession bool
var vizierDatWrittenThisSession bool

// sodisNegativeReportSaved is set to true when a NEGATIVE SODIS report is saved.
// A negative report does not require a VizieR .dat file, so the close warning is skipped.
var sodisNegativeReportSaved bool

// afterOccParamsSaved, when non-nil, is called with the saved file path immediately after
// showOccultationParametersDialog successfully writes a new .occparams file.
// Assigned in main() once all UI elements needed by IOTAdiffraction are initialized.
var afterOccParamsSaved func(string)

// appDir is the directory containing the executable. Used to resolve relative file paths
// (diffraction images, IOTAdiffraction.exe, etc.) regardless of the OS working directory.
var appDir string

// grayPlotBackground controls whether plots use a gray background instead of white.
var grayPlotBackground bool

// plotBackgroundColor returns the color to use for plot backgrounds based on the preference.
var plotBackgroundGray = color.RGBA{R: 170, G: 170, B: 170, A: 255}

// ForcedVariantTheme delegates everything to Base, but forces Color() to use Variant.
// This replaces deprecated theme.DarkTheme()/theme.LightTheme() calls.
type ForcedVariantTheme struct {
	Base    fyne.Theme
	Variant fyne.ThemeVariant
}

func (t *ForcedVariantTheme) Color(name fyne.ThemeColorName, _ fyne.ThemeVariant) color.Color {
	return t.Base.Color(name, t.Variant)
}

func (t *ForcedVariantTheme) Font(style fyne.TextStyle) fyne.Resource {
	return t.Base.Font(style)
}

func (t *ForcedVariantTheme) Icon(name fyne.ThemeIconName) fyne.Resource {
	return t.Base.Icon(name)
}

func (t *ForcedVariantTheme) Size(name fyne.ThemeSizeName) float32 {
	return t.Base.Size(name)
}

// Maximum number of recent folders to keep
const maxRecentFolders = 6

// getRecentFolders retrieves the list of recent folders from preferences
func getRecentFolders(prefs fyne.Preferences) []string {
	var folders []string
	for i := 0; i < maxRecentFolders; i++ {
		folder := prefs.String(fmt.Sprintf("recentFolder%d", i))
		if folder != "" {
			folders = append(folders, folder)
		}
	}
	return folders
}

// saveRecentFolders saves the list of recent folders to preferences
func saveRecentFolders(prefs fyne.Preferences, folders []string) {
	for i := 0; i < maxRecentFolders; i++ {
		if i < len(folders) {
			prefs.SetString(fmt.Sprintf("recentFolder%d", i), folders[i])
		} else {
			prefs.SetString(fmt.Sprintf("recentFolder%d", i), "")
		}
	}
}

// addRecentFolder adds a folder to the recent list (pushdown stack behavior)
func addRecentFolder(prefs fyne.Preferences, folderPath string) {
	// Don't add internal application directories to the recent list
	base := filepath.Base(folderPath)
	if base == "OCCULTATION-PARAMETERS" || base == "SITE-FILES" {
		return
	}

	folders := getRecentFolders(prefs)

	// Remove if already exists (to move it to the top)
	newFolders := []string{folderPath}
	for _, f := range folders {
		if f != folderPath {
			newFolders = append(newFolders, f)
		}
	}

	// Limit to max size
	if len(newFolders) > maxRecentFolders {
		newFolders = newFolders[:maxRecentFolders]
	}

	saveRecentFolders(prefs, newFolders)
}

// showFileOpenWithRecents shows a dialog with recent folders, then opens the file dialog.
// homeDir is the observation home directory (from settings); if non-empty a blue Home button is shown.
func showFileOpenWithRecents(w fyne.Window, prefs fyne.Preferences, title string, filter storage.FileFilter, homeDir string, callback func(fyne.URIReadCloser, error)) {
	folders := getRecentFolders(prefs)

	// If no recent folders and no home dir, show the file dialog directly
	if len(folders) == 0 && homeDir == "" {
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if reader != nil && err == nil {
				// Add the parent folder to recents
				folderPath := filepath.Dir(reader.URI().Path())
				addRecentFolder(prefs, folderPath)
			}
			callback(reader, err)
		}, w)
		if filter != nil {
			fileDialog.SetFilter(filter)
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
		return
	}

	// Create buttons for each recent folder
	var buttons []fyne.CanvasObject
	var customDialog *dialog.CustomDialog

	// Helper to open the file dialog at a specific location
	openAtLocation := func(folderPath string) {
		customDialog.Hide()
		fileDialog := dialog.NewFileOpen(func(reader fyne.URIReadCloser, err error) {
			if reader != nil && err == nil {
				// Add the parent folder to recents
				newFolderPath := filepath.Dir(reader.URI().Path())
				addRecentFolder(prefs, newFolderPath)
			}
			callback(reader, err)
		}, w)
		if filter != nil {
			fileDialog.SetFilter(filter)
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))

		// Set the starting location if the folder exists
		if folderPath != "" {
			folderURI := storage.NewFileURI(folderPath)
			listableURI, err := storage.ListerForURI(folderURI)
			if err == nil {
				fileDialog.SetLocation(listableURI)
			}
		}
		fileDialog.Show()
	}

	// Add a Home button with a directory label
	homeBtn := widget.NewButton("Home", func() {
		if info, err := os.Stat(homeDir); err != nil || !info.IsDir() {
			dialog.ShowError(fmt.Errorf("Home directory not found:\n%s", homeDir), w)
			return
		}
		openAtLocation(homeDir)
	})
	homeBtn.Importance = widget.HighImportance
	var homeDirLabel *widget.Label
	if homeDir != "" {
		homeDirLabel = widget.NewLabel(homeDir)
	} else {
		homeDirLabel = widget.NewLabel("use the Settings tab to set the Home directory")
		homeDirLabel.TextStyle = fyne.TextStyle{Italic: true}
		homeBtn.Disable()
	}
	buttons = append(buttons, container.NewHBox(homeBtn, homeDirLabel))

	// Add separator
	buttons = append(buttons, widget.NewSeparator())

	// Add label for recent folders
	buttons = append(buttons, widget.NewLabel("Recent folders (click to select):"))

	// Add a button for each recent folder
	for _, folder := range folders {
		folderCopy := folder // Capture for closure
		// Show an abbreviated path for display
		displayName := folder
		if len(displayName) > 135 {
			displayName = "..." + displayName[len(displayName)-132:]
		}
		btn := widget.NewButton(displayName, func() {
			openAtLocation(folderCopy)
		})
		btn.Importance = widget.LowImportance
		buttons = append(buttons, btn)
	}

	// Add a Clear history button
	buttons = append(buttons, widget.NewSeparator())
	clearHistoryBtn := widget.NewButton("Clear history", func() {
		saveRecentFolders(prefs, nil)
		customDialog.Hide()
	})
	clearHistoryBtn.Importance = widget.HighImportance
	buttons = append(buttons, container.NewHBox(clearHistoryBtn))

	content := container.NewVBox(buttons...)
	customDialog = dialog.NewCustom(title, "Cancel", content, w)
	customDialog.Resize(fyne.NewSize(900, 0))
	customDialog.Show()
}

// showOccultationParametersDialog displays a form dialog for editing occultation parameters.
// Pass clearAll=true to open with all entries blank (e.g., from the Edit Occultation Parameters button).
// Pass a non-nil preload to pre-populate entries directly (e.g., from Create Occultation).
// Pass a non-empty obsDir to have the Write button auto-save directly to that folder (no file dialog).
func showOccultationParametersDialog(w fyne.Window, clearAll bool, preload *OccultationParameters, obsDir string) {
	// Build dropdown choices from files in the CAMERA-QE folder
	cameraQEDir := filepath.Join(appDir, "CAMERA-QE")
	qeFileNames := []string{"(none)"}
	if dirEntries, err := os.ReadDir(cameraQEDir); err == nil {
		for _, entry := range dirEntries {
			if !entry.IsDir() {
				qeFileNames = append(qeFileNames, entry.Name())
			}
		}
	}

	// Create entry fields for all parameters
	windowSizeEntry := widget.NewEntry()
	titleEntry := widget.NewEntry()
	fundamentalPlaneWidthKmEntry := widget.NewEntry()
	fundamentalPlaneWidthNumPointsEntry := widget.NewEntry()
	parallaxArcsecEntry := widget.NewEntry()
	distanceAuEntry := widget.NewEntry()
	pathToQeTableFileSelect := widget.NewSelect(qeFileNames, nil)
	// setQeSelected sets the dropdown from a stored path (may include CAMERA-QE/ prefix)
	setQeSelected := func(path string) {
		if path == "" {
			pathToQeTableFileSelect.SetSelected("(none)")
			return
		}
		pathToQeTableFileSelect.SetSelected(filepath.Base(path))
	}
	// getQeSelected returns the selected QE file name, or "" when "(none)" is chosen.
	getQeSelected := func() string {
		if pathToQeTableFileSelect.Selected == "(none)" {
			return ""
		}
		return pathToQeTableFileSelect.Selected
	}
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

	// Label to display the loaded parameters file name
	fileNameLabel := widget.NewLabel("")
	fileNameLabel.TextStyle = fyne.TextStyle{Bold: true}

	// Restore last loaded parameters path from preferences
	prefs := fyne.CurrentApp().Preferences()
	if lastLoadedParamsPath == "" {
		lastLoadedParamsPath = prefs.StringWithFallback("lastLoadedParamsPath", "")
	}

	// occelmntXml associated with the currently displayed params file (empty if none)
	var dialogOccelmntXml string

	// Autoload previously opened parameters file if available (skipped when clearAll or preload is set)
	if !clearAll && preload == nil && lastLoadedParamsPath != "" {
		file, err := os.Open(lastLoadedParamsPath)
		if err == nil {
			params, parseErr := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", closeErr), w)
			}
			if parseErr == nil {
				dialogOccelmntXml = params.OccelmntXml
				windowSizeEntry.SetText(strconv.Itoa(params.WindowSizePixels))
				titleEntry.SetText(params.Title)
				fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(params.FundamentalPlaneWidthKm, 'f', -1, 64))
				fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(params.FundamentalPlaneWidthNumPoints))
				parallaxArcsecEntry.SetText(strconv.FormatFloat(params.ParallaxArcsec, 'f', -1, 64))
				distanceAuEntry.SetText(strconv.FormatFloat(params.DistanceAu, 'f', -1, 64))
				setQeSelected(params.PathToQeTableFile)
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
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)
				logAction(fmt.Sprintf("Auto-loaded parameters file: %s", lastLoadedParamsPath))
			}
		}
	}

	// Populate entries from preload if provided (e.g., from Create Occultation)
	if preload != nil {
		dialogOccelmntXml = preload.OccelmntXml
		windowSizeEntry.SetText(strconv.Itoa(preload.WindowSizePixels))
		titleEntry.SetText(preload.Title)
		fundamentalPlaneWidthKmEntry.SetText(strconv.FormatFloat(preload.FundamentalPlaneWidthKm, 'f', -1, 64))
		fundamentalPlaneWidthNumPointsEntry.SetText(strconv.Itoa(preload.FundamentalPlaneWidthNumPoints))
		parallaxArcsecEntry.SetText(strconv.FormatFloat(preload.ParallaxArcsec, 'f', -1, 64))
		distanceAuEntry.SetText(strconv.FormatFloat(preload.DistanceAu, 'f', -1, 64))
		setQeSelected(preload.PathToQeTableFile)
		observationWavelengthNmEntry.SetText(strconv.Itoa(preload.ObservationWavelengthNm))
		dXKmPerSecEntry.SetText(strconv.FormatFloat(preload.DXKmPerSec, 'f', -1, 64))
		dYKmPerSecEntry.SetText(strconv.FormatFloat(preload.DYKmPerSec, 'f', -1, 64))
		pathPerpendicularOffsetKmEntry.SetText(strconv.FormatFloat(preload.PathPerpendicularOffsetKm, 'f', -1, 64))
		percentMagDropEntry.SetText(strconv.Itoa(preload.PercentMagDrop))
		starDiamOnPlaneMasEntry.SetText(strconv.FormatFloat(preload.StarDiamOnPlaneMas, 'f', -1, 64))
		limbDarkeningCoeffEntry.SetText(strconv.FormatFloat(preload.LimbDarkeningCoeff, 'f', -1, 64))
		starClassEntry.SetText(preload.StarClass)
		mainBodyXCenterEntry.SetText(strconv.FormatFloat(preload.MainBody.XCenterKm, 'f', -1, 64))
		mainBodyYCenterEntry.SetText(strconv.FormatFloat(preload.MainBody.YCenterKm, 'f', -1, 64))
		mainBodyMajorAxisEntry.SetText(strconv.FormatFloat(preload.MainBody.MajorAxisKm, 'f', -1, 64))
		mainBodyMinorAxisEntry.SetText(strconv.FormatFloat(preload.MainBody.MinorAxisKm, 'f', -1, 64))
		mainBodyPaDegreesEntry.SetText(strconv.FormatFloat(preload.MainBody.MajorAxisPaDegrees, 'f', -1, 64))
		satelliteXCenterEntry.SetText(strconv.FormatFloat(preload.Satellite.XCenterKm, 'f', -1, 64))
		satelliteYCenterEntry.SetText(strconv.FormatFloat(preload.Satellite.YCenterKm, 'f', -1, 64))
		satelliteMajorAxisEntry.SetText(strconv.FormatFloat(preload.Satellite.MajorAxisKm, 'f', -1, 64))
		satelliteMinorAxisEntry.SetText(strconv.FormatFloat(preload.Satellite.MinorAxisKm, 'f', -1, 64))
		satellitePaDegreesEntry.SetText(strconv.FormatFloat(preload.Satellite.MajorAxisPaDegrees, 'f', -1, 64))
		pathToExternalImageEntry.SetText(preload.PathToExternalImage)
		fileNameLabel.SetText("New parameters — use Write to save")
	}

	// If the QE file dropdown has no real file selected, fill from the saved preference
	if pathToQeTableFileSelect.Selected == "" || pathToQeTableFileSelect.Selected == "(none)" {
		if savedQe := prefs.StringWithFallback("stickyQeTableFile", ""); savedQe != "" {
			setQeSelected(savedQe)
		}
	}

	// Collect all entries for dirty-checking on Cancel
	allEntries := []*widget.Entry{
		windowSizeEntry, titleEntry, fundamentalPlaneWidthKmEntry,
		fundamentalPlaneWidthNumPointsEntry, parallaxArcsecEntry, distanceAuEntry,
		observationWavelengthNmEntry, dXKmPerSecEntry,
		dYKmPerSecEntry, pathPerpendicularOffsetKmEntry, percentMagDropEntry,
		starDiamOnPlaneMasEntry, limbDarkeningCoeffEntry, starClassEntry,
		mainBodyXCenterEntry, mainBodyYCenterEntry, mainBodyMajorAxisEntry,
		mainBodyMinorAxisEntry, mainBodyPaDegreesEntry,
		satelliteXCenterEntry, satelliteYCenterEntry, satelliteMajorAxisEntry,
		satelliteMinorAxisEntry, satellitePaDegreesEntry,
		pathToExternalImageEntry,
	}
	// Snapshot initial values so we can detect edits
	initialValues := make([]string, len(allEntries))
	for i, e := range allEntries {
		initialValues[i] = e.Text
	}
	qeInitialValue := pathToQeTableFileSelect.Selected

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
		&widget.FormItem{Text: "Name of camera QE file", Widget: pathToQeTableFileSelect},
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

	// Create a custom dialog with Browse/Write/Cancel buttons
	var customDialog *dialog.CustomDialog
	cancelBtn := widget.NewButton("Cancel", func() {
		// Check if any entry has been modified
		dirty := pathToQeTableFileSelect.Selected != qeInitialValue
		for i, e := range allEntries {
			if e.Text != initialValues[i] {
				dirty = true
				break
			}
		}
		if dirty {
			dialog.ShowConfirm("Unsaved Changes",
				"You have unsaved changes. Use Write to save them.\n\nDiscard changes and close?",
				func(confirmed bool) {
					if confirmed {
						customDialog.Hide()
					}
				}, w)
		} else {
			customDialog.Hide()
		}
	})

	// File open button
	loadBtn := widget.NewButton("Browse", func() {
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
			setQeSelected(params.PathToQeTableFile)
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

			dialogOccelmntXml = params.OccelmntXml

			// Store the loaded file name for use as default in the save dialog
			loadedFileName = reader.URI().Name()
			// Store the full path for use by Run IOTAdiffraction
			lastLoadedParamsPath = reader.URI().Path()
			// Persist to preferences so it autoloads next time
			prefs.SetString("lastLoadedParamsPath", lastLoadedParamsPath)
			fileNameLabel.SetText("File being displayed:  " + loadedFileName)
			logAction(fmt.Sprintf("Loaded parameters file: %s", lastLoadedParamsPath))
			// Re-snapshot so a fresh load is considered clean
			for i, e := range allEntries {
				initialValues[i] = e.Text
			}
			qeInitialValue = pathToQeTableFileSelect.Selected
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".occparams"}))
		occParamsDir := filepath.Join(appDir, "OCCULTATION-PARAMETERS")
		folderURI := storage.NewFileURI(occParamsDir)
		if listableURI, err := storage.ListerForURI(folderURI); err == nil {
			fileDialog.SetLocation(listableURI)
		}
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
	saveBtn := widget.NewButton("Write", func() {
		doSave := func() {
			if obsDir != "" {
				// Auto-save directly to the observation folder — no file dialog
				saveFileName := "occultation.occparams"
				if title := strings.TrimSpace(titleEntry.Text); title != "" {
					sanitized := strings.Map(func(r rune) rune {
						if strings.ContainsRune(`\/:*?"<>|`, r) {
							return '_'
						}
						return r
					}, title)
					saveFileName = sanitized + ".occparams"
				} else if loadedFileName != "" {
					saveFileName = loadedFileName
				}
				savePath := filepath.Join(obsDir, saveFileName)
				// Build parameters struct from entry fields
				params := OccultationParameters{
					WindowSizePixels:               parseInt(windowSizeEntry.Text),
					Title:                          titleEntry.Text,
					FundamentalPlaneWidthKm:        parseFloat(fundamentalPlaneWidthKmEntry.Text),
					FundamentalPlaneWidthNumPoints: parseInt(fundamentalPlaneWidthNumPointsEntry.Text),
					ParallaxArcsec:                 parseFloat(parallaxArcsecEntry.Text),
					DistanceAu:                     parseFloat(distanceAuEntry.Text),
					PathToQeTableFile:              getQeSelected(),
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
					ExposureTimeSecs:    0,
					OccelmntXml:         dialogOccelmntXml,
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

				// Write to the file with the enforced extension
				if werr := os.WriteFile(savePath, data, 0644); werr != nil {
					dialog.ShowError(fmt.Errorf("failed to write file: %w", werr), w)
					return
				}

				logAction(fmt.Sprintf("Saved parameters file: %s", savePath))

				// Track the saved path so CSV-read autofill and future Browse defaults use it
				lastLoadedParamsPath = savePath
				prefs.SetString("lastLoadedParamsPath", lastLoadedParamsPath)
				loadedFileName = filepath.Base(savePath)
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)

				// Persist QE file name so it autofills next time
				if qe := getQeSelected(); qe != "" {
					prefs.SetString("stickyQeTableFile", qe)
				}

				// Re-snapshot so saved state is considered clean
				for i, e := range allEntries {
					initialValues[i] = e.Text
				}
				qeInitialValue = pathToQeTableFileSelect.Selected

				// Close the parameters dialog after a successful save
				customDialog.Hide()
				if afterOccParamsSaved != nil {
					afterOccParamsSaved(savePath)
				}
				return
			}

			fileDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				if writer == nil {
					return // User cancelled
				}

				// Enforce .occparams extension
				originalSavePath := writer.URI().Path()
				savePath := originalSavePath
				ext := filepath.Ext(savePath)
				if strings.ToLower(ext) != ".occparams" {
					savePath = strings.TrimSuffix(savePath, ext) + ".occparams"
				}

				// Close the writer first so the file handle is released
				if cerr := writer.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
				}

				// Remove the empty file created by the save dialog if the path changed
				if savePath != originalSavePath {
					if rerr := os.Remove(originalSavePath); rerr != nil {
						fmt.Printf("Warning: could not remove empty file %s: %v\n", originalSavePath, rerr)
					}
				}

				// Build parameters struct from entry fields
				params := OccultationParameters{
					WindowSizePixels:               parseInt(windowSizeEntry.Text),
					Title:                          titleEntry.Text,
					FundamentalPlaneWidthKm:        parseFloat(fundamentalPlaneWidthKmEntry.Text),
					FundamentalPlaneWidthNumPoints: parseInt(fundamentalPlaneWidthNumPointsEntry.Text),
					ParallaxArcsec:                 parseFloat(parallaxArcsecEntry.Text),
					DistanceAu:                     parseFloat(distanceAuEntry.Text),
					PathToQeTableFile:              getQeSelected(),
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
					ExposureTimeSecs:    0,
					OccelmntXml:         dialogOccelmntXml,
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

				// Write to the file with the enforced extension
				if werr := os.WriteFile(savePath, data, 0644); werr != nil {
					dialog.ShowError(fmt.Errorf("failed to write file: %w", werr), w)
					return
				}

				logAction(fmt.Sprintf("Saved parameters file: %s", savePath))

				// Track the saved path so CSV-read autofill and future Browse defaults use it
				lastLoadedParamsPath = savePath
				prefs.SetString("lastLoadedParamsPath", lastLoadedParamsPath)
				loadedFileName = filepath.Base(savePath)
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)

				// Persist QE file name so it autofills next time
				if qe := getQeSelected(); qe != "" {
					prefs.SetString("stickyQeTableFile", qe)
				}

				// Re-snapshot so saved state is considered clean
				for i, e := range allEntries {
					initialValues[i] = e.Text
				}
				qeInitialValue = pathToQeTableFileSelect.Selected

				// Close the parameters dialog after a successful save
				customDialog.Hide()
				if afterOccParamsSaved != nil {
					afterOccParamsSaved(savePath)
				}
			}, w)
			fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".occparams"}))
			if loadedFileName != "" {
				fileDialog.SetFileName(loadedFileName)
			} else if title := strings.TrimSpace(titleEntry.Text); title != "" {
				sanitized := strings.Map(func(r rune) rune {
					if strings.ContainsRune(`\/:*?"<>|`, r) {
						return '_'
					}
					return r
				}, title)
				fileDialog.SetFileName(sanitized + ".occparams")
			}
			// Always save to the OCCULTATION-PARAMETERS directory, creating it if needed
			occParamsDir := filepath.Join(appDir, "OCCULTATION-PARAMETERS")
			if merr := os.MkdirAll(occParamsDir, 0755); merr != nil {
				dialog.ShowError(fmt.Errorf("failed to create OCCULTATION-PARAMETERS directory: %w", merr), w)
				return
			}
			folderURI := storage.NewFileURI(occParamsDir)
			if listableURI, locErr := storage.ListerForURI(folderURI); locErr == nil {
				fileDialog.SetLocation(listableURI)
			}
			fileDialog.Resize(fyne.NewSize(1200, 800))
			fileDialog.Show()
		} // end doSave

		if pathToQeTableFileSelect.Selected == "" {
			dialog.ShowConfirm(
				"No camera QE file",
				"No camera QE file is selected. Save without one?",
				func(saveWithout bool) {
					if saveWithout {
						doSave()
					}
				}, w)
			return
		}
		doSave()
	})
	saveBtn.Importance = widget.HighImportance

	showOccelmntBtn := widget.NewButton("Show associated occelmnt.xml", func() {
		if dialogOccelmntXml == "" {
			dialog.ShowInformation("No occelmnt.xml data", "No occelmnt.xml data is associated with the current parameters file.", w)
			return
		}
		xmlEntry := widget.NewMultiLineEntry()
		xmlEntry.SetText(dialogOccelmntXml)
		xmlEntry.Wrapping = fyne.TextWrapOff
		scroll := container.NewVScroll(xmlEntry)
		scroll.SetMinSize(fyne.NewSize(800, 300))
		d := dialog.NewCustom("Associated occelmnt.xml", "Close", scroll, w)
		d.Resize(fyne.NewSize(840, 400))
		d.Show()
	})

	buttons := container.NewHBox(loadBtn, saveBtn, showOccelmntBtn, layout.NewSpacer(), cancelBtn)
	bottomSection := container.NewVBox(fileNameLabel, buttons)
	content := container.NewBorder(nil, bottomSection, nil, nil, scrollContent)

	customDialog = dialog.NewCustomWithoutButtons("Edit Occultation Parameters", content, w)
	customDialog.Resize(fyne.NewSize(840, 750))
	customDialog.Show()
}

func showProcessOccelemntDialog(w fyne.Window, vt *VizieRTab, initialXml string) {
	pasteEntry := widget.NewMultiLineEntry()
	pasteEntry.SetPlaceHolder("Use the load button above or, in OWC, click copy to clipboard, then paste (Ctrl V) to fill this panel.")
	pasteEntry.Wrapping = fyne.TextWrapOff
	if initialXml != "" {
		pasteEntry.SetText(initialXml)
	}

	// --- Load occelmnt file button ---
	loadOccelmntBtn := widget.NewButton("Load Occelmnt file", func() {
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
			data, rerr := io.ReadAll(reader)
			if rerr != nil {
				dialog.ShowError(fmt.Errorf("failed to read occelmnt file: %w", rerr), w)
				return
			}
			xmlStr := strings.TrimPrefix(string(data), "\xef\xbb\xbf")
			pasteEntry.SetText(xmlStr)
			lastLoadedOccelmntXml = xmlStr
			fyne.CurrentApp().Preferences().SetString("lastLoadedOccelmntXml", xmlStr)
			vt.FillStarFromOccelmntXml(xmlStr)
			logAction(fmt.Sprintf("Loaded occelmnt file: %s", reader.URI().Path()))
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".xml", ".txt"}))
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
			folderURI := storage.NewFileURI(obsDir)
			if listableURI, lerr := storage.ListerForURI(folderURI); lerr == nil {
				fileDialog.SetLocation(listableURI)
			}
		}
		fileDialog.Resize(fyne.NewSize(800, 600))
		fileDialog.Show()
	})

	pasteEntry.OnChanged = func(text string) {
		trimmed := strings.TrimSpace(text)
		if trimmed == "" {
			return
		}
		if loadedLightCurveData == nil || loadedLightCurveData.SourceFilePath == "" {
			return
		}
		obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
		savePath := filepath.Join(obsDir, "occelmnt-pasted.xml")
		if err := os.WriteFile(savePath, []byte(trimmed), 0644); err != nil {
			fmt.Printf("Warning: could not write %s: %v\n", savePath, err)
		} else {
			logAction(fmt.Sprintf("Pasted occelmnt saved to %s", savePath))
		}
	}

	scrollable := container.NewVScroll(pasteEntry)
	scrollable.SetMinSize(fyne.NewSize(800, 300))

	// --- Site Location section ---
	longDegEntry := widget.NewEntry()
	longDegEntry.SetPlaceHolder("+/-deg")
	longMinEntry := widget.NewEntry()
	longMinEntry.SetPlaceHolder("min")
	longSecsEntry := widget.NewEntry()
	longSecsEntry.SetPlaceHolder("sec")
	longDegContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), longDegEntry)
	longMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), longMinEntry)
	longSecsContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), longSecsEntry)

	longDecimalEntry := widget.NewEntry()
	longDecimalEntry.SetPlaceHolder("decimal")
	longDecimalContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), longDecimalEntry)

	longUpdating := false

	updateLongDecimal := func() {
		if longUpdating {
			return
		}
		longUpdating = true
		defer func() { longUpdating = false }()
		degVal, err1 := strconv.ParseFloat(strings.TrimSpace(longDegEntry.Text), 64)
		minVal, err2 := strconv.ParseFloat(strings.TrimSpace(longMinEntry.Text), 64)
		secVal, err3 := strconv.ParseFloat(strings.TrimSpace(longSecsEntry.Text), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			longDecimalEntry.SetText("")
			return
		}
		sign := 1.0
		if degVal < 0 {
			sign = -1.0
			degVal = -degVal
		}
		decimal := sign * (degVal + minVal/60.0 + secVal/3600.0)
		longDecimalEntry.SetText(fmt.Sprintf("%.6f", decimal))
	}

	updateLongDMS := func() {
		if longUpdating {
			return
		}
		longUpdating = true
		defer func() { longUpdating = false }()
		decVal, err := strconv.ParseFloat(strings.TrimSpace(longDecimalEntry.Text), 64)
		if err != nil {
			longDegEntry.SetText("")
			longMinEntry.SetText("")
			longSecsEntry.SetText("")
			return
		}
		deg, minute, sec := decimalToDMS(decVal)
		longDegEntry.SetText(deg)
		longMinEntry.SetText(minute)
		longSecsEntry.SetText(sec)
	}

	longDegEntry.OnChanged = func(_ string) { updateLongDecimal() }
	longMinEntry.OnChanged = func(_ string) { updateLongDecimal() }
	longSecsEntry.OnChanged = func(_ string) { updateLongDecimal() }
	longDecimalEntry.OnChanged = func(_ string) { updateLongDMS() }

	latDegEntry := widget.NewEntry()
	latDegEntry.SetPlaceHolder("+/-deg")
	latMinEntry := widget.NewEntry()
	latMinEntry.SetPlaceHolder("min")
	latSecsEntry := widget.NewEntry()
	latSecsEntry.SetPlaceHolder("sec")
	latDegContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), latDegEntry)
	latMinContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), latMinEntry)
	latSecsContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(70, 36)), latSecsEntry)

	latDecimalEntry := widget.NewEntry()
	latDecimalEntry.SetPlaceHolder("decimal")
	latDecimalContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), latDecimalEntry)

	latUpdating := false

	updateLatDecimal := func() {
		if latUpdating {
			return
		}
		latUpdating = true
		defer func() { latUpdating = false }()
		degVal, err1 := strconv.ParseFloat(strings.TrimSpace(latDegEntry.Text), 64)
		minVal, err2 := strconv.ParseFloat(strings.TrimSpace(latMinEntry.Text), 64)
		secVal, err3 := strconv.ParseFloat(strings.TrimSpace(latSecsEntry.Text), 64)
		if err1 != nil || err2 != nil || err3 != nil {
			latDecimalEntry.SetText("")
			return
		}
		sign := 1.0
		if degVal < 0 {
			sign = -1.0
			degVal = -degVal
		}
		decimal := sign * (degVal + minVal/60.0 + secVal/3600.0)
		latDecimalEntry.SetText(fmt.Sprintf("%.6f", decimal))
	}

	updateLatDMS := func() {
		if latUpdating {
			return
		}
		latUpdating = true
		defer func() { latUpdating = false }()
		decVal, err := strconv.ParseFloat(strings.TrimSpace(latDecimalEntry.Text), 64)
		if err != nil {
			latDegEntry.SetText("")
			latMinEntry.SetText("")
			latSecsEntry.SetText("")
			return
		}
		deg, minute, sec := decimalToDMS(decVal)
		latDegEntry.SetText(deg)
		latMinEntry.SetText(minute)
		latSecsEntry.SetText(sec)
	}

	latDegEntry.OnChanged = func(_ string) { updateLatDecimal() }
	latMinEntry.OnChanged = func(_ string) { updateLatDecimal() }
	latSecsEntry.OnChanged = func(_ string) { updateLatDecimal() }
	latDecimalEntry.OnChanged = func(_ string) { updateLatDMS() }

	altitudeEntry := widget.NewEntry()
	altitudeEntry.SetPlaceHolder("meters")
	altitudeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 36)), altitudeEntry)

	// --- Observer / Equipment entries ---
	observer1Entry := widget.NewEntry()
	observer1Entry.SetPlaceHolder("first observer name")
	observer1Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), observer1Entry)

	observer2Entry := widget.NewEntry()
	observer2Entry.SetPlaceHolder("second observer (optional)")
	observer2Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), observer2Entry)

	observatoryEntry := widget.NewEntry()
	observatoryEntry.SetPlaceHolder("observatory name")
	observatoryContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), observatoryEntry)

	emailEntry := widget.NewEntry()
	emailEntry.SetPlaceHolder("e-mail address")
	emailContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), emailEntry)

	addressEntry := widget.NewEntry()
	addressEntry.SetPlaceHolder("postal address")
	addressContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(280, 36)), addressEntry)

	nearestCityEntry := widget.NewEntry()
	nearestCityEntry.SetPlaceHolder("nearest city")
	nearestCityContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(160, 36)), nearestCityEntry)

	countryCodeEntry := widget.NewEntry()
	countryCodeEntry.SetPlaceHolder("e.g. US")
	countryCodeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(60, 36)), countryCodeEntry)

	// Telescope dropdown
	telescopeOpts := []string{"unstated", "1=Refractor", "2=Newtonian", "3=SCT", "4=Dobsonian", "5=Binoculars", "6=Other", "7=None", "8=eVscope"}
	telescopeOptToVal := map[string]string{
		"unstated": "", "1=Refractor": "1", "2=Newtonian": "2", "3=SCT": "3",
		"4=Dobsonian": "4", "5=Binoculars": "5", "6=Other": "6", "7=None": "7", "8=eVscope": "8",
	}
	telescopeValToOpt := map[string]string{
		"": "unstated", "1": "1=Refractor", "2": "2=Newtonian", "3": "3=SCT",
		"4": "4=Dobsonian", "5": "5=Binoculars", "6": "6=Other", "7": "7=None", "8": "8=eVscope",
	}
	telescopeSelect := widget.NewSelect(telescopeOpts, nil)
	telescopeSelect.SetSelected("unstated")
	telescopeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(220, 36)), telescopeSelect)

	apertureEntry := widget.NewEntry()
	apertureEntry.SetPlaceHolder("e.g. 80mm")
	apertureContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(90, 36)), apertureEntry)

	focalLengthEntry := widget.NewEntry()
	focalLengthEntry.SetPlaceHolder("e.g. 500mm")
	focalLengthContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(90, 36)), focalLengthEntry)

	// Observing method dropdown
	observingMethodOpts := []string{"unspecified", "a=Analogue & digital video", "b=Digital SLR-camera video", "c=Photometer", "d=Sequential images", "e=Drift scan", "f=Visual", "g=Other"}
	observingMethodOptToVal := map[string]string{
		"unspecified": "", "a=Analogue & digital video": "a", "b=Digital SLR-camera video": "b",
		"c=Photometer": "c", "d=Sequential images": "d", "e=Drift scan": "e", "f=Visual": "f", "g=Other": "g",
	}
	observingMethodValToOpt := map[string]string{
		"": "unspecified", "a": "a=Analogue & digital video", "b": "b=Digital SLR-camera video",
		"c": "c=Photometer", "d": "d=Sequential images", "e": "e=Drift scan", "f": "f=Visual", "g": "g=Other",
	}
	observingMethodSelect := widget.NewSelect(observingMethodOpts, nil)
	observingMethodSelect.SetSelected("unspecified")
	observingMethodContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(250, 36)), observingMethodSelect)

	// Time source dropdown
	timeSourceOpts := []string{"unspecified", "a=GPS", "b=NTP", "c=Telephone (fixed or mobile)", "d=Radio time signal", "e=Internal clock of recorder", "f=Stopwatch", "g=Other"}
	timeSourceOptToVal := map[string]string{
		"unspecified": "", "a=GPS": "a", "b=NTP": "b", "c=Telephone (fixed or mobile)": "c",
		"d=Radio time signal": "d", "e=Internal clock of recorder": "e", "f=Stopwatch": "f", "g=Other": "g",
	}
	timeSourceValToOpt := map[string]string{
		"": "unspecified", "a": "a=GPS", "b": "b=NTP", "c": "c=Telephone (fixed or mobile)",
		"d": "d=Radio time signal", "e": "e=Internal clock of recorder", "f": "f=Stopwatch", "g": "g=Other",
	}
	timeSourceSelect := widget.NewSelect(timeSourceOpts, nil)
	timeSourceSelect.SetSelected("unspecified")
	timeSourceContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(250, 36)), timeSourceSelect)

	cameraEntry := widget.NewEntry()
	cameraEntry.SetPlaceHolder("camera model")
	cameraContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 36)), cameraEntry)

	observerEquipSection := container.NewVBox(
		widget.NewLabel("Observer / Equipment:"),
		container.NewHBox(widget.NewLabel("Observer1:"), observer1Container, layout.NewSpacer(), widget.NewLabel("Observer2:"), observer2Container),
		container.NewHBox(widget.NewLabel("Observatory:"), observatoryContainer, layout.NewSpacer(), widget.NewLabel("E-mail:"), emailContainer),
		container.NewHBox(widget.NewLabel("Address:"), addressContainer, layout.NewSpacer(), widget.NewLabel("NearestCity:"), nearestCityContainer, layout.NewSpacer(), widget.NewLabel("CountryCode:"), countryCodeContainer),
		container.NewHBox(widget.NewLabel("Telescope:"), telescopeContainer, layout.NewSpacer(), widget.NewLabel("Aperture:"), apertureContainer, layout.NewSpacer(), widget.NewLabel("FocalLength:"), focalLengthContainer),
		container.NewHBox(widget.NewLabel("ObservingMethod:"), observingMethodContainer, layout.NewSpacer(), widget.NewLabel("TimeSource:"), timeSourceContainer, layout.NewSpacer(), widget.NewLabel("Camera:"), cameraContainer),
	)

	siteLocationSection := container.NewVBox(
		widget.NewLabel("Site Location:"),
		container.NewHBox(widget.NewLabel("Longitude(DMS):"), longDegContainer, widget.NewLabel("\u00b0"), longMinContainer, widget.NewLabel("'"), longSecsContainer, widget.NewLabel("\""), layout.NewSpacer(), widget.NewLabel("Longitude(degrees):"), longDecimalContainer),
		container.NewHBox(widget.NewLabel("Latitude(DMS):"), latDegContainer, widget.NewLabel("\u00b0"), latMinContainer, widget.NewLabel("'"), latSecsContainer, widget.NewLabel("\""), layout.NewSpacer(), widget.NewLabel("Latitude(degrees):"), latDecimalContainer),
		container.NewHBox(widget.NewLabel("Altitude (m):"), altitudeContainer),
	)

	// Flag to suppress enabling writeSiteBtn during programmatic SetText calls
	suppressWriteSiteEnable := false
	var writeSiteBtn *widget.Button

	// --- Load site file button ---
	loadSiteBtn := widget.NewButton("Load site file", func() {
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

			// Clear all entry fields before loading (suppress Write button enable)
			suppressWriteSiteEnable = true
			latDecimalEntry.SetText("")
			longDecimalEntry.SetText("")
			altitudeEntry.SetText("")
			observer1Entry.SetText("")
			observer2Entry.SetText("")
			observatoryEntry.SetText("")
			emailEntry.SetText("")
			addressEntry.SetText("")
			nearestCityEntry.SetText("")
			countryCodeEntry.SetText("")
			telescopeSelect.SetSelected("unstated")
			apertureEntry.SetText("")
			focalLengthEntry.SetText("")
			observingMethodSelect.SetSelected("unspecified")
			timeSourceSelect.SetSelected("unspecified")
			cameraEntry.SetText("")

			// Parse the site file and fill the entry fields
			file, ferr := os.Open(filePath)
			if ferr != nil {
				suppressWriteSiteEnable = false
				dialog.ShowError(fmt.Errorf("error opening site file: %w", ferr), w)
				return
			}
			defer func() {
				if cerr := file.Close(); cerr != nil {
					dialog.ShowError(fmt.Errorf("error closing site file: %w", cerr), w)
				}
			}()

			scanner := bufio.NewScanner(file)
			for scanner.Scan() {
				line := scanner.Text()

				if strings.HasPrefix(line, "latitude_decimal:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "latitude_decimal:"))
					latDecimalEntry.SetText(value)
					if value != "" {
						if lat, err := strconv.ParseFloat(value, 64); err == nil {
							deg, minutes, sec := decimalToDMS(lat)
							vt.SiteLatDegEntry.SetText(deg)
							vt.SiteLatMinEntry.SetText(minutes)
							vt.SiteLatSecsEntry.SetText(sec)
						}
					}
					continue
				}

				if strings.HasPrefix(line, "longitude_decimal:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "longitude_decimal:"))
					longDecimalEntry.SetText(value)
					if value != "" {
						if lon, err := strconv.ParseFloat(value, 64); err == nil {
							deg, minutes, sec := decimalToDMS(lon)
							vt.SiteLongDegEntry.SetText(deg)
							vt.SiteLongMinEntry.SetText(minutes)
							vt.SiteLongSecsEntry.SetText(sec)
						}
					}
					continue
				}

				if strings.HasPrefix(line, "altitude:") {
					value := strings.TrimSpace(strings.TrimPrefix(line, "altitude:"))
					altitudeEntry.SetText(value)
					if value != "" {
						vt.SiteAltitudeEntry.SetText(value)
					}
					continue
				}
				if strings.HasPrefix(line, "observer1:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "observer1:"))
					observer1Entry.SetText(val)
					vt.ObserverNameEntry.SetText(val)
					continue
				}
				if strings.HasPrefix(line, "observer2:") {
					observer2Entry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "observer2:")))
					continue
				}
				if strings.HasPrefix(line, "observatory:") {
					observatoryEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "observatory:")))
					continue
				}
				if strings.HasPrefix(line, "email:") {
					emailEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "email:")))
					continue
				}
				if strings.HasPrefix(line, "address:") {
					addressEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "address:")))
					continue
				}
				if strings.HasPrefix(line, "nearest_city:") {
					nearestCityEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "nearest_city:")))
					continue
				}
				if strings.HasPrefix(line, "country_code:") {
					countryCodeEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "country_code:")))
					continue
				}
				if strings.HasPrefix(line, "telescope:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "telescope:"))
					if opt, ok := telescopeValToOpt[val]; ok {
						telescopeSelect.SetSelected(opt)
					}
					continue
				}
				if strings.HasPrefix(line, "aperture:") {
					apertureEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "aperture:")))
					continue
				}
				if strings.HasPrefix(line, "focal_length:") {
					focalLengthEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "focal_length:")))
					continue
				}
				if strings.HasPrefix(line, "observing_method:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "observing_method:"))
					if opt, ok := observingMethodValToOpt[val]; ok {
						observingMethodSelect.SetSelected(opt)
					}
					continue
				}
				if strings.HasPrefix(line, "time_source:") {
					val := strings.TrimSpace(strings.TrimPrefix(line, "time_source:"))
					if opt, ok := timeSourceValToOpt[val]; ok {
						timeSourceSelect.SetSelected(opt)
					}
					continue
				}
				if strings.HasPrefix(line, "camera:") {
					cameraEntry.SetText(strings.TrimSpace(strings.TrimPrefix(line, "camera:")))
					continue
				}
			}

			if serr := scanner.Err(); serr != nil {
				suppressWriteSiteEnable = false
				dialog.ShowError(fmt.Errorf("error reading site file: %w", serr), w)
				return
			}

			suppressWriteSiteEnable = false
			writeSiteBtn.Disable()
			lastLoadedSitePath = filePath
			logAction(fmt.Sprintf("Site file loaded: %s", filePath))
		}, w)

		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".site"}))
		fileDialog.Resize(fyne.NewSize(800, 600))

		siteDir := filepath.Join(appDir, "SITE-FILES")
		if merr := os.MkdirAll(siteDir, 0755); merr != nil {
			dialog.ShowError(fmt.Errorf("failed to create SITE-FILES directory: %w", merr), w)
			return
		}
		folderURI := storage.NewFileURI(siteDir)
		if listableURI, lerr := storage.ListerForURI(folderURI); lerr == nil {
			fileDialog.SetLocation(listableURI)
		}

		fileDialog.Show()
	})
	loadSiteBtn.Importance = widget.HighImportance

	// --- Write site file button ---
	writeSiteBtn = widget.NewButton("Write site file", func() {
		saveDialog := dialog.NewFileSave(func(writer fyne.URIWriteCloser, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if writer == nil {
				return // User cancelled
			}

			// Enforce .site extension
			originalPath := writer.URI().Path()
			filePath := originalPath
			ext := filepath.Ext(filePath)
			if strings.ToLower(ext) != ".site" {
				filePath = strings.TrimSuffix(filePath, ext) + ".site"
			}

			// Close the writer first so the file handle is released
			if cerr := writer.Close(); cerr != nil {
				dialog.ShowError(fmt.Errorf("failed to close file: %w", cerr), w)
			}

			// Remove the empty file created by the save dialog if the path changed
			if filePath != originalPath {
				if rerr := os.Remove(originalPath); rerr != nil {
					fmt.Printf("Warning: could not remove empty file %s: %v\n", originalPath, rerr)
				}
			}

			var sb strings.Builder
			sb.WriteString("latitude_decimal: " + strings.TrimSpace(latDecimalEntry.Text) + "\n")
			sb.WriteString("longitude_decimal: " + strings.TrimSpace(longDecimalEntry.Text) + "\n")
			sb.WriteString("altitude: " + strings.TrimSpace(altitudeEntry.Text) + "\n")
			sb.WriteString("observer1: " + strings.TrimSpace(observer1Entry.Text) + "\n")
			sb.WriteString("observer2: " + strings.TrimSpace(observer2Entry.Text) + "\n")
			sb.WriteString("observatory: " + strings.TrimSpace(observatoryEntry.Text) + "\n")
			sb.WriteString("email: " + strings.TrimSpace(emailEntry.Text) + "\n")
			sb.WriteString("address: " + strings.TrimSpace(addressEntry.Text) + "\n")
			sb.WriteString("nearest_city: " + strings.TrimSpace(nearestCityEntry.Text) + "\n")
			sb.WriteString("country_code: " + strings.TrimSpace(countryCodeEntry.Text) + "\n")
			sb.WriteString("telescope: " + telescopeOptToVal[telescopeSelect.Selected] + "\n")
			sb.WriteString("aperture: " + strings.TrimSpace(apertureEntry.Text) + "\n")
			sb.WriteString("focal_length: " + strings.TrimSpace(focalLengthEntry.Text) + "\n")
			sb.WriteString("observing_method: " + observingMethodOptToVal[observingMethodSelect.Selected] + "\n")
			sb.WriteString("time_source: " + timeSourceOptToVal[timeSourceSelect.Selected] + "\n")
			sb.WriteString("camera: " + strings.TrimSpace(cameraEntry.Text) + "\n")
			siteContent := sb.String()

			if werr := os.WriteFile(filePath, []byte(siteContent), 0644); werr != nil {
				dialog.ShowError(fmt.Errorf("failed to write site file: %w", werr), w)
				return
			}
			logAction(fmt.Sprintf("Site file written: %s", filePath))
		}, w)

		saveDialog.SetFileName("")
		saveDialog.Resize(fyne.NewSize(800, 600))

		siteDir := filepath.Join(appDir, "SITE-FILES")
		if merr := os.MkdirAll(siteDir, 0755); merr != nil {
			dialog.ShowError(fmt.Errorf("failed to create SITE-FILES directory: %w", merr), w)
			return
		}
		folderURI := storage.NewFileURI(siteDir)
		if listableURI, lerr := storage.ListerForURI(folderURI); lerr == nil {
			saveDialog.SetLocation(listableURI)
		}

		saveDialog.Show()
	})
	writeSiteBtn.Importance = widget.HighImportance
	writeSiteBtn.Disable()

	// Enable writeSiteBtn only on user-driven edits (not programmatic SetText).
	enableWriteSite := func(_ string) {
		if !suppressWriteSiteEnable && writeSiteBtn.Disabled() {
			writeSiteBtn.Enable()
		}
	}
	for _, e := range []*widget.Entry{
		latDecimalEntry, longDecimalEntry, altitudeEntry,
		observer1Entry, observer2Entry, observatoryEntry,
		emailEntry, addressEntry, nearestCityEntry, countryCodeEntry,
		apertureEntry, focalLengthEntry, cameraEntry,
	} {
		prev := e.OnChanged
		if prev != nil {
			origPrev := prev
			e.OnChanged = func(s string) {
				origPrev(s)
				enableWriteSite(s)
			}
		} else {
			e.OnChanged = enableWriteSite
		}
	}

	// --- Calculate observer dX dY button ---
	var occelmntDialog dialog.Dialog
	calcDxDyBtn := widget.NewButton("Create Occultation Parameter file", func() {
		xmlContent := strings.TrimSpace(pasteEntry.Text)
		if xmlContent == "" {
			dialog.ShowError(fmt.Errorf("please paste occelmnt XML content first"), w)
			return
		}
		lastLoadedOccelmntXml = xmlContent
		fyne.CurrentApp().Preferences().SetString("lastLoadedOccelmntXml", lastLoadedOccelmntXml)
		vt.FillStarFromOccelmntXml(xmlContent)
		lat, err := strconv.ParseFloat(strings.TrimSpace(latDecimalEntry.Text), 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid latitude (degrees): %v", err), w)
			return
		}
		lon, err := strconv.ParseFloat(strings.TrimSpace(longDecimalEntry.Text), 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid longitude (degrees): %v", err), w)
			return
		}
		alt := 0.0
		if strings.TrimSpace(altitudeEntry.Text) != "" {
			alt, err = strconv.ParseFloat(strings.TrimSpace(altitudeEntry.Text), 64)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid altitude: %v", err), w)
				return
			}
		}

		vx, vy, calcErr := ShadowVelocityFromOWCEventKmPerSec(xmlContent, lat, lon, alt, 0.0, 0.0, 0.0)
		if calcErr != nil {
			dialog.ShowError(fmt.Errorf("calculation error: %v", calcErr), w)
			return
		}

		geocT0, obsT0, corrSecs, t0Err := ObserverT0CorrectionFromOWC(xmlContent, lat, lon, alt, 0.0, 0.0, 0.0)

		// Persist the observer location so the SODIS fill can use it even after app restart.
		if t0Err == nil {
			lastObserverLatDeg = lat
			lastObserverLonDeg = lon
			lastObserverAltMeters = alt
			lastObserverLocationSet = true
			p := fyne.CurrentApp().Preferences()
			p.SetFloat("lastObserverLatDeg", lat)
			p.SetFloat("lastObserverLonDeg", lon)
			p.SetFloat("lastObserverAltMeters", alt)
			p.SetBool("lastObserverLocationSet", true)
		}

		// Parse <Object> or <object> for distance_au (index 4) and body diameter (index 3)
		var occ Occultations
		if xmlErr := xml.Unmarshal([]byte(xmlContent), &occ); xmlErr != nil {
			dialog.ShowError(fmt.Errorf("failed to parse XML for Object: %v", xmlErr), w)
			return
		}
		distanceAu := 0.0
		bodyDiamKm := 0.0
		titleStr := ""
		if len(occ.Events) > 0 {
			objectText := occ.Events[0].Object
			if objectText == "" {
				objectText = occ.Events[0].ObjectLC
			}
			if objectText != "" {
				objFields := splitCSVPreserveEmpty(objectText)
				if len(objFields) > 4 {
					if d, derr := strconv.ParseFloat(objFields[4], 64); derr == nil {
						distanceAu = d
					}
				}
				if len(objFields) > 3 {
					if d, derr := strconv.ParseFloat(objFields[3], 64); derr == nil {
						bodyDiamKm = d
					}
				}
				if len(objFields) > 1 {
					titleStr = fmt.Sprintf("(%s) %s", objFields[0], objFields[1])
				}
			}
		}

		// Build a parameters struct with the computed values
		params := OccultationParameters{
			Title:                          titleStr,
			WindowSizePixels:               600,
			FundamentalPlaneWidthKm:        math.Ceil(3 * bodyDiamKm),
			FundamentalPlaneWidthNumPoints: 2000,
			DXKmPerSec:                     vx,
			DYKmPerSec:                     vy,
			DistanceAu:                     distanceAu,
			ObservationWavelengthNm:        550,
			MainBody: EllipseParams{
				MajorAxisKm: bodyDiamKm,
				MinorAxisKm: bodyDiamKm,
			},
			OccelmntXml: xmlContent,
		}

		// Close this dialog and open the parameters dialog pre-populated from the computed values
		if occelmntDialog != nil {
			occelmntDialog.Hide()
		}
		obsDir := ""
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir = filepath.Dir(loadedLightCurveData.SourceFilePath)
		}
		showOccultationParametersDialog(w, false, &params, obsDir)

		// Calculate and display t0 correction and Fresnel scale
		wavelength := float64(params.ObservationWavelengthNm)
		if wavelength == 0 {
			wavelength = 550
		}
		infoMsg := ""
		if t0Err == nil {
			infoMsg += fmt.Sprintf(
				"Geocentric t0:        %s UTC\nObserver event time:  %s UTC\nCorrection:           %+.3f sec",
				geocT0.Format("15:04:05.000"),
				obsT0.Format("15:04:05.000"),
				corrSecs)
		}
		if params.DistanceAu > 0 {
			fresnelKm := FresnelScale(wavelength, params.DistanceAu)
			fresnelM := fresnelKm * 1000
			if infoMsg != "" {
				infoMsg += "\n\n"
			}
			infoMsg += fmt.Sprintf("Fresnel scale: %.4f km (%.1f meters)\n\nWavelength: %.0f nm\nDistance: %.4f AU",
				fresnelKm, fresnelM, wavelength, params.DistanceAu)
			if params.FundamentalPlaneWidthKm > 0 && params.FundamentalPlaneWidthNumPoints > 0 {
				samplesPerFresnel := int(float64(params.FundamentalPlaneWidthNumPoints) * fresnelKm / params.FundamentalPlaneWidthKm)
				infoMsg += fmt.Sprintf("\n\nSamples per Fresnel scale: %d", samplesPerFresnel)
			}
			infoMsg += "\n\nIf your observation exhibits diffraction effects (sloped D and R transitions), " +
				"you will need the Samples per Fresnel scale to be 5 or 6 at a minimum. " +
				"See the Help Topics entry titled 'Fresnel scale resolution' for more information."
		}
		if infoMsg != "" {
			dialog.ShowInformation("Event Prediction & Fresnel Scale", infoMsg, w)
		}
	})
	calcDxDyBtn.Importance = widget.HighImportance

	// --- Assemble bottom sections ---
	bottomSection := container.NewVBox(
		widget.NewSeparator(),
		observerEquipSection,
		widget.NewSeparator(),
		siteLocationSection,
		widget.NewSeparator(),
		container.NewHBox(writeSiteBtn),
		widget.NewSeparator(),
		widget.NewButton("Cancel", func() {
			if occelmntDialog != nil {
				occelmntDialog.Hide()
			}
		}),
	)

	loadOccelmntBtn.Importance = widget.HighImportance
	pasteSection := container.NewBorder(container.NewHBox(loadOccelmntBtn), container.NewHBox(loadSiteBtn, calcDxDyBtn, layout.NewSpacer()), nil, nil, scrollable)
	content := container.NewBorder(nil, bottomSection, nil, nil, pasteSection)

	occelmntDialog = dialog.NewCustomWithoutButtons("Process OWC Occelmnt", content, w)
	occelmntDialog.Resize(fyne.NewSize(840, 1000))
	occelmntDialog.Show()

	// Focus the entry so the user can immediately paste
	w.Canvas().Focus(pasteEntry)
}

func main() {
	// Declared early so all closures throughout main() can reference it;
	// initialized below after the tab structure is ready.
	var vizierTab *VizieRTab

	// Determine the directory containing the executable so that relative file
	// references (diffraction images, IOTAdiffraction.exe, etc.) resolve correctly
	// regardless of how the program is launched (e.g., from an IDE).
	if exePath, err := os.Executable(); err == nil {
		appDir = filepath.Dir(exePath)
	} else {
		appDir, _ = os.Getwd()
	}

	a := app.NewWithID("com.gopyote.app")
	w := a.NewWindow("GoPyOTE Version: " + Version)
	w.SetMaster() // Closing this window will quit the app and close all other windows

	// Load saved window geometry
	prefs := a.Preferences()

	// Apply persisted dark mode preference
	if prefs.BoolWithFallback("darkMode", false) {
		a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantDark})
	} else {
		a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantLight})
	}

	// Apply persisted gray plot background preference
	grayPlotBackground = prefs.BoolWithFallback("grayPlotBackground", false)
	lightcurve.GrayPlotBackground = grayPlotBackground

	// Initialize preferences on the first run to avoid EOF errors
	if prefs.Int("initialized") == 0 {
		prefs.SetInt("initialized", 1)
		prefs.SetInt("windowX", -1)
		prefs.SetInt("windowY", -1)
		prefs.SetInt("windowW", 1000)
		prefs.SetInt("windowH", 600)
		prefs.SetFloat("splitOffset", 0.6)
	}

	// Load the last used parameters path from preferences for startup display
	lastLoadedParamsPath = prefs.StringWithFallback("lastLoadedParamsPath", "")
	// Restore last loaded occelmnt XML text
	lastLoadedOccelmntXml = prefs.StringWithFallback("lastLoadedOccelmntXml", "")
	// Restore observer GPS location for SODIS fill
	lastObserverLocationSet = prefs.BoolWithFallback("lastObserverLocationSet", false)
	if lastObserverLocationSet {
		lastObserverLatDeg = prefs.FloatWithFallback("lastObserverLatDeg", 0.0)
		lastObserverLonDeg = prefs.FloatWithFallback("lastObserverLonDeg", 0.0)
		lastObserverAltMeters = prefs.FloatWithFallback("lastObserverAltMeters", 0.0)
	}
	lastDiffractionParamsPath = prefs.StringWithFallback("lastDiffractionParamsPath", "")
	lastDiffractionTitle = prefs.StringWithFallback("lastDiffractionTitle", "")
	// Backfill the title from the parameters file if a path exists but title was never saved
	if lastDiffractionParamsPath != "" && lastDiffractionTitle == "" {
		if f, err := os.Open(lastDiffractionParamsPath); err == nil {
			if p, err := parseOccultationParameters(f); err == nil && p.Title != "" {
				lastDiffractionTitle = p.Title
				prefs.SetString("lastDiffractionTitle", lastDiffractionTitle)
			}
			if err := f.Close(); err != nil {
				fmt.Printf("Warning: failed to close parameters file: %v\n", err)
			}
		}
	}

	savedX := int32(prefs.IntWithFallback("windowX", -1))
	savedY := int32(prefs.IntWithFallback("windowY", -1))
	savedW := int32(prefs.IntWithFallback("windowW", 1000))
	savedH := int32(prefs.IntWithFallback("windowH", 600))
	firstRun := savedX == -1 && savedY == -1

	// Create a menu
	helpMenu := fyne.NewMenu("Help Topics",
		fyne.NewMenuItem("Block Integration", func() {
			content, err := blockIntegrationMarkdown.ReadFile("help_markdown/blockIntegration.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load blockIntegration.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Block Integration", string(content), &blockIntegrationMarkdown, w)
		}),
		fyne.NewMenuItem("Smoothing and Normalization", func() {
			content, err := smoothingMarkdown.ReadFile("help_markdown/smoothingAndNormalization.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load smoothingAndNormalization.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Smoothing and Normalization", string(content), &smoothingMarkdown, w)
		}),
		fyne.NewMenuItem("Dropped frames and OCR issues", func() {
			content, err := timestampAnalysisMarkdown.ReadFile("help_markdown/timestampAnalysis.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load timestampAnalysis_old.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Dropped frames and OCR issues", string(content), &timestampAnalysisMarkdown, w)
		}),
		fyne.NewMenuItem("VizieR export", func() {
			content, err := vizierExportMarkdown.ReadFile("help_markdown/vizierMarkdown.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load vizierMarkdown.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("VizieR export", string(content), &vizierExportMarkdown, w)
		}),
		fyne.NewMenuItem("Fit explanation", func() {
			content, err := fitExplanationMarkdown.ReadFile("help_markdown/fitMarkdown.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load fitMarkdown.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Fit explanation", string(content), &fitExplanationMarkdown, w)
		}),
		fyne.NewMenuItem("Process OWC occelmnt.xml", func() {
			content, err := occelmntButtonExplanation.ReadFile("help_markdown/occelmntOWC.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load occelmntOWC.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Process OWC occelmnt.xml", string(content), &occelmntButtonExplanation, w)
		}),
		fyne.NewMenuItem("Edit Occ Params", func() {
			content, err := editOccParamsExplanation.ReadFile("help_markdown/editOccParams.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load editOccParams.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Edit Occ Params", string(content), &editOccParamsExplanation, w)
		}),
		fyne.NewMenuItem("Run IOTAdiffraction", func() {
			content, err := runIOTAdiffractionExplanation.ReadFile("help_markdown/runIOTAdiffraction.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load runIOTAdiffraction.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Run IOTAdiffraction", string(content), &runIOTAdiffractionExplanation, w)
		}),
		fyne.NewMenuItem("Fresnel scale resolution", func() {
			content, err := fresnelScaleResolutionMarkdown.ReadFile("help_markdown/fresnelScaleResolution.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load fresnelScaleResolution.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Fresnel scale resolution", string(content), &fresnelScaleResolutionMarkdown, w)
		}),
		fyne.NewMenuItem("Monte Carlo sigma estimation", func() {
			content, err := monteCarloExplanation.ReadFile("help_markdown/edgeTimeSigmaExplanation.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load edgeTimeSigmaExplanation.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Monte Carlo sigma estimation", string(content), &monteCarloExplanation, w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About GoPyOTE", func() {
			content, err := aboutMarkdown.ReadFile("help_markdown/about.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load about.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("About GoPyOTE", string(content), &aboutMarkdown, w)
		}),
	)
	mainMenu := fyne.NewMainMenu(helpMenu)
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
	anyNameCheck := widget.NewCheck("any name (use for Tangra)", func(checked bool) {
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

	// Track all tab background rectangles with their light/dark colors
	type tabBgEntry struct {
		rect       *canvas.Rectangle
		lightColor color.RGBA
		darkColor  color.RGBA
	}
	var tabBgs []tabBgEntry
	makeTabBg := func(light, dark color.RGBA) *canvas.Rectangle {
		rect := canvas.NewRectangle(light)
		tabBgs = append(tabBgs, tabBgEntry{rect, light, dark})
		return rect
	}
	applyTabBgTheme := func(isDark bool) {
		for _, entry := range tabBgs {
			if isDark {
				entry.rect.FillColor = entry.darkColor
			} else {
				entry.rect.FillColor = entry.lightColor
			}
			entry.rect.Refresh()
		}
	}

	darkModeCheck := widget.NewCheck("Dark mode", func(checked bool) {
		if checked {
			a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantDark})
		} else {
			a.Settings().SetTheme(&ForcedVariantTheme{Base: theme.DefaultTheme(), Variant: theme.VariantLight})
		}
		applyTabBgTheme(checked)
		prefs.SetBool("darkMode", checked)
	})
	darkModeCheck.Checked = prefs.BoolWithFallback("darkMode", false)

	grayBgCheck := widget.NewCheck("Gray plot backgrounds", func(checked bool) {
		grayPlotBackground = checked
		lightcurve.GrayPlotBackground = checked
		prefs.SetBool("grayPlotBackground", checked)
	})
	grayBgCheck.Checked = prefs.BoolWithFallback("grayPlotBackground", false)

	// Checkbox for timestamp tick format (callback set later after lightCurvePlot is created)
	timestampTicksCheck := widget.NewCheck("Use timestamp format to display time value", nil)
	timestampTicksCheck.Checked = true

	showIOTAPlotsCheck := widget.NewCheck("Show plots from IOTAdiffraction", func(checked bool) {
		prefs.SetBool("showIOTAPlots", checked)
	})
	showIOTAPlotsCheck.Checked = prefs.BoolWithFallback("showIOTAPlots", true)

	obsHomeDirEntry := widget.NewEntry()
	obsHomeDirEntry.SetPlaceHolder("Path to your observations folder...")
	obsHomeDirEntry.SetText(prefs.StringWithFallback("obsHomeDir", ""))
	obsHomeDirEntry.OnChanged = func(s string) {
		prefs.SetString("obsHomeDir", s)
	}
	obsHomeDirBrowseBtn := widget.NewButton("Browse", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, w)
				return
			}
			if uri == nil {
				return
			}
			obsHomeDirEntry.SetText(uri.Path())
		}, w)
	})
	obsHomeDirBrowseBtn.Importance = widget.HighImportance
	obsHomeDirBox := container.NewVBox(
		widget.NewLabel("Observation home directory:"),
		container.NewBorder(nil, nil, nil, obsHomeDirBrowseBtn, obsHomeDirEntry),
	)

	tab2Bg := makeTabBg(color.RGBA{R: 200, G: 200, B: 230, A: 255}, color.RGBA{R: 50, G: 50, B: 80, A: 255})
	tab2Content := container.NewStack(tab2Bg, container.NewPadded(container.NewVBox(prefixCheckboxes, widget.NewSeparator(), darkModeCheck, grayBgCheck, timestampTicksCheck, showIOTAPlotsCheck, widget.NewSeparator(), obsHomeDirBox)))
	tab2 := container.NewTabItem("Settings", tab2Content)

	// Create the plot area with an interactive light curve (before Tab 3 so it can be referenced)
	plotStatusLabel := widget.NewLabel("Click on a point to see details")
	plotStatusLabel.Wrapping = fyne.TextWrapWord

	// Frame number range entry boxes (created here so they can be in the plot area bottom)
	startFrameEntry := NewFocusLossEntry()
	startFrameEntry.SetPlaceHolder("Start Frame")
	endFrameEntry := NewFocusLossEntry()
	endFrameEntry.SetPlaceHolder("End Frame")
	startFrameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), startFrameEntry)
	endFrameContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), endFrameEntry)

	// Trim entry boxes
	trimStartEntry := NewFocusLossEntry()
	trimStartEntry.SetPlaceHolder("Trim start")
	trimEndEntry := NewFocusLossEntry()
	trimEndEntry.SetPlaceHolder("Trim end")
	trimStartContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), trimStartEntry)
	trimEndContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(120, 36)), trimEndEntry)

	// Use a fixed-width label container so labels align vertically
	labelWidth := fyne.NewSize(80, 36)

	setTrimBtn := widget.NewButton("Set\ntrim", nil)
	setTrimBtn.Importance = widget.HighImportance
	trimPerformed := false

	trimStack := container.NewVBox(
		container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("Trim start:")), trimStartContainer),
		container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("Trim end:")), trimEndContainer),
	)
	applyTrimBtn := widget.NewButton("Show current trim", nil)
	applyTrimBtn.Importance = widget.HighImportance
	showAllBtn := widget.NewButton("Show all", nil)
	showAllBtn.Importance = widget.HighImportance
	trimBtnWidth := fyne.NewSize(150, 36)

	trimStackWithBtn := container.NewHBox(
		trimStack,
		container.New(layout.NewGridWrapLayout(fyne.NewSize(76, 76)), setTrimBtn),
		container.NewVBox(
			container.New(layout.NewGridWrapLayout(trimBtnWidth), applyTrimBtn),
			container.New(layout.NewGridWrapLayout(trimBtnWidth), showAllBtn),
		),
	)

	frameRangeRow := container.NewHBox(
		container.NewVBox(
			container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("Start frame:")), startFrameContainer),
			container.NewHBox(container.New(layout.NewGridWrapLayout(labelWidth), widget.NewLabel("End frame:")), endFrameContainer),
		),
		trimStackWithBtn,
	)

	// Track the current x-axis label for click callback
	currentXAxisLabel := "Time"

	var onFitTab bool // Track whether the Fit tab is active

	// Create the plot with an empty series (will be populated when CSV is loaded)
	var lightCurvePlot *LightCurvePlot
	lightCurvePlot = NewLightCurvePlot(nil, func(point PlotPoint) {
		if point.Series < 0 || point.Series >= len(lightCurvePlot.series) {
			return
		}
		seriesName := lightCurvePlot.series[point.Series].Name

		// Get frame number from loaded data
		frameNum := point.Index // fallback to index
		if loadedLightCurveData != nil && point.Index >= 0 && point.Index < len(loadedLightCurveData.FrameNumbers) {
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

	// Set the timestamp ticks callback now that lightCurvePlot and updateRangeEntries exist
	timestampTicksCheck.OnChanged = func(checked bool) {
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
	}
	lightCurvePlot.SetUseTimestampTicks(true)

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
		widget.NewButton("Clear marked points", func() {
			// Clear selected point 1
			lightCurvePlot.selectedSeries = -1
			lightCurvePlot.selectedIndex = -1
			lightCurvePlot.selectedPointDataIndex = -1
			lightCurvePlot.selectedSeriesName = ""
			lightCurvePlot.SelectedPoint1Valid = false
			lightCurvePlot.SelectedPoint1Frame = 0
			lightCurvePlot.SelectedPoint1Value = 0

			// Clear selected point 2
			lightCurvePlot.selectedSeries2 = -1
			lightCurvePlot.selectedIndex2 = -1
			lightCurvePlot.selectedPointDataIndex2 = -1
			lightCurvePlot.selectedSeriesName2 = ""
			lightCurvePlot.SelectedPoint2Valid = false
			lightCurvePlot.SelectedPoint2Frame = 0
			lightCurvePlot.SelectedPoint2Value = 0

			// Clear all selected pairs
			lightCurvePlot.SelectedPairs = nil

			lightCurvePlot.Refresh()
			plotStatusLabel.SetText("Click on a point to see details")
			logAction("Cleared all marked points")
		}),
	)

	// Bottom section with frame range controls on the left and status label on the right
	plotBottomRow := container.NewBorder(
		nil,             // top
		nil,             // bottom
		frameRangeRow,   // left
		nil,             // right
		plotStatusLabel, // center (takes remaining space)
	)

	plotCenter := container.NewStack(lightCurvePlot)

	plotArea := container.NewBorder(
		rangeControls, // top
		plotBottomRow, // bottom
		nil,           // left
		nil,           // right
		plotCenter,    // center
	)

	// Tab 3: Data - Light curve list with click to toggle on/off plot
	tab3Bg := makeTabBg(color.RGBA{R: 230, G: 220, B: 200, A: 255}, color.RGBA{R: 80, G: 70, B: 50, A: 255})

	// Create a list to display light curve column names
	var lightCurveListData []string       // Will be populated when CSV is loaded (filtered by prefixes)
	var listIndexToColumnIndex []int      // Maps list index to actual column index in data
	displayedCurves := make(map[int]bool) // Track which curves are currently displayed (uses actual column indices)
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

	// Savitzky-Golay smoothed series (nil if no smoothing has been applied)
	var smoothedSeries *PlotSeries

	// Theoretical lightcurve series overlaid after a Monte Carlo run (nil if not set)
	var theorySeries *PlotSeries

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
					X:            xVal,
					Y:            val,
					Index:        i,
					Series:       len(allSeries),
					Interpolated: isInterpolatedIndex(i),
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

		// Add smoothed series if available
		if smoothedSeries != nil {
			// Rebuild smoothed series points with correct X values (frame numbers or timestamps)
			var smoothPoints []PlotPoint
			for _, pt := range smoothedSeries.Points {
				// Filter by frame range
				frameNum := loadedLightCurveData.FrameNumbers[pt.Index]
				if frameRangeStart > 0 && frameNum < frameRangeStart {
					continue
				}
				if frameRangeEnd > 0 && frameNum > frameRangeEnd {
					continue
				}
				xVal := loadedLightCurveData.TimeValues[pt.Index]
				if useFrameNumbers {
					xVal = frameNum
				}
				smoothPoints = append(smoothPoints, PlotPoint{
					X:            xVal,
					Y:            pt.Y,
					Index:        pt.Index,
					Series:       len(allSeries),
					Interpolated: isInterpolatedIndex(pt.Index),
				})
			}
			if len(smoothPoints) > 0 {
				allSeries = append(allSeries, PlotSeries{
					Points: smoothPoints,
					Color:  smoothedSeries.Color,
					Name:   smoothedSeries.Name,
				})
				displayedNames = append(displayedNames, smoothedSeries.Name)
			}
		}

		// Add theoretical lightcurve series if available (from the last Monte Carlo run)
		// Filter to only include points within the X range of the displayed light curve data.
		if theorySeries != nil && len(allSeries) > 0 {
			// Determine the min and max X of all currently displayed light curve points
			xMin := math.Inf(1)
			xMax := math.Inf(-1)
			for _, s := range allSeries {
				for _, pt := range s.Points {
					if pt.X < xMin {
						xMin = pt.X
					}
					if pt.X > xMax {
						xMax = pt.X
					}
				}
			}
			var filteredPts []PlotPoint
			for _, pt := range theorySeries.Points {
				if pt.X >= xMin && pt.X <= xMax {
					filteredPts = append(filteredPts, pt)
				}
			}
			if len(filteredPts) > 0 {
				allSeries = append(allSeries, PlotSeries{
					Points:   filteredPts,
					Color:    theorySeries.Color,
					Name:     theorySeries.Name,
					LineOnly: theorySeries.LineOnly,
				})
			}
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
			label := widget.NewLabel("Light Curve Name")
			return label
		},
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			label := obj.(*widget.Label)

			name := lightCurveListData[id]
			label.SetText(name)

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
		lightCurveList.UnselectAll()
		lightCurveList.Refresh()

		// Automatically display the first light curve if available
		if len(listIndexToColumnIndex) > 0 {
			toggleLightCurve(listIndexToColumnIndex[0])
		} else {
			rebuildPlot() // Rebuild with an empty plot if no curves match
		}

		logAction("Filter settings changed, refreshed light curve list")
	}

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
			// Save Y bounds before rebuild to preserve Y axis scaling
			savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
			rebuildPlot()
			lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
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
			// Save Y bounds before rebuild to preserve Y axis scaling
			savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
			rebuildPlot()
			lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		}
	}

	// Set the Set trim button callback now that lightCurvePlot, frameRangeStart/End, and rebuildPlot exist
	setTrimBtn.OnTapped = func() {
		var frame1, frame2 float64
		pointsSelected := false

		if lightCurvePlot.MultiPairSelectMode {
			// On the Fit page, two clicks save a PointPair rather than setting
			// SelectedPoint1/2Valid. Use the most recently saved pair for trim.
			if !lightCurvePlot.SelectedPoint1Valid && len(lightCurvePlot.SelectedPairs) > 0 {
				lastPair := lightCurvePlot.SelectedPairs[len(lightCurvePlot.SelectedPairs)-1]
				idx1 := lastPair.Point1DataIdx
				idx2 := lastPair.Point2DataIdx
				lightCurvePlot.SelectedPairs = lightCurvePlot.SelectedPairs[:len(lightCurvePlot.SelectedPairs)-1]
				if loadedLightCurveData != nil && idx1 < len(loadedLightCurveData.FrameNumbers) {
					frame1 = loadedLightCurveData.FrameNumbers[idx1]
				} else {
					frame1 = float64(idx1)
				}
				if loadedLightCurveData != nil && idx2 < len(loadedLightCurveData.FrameNumbers) {
					frame2 = loadedLightCurveData.FrameNumbers[idx2]
				} else {
					frame2 = float64(idx2)
				}
				pointsSelected = true
			}
		} else {
			if lightCurvePlot.SelectedPoint1Valid && lightCurvePlot.SelectedPoint2Valid {
				idx1 := lightCurvePlot.selectedPointDataIndex
				idx2 := lightCurvePlot.selectedPointDataIndex2
				if loadedLightCurveData != nil && idx1 < len(loadedLightCurveData.FrameNumbers) {
					frame1 = loadedLightCurveData.FrameNumbers[idx1]
				} else {
					frame1 = float64(idx1)
				}
				if loadedLightCurveData != nil && idx2 < len(loadedLightCurveData.FrameNumbers) {
					frame2 = loadedLightCurveData.FrameNumbers[idx2]
				} else {
					frame2 = float64(idx2)
				}
				pointsSelected = true
			}
		}

		// Fall back to trim entry box values if no points were selected
		if !pointsSelected {
			trimStartText := strings.TrimSpace(trimStartEntry.Text)
			trimEndText := strings.TrimSpace(trimEndEntry.Text)
			if trimStartText == "" || trimEndText == "" {
				dialog.ShowError(fmt.Errorf("select two points on the plot or enter values in the Trim start and Trim end boxes"), w)
				return
			}
			var err error
			frame1, err = strconv.ParseFloat(trimStartText, 64)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid Trim start value: %s", trimStartText), w)
				return
			}
			frame2, err = strconv.ParseFloat(trimEndText, 64)
			if err != nil {
				dialog.ShowError(fmt.Errorf("invalid Trim end value: %s", trimEndText), w)
				return
			}
		}

		// Put the smaller frame in Trim start, larger in the Trim end
		if frame1 > frame2 {
			frame1, frame2 = frame2, frame1
		}
		trimStartEntry.SetText(fmt.Sprintf("%.0f", frame1))
		trimEndEntry.SetText(fmt.Sprintf("%.0f", frame2))
		startFrameEntry.SetText(fmt.Sprintf("%.0f", frame1))
		endFrameEntry.SetText(fmt.Sprintf("%.0f", frame2))

		// Update the plot display range
		frameRangeStart = frame1
		frameRangeEnd = frame2
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

		// Clear selected points
		lightCurvePlot.selectedSeries = -1
		lightCurvePlot.selectedIndex = -1
		lightCurvePlot.selectedPointDataIndex = -1
		lightCurvePlot.selectedSeriesName = ""
		lightCurvePlot.SelectedPoint1Valid = false
		lightCurvePlot.SelectedPoint1Frame = 0
		lightCurvePlot.SelectedPoint1Value = 0
		lightCurvePlot.selectedSeries2 = -1
		lightCurvePlot.selectedIndex2 = -1
		lightCurvePlot.selectedPointDataIndex2 = -1
		lightCurvePlot.selectedSeriesName2 = ""
		lightCurvePlot.SelectedPoint2Valid = false
		lightCurvePlot.SelectedPoint2Frame = 0
		lightCurvePlot.SelectedPoint2Value = 0
		lightCurvePlot.Refresh()
		plotStatusLabel.SetText("Click on a point to see details")

		logAction(fmt.Sprintf("Set trim range: %.0f to %.0f", frame1, frame2))
		trimPerformed = true
		setTrimBtn.Importance = widget.WarningImportance
		setTrimBtn.Refresh()
	}

	// Set the Apply trim button callback
	applyTrimBtn.OnTapped = func() {
		trimStartText := strings.TrimSpace(trimStartEntry.Text)
		trimEndText := strings.TrimSpace(trimEndEntry.Text)
		if trimStartText == "" || trimEndText == "" {
			dialog.ShowError(fmt.Errorf("trim start and trim end values must be set first"), w)
			return
		}
		startVal, err := strconv.ParseFloat(trimStartText, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid Trim start value: %v", err), w)
			return
		}
		endVal, err := strconv.ParseFloat(trimEndText, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid Trim end value: %v", err), w)
			return
		}
		frameRangeStart = startVal
		frameRangeEnd = endVal
		startFrameEntry.SetText(fmt.Sprintf("%.0f", startVal))
		endFrameEntry.SetText(fmt.Sprintf("%.0f", endVal))
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		logAction(fmt.Sprintf("Applied trim: %.0f to %.0f", startVal, endVal))
	}

	// Set the Show all button callback
	showAllBtn.OnTapped = func() {
		if minFrameNum == 0 && maxFrameNum == 0 {
			return
		}
		frameRangeStart = minFrameNum
		frameRangeEnd = maxFrameNum
		startFrameEntry.SetText(fmt.Sprintf("%.0f", minFrameNum))
		endFrameEntry.SetText(fmt.Sprintf("%.0f", maxFrameNum))
		rebuildPlot()
		logAction("Show all: reset frame range to full extent")
	}

	// Right-click on the plot shows all
	lightCurvePlot.SetOnSecondaryTapped(func() {
		showAllBtn.OnTapped()
	})

	// Set the occultation title on the main plot from the last diffraction run
	lightCurvePlot.occultationTitle = lastDiffractionTitle

	// Set up a warning callback for the plot
	lightCurvePlot.SetOnWarning(func(message string) {
		dialog.ShowInformation("Warning", message, w)
	})

	// Set up scroll wheel zoom on the plot with a debounced re-draw
	var scrollDebounceTimer *time.Timer
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

		// Track if the mouse is at/beyond the edges of the plot area
		// Use a small threshold (5%) to make it easier to anchor at edges
		mouseLeftOfPlot := relX <= 0.05
		mouseRightOfPlot := relX >= 0.95

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

		var newStart, newEnd float64

		if mouseLeftOfPlot {
			// Mouse is to the left of the plot - anchor at the start, only adjust the end
			newStart = frameRangeStart
			newEnd = frameRangeStart + newRange
			// Clamp end only, keep the start anchored
			if newEnd > maxFrameNum {
				newEnd = maxFrameNum
			}
		} else if mouseRightOfPlot {
			// Mouse is to the right of the plot - anchor at the end, only adjust start
			newEnd = frameRangeEnd
			newStart = frameRangeEnd - newRange
			// Clamp start only, keep the end anchored
			if newStart < minFrameNum {
				newStart = minFrameNum
			}
		} else {
			// Mouse is within the plot - keep frameUnderCursor at the same relative position
			newStart = frameUnderCursor - relX*newRange
			newEnd = frameUnderCursor + (1-relX)*newRange

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
		}

		// Update frame range and UI
		if newStart != frameRangeStart || newEnd != frameRangeEnd {
			frameRangeStart = newStart
			frameRangeEnd = newEnd
			startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
			endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))

			// Debounce: reset the timer on each scroll event so we only
			// rebuild the plot once the scroll wheel has stopped.
			if scrollDebounceTimer != nil {
				scrollDebounceTimer.Stop()
			}
			scrollDebounceTimer = time.AfterFunc(150*time.Millisecond, func() {
				fyne.Do(func() {
					savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
					rebuildPlot()
					lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
				})
			})
		}
	})

	// Create VizieR tab early so it can be populated from RAVF headers during a file load
	vizierTab = NewVizieRTab()
	// Register vizier tab background for dark mode toggling
	tabBgs = append(tabBgs, tabBgEntry{vizierTab.TabBg, color.RGBA{R: 210, G: 220, B: 210, A: 255}, color.RGBA{R: 60, G: 70, B: 60, A: 255}})
	// Pre-fill asteroid number and name from the persisted diffraction title (e.g. "(2731) Cucula")
	if strings.HasPrefix(lastDiffractionTitle, "(") {
		if end := strings.Index(lastDiffractionTitle, ")"); end > 0 {
			if num := strings.TrimSpace(lastDiffractionTitle[1:end]); num != "" {
				vizierTab.AsteroidNumberEntry.SetText(num)
			}
			if name := strings.TrimSpace(lastDiffractionTitle[end+1:]); name != "" {
				vizierTab.AsteroidNameEntry.SetText(name)
			}
		}
	}
	// Pre-fill UCAC4 star entry from persisted occelmnt XML
	vizierTab.FillStarFromOccelmntXml(lastLoadedOccelmntXml)

	// Track if csv ops tab has been opened for the first time
	csvOpsTabFirstOpen := true

	// resetFitButtons restores all four fit-page action buttons to their default
	// (HighImportance) color. Assigned after the buttons are created below.
	var resetFitButtons func()

	// resetNormalizeBtn restores the Normalize baseline button to blue. Assigned after the button is created.
	var resetNormalizeBtn func()

	// enablePostFitButtons enables Monte Carlo, NIE, and Fill SODIS after a successful fit. Assigned after those buttons are created.
	var enablePostFitButtons func()

	// resetProcessOccelmntBtn restores the Process occelmnt file button to blue. Assigned after the button is created.
	var resetProcessOccelmntBtn func()

	// resetIOTABtn disables the Run IOTAdiffraction button. Assigned after the button is created.
	var resetIOTABtn func()
	// enableShowIOTAPlots enables the Show IOTAdiffraction plots button. Assigned after the button is created.
	var enableShowIOTAPlots func()

	// Function to open the CSV file dialog
	openCSVDialog := func() {
		showFileOpenWithRecents(w, prefs, "Select OBS folder then select the light curve csv file", storage.NewExtensionFileFilter([]string{".csv"}), prefs.String("obsHomeDir"), func(reader fyne.URIReadCloser, err error) {
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
			base := filepath.Base(filePath)
			data, err := parseLightCurveCSV(filePath)
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s does not appear to be a light curve file. It is lacking a header line that is normally part of a valid light curve file", base), w)
				return
			}

			// Create a -RESULTS folder in the observation folder
			ext := filepath.Ext(base)
			nameWithoutExt := base[:len(base)-len(ext)]
			resultsFolder = filepath.Join(filepath.Dir(filePath), nameWithoutExt+"-RESULTS")
			if err := os.MkdirAll(resultsFolder, 0755); err != nil {
				fmt.Printf("Warning: could not create results folder %s: %v\n", resultsFolder, err)
				resultsFolder = ""
			}

			// Copy the CSV file into the results folder
			if resultsFolder != "" {
				csvDest := filepath.Join(resultsFolder, base)
				if srcBytes, err := os.ReadFile(filePath); err == nil {
					if err := os.WriteFile(csvDest, srcBytes, 0644); err != nil {
						fmt.Printf("Warning: could not copy CSV to results folder: %v\n", err)
					}
				}
			}

			loadedLightCurveData = data
			sodisReportSavedThisSession = false
			vizierDatWrittenThisSession = false
			sodisNegativeReportSaved = false
			if resetFitButtons != nil {
				resetFitButtons()
			}
			if resetNormalizeBtn != nil {
				resetNormalizeBtn()
			}
			trimPerformed = false
			setTrimBtn.Importance = widget.HighImportance
			setTrimBtn.Refresh()
			if resetProcessOccelmntBtn != nil {
				resetProcessOccelmntBtn()
			}
			if resetIOTABtn != nil {
				resetIOTABtn()
			}
			// Create an action log file for this CSV
			if err := createActionLog(filePath); err != nil {
				fmt.Printf("Warning: could not create log file: %v\n", err)
			}
			logAction(fmt.Sprintf("Loaded CSV with %d columns and %d data points", len(data.Columns), len(data.TimeValues)))
			w.SetTitle("GoPyOTE Version: " + Version + " — " + filePath)

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

			// Analyze timing errors if timestamps are available
			resetInterpolatedIndices()  // Clear any previous interpolated indices
			resetNegativeDeltaIndices() // Clear any previous negative delta indices
			if !timestampsEmpty && len(data.TimeValues) > 1 {
				timingResult := analyzeTimingErrors(data.TimeValues)
				if timingResult != nil && (len(timingResult.CadenceErrors) > 0 || len(timingResult.DroppedFrameErrors) > 0 || len(timingResult.NegativeDeltaErrors) > 0) {
					// Fix negative delta timestamps first (before interpolation)
					if len(timingResult.NegativeDeltaErrors) > 0 {
						timingResult.NegativeDeltaFixed = fixNegativeDeltaTimestamps(data, timingResult.NegativeDeltaErrors, timingResult.AverageTimeStep)
						logAction(fmt.Sprintf("Fixed %d negative delta timestamps", timingResult.NegativeDeltaFixed))
					}
					// Interpolate dropped frames
					if len(timingResult.DroppedFrameErrors) > 0 {
						timingResult.InterpolatedCount = interpolateDroppedFrames(data, timingResult.DroppedFrameErrors)
						logAction(fmt.Sprintf("Interpolated %d dropped frames", timingResult.InterpolatedCount))
					}
					// Mark negative delta indices AFTER interpolation, adjusting for inserted points
					if len(timingResult.NegativeDeltaErrors) > 0 {
						for _, negErr := range timingResult.NegativeDeltaErrors {
							// Calculate how many interpolated points were inserted before this index
							offset := 0
							for _, dropErr := range timingResult.DroppedFrameErrors {
								if dropErr.Index <= negErr.Index {
									offset += dropErr.DroppedCount
								}
							}
							markNegativeDeltaIndex(negErr.Index + offset)
						}
					}
					// Show timing error report dialog
					report := formatTimingReport(timingResult)
					logAction(fmt.Sprintf("Timing analysis: %d cadence errors, %d dropped frame gaps, %d negative deltas",
						len(timingResult.CadenceErrors), len(timingResult.DroppedFrameErrors), len(timingResult.NegativeDeltaErrors)))
					dialog.ShowInformation("Timing Analysis", report, w)
				}

				if timingResult != nil {
					lastCsvExposureSecs = timingResult.MedianTimeStep
				}
			}

			// Clear displayed curves and reset the plot
			for k := range displayedCurves {
				delete(displayedCurves, k)
			}
			lightCurvePlot.SetSeries(nil)
			smoothedSeries = nil         // Clear any previous smooth curve
			normalizationApplied = false // Reset normalization flag
			baselineScaledToUnity = false
			theorySeries = nil
			lightCurvePlot.SetVerticalLines(nil, false)
			lightCurvePlot.SetSigmaLines(nil, false)
			lightCurvePlot.ShowBaselineLine = false

			// Clear range entries and reset the user bounds flag
			userSetBounds = false
			xMinEntry.SetText("")
			xMaxEntry.SetText("")
			yMinEntry.SetText("")
			yMaxEntry.SetText("")

			// Update the list with column names, filtered by selected prefixes
			lightCurveListData = nil
			listIndexToColumnIndex = nil
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
			lightCurveList.UnselectAll()
			lightCurveList.Refresh()

			// Initialize frame number range entries and variables BEFORE displaying curves
			if len(data.FrameNumbers) > 0 {
				minFrameNum = data.FrameNumbers[0]
				maxFrameNum = data.FrameNumbers[len(data.FrameNumbers)-1]
				frameRangeStart = minFrameNum
				frameRangeEnd = maxFrameNum
				startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
				endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
			} else {
				// Reset frame range if no frame numbers
				frameRangeStart = 0
				frameRangeEnd = 0
			}

			// Automatically display the first light curve if available
			if len(listIndexToColumnIndex) > 0 {
				toggleLightCurve(listIndexToColumnIndex[0])
			}

			plotStatusLabel.SetText("Click on a point to see details")

			// Clear VizieR fields, then populate from RAVF or ADV headers if applicable
			vizierTab.ClearInputs()
			vizierTab.FillFromRavfHeaders(data.SkippedLines)
			vizierTab.FillFromAdvHeaders(data.SkippedLines)
		})
	}

	// Button to load a CSV file
	loadCSVBtn := widget.NewButton("Open browser to select csv file", func() {
		openCSVDialog()
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

	tab3Content := container.NewStack(tab3Bg, container.NewPadded(container.NewBorder(
		dataTabButtons,       // top
		nil,                  // bottom (frame controls moved to plot area)
		nil,                  // left
		nil,                  // right
		lightCurveListScroll, // center
	)))
	tab3 := container.NewTabItem("OBS select", tab3Content)

	// Tab 5: Block integration
	tab5Bg := makeTabBg(color.RGBA{R: 200, G: 220, B: 200, A: 255}, color.RGBA{R: 50, G: 70, B: 50, A: 255})

	// Status label for block integration
	blockIntStatusLabel := widget.NewLabel("Select two points on the plot to define a block size")

	// Block integrate button
	blockIntegrateButton := widget.NewButton("Block Integrate", func() {
		// Check if we have loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Check if two points are selected
		if lightCurvePlot.selectedSeries < 0 || lightCurvePlot.selectedIndex < 0 {
			dialog.ShowError(fmt.Errorf("point 1 not selected - click on a point to select it"), w)
			return
		}
		if lightCurvePlot.selectedSeries2 < 0 || lightCurvePlot.selectedIndex2 < 0 {
			dialog.ShowError(fmt.Errorf("point 2 not selected - click on another point in the same light curve"), w)
			return
		}

		// Check if both points are on the same series
		if lightCurvePlot.selectedSeries != lightCurvePlot.selectedSeries2 {
			dialog.ShowError(fmt.Errorf("both points must be on the same series to define a block"), w)
			return
		}

		// Get the indices of the two selected points (in the original data)
		series := lightCurvePlot.series[lightCurvePlot.selectedSeries]
		idx1 := series.Points[lightCurvePlot.selectedIndex].Index
		idx2 := series.Points[lightCurvePlot.selectedIndex2].Index

		// Ensure idx1 < idx2
		if idx1 > idx2 {
			idx1, idx2 = idx2, idx1
		}

		// Calculate block size (number of points inclusive)
		blockSize := idx2 - idx1 + 1
		if blockSize < 2 {
			dialog.ShowError(fmt.Errorf("block size must be at least 2 points (current: %d)", blockSize), w)
			return
		}

		logAction(fmt.Sprintf("Block Integration: block size = %d points (from index %d to %d)", blockSize, idx1, idx2))

		// Apply block integration to all columns in the loaded data
		numPoints := len(loadedLightCurveData.FrameNumbers)

		if numPoints == 0 {
			dialog.ShowError(fmt.Errorf("no data points in loaded file"), w)
			return
		}

		// Block integration starts from the first selected point (idx1)
		// Calculate complete blocks going left and right from idx1
		pointsBefore := idx1               // points before idx1 (indices 0 to idx1-1)
		pointsFromIdx1 := numPoints - idx1 // points from idx1 to end (indices idx1 to numPoints-1)

		blocksBefore := pointsBefore / blockSize     // complete blocks to the left
		blocksFromIdx1 := pointsFromIdx1 / blockSize // complete blocks from idx1 onward

		numBlocks := blocksBefore + blocksFromIdx1

		if numBlocks == 0 {
			dialog.ShowError(fmt.Errorf("not enough points for even one complete block of size %d", blockSize), w)
			return
		}

		// Calculate where the first complete block starts
		// Blocks to the left: the leftmost complete block starts at idx1 - (blocksBefore * blockSize)
		firstBlockStart := idx1 - (blocksBefore * blockSize)

		logAction(fmt.Sprintf("Block Integration: idx1=%d, blocksBefore=%d, blocksFromIdx1=%d, total=%d, firstBlockStart=%d",
			idx1, blocksBefore, blocksFromIdx1, numBlocks, firstBlockStart))

		// Create new arrays for block-integrated data
		newFrameNumbers := make([]float64, numBlocks)
		newTimeValues := make([]float64, numBlocks)
		newColumns := make([]LightCurveColumn, len(loadedLightCurveData.Columns))

		for i := range newColumns {
			newColumns[i].Name = loadedLightCurveData.Columns[i].Name
			newColumns[i].Values = make([]float64, numBlocks)
		}

		// Process each block starting from the firstBlockStart
		for blockIdx := 0; blockIdx < numBlocks; blockIdx++ {
			startIdx := firstBlockStart + (blockIdx * blockSize)
			endIdx := startIdx + blockSize // exclusive

			// Use the frame number and time of the first point in the block
			newFrameNumbers[blockIdx] = loadedLightCurveData.FrameNumbers[startIdx]
			newTimeValues[blockIdx] = loadedLightCurveData.TimeValues[startIdx]

			// Average each column's values in this block
			for colIdx := range loadedLightCurveData.Columns {
				sum := 0.0
				for i := startIdx; i < endIdx; i++ {
					sum += loadedLightCurveData.Columns[colIdx].Values[i]
				}
				newColumns[colIdx].Values[blockIdx] = sum / float64(blockSize)
			}
		}

		// Update the loaded data with block-integrated values
		loadedLightCurveData.FrameNumbers = newFrameNumbers
		loadedLightCurveData.TimeValues = newTimeValues
		loadedLightCurveData.Columns = newColumns

		// Clear smooth curve since indices are now invalid
		smoothedSeries = nil

		// Update frame range limits
		minFrameNum = newFrameNumbers[0]
		maxFrameNum = newFrameNumbers[len(newFrameNumbers)-1]
		frameRangeStart = minFrameNum
		frameRangeEnd = maxFrameNum
		startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
		endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))

		// Clear selections since indices are now invalid
		lightCurvePlot.selectedSeries = -1
		lightCurvePlot.selectedIndex = -1
		lightCurvePlot.selectedSeries2 = -1
		lightCurvePlot.selectedIndex2 = -1
		lightCurvePlot.selectedPointDataIndex = -1
		lightCurvePlot.selectedPointDataIndex2 = -1
		lightCurvePlot.selectedSeriesName = ""
		lightCurvePlot.selectedSeriesName2 = ""

		// Save Y bounds before rebuilding (preserve user scaling)
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()

		// Rebuild the plot with the new data
		rebuildPlot()

		// Restore Y bounds to preserve user scaling
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

		// Update status
		statusMsg := fmt.Sprintf("Block integrated: %d points → %d blocks (block size: %d)", numPoints, numBlocks, blockSize)
		blockIntStatusLabel.SetText(statusMsg)
		logAction(statusMsg)

		dialog.ShowInformation("Block Integration Complete",
			fmt.Sprintf("Original: %d points\nBlock size: %d\nResult: %d averaged blocks\n\nClick 'Undo' to restore original data.", numPoints, blockSize, numBlocks), w)
	})

	// Undo button - reloads the original CSV file to restore original data
	undoBlockIntButton := widget.NewButton("Undo", func() {
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		if loadedLightCurveData.SourceFilePath == "" {
			dialog.ShowError(fmt.Errorf("no source file path available"), w)
			return
		}

		sourcePath := loadedLightCurveData.SourceFilePath

		// Save the currently displayed curves before reloading
		savedDisplayedCurves := make(map[int]bool)
		for k, v := range displayedCurves {
			savedDisplayedCurves[k] = v
		}

		// Re-read the original file
		data, err := parseLightCurveCSV(sourcePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to reload CSV: %w", err), w)
			return
		}

		loadedLightCurveData = data
		normalizationApplied = false
		smoothedSeries = nil
		theorySeries = nil
		lightCurvePlot.SetVerticalLines(nil, false)
		lightCurvePlot.SetSigmaLines(nil, false)
		lightCurvePlot.ShowBaselineLine = false

		// Reset interpolated/negative delta indices
		resetInterpolatedIndices()
		resetNegativeDeltaIndices()

		// Run timing analysis (same as initial load)
		timestampsEmpty := true
		for _, t := range data.TimeValues {
			if t != 0 {
				timestampsEmpty = false
				break
			}
		}
		if !timestampsEmpty && len(data.TimeValues) > 1 {
			timingResult := analyzeTimingErrors(data.TimeValues)
			if timingResult != nil && (len(timingResult.CadenceErrors) > 0 || len(timingResult.DroppedFrameErrors) > 0 || len(timingResult.NegativeDeltaErrors) > 0) {
				if len(timingResult.NegativeDeltaErrors) > 0 {
					timingResult.NegativeDeltaFixed = fixNegativeDeltaTimestamps(data, timingResult.NegativeDeltaErrors, timingResult.AverageTimeStep)
				}
				if len(timingResult.DroppedFrameErrors) > 0 {
					timingResult.InterpolatedCount = interpolateDroppedFrames(data, timingResult.DroppedFrameErrors)
				}
				if len(timingResult.NegativeDeltaErrors) > 0 {
					for _, negErr := range timingResult.NegativeDeltaErrors {
						offset := 0
						for _, dropErr := range timingResult.DroppedFrameErrors {
							if dropErr.Index <= negErr.Index {
								offset += dropErr.DroppedCount
							}
						}
						markNegativeDeltaIndex(negErr.Index + offset)
					}
				}
			}

			if timingResult != nil {
				lastCsvExposureSecs = timingResult.MedianTimeStep
			}
		}

		// Clear displayed curves
		for k := range displayedCurves {
			delete(displayedCurves, k)
		}
		lightCurvePlot.SetSeries(nil)

		// Clear selected points
		lightCurvePlot.selectedSeries = -1
		lightCurvePlot.selectedIndex = -1
		lightCurvePlot.selectedSeries2 = -1
		lightCurvePlot.selectedIndex2 = -1
		lightCurvePlot.selectedPointDataIndex = -1
		lightCurvePlot.selectedPointDataIndex2 = -1
		lightCurvePlot.selectedSeriesName = ""
		lightCurvePlot.selectedSeriesName2 = ""
		lightCurvePlot.SelectedPoint1Valid = false
		lightCurvePlot.SelectedPoint2Valid = false

		// Update frame range
		if len(data.FrameNumbers) > 0 {
			minFrameNum = data.FrameNumbers[0]
			maxFrameNum = data.FrameNumbers[len(data.FrameNumbers)-1]
			frameRangeStart = minFrameNum
			frameRangeEnd = maxFrameNum
			startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
			endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
		}

		// Restore the previously displayed curves
		for colIdx := range savedDisplayedCurves {
			if colIdx < len(data.Columns) {
				toggleLightCurve(colIdx)
			}
		}

		lightCurvePlot.Refresh()
		blockIntStatusLabel.SetText("Original data restored from file")
		logAction(fmt.Sprintf("Undo: Reloaded original data from %s", sourcePath))
	})

	tab5Content := container.NewStack(tab5Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Block Integration"),
		widget.NewSeparator(),
		widget.NewLabel("Instructions:"),
		widget.NewLabel("1. Click on the first point of a 'block'"),
		widget.NewLabel("2. Click on the last point in that 'block'"),
		widget.NewLabel("3. Click the Block Integrate button"),
		widget.NewSeparator(),
		container.NewHBox(blockIntegrateButton, undoBlockIntButton),
		widget.NewSeparator(),
		blockIntStatusLabel,
	)))
	tab5 := container.NewTabItem("BlockInt", tab5Content)

	// Tab 6: Flash tags
	tab6Bg := makeTabBg(color.RGBA{R: 220, G: 200, B: 220, A: 255}, color.RGBA{R: 70, G: 50, B: 70, A: 255})

	// Alevel, Blevel display labels (read-only)
	alevelValue := widget.NewLabel("---")
	blevelValue := widget.NewLabel("---")

	// Two flashEdgeNum calculations with stored values
	var savedFlashEdge1, savedFlashEdge2 float64
	var savedPoint1Frame float64 // Frame number of the point used for Edge 1
	flashEdge1Valid := false
	flashEdge2Valid := false
	flashEdge1Value := widget.NewLabel("---")
	flashEdge2Value := widget.NewLabel("---")

	// Helper function to compute flash edge number from the current selection
	computeFlashEdge := func() (float64, bool) {
		// Check if a point is selected
		if lightCurvePlot.selectedSeries < 0 || lightCurvePlot.selectedIndex < 0 {
			dialog.ShowError(fmt.Errorf("no point selected - click on a point first"), w)
			return 0, false
		}

		// Check if we have loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return 0, false
		}

		// Get the selected series and find the data
		series := lightCurvePlot.series[lightCurvePlot.selectedSeries]
		selectedIdx := lightCurvePlot.selectedIndex
		selectedPointValue := series.Points[selectedIdx].Y

		// Get the frame number of the selected point
		selectedFrameNum := float64(series.Points[selectedIdx].Index)
		if loadedLightCurveData != nil && series.Points[selectedIdx].Index < len(loadedLightCurveData.FrameNumbers) {
			selectedFrameNum = loadedLightCurveData.FrameNumbers[series.Points[selectedIdx].Index]
		}
		logAction(fmt.Sprintf("Flash tag: Computing levels for selected point at frame %.0f, value %.4f", selectedFrameNum, selectedPointValue))

		// Compute Alevel: average of 10 points to the left (before selected point)
		aCount := 0
		aSum := 0.0
		for i := selectedIdx - 1; i >= 0 && aCount < 10; i-- {
			aSum += series.Points[i].Y
			aCount++
		}

		var alevel float64
		alevelValid := false
		if aCount == 0 {
			alevelValue.SetText("N/A")
			logAction("Flash tag: Alevel N/A (no points to the left)")
		} else {
			alevel = aSum / float64(aCount)
			alevelValid = true
			alevelValue.SetText(fmt.Sprintf("%.4f", alevel))
			logAction(fmt.Sprintf("Flash tag: Alevel = %.4f (average of %d points)", alevel, aCount))
		}

		// Compute Blevel: average of 10 points to the right (after the selected point)
		bCount := 0
		bSum := 0.0
		for i := selectedIdx + 1; i < len(series.Points) && bCount < 10; i++ {
			bSum += series.Points[i].Y
			bCount++
		}

		var blevel float64
		blevelValid := false
		if bCount == 0 {
			blevelValue.SetText("N/A")
			logAction("Flash tag: Blevel N/A (no points to the right)")
		} else {
			blevel = bSum / float64(bCount)
			blevelValid = true
			blevelValue.SetText(fmt.Sprintf("%.4f", blevel))
			logAction(fmt.Sprintf("Flash tag: Blevel = %.4f (average of %d points)", blevel, bCount))
		}

		// Compute flashEdgeNum = (Blevel - selected point value) / (Blevel - Alevel) + selected point frame num - 1.0
		if alevelValid && blevelValid {
			// Check for rising edge (Alevel must be less than Blevel)
			if alevel > blevel {
				dialog.ShowError(fmt.Errorf("flash tag edges must always be rising edges (falling edges are subject to slow responses which could cause timing inaccuracies)"), w)
				logAction(fmt.Sprintf("Flash tag: Error - not a rising edge"))
				return 0, false
			}
			denominator := blevel - alevel
			if denominator == 0 {
				logAction("Flash tag: flashEdgeNum N/A (division by zero, Blevel equals Alevel)")
				return 0, false
			}
			flashEdgeNum := (blevel-selectedPointValue)/denominator + selectedFrameNum - 1.0
			logAction(fmt.Sprintf("Flash tag: flashEdgeNum = %.4f", flashEdgeNum))
			return flashEdgeNum, true
		}
		logAction("Flash tag: flashEdgeNum N/A (Alevel or Blevel unavailable)")
		return 0, false
	}

	// Button to compute and save flash edge 1
	computeEdge1Btn := widget.NewButton("Use selected point as Flash 1", func() {
		if val, ok := computeFlashEdge(); ok {
			// The +1 is to maintain the QHY camera model (zero camera delay)
			savedFlashEdge1 = val + 1
			flashEdge1Valid = true
			flashEdge1Value.SetText(fmt.Sprintf("%.4f", savedFlashEdge1))
			// Save the frame number of the point used for Edge 1
			if lightCurvePlot.selectedSeries >= 0 && lightCurvePlot.selectedIndex >= 0 {
				series := lightCurvePlot.series[lightCurvePlot.selectedSeries]
				pointDataIdx := series.Points[lightCurvePlot.selectedIndex].Index
				if loadedLightCurveData != nil && pointDataIdx < len(loadedLightCurveData.FrameNumbers) {
					savedPoint1Frame = loadedLightCurveData.FrameNumbers[pointDataIdx]
				}
			}
			logAction(fmt.Sprintf("Flash tag: Saved Edge 1 = %.4f, Point1 Frame = %.0f", savedFlashEdge1, savedPoint1Frame))
		} else {
			flashEdge1Value.SetText("N/A")
			flashEdge1Valid = false
		}
	})

	// Button to compute and save flash edge 2
	computeEdge2Btn := widget.NewButton("Use selected point as Flash 2", func() {
		if val, ok := computeFlashEdge(); ok {
			// The +1 is to maintain the QHY camera model (zero camera delay)
			savedFlashEdge2 = val + 1
			flashEdge2Valid = true
			flashEdge2Value.SetText(fmt.Sprintf("%.4f", savedFlashEdge2))
			logAction(fmt.Sprintf("Flash tag: Saved Edge 2 = %.4f", savedFlashEdge2))
		} else {
			flashEdge2Value.SetText("N/A")
			flashEdge2Valid = false
		}
	})

	// Timestamp entry boxes with parsed values
	var timestamp1Seconds, timestamp2Seconds float64
	var timestamp1Valid, timestamp2Valid bool

	timestamp1Entry := NewFocusLossEntry()
	timestamp1Entry.SetPlaceHolder("hh:mm:ss.ssss")
	timestamp1Entry.OnSubmitted = func(text string) {
		if text == "" {
			timestamp1Valid = false
			return
		}
		if val, ok := parseTimestampInput(text); ok {
			timestamp1Seconds = val
			timestamp1Valid = true
			logAction(fmt.Sprintf("Flash tag: Timestamp 1 = %s (%.4f seconds)", text, val))
		} else {
			timestamp1Valid = false
			dialog.ShowError(fmt.Errorf("invalid timestamp format: %s", text), w)
		}
	}

	timestamp2Entry := NewFocusLossEntry()
	timestamp2Entry.SetPlaceHolder("hh:mm:ss.ssss")
	timestamp2Entry.OnSubmitted = func(text string) {
		if text == "" {
			timestamp2Valid = false
			return
		}
		if val, ok := parseTimestampInput(text); ok {
			timestamp2Seconds = val
			timestamp2Valid = true
			logAction(fmt.Sprintf("Flash tag: Timestamp 2 = %s (%.4f seconds)", text, val))
		} else {
			timestamp2Valid = false
			dialog.ShowError(fmt.Errorf("invalid timestamp format: %s", text), w)
		}
	}

	// Wrap timestamp entries in containers
	timestamp1Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(187.5, 36)), timestamp1Entry)
	timestamp2Container := container.New(layout.NewGridWrapLayout(fyne.NewSize(187.5, 36)), timestamp2Entry)

	// Camera exposure time entry
	var cameraExposureTime float64
	exposureTimeEntry := NewFocusLossEntry()
	exposureTimeEntry.SetPlaceHolder("seconds")
	exposureTimeEntry.OnSubmitted = func(text string) {
		if text == "" {
			return
		}
		val, err := strconv.ParseFloat(text, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid exposure time: %s", text), w)
			return
		}
		cameraExposureTime = val
		logAction(fmt.Sprintf("Flash tag: Camera exposure time set to %.4f seconds", cameraExposureTime))
	}
	exposureTimeContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(100, 36)), exposureTimeEntry)

	// Time per frame calculation
	timePerFrameValue := widget.NewLabel("---")
	var timePerFrame float64
	calcTimePerFrameBtn := widget.NewButton("Calc time/frame", func() {
		if !timestamp1Valid || !timestamp2Valid {
			dialog.ShowError(fmt.Errorf("both timestamps must be entered"), w)
			timePerFrameValue.SetText("N/A")
			return
		}
		if !flashEdge1Valid || !flashEdge2Valid {
			dialog.ShowError(fmt.Errorf("both edge values must be set"), w)
			timePerFrameValue.SetText("N/A")
			return
		}
		edgeDiff := savedFlashEdge2 - savedFlashEdge1
		if edgeDiff == 0 {
			dialog.ShowError(fmt.Errorf("edge 2 and edge 1 are equal (division by zero)"), w)
			timePerFrameValue.SetText("N/A")
			return
		}
		timePerFrame = (timestamp2Seconds - timestamp1Seconds) / edgeDiff
		timePerFrameValue.SetText(fmt.Sprintf("%.6f", timePerFrame))
		logAction(fmt.Sprintf("Flash tag: timePerFrame = (%.4f - %.4f) / (%.4f - %.4f) = %.6f seconds",
			timestamp2Seconds, timestamp1Seconds, savedFlashEdge2, savedFlashEdge1, timePerFrame))
	})

	// Tzero calculation: Tzero = timestamp1 - (flash1Frame - minFrame) * time per frame
	tzeroValue := widget.NewLabel("---")
	var tzero float64
	calcTzeroBtn := widget.NewButton("Calc Tzero", func() {
		if !timestamp1Valid {
			dialog.ShowError(fmt.Errorf("timestamp 1 must be entered"), w)
			tzeroValue.SetText("N/A")
			return
		}
		if timePerFrame == 0 {
			dialog.ShowError(fmt.Errorf("time per frame must be calculated first"), w)
			tzeroValue.SetText("N/A")
			return
		}
		if !flashEdge1Valid {
			dialog.ShowError(fmt.Errorf("flash 1 must be set first"), w)
			tzeroValue.SetText("N/A")
			return
		}
		tzero = timestamp1Seconds - (savedFlashEdge1-minFrameNum)*timePerFrame
		tzeroValue.SetText(formatSecondsAsTimestamp(tzero))
		logAction(fmt.Sprintf("Flash tag: Tzero = %.4f - (%.4f - %.0f) * %.6f = %.4f (%s)",
			timestamp1Seconds, savedFlashEdge1, minFrameNum, timePerFrame, tzero, formatSecondsAsTimestamp(tzero)))

		// Update all light curve timestamps: timestamp = Tzero + (frameNumber - minFrameNum) * timePerFrame
		if loadedLightCurveData != nil {
			for i, frameNum := range loadedLightCurveData.FrameNumbers {
				loadedLightCurveData.TimeValues[i] = tzero + (frameNum-minFrameNum)*timePerFrame
			}
			logAction(fmt.Sprintf("Flash tag: Updated %d light curve timestamps", len(loadedLightCurveData.TimeValues)))
			dialog.ShowInformation("Timestamps updated/inserted",
				fmt.Sprintf("Updated %d light curve timestamps", len(loadedLightCurveData.TimeValues)), w)
		}
	})

	// Frame timestamp calculation: frameTime = tzero + (frameNum - minFrameNum) * timePerFrame
	frameNumEntry := NewFocusLossEntry()
	frameNumEntry.SetPlaceHolder("frame #")
	frameNumContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(80, 36)), frameNumEntry)
	frameTimeValue := widget.NewLabel("---")
	calcFrameTimeBtn := widget.NewButton("Calc frame time", func() {
		if tzero == 0 {
			dialog.ShowError(fmt.Errorf("tzero must be calculated first"), w)
			frameTimeValue.SetText("N/A")
			return
		}
		if timePerFrame == 0 {
			dialog.ShowError(fmt.Errorf("time per frame must be calculated first"), w)
			frameTimeValue.SetText("N/A")
			return
		}
		frameNumText := frameNumEntry.Text
		if frameNumText == "" {
			dialog.ShowError(fmt.Errorf("frame number must be entered"), w)
			frameTimeValue.SetText("N/A")
			return
		}
		frameNum, err := strconv.ParseFloat(frameNumText, 64)
		if err != nil {
			dialog.ShowError(fmt.Errorf("invalid frame number: %s", frameNumText), w)
			frameTimeValue.SetText("N/A")
			return
		}
		frameTime := tzero + (frameNum-minFrameNum)*timePerFrame
		frameTimeValue.SetText(formatSecondsAsTimestamp(frameTime))
		logAction(fmt.Sprintf("Flash tag: Frame %g time = %.4f + (%g - %.0f) * %.6f = %.4f (%s)",
			frameNum, tzero, frameNum, minFrameNum, timePerFrame, frameTime, formatSecondsAsTimestamp(frameTime)))
	})

	// Suppress unused variable warning
	_ = cameraExposureTime

	tab6Content := container.NewStack(tab6Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Flash tags"),
		container.NewHBox(widget.NewLabel("Alevel:"), alevelValue),
		container.NewHBox(widget.NewLabel("Blevel:"), blevelValue),
		container.NewHBox(computeEdge1Btn, widget.NewLabel("Flash 1 frame"), flashEdge1Value),
		container.NewHBox(computeEdge2Btn, widget.NewLabel("Flash 2 frame"), flashEdge2Value),
		container.NewHBox(widget.NewLabel("Flash 1 timestamp"), timestamp1Container),
		container.NewHBox(widget.NewLabel("Flash 2 timestamp"), timestamp2Container),
		container.NewHBox(widget.NewLabel("Exposure time:"), exposureTimeContainer),
		container.NewHBox(calcTimePerFrameBtn, widget.NewLabel("Time/frame:"), timePerFrameValue),
		container.NewHBox(calcTzeroBtn, widget.NewLabel("Tzero:"), tzeroValue),
		container.NewHBox(calcFrameTimeBtn, widget.NewLabel("Frame:"), frameNumContainer, widget.NewLabel("Time:"), frameTimeValue),
	)))
	tab6 := container.NewTabItem("Flash tags", tab6Content)

	// Tab 7: Savitzky-Golay Smoothing
	tab7Bg := makeTabBg(color.RGBA{R: 200, G: 200, B: 230, A: 255}, color.RGBA{R: 50, G: 50, B: 80, A: 255})

	// Status label for smoothing
	smoothStatusLabel := widget.NewLabel("Click on a point to select the reference curve, then click another point to define window size")

	// Clear smooth button
	clearSmoothButton := widget.NewButton("Clear Smooth", func() {
		smoothedSeries = nil
		// Save Y bounds before rebuild
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		smoothStatusLabel.SetText("Smooth curve cleared")
		logAction("Cleared Savitzky-Golay smooth curve")
	})

	// Smooth button
	smoothButton := widget.NewButton("Smooth", func() {
		// Check if we have loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Check if two points are selected
		if lightCurvePlot.selectedSeries < 0 || lightCurvePlot.selectedIndex < 0 {
			dialog.ShowError(fmt.Errorf("point 1 not selected - click on a point to select it"), w)
			return
		}
		if lightCurvePlot.selectedSeries2 < 0 || lightCurvePlot.selectedIndex2 < 0 {
			dialog.ShowError(fmt.Errorf("point 2 not selected - click on another point to define window size"), w)
			return
		}

		// Use the clicked curve (from the first selected point) as the reference
		series := lightCurvePlot.series[lightCurvePlot.selectedSeries]
		refCurveName := series.Name

		// Find the column in loadedLightCurveData that matches the clicked curve name
		var refColumn *LightCurveColumn
		for i := range loadedLightCurveData.Columns {
			if loadedLightCurveData.Columns[i].Name == refCurveName {
				refColumn = &loadedLightCurveData.Columns[i]
				break
			}
		}
		if refColumn == nil {
			dialog.ShowError(fmt.Errorf("could not find reference column for curve: %s", refCurveName), w)
			return
		}

		// Get the indices of the two selected points to determine window size
		idx1 := series.Points[lightCurvePlot.selectedIndex].Index
		series2 := lightCurvePlot.series[lightCurvePlot.selectedSeries2]
		idx2 := series2.Points[lightCurvePlot.selectedIndex2].Index

		// Calculate window size
		windowSize := idx2 - idx1
		if windowSize < 0 {
			windowSize = -windowSize
		}
		windowSize++ // inclusive

		// Get Y values for the reference column
		ys := refColumn.Values
		numPoints := len(ys)

		// Make window size odd if needed (Savitzky-Golay requires an odd window)
		if windowSize%2 == 0 {
			// Prefer adding 1, but subtract 1 if that would exceed available data
			if windowSize+1 <= numPoints {
				windowSize++
			} else {
				windowSize--
			}
		}

		// Minimum window size check
		if windowSize < 3 {
			windowSize = 3
		}

		logAction(fmt.Sprintf("Savitzky-Golay smoothing: window size = %d, reference curve = %s", windowSize, refColumn.Name))

		if numPoints < windowSize {
			dialog.ShowError(fmt.Errorf("not enough data points (%d) for window size %d", numPoints, windowSize), w)
			return
		}

		// Create X values (just indices for the filter)
		xs := make([]float64, numPoints)
		for i := range xs {
			xs[i] = float64(i)
		}

		// Create Savitzky-Golay filter
		filter, err := savitzkygolay.NewFilterWindow(windowSize)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to create Savitzky-Golay filter: %v", err), w)
			return
		}

		// Apply the filter
		smoothedYs, err := filter.Process(ys, xs)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to apply Savitzky-Golay filter: %v", err), w)
			return
		}

		// Create the smoothed series
		var smoothPoints []PlotPoint
		for i, y := range smoothedYs {
			smoothPoints = append(smoothPoints, PlotPoint{
				X:      0, // Will be set in rebuildPlot based on frame numbers or timestamps
				Y:      y,
				Index:  i,
				Series: 0,
			})
		}

		smoothedSeries = &PlotSeries{
			Points: smoothPoints,
			Color:  color.RGBA{R: 255, G: 0, B: 255, A: 255}, // Magenta for a smooth curve
			Name:   "Smooth(" + refColumn.Name + ")",
		}

		// Save Y bounds before rebuild
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()

		// Rebuild the plot to include the smoothed series
		rebuildPlot()

		// Restore Y bounds
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

		// Update status
		statusMsg := fmt.Sprintf("Smoothed %s with window size %d", refColumn.Name, windowSize)
		smoothStatusLabel.SetText(statusMsg)
		logAction(statusMsg)
	})

	// Normalize button - uses the smoothed reference curve to normalize all light curves
	normalizeButton := widget.NewButton("Normalize", func() {
		// Check if we have loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Check if a smoothed curve exists
		if smoothedSeries == nil {
			dialog.ShowError(fmt.Errorf("choose a reference curve and smoothing window size by clicking two points on the desired reference curve"), w)
			return
		}

		// Check that the smoothed series has the same number of points as the data
		if len(smoothedSeries.Points) != len(loadedLightCurveData.FrameNumbers) {
			dialog.ShowError(fmt.Errorf("smoothed curve length (%d) does not match data length (%d) - please re-smooth after any data changes",
				len(smoothedSeries.Points), len(loadedLightCurveData.FrameNumbers)), w)
			return
		}

		// Calculate the mean of the smoothed reference curve
		var sumSmooth float64
		for _, pt := range smoothedSeries.Points {
			sumSmooth += pt.Y
		}
		meanSmooth := sumSmooth / float64(len(smoothedSeries.Points))

		logAction(fmt.Sprintf("Normalizing light curves using smoothed reference (mean = %.4f)", meanSmooth))

		// Apply normalization to all columns: y_norm[i] = (mean * y[i]) / smooth[i]
		for colIdx := range loadedLightCurveData.Columns {
			for i := range loadedLightCurveData.Columns[colIdx].Values {
				smoothVal := smoothedSeries.Points[i].Y
				if smoothVal != 0 {
					loadedLightCurveData.Columns[colIdx].Values[i] =
						(meanSmooth * loadedLightCurveData.Columns[colIdx].Values[i]) / smoothVal
				}
			}
		}

		// Clear the smoothed series since it's now incorporated into the data
		smoothedSeries = nil

		// Set normalization flag for filename generation
		normalizationApplied = true

		// Save Y bounds before rebuild
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()

		// Rebuild the plot with normalized data
		rebuildPlot()

		// Restore Y bounds
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

		// Clear selected points on the reference curve
		lightCurvePlot.selectedSeries = -1
		lightCurvePlot.selectedIndex = -1
		lightCurvePlot.selectedSeries2 = -1
		lightCurvePlot.selectedIndex2 = -1
		lightCurvePlot.selectedSeriesName = ""
		lightCurvePlot.selectedSeriesName2 = ""
		lightCurvePlot.Refresh()

		// Update status
		statusMsg := fmt.Sprintf("Normalized all light curves (reference mean = %.4f)", meanSmooth)
		smoothStatusLabel.SetText(statusMsg)
		logAction(statusMsg)

		dialog.ShowInformation("Normalization Complete",
			fmt.Sprintf("All light curves have been normalized.\n\nReference mean: %.4f\n\nClick 'Undo' to restore original data.", meanSmooth), w)
	})

	// Undo button - reloads the original CSV file to restore original data
	undoNormalizeButton := widget.NewButton("Undo", func() {
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		if loadedLightCurveData.SourceFilePath == "" {
			dialog.ShowError(fmt.Errorf("no source file path available"), w)
			return
		}

		sourcePath := loadedLightCurveData.SourceFilePath

		// Save the currently displayed curves before reloading
		savedDisplayedCurves := make(map[int]bool)
		for k, v := range displayedCurves {
			savedDisplayedCurves[k] = v
		}

		// Re-read the original file
		data, err := parseLightCurveCSV(sourcePath)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to reload CSV: %w", err), w)
			return
		}

		loadedLightCurveData = data
		normalizationApplied = false
		smoothedSeries = nil
		theorySeries = nil
		lightCurvePlot.SetVerticalLines(nil, false)
		lightCurvePlot.SetSigmaLines(nil, false)
		lightCurvePlot.ShowBaselineLine = false

		// Reset interpolated/negative delta indices
		resetInterpolatedIndices()
		resetNegativeDeltaIndices()

		// Run timing analysis (same as initial load)
		timestampsEmpty := true
		for _, t := range data.TimeValues {
			if t != 0 {
				timestampsEmpty = false
				break
			}
		}
		if !timestampsEmpty && len(data.TimeValues) > 1 {
			timingResult := analyzeTimingErrors(data.TimeValues)
			if timingResult != nil && (len(timingResult.CadenceErrors) > 0 || len(timingResult.DroppedFrameErrors) > 0 || len(timingResult.NegativeDeltaErrors) > 0) {
				if len(timingResult.NegativeDeltaErrors) > 0 {
					timingResult.NegativeDeltaFixed = fixNegativeDeltaTimestamps(data, timingResult.NegativeDeltaErrors, timingResult.AverageTimeStep)
				}
				if len(timingResult.DroppedFrameErrors) > 0 {
					timingResult.InterpolatedCount = interpolateDroppedFrames(data, timingResult.DroppedFrameErrors)
				}
				if len(timingResult.NegativeDeltaErrors) > 0 {
					for _, negErr := range timingResult.NegativeDeltaErrors {
						offset := 0
						for _, dropErr := range timingResult.DroppedFrameErrors {
							if dropErr.Index <= negErr.Index {
								offset += dropErr.DroppedCount
							}
						}
						markNegativeDeltaIndex(negErr.Index + offset)
					}
				}
			}

			if timingResult != nil {
				lastCsvExposureSecs = timingResult.MedianTimeStep
			}
		}

		// Clear displayed curves
		for k := range displayedCurves {
			delete(displayedCurves, k)
		}
		lightCurvePlot.SetSeries(nil)

		// Clear selected points
		lightCurvePlot.selectedSeries = -1
		lightCurvePlot.selectedIndex = -1
		lightCurvePlot.selectedSeries2 = -1
		lightCurvePlot.selectedIndex2 = -1
		lightCurvePlot.selectedPointDataIndex = -1
		lightCurvePlot.selectedPointDataIndex2 = -1
		lightCurvePlot.selectedSeriesName = ""
		lightCurvePlot.selectedSeriesName2 = ""
		lightCurvePlot.SelectedPoint1Valid = false
		lightCurvePlot.SelectedPoint2Valid = false

		// Update frame range
		if len(data.FrameNumbers) > 0 {
			minFrameNum = data.FrameNumbers[0]
			maxFrameNum = data.FrameNumbers[len(data.FrameNumbers)-1]
			frameRangeStart = minFrameNum
			frameRangeEnd = maxFrameNum
			startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
			endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
		}

		// Restore the previously displayed curves
		for colIdx := range savedDisplayedCurves {
			if colIdx < len(data.Columns) {
				toggleLightCurve(colIdx)
			}
		}

		lightCurvePlot.Refresh()
		smoothStatusLabel.SetText("Original data restored from file")
		logAction(fmt.Sprintf("Undo: Reloaded original data from %s", sourcePath))
	})

	tab7Content := container.NewStack(tab7Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Savitzky-Golay Smoothing & Normalization"),
		widget.NewSeparator(),
		widget.NewLabel("Smoothing Instructions:"),
		widget.NewLabel("1. Click on a point on the reference curve to select point 1"),
		widget.NewLabel("2. Click on another point to define window size"),
		widget.NewLabel("3. Click 'Smooth' to apply Savitzky-Golay filter"),
		widget.NewLabel("   (The curve of the first clicked point is used as reference)"),
		widget.NewSeparator(),
		container.NewHBox(smoothButton, clearSmoothButton),
		widget.NewSeparator(),
		widget.NewLabel("Normalization (after smoothing):"),
		widget.NewLabel("4. Click 'Normalize' to apply smoothed reference to all curves"),
		widget.NewLabel("Formula: y_norm[i] = (mean_ref * y[i]) / smooth_ref[i]"),
		widget.NewSeparator(),
		container.NewHBox(normalizeButton, undoNormalizeButton),
		widget.NewSeparator(),
		smoothStatusLabel,
	)))
	tab7 := container.NewTabItem("Smooth", tab7Content)

	// Tab 8: VizieR export - vizierTab was created earlier to allow RAVF header parsing during file load
	// Set up Generate button callback (needs access to local variables)
	vizierTab.GenerateBtn.OnTapped = func() {
		// Validate inputs
		year, month, day, err := vizierTab.ValidateInputs(w)
		if err != nil {
			dialog.ShowError(err, w)
			return
		}

		// Check for loaded data
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Get formatted values
		hipparcos, tycho2, ucac4 := vizierTab.GetFormattedStarIDs()
		longDeg, longMin, longSecs, latDeg, latMin, latSecs, altitude := vizierTab.GetFormattedLocation()

		// Get min/max frame numbers directly from loaded data
		dataMinFrame := loadedLightCurveData.FrameNumbers[0]
		dataMaxFrame := loadedLightCurveData.FrameNumbers[len(loadedLightCurveData.FrameNumbers)-1]

		// Check if we need to use a frame range
		rangeStart := int(frameRangeStart)
		rangeEnd := int(frameRangeEnd)
		if rangeStart == 0 {
			rangeStart = int(dataMinFrame)
		}
		if rangeEnd == 0 {
			rangeEnd = int(dataMaxFrame)
		}

		// Check if data is not trimmed and warn the user
		if rangeStart == int(dataMinFrame) && rangeEnd == int(dataMaxFrame) {
			dialog.ShowConfirm("Have you trimmed the light curve?",
				"Only the occultation event and enough points on either side\n"+
					"of the event to allow baseline noise to be well represented\n"+
					"are needed. Typically around a hundred points on either side\n"+
					"will be sufficient.\n\n"+
					"Do you wish to write the light curve as is?",
				func(proceed bool) {
					if !proceed {
						return
					}
					generateVizieRFile(w, loadedLightCurveData, year, month, day,
						hipparcos, tycho2, ucac4,
						longDeg, longMin, longSecs,
						latDeg, latMin, latSecs,
						altitude, vizierTab.ObserverNameEntry.Text,
						vizierTab.AsteroidNumberEntry.Text, vizierTab.AsteroidNameEntry.Text,
						rangeStart, rangeEnd,
						vizierTab.OutputFolderEntry.Text, vizierTab.StatusLabel)
				}, w)
			return
		}

		generateVizieRFile(w, loadedLightCurveData, year, month, day,
			hipparcos, tycho2, ucac4,
			longDeg, longMin, longSecs,
			latDeg, latMin, latSecs,
			altitude, vizierTab.ObserverNameEntry.Text,
			vizierTab.AsteroidNumberEntry.Text, vizierTab.AsteroidNameEntry.Text,
			rangeStart, rangeEnd,
			vizierTab.OutputFolderEntry.Text, vizierTab.StatusLabel)
	}

	// Set up the Preview submission button callback
	vizierTab.PreviewBtn.OnTapped = func() {
		// Validate all fields - must be complete before preview/generate.
		if _, _, _, err := vizierTab.ValidateInputs(w); err != nil {
			dialog.ShowError(err, w)
			return
		}

		// Check for loaded data.
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Determine the active frame range.
		dataMinFrame := loadedLightCurveData.FrameNumbers[0]
		dataMaxFrame := loadedLightCurveData.FrameNumbers[len(loadedLightCurveData.FrameNumbers)-1]
		rangeStart := int(frameRangeStart)
		rangeEnd := int(frameRangeEnd)
		if rangeStart == 0 {
			rangeStart = int(dataMinFrame)
		}
		if rangeEnd == 0 {
			rangeEnd = int(dataMaxFrame)
		}

		// Find start/end indices.
		startIdx := 0
		endIdx := len(loadedLightCurveData.FrameNumbers) - 1
		foundStart := false
		for i, frameNum := range loadedLightCurveData.FrameNumbers {
			if int(frameNum) >= rangeStart && !foundStart {
				startIdx = i
				foundStart = true
			}
			if int(frameNum) <= rangeEnd {
				endIdx = i
			}
		}

		// Find the signal column (same logic as generateVizieRFile).
		var valueColumn []float64
		for _, col := range loadedLightCurveData.Columns {
			if strings.HasPrefix(col.Name, "signal") {
				valueColumn = col.Values
				break
			}
		}
		if valueColumn == nil && len(loadedLightCurveData.Columns) > 0 {
			valueColumn = loadedLightCurveData.Columns[0].Values
		}
		if valueColumn == nil {
			dialog.ShowError(fmt.Errorf("no data columns available"), w)
			return
		}

		// Compute VizieR scale factor (normalise to 0–9524).
		maxValue := 0.0
		for i := startIdx; i <= endIdx; i++ {
			if !isInterpolatedIndex(i) && valueColumn[i] > maxValue {
				maxValue = valueColumn[i]
			}
		}
		scaleFactor := 9524.0 / maxValue
		if maxValue == 0 {
			scaleFactor = 1.0
		}

		// Build per-point slices for the submission range.
		numPts := endIdx - startIdx + 1
		timestamps := make([]float64, numPts)
		scaledValues := make([]int, numPts)
		dropped := make([]bool, numPts)
		for j := 0; j < numPts; j++ {
			idx := startIdx + j
			timestamps[j] = loadedLightCurveData.TimeValues[idx]
			if isInterpolatedIndex(idx) {
				dropped[j] = true
			} else {
				scaledValues[j] = int(valueColumn[idx] * scaleFactor)
			}
		}

		// Build plot title.
		plotTitle := fmt.Sprintf("VizieR Preview — Asteroid %s (%s)",
			vizierTab.AsteroidNumberEntry.Text, vizierTab.AsteroidNameEntry.Text)

		// Create the scaled light-curve plot.
		plotImg, err := createVizieRPreviewPlotImage(timestamps, scaledValues, dropped, plotTitle, 1100, 500)
		if err != nil {
			dialog.ShowError(fmt.Errorf("failed to create preview plot: %w", err), w)
			return
		}

		// Save the preview plot PNG to the results' folder.
		if resultsFolder != "" {
			var pngBuf bytes.Buffer
			if encErr := png.Encode(&pngBuf, plotImg); encErr == nil {
				savePath := filepath.Join(resultsFolder, "vizierPreviewPlot.png")
				if werr := os.WriteFile(savePath, pngBuf.Bytes(), 0644); werr != nil {
					fmt.Printf("Warning: could not save VizieR preview plot: %v\n", werr)
				} else {
					logAction("Saved VizieR preview plot: " + savePath)
				}
			}
		}

		// Show the plot in a new window.
		previewWin := a.NewWindow("VizieR Submission Preview")
		plotCanvas := canvas.NewImageFromImage(plotImg)
		plotCanvas.FillMode = canvas.ImageFillOriginal
		previewWin.SetContent(container.NewScroll(plotCanvas))
		previewWin.Resize(fyne.NewSize(1150, 550))
		previewWin.CenterOnScreen()
		previewWin.Show()

		// Enable the Generate button now that the user has previewed the submission.
		vizierTab.GenerateBtn.Enable()
	}

	// Set up a Zip button callback
	vizierTab.ZipBtn.OnTapped = func() {
		zipDatFiles(w, vizierTab.OutputFolderEntry.Text, vizierTab.StatusLabel)
	}

	// Set up Load from NA spreadsheet button callback
	vizierTab.LoadXlsxBtn.OnTapped = func() {
		vizierTab.FillFromNASpreadsheet(w)
	}

	// Set up Load from SODIS form button callback
	vizierTab.LoadSodisBtn.OnTapped = func() {
		vizierTab.FillFromSodisForm(w)
	}

	// Tab 10: Fit
	tab10Bg := makeTabBg(color.RGBA{R: 200, G: 220, B: 240, A: 255}, color.RGBA{R: 50, G: 70, B: 90, A: 255})

	// Status label for Fit tab
	fitStatusLabel := widget.NewLabel("Select pairs of points to define baseline regions")

	// Stored baseline noise sigma for Monte Carlo and NIE
	var noiseSigma float64

	// Stored last fit result, params, candidate curves, and target data for Monte Carlo
	var lastFitResult *fitResult
	var lastFitParams *OccultationParameters
	var lastFitCandidates []*precomputedCurve
	var lastFitBestIdx int // index into lastFitCandidates of the best path offset
	var lastFitTargetTimes, lastFitTargetValues []float64
	var lastMCResult *mcTrialsResult // most recent successful Monte Carlo run

	mcShowDiagnosticsCheck := widget.NewCheck("Show diagnostics plots", nil)
	mcShowDiagnosticsCheck.Checked = false

	mcNarrowSearchCheck := widget.NewCheck("Narrow MC offset search (±20 steps)", nil)
	mcNarrowSearchCheck.Checked = true

	// Calculate Baseline mean button: computes mean, extracts noise, scales to unity
	var calcBaselineMeanBtn *widget.Button
	calcBaselineMeanBtn = widget.NewButton("Normalize baseline and estimate noise sigma (used for Monte Carlo and NIE trials)", func() {
		if len(lightCurvePlot.SelectedPairs) == 0 {
			dialog.ShowError(fmt.Errorf("no point pairs selected - click on points to select baseline regions"), w)
			return
		}
		if loadedLightCurveData == nil {
			dialog.ShowError(fmt.Errorf("no light curve data loaded"), w)
			return
		}

		// Collect all points from all pairs and calculate the average
		var sum float64
		var count int

		for _, pair := range lightCurvePlot.SelectedPairs {
			idx1 := pair.Point1DataIdx
			idx2 := pair.Point2DataIdx
			if idx1 > idx2 {
				idx1, idx2 = idx2, idx1
			}

			var col *LightCurveColumn
			for i := range loadedLightCurveData.Columns {
				if loadedLightCurveData.Columns[i].Name == pair.Point1Series {
					col = &loadedLightCurveData.Columns[i]
					break
				}
			}
			if col == nil {
				continue
			}

			for i := idx1; i <= idx2 && i < len(col.Values); i++ {
				sum += col.Values[i]
				count++
			}
		}

		if count == 0 {
			dialog.ShowError(fmt.Errorf("no valid points found in selected pairs"), w)
			return
		}

		mean := sum / float64(count)
		logAction(fmt.Sprintf("Fit: Calculated baseline mean = %.4f from %d points in %d pairs", mean, count, len(lightCurvePlot.SelectedPairs)))

		if mean == 0 {
			dialog.ShowError(fmt.Errorf("baseline mean is zero - cannot scale"), w)
			return
		}

		// Extract noise before scaling: noise = value/mean - 1.0 (equivalent to (value - mean)/mean)
		var noise []float64
		for _, pair := range lightCurvePlot.SelectedPairs {
			idx1 := pair.Point1DataIdx
			idx2 := pair.Point2DataIdx
			if idx1 > idx2 {
				idx1, idx2 = idx2, idx1
			}

			var col *LightCurveColumn
			for i := range loadedLightCurveData.Columns {
				if loadedLightCurveData.Columns[i].Name == pair.Point1Series {
					col = &loadedLightCurveData.Columns[i]
					break
				}
			}
			if col == nil {
				continue
			}

			for i := idx1; i <= idx2 && i < len(col.Values); i++ {
				noise = append(noise, col.Values[i]/mean-1.0)
			}
		}
		// Scale all column values to unity
		scaleFactor := mean
		logAction(fmt.Sprintf("Fit: Scaling all light curves by 1/%.4f to set baseline mean to unity", scaleFactor))
		for colIdx := range loadedLightCurveData.Columns {
			for i := range loadedLightCurveData.Columns[colIdx].Values {
				loadedLightCurveData.Columns[colIdx].Values[i] /= scaleFactor
			}
		}

		lightCurvePlot.BaselineValue = 1.0
		lightCurvePlot.ShowBaselineLine = true
		lightCurvePlot.SelectedPairs = nil
		baselineScaledToUnity = true

		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY/scaleFactor, savedMaxY/scaleFactor)

		// Show noise histogram if we have enough points and diagnostics are enabled
		if len(noise) >= 2 {
			histImg, noiseMean, sigma, err := createNoiseHistogramImage(noise, lastDiffractionTitle, 800, 500)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to create noise histogram: %v", err), w)
			} else {
				noiseSigma = sigma
				if mcShowDiagnosticsCheck.Checked {
					histWindow := a.NewWindow("Baseline Noise Histogram")
					histCanvas := canvas.NewImageFromImage(histImg)
					histCanvas.FillMode = canvas.ImageFillOriginal
					histWindow.SetContent(container.NewScroll(histCanvas))
					histWindow.Resize(fyne.NewSize(850, 550))
					histWindow.CenterOnScreen()
					histWindow.Show()
				}
				logAction(fmt.Sprintf("Fit: Extracted baseline noise: %d points, mean=%.6f, sigma=%.6f", len(noise), noiseMean, noiseSigma))
			}
		}

		fitStatusLabel.SetText(fmt.Sprintf("Scaled to unity (baseline=%.4f, %d points) — noise sigma=%.6f", mean, count, noiseSigma))
		calcBaselineMeanBtn.Importance = widget.WarningImportance
		calcBaselineMeanBtn.Refresh()
	})
	calcBaselineMeanBtn.Importance = widget.HighImportance
	resetNormalizeBtn = func() {
		calcBaselineMeanBtn.Importance = widget.HighImportance
		calcBaselineMeanBtn.Refresh()
	}

	// Search range for observation path offset
	searchInitialOffsetEntry := NewFocusLossEntry()
	searchInitialOffsetEntry.SetPlaceHolder("")
	searchFinalOffsetEntry := NewFocusLossEntry()
	searchFinalOffsetEntry.SetPlaceHolder("")
	searchNumStepsEntry := widget.NewEntry()
	searchNumStepsEntry.SetPlaceHolder("")

	showSearchRangeHelp := func() {
		dialog.ShowInformation("Search range for observation path offsets",
			"You may wish to narrow the search range after the initial full range search has completed because the Monte Carlo process will then take less time.\n\nRemember to click Run Fit Search after making changes to the search range because the Monte Carlo trials use the path range from the last click on Run Fit Search.", w)
	}

	// updateSearchNumSteps recalculates the number of steps from the current offset
	// range and the pixel resolution of the diffraction image parameters file.
	updateSearchNumSteps := func() {
		initVal, err1 := strconv.ParseFloat(strings.TrimSpace(searchInitialOffsetEntry.Text), 64)
		finalVal, err2 := strconv.ParseFloat(strings.TrimSpace(searchFinalOffsetEntry.Text), 64)
		if err1 != nil || err2 != nil || lastDiffractionParamsPath == "" {
			return
		}
		go func() {
			file, err := os.Open(lastDiffractionParamsPath)
			if err != nil {
				return
			}
			params, err := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				fmt.Printf("Failed to close parameters file: %v\n", closeErr)
			}
			if err != nil || params.FundamentalPlaneWidthNumPoints == 0 {
				return
			}
			stepSize := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
			if stepSize <= 0 {
				return
			}
			numSteps := int(math.Abs(finalVal-initVal)/stepSize) + 1
			fyne.Do(func() {
				searchNumStepsEntry.SetText(fmt.Sprintf("%d", numSteps))
			})
		}()
	}

	var showSearchRangeBtn *widget.Button
	suppressSearchRangeEnable := false
	searchInitialOffsetEntry.OnSubmitted = func(_ string) { updateSearchNumSteps() }
	searchFinalOffsetEntry.OnSubmitted = func(_ string) { updateSearchNumSteps() }
	searchInitialOffsetEntry.OnChanged = func(_ string) {
		if resetFitButtons != nil {
			resetFitButtons()
		}
		if !suppressSearchRangeEnable && showSearchRangeBtn != nil {
			showSearchRangeBtn.Enable()
		}
	}
	searchFinalOffsetEntry.OnChanged = func(_ string) {
		if resetFitButtons != nil {
			resetFitButtons()
		}
		if !suppressSearchRangeEnable && showSearchRangeBtn != nil {
			showSearchRangeBtn.Enable()
		}
	}

	// Preview window for search range paths — kept so we can update in place
	var searchPreviewWindow fyne.Window

	showSearchRangePreview := func() {
		initText := strings.TrimSpace(searchInitialOffsetEntry.Text)
		finalText := strings.TrimSpace(searchFinalOffsetEntry.Text)
		if initText == "" || finalText == "" {
			return
		}
		initVal, err1 := strconv.ParseFloat(initText, 64)
		finalVal, err2 := strconv.ParseFloat(finalText, 64)
		if err1 != nil || err2 != nil {
			return
		}
		if lastDiffractionParamsPath == "" {
			return
		}
		go func() {
			file, err := os.Open(lastDiffractionParamsPath)
			if err != nil {
				return
			}
			params, err := parseOccultationParameters(file)
			if closeErr := file.Close(); closeErr != nil {
				fmt.Printf("Failed to close parameters file: %v\n", closeErr)
			}
			if err != nil {
				return
			}

			// Auto-calculate the number of steps from image resolution
			if params.FundamentalPlaneWidthNumPoints > 0 {
				stepSize := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
				if stepSize > 0 {
					numSteps := int(math.Abs(finalVal-initVal)/stepSize) + 1
					fyne.Do(func() {
						searchNumStepsEntry.SetText(fmt.Sprintf("%d", numSteps))
					})
				}
			}
			baseImg, err := lightcurve.LoadImageFromFile(filepath.Join(appDir, "diffractionImage8bit.png"))
			if err != nil {
				return
			}
			fundPlaneWidthPts := params.FundamentalPlaneWidthNumPoints
			if params.PathToExternalImage != "" {
				fundPlaneWidthPts = baseImg.Bounds().Dx()
			}
			// Draw the initial offset path
			path1 := &lightcurve.ObservationPath{
				DxKmPerSec:               params.DXKmPerSec,
				DyKmPerSec:               params.DYKmPerSec,
				PathOffsetFromCenterKm:   initVal,
				FundamentalPlaneWidthKm:  params.FundamentalPlaneWidthKm,
				FundamentalPlaneWidthPts: fundPlaneWidthPts,
			}
			if err := path1.ComputePathFromVelocity(); err != nil {
				return
			}
			annotatedImg, err := lightcurve.DrawObservationLineOnImage(baseImg, path1)
			if err != nil {
				return
			}
			// Draw the final offset path on the same image
			path2 := &lightcurve.ObservationPath{
				DxKmPerSec:               params.DXKmPerSec,
				DyKmPerSec:               params.DYKmPerSec,
				PathOffsetFromCenterKm:   finalVal,
				FundamentalPlaneWidthKm:  params.FundamentalPlaneWidthKm,
				FundamentalPlaneWidthPts: fundPlaneWidthPts,
			}
			if err := path2.ComputePathFromVelocity(); err != nil {
				return
			}
			annotatedImg, err = lightcurve.DrawObservationLineOnImage(annotatedImg, path2)
			if err != nil {
				return
			}
			// Save the search range image to the results folder
			if resultsFolder != "" {
				var buf bytes.Buffer
				if err := png.Encode(&buf, annotatedImg); err == nil {
					savePath := filepath.Join(resultsFolder, "searchRange.png")
					if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
						fmt.Printf("Warning: could not save searchRange.png: %v\n", err)
					}
				}
			}
			fyne.Do(func() {
				if searchPreviewWindow != nil {
					searchPreviewWindow.Close()
				}
				previewTitle := fmt.Sprintf("Search Range: %.3f to %.3f km", initVal, finalVal)
				if lastDiffractionTitle != "" {
					previewTitle = lastDiffractionTitle + " — " + previewTitle
				}
				searchPreviewWindow = a.NewWindow(previewTitle)
				previewCanvas := canvas.NewImageFromImage(annotatedImg)
				previewCanvas.FillMode = canvas.ImageFillContain
				searchPreviewWindow.SetContent(previewCanvas)
				searchPreviewWindow.Resize(fyne.NewSize(600, 600))
				searchPreviewWindow.CenterOnScreen()
				searchPreviewWindow.Show()
			})
		}()
	}

	searchRangeForm := widget.NewForm(
		&widget.FormItem{Text: "Initial offset", Widget: searchInitialOffsetEntry},
		&widget.FormItem{Text: "Final offset", Widget: searchFinalOffsetEntry},
		&widget.FormItem{Text: "Number of steps", Widget: searchNumStepsEntry},
	)

	showSearchRangeBtn = widget.NewButton("Show search range", func() {
		showSearchRangePreview()
	})
	showSearchRangeBtn.Importance = widget.HighImportance
	showSearchRangeBtn.Disable()

	searchRangeTitle := widget.NewLabel("Search range for observation path offset")
	searchRangeTitle.TextStyle = fyne.TextStyle{Bold: true}
	searchRangeTitleRow := container.NewBorder(nil, nil, nil, showSearchRangeBtn, searchRangeTitle)
	searchRangeCard := widget.NewCard("", "", container.NewVBox(searchRangeTitleRow, searchRangeForm))

	fitProgressBar := widget.NewProgressBar()
	fitProgressBar.Hide()

	// Fit abort button
	var fitAbortFlag atomic.Bool
	var fitAbortBtn *widget.Button
	fitAbortBtn = widget.NewButton("Abort", func() {
		fitAbortFlag.Store(true)
		fitAbortBtn.Disable()
	})
	fitAbortBtn.Importance = widget.DangerImportance
	fitAbortBtn.Hide()

	// Fit button - checks preconditions and reports readiness
	var fitBtn *widget.Button
	fitBtn = widget.NewButton("Run fit search", func() {
		var issues []string

		// Check 1: Single curve selected
		if len(displayedCurves) != 1 {
			issues = append(issues, fmt.Sprintf("A single light curve must be selected (currently %d displayed)", len(displayedCurves)))
		}

		// Check 2: Scaled to unity
		if !baselineScaledToUnity {
			issues = append(issues, "Light curve has not been scaled to unity")
		}

		// Check 3: Parameters file from IOTAdiffraction run
		if lastDiffractionParamsPath == "" {
			issues = append(issues, "No diffraction has been generated (run IOTAdiffraction first)")
		}

		// Check 4: Diffraction image available
		if _, err := os.Stat(filepath.Join(appDir, "targetImage16bit.png")); os.IsNotExist(err) {
			issues = append(issues, "No diffraction image available (targetImage16bit.png not found)")
		}

		if len(issues) > 0 {
			msg := "Cannot perform fit. The following conditions are not met:\n\n"
			for i, issue := range issues {
				msg += fmt.Sprintf("%d. %s\n", i+1, issue)
			}
			dialog.ShowError(fmt.Errorf("%s", msg), w)
		} else {
			runFitBody := func() {
				// Clear previous fit results so edges don't accumulate across runs
				lastFitResult = nil
				lastFitParams = nil
				lastFitCandidates = nil
				lastFitTargetTimes = nil
				lastFitTargetValues = nil
				theorySeries = nil
				lightCurvePlot.SetVerticalLines(nil, false)
				lightCurvePlot.SetSigmaLines(nil, false)

				// Load parameters from the file used to generate the diffraction image
				file, err := os.Open(lastDiffractionParamsPath)
				if err != nil {
					dialog.ShowError(fmt.Errorf("could not open parameters file: %v", err), w)
					return
				}
				params, err := parseOccultationParameters(file)
				if closeErr := file.Close(); closeErr != nil {
					dialog.ShowError(fmt.Errorf("failed to close file: %w", closeErr), w)
				}
				if err != nil {
					dialog.ShowError(fmt.Errorf("could not parse parameters: %v", err), w)
					return
				}

				// Find the single displayed column index
				var displayedColIdx int
				for k := range displayedCurves {
					displayedColIdx = k
					break
				}

				// Verify timestamps are real (not all zeros)
				allZero := true
				for _, t := range loadedLightCurveData.TimeValues {
					if t != 0 {
						allZero = false
						break
					}
				}
				if allZero {
					dialog.ShowError(fmt.Errorf("timestamps are all zero — real timestamps are required for fitting"), w)
					return
				}

				// Collect target times and values within the frame range
				col := loadedLightCurveData.Columns[displayedColIdx]
				var targetTimes, targetValues []float64
				for i, val := range col.Values {
					frameNum := loadedLightCurveData.FrameNumbers[i]
					if frameRangeStart > 0 && frameNum < frameRangeStart {
						continue
					}
					if frameRangeEnd > 0 && frameNum > frameRangeEnd {
						continue
					}
					targetTimes = append(targetTimes, loadedLightCurveData.TimeValues[i])
					targetValues = append(targetValues, val)
				}

				if len(targetTimes) < 2 {
					dialog.ShowError(fmt.Errorf("not enough data points in displayed range for fitting"), w)
					return
				}

				params.ExposureTimeSecs = lastCsvExposureSecs
				if lastCsvExposureSecs == 0 {
					logAction("Fit: camera exposure time not set (0 seconds)")
				} else {
					logAction(fmt.Sprintf("Fit: camera exposure time: %.6f seconds", lastCsvExposureSecs))
				}
				// Check if search range fields are all filled in
				searchInitial := strings.TrimSpace(searchInitialOffsetEntry.Text)
				searchFinal := strings.TrimSpace(searchFinalOffsetEntry.Text)
				searchSteps := strings.TrimSpace(searchNumStepsEntry.Text)

				if searchInitial != "" && searchFinal != "" && searchSteps != "" {
					initVal, err := strconv.ParseFloat(searchInitial, 64)
					if err != nil {
						dialog.ShowError(fmt.Errorf("invalid Initial offset: %v", err), w)
						return
					}
					finalVal, err := strconv.ParseFloat(searchFinal, 64)
					if err != nil {
						dialog.ShowError(fmt.Errorf("invalid Final offset: %v", err), w)
						return
					}
					stepsVal, err := strconv.Atoi(searchSteps)
					if err != nil || stepsVal < 1 {
						dialog.ShowError(fmt.Errorf("number of steps must be a positive integer"), w)
						return
					}
					fitProgressBar.SetValue(0)
					fitProgressBar.Show()
					fitBtn.Disable()
					fitAbortFlag.Store(false)
					fitAbortBtn.Show()
					fitAbortBtn.Enable()
					go func() {
						fsr, err := runFitSearch(params, targetTimes, targetValues, initVal, finalVal, stepsVal, &fitAbortFlag, func(progress float64) {
							fyne.Do(func() {
								fitProgressBar.SetValue(progress)
							})
						})
						fyne.Do(func() {
							fitProgressBar.Hide()
							fitAbortBtn.Hide()
							fitBtn.Enable()
							if err != nil {
								dialog.ShowError(err, w)
							} else {
								fr, err := displayFitSearchResult(a, w, params, fsr, targetTimes, targetValues, mcShowDiagnosticsCheck.Checked)
								if err != nil {
									dialog.ShowError(err, w)
								} else {
									lastFitResult = fr
									paramsCopy := *params
									lastFitParams = &paramsCopy
									// Save all precomputed curves from the search for Monte Carlo
									lastFitCandidates = make([]*precomputedCurve, 0, len(fsr.results))
									for _, sr := range fsr.results {
										lastFitCandidates = append(lastFitCandidates, sr.pc)
									}
									lastFitBestIdx = fsr.bestIdx
									logAction(fmt.Sprintf("Fit search: %d of %d path offset steps succeeded, %d candidate curves saved for Monte Carlo", len(fsr.results), stepsVal, len(lastFitCandidates)))
									lastFitTargetTimes = targetTimes
									lastFitTargetValues = targetValues
									fitBtn.Importance = widget.WarningImportance
									fitBtn.Refresh()
									if enablePostFitButtons != nil {
										enablePostFitButtons()
									}
								}
							}
						})
					}()
				} else {
					fr, pc, err := performFit(a, w, params, targetTimes, targetValues, mcShowDiagnosticsCheck.Checked)
					if err != nil {
						dialog.ShowError(err, w)
					} else {
						lastFitResult = fr
						paramsCopy := *params
						lastFitParams = &paramsCopy
						lastFitCandidates = []*precomputedCurve{pc}
						lastFitTargetTimes = targetTimes
						lastFitTargetValues = targetValues
						fitBtn.Importance = widget.WarningImportance
						fitBtn.Refresh()
						if enablePostFitButtons != nil {
							enablePostFitButtons()
						}
					}
				}
			}
			if !trimPerformed {
				noBtn := widget.NewButton("No", nil)
				noBtn.Importance = widget.HighImportance
				yesBtn := widget.NewButton("Yes, run anyway", nil)
				trimDlg := dialog.NewCustom("Trim not set",
					"",
					container.NewVBox(
						widget.NewLabel("A Set trim operation is recommended before running a fit search.\n\nDo you want to run anyway?"),
						container.NewHBox(layout.NewSpacer(), noBtn, yesBtn),
					), w)
				noBtn.OnTapped = func() { trimDlg.Hide() }
				yesBtn.OnTapped = func() { trimDlg.Hide(); runFitBody() }
				trimDlg.Show()
			} else {
				runFitBody()
			}
		}
	})
	fitBtn.Importance = widget.HighImportance

	// Monte Carlo UI elements
	mcNumTrialsEntry := widget.NewEntry()
	mcNumTrialsEntry.SetText("1000")
	mcNumTrialsEntry.SetPlaceHolder("number of trials")
	mcProgressBar := widget.NewProgressBar()
	mcProgressBar.Hide()

	var mcAbortFlag atomic.Bool
	var mcAbortBtn *widget.Button
	mcAbortBtn = widget.NewButton("Abort", func() {
		mcAbortFlag.Store(true)
		mcAbortBtn.Disable()
	})
	mcAbortBtn.Importance = widget.DangerImportance
	mcAbortBtn.Hide()

	var mcBtn *widget.Button
	mcBtn = widget.NewButton("Run Monte Carlo", func() {
		if lastFitResult == nil || lastFitParams == nil {
			dialog.ShowError(fmt.Errorf("no fit result available — run a fit first"), w)
			return
		}
		if len(lastFitCandidates) == 0 {
			dialog.ShowError(fmt.Errorf("no candidate curves available — run a fit first"), w)
			return
		}
		if noiseSigma == 0 {
			dialog.ShowError(fmt.Errorf("no noise sigma available — run Normalize Baseline first"), w)
			return
		}
		numTrials, err := strconv.Atoi(mcNumTrialsEntry.Text)
		if err != nil || numTrials < 1 {
			dialog.ShowError(fmt.Errorf("number of Monte Carlo trials must be a positive integer"), w)
			return
		}
		// Capture all fit state on the main thread before launching the goroutine.
		// This ensures MC always uses the candidates and fit result that were in
		// place when the user clicked Run Monte Carlo, regardless of any concurrent
		// changes (e.g., a new fit started during the MC run).
		mcCandidates := lastFitCandidates
		if mcNarrowSearchCheck.Checked && len(mcCandidates) > 41 {
			center := lastFitBestIdx
			lo := center - 20
			if lo < 0 {
				lo = 0
			}
			hi := center + 20 + 1 // exclusive upper bound
			if hi > len(mcCandidates) {
				hi = len(mcCandidates)
			}
			mcCandidates = mcCandidates[lo:hi]
			logAction(fmt.Sprintf("MC narrow search: using %d candidates (indices %d–%d) around best offset at index %d", len(mcCandidates), lo, hi-1, center))
		}
		mcFitResult := lastFitResult
		mcFitParams := lastFitParams
		mcTargetTimes := lastFitTargetTimes
		mcTargetValues := lastFitTargetValues
		mcNoiseSigma := noiseSigma
		mcTitle := lastDiffractionTitle
		logAction(fmt.Sprintf("Run Monte Carlo: %d candidate curves from fit search, noise sigma=%.6f", len(mcCandidates), mcNoiseSigma))
		mcProgressBar.SetValue(0)
		mcProgressBar.Show()
		mcBtn.Disable()
		mcAbortFlag.Store(false)
		mcAbortBtn.Show()
		mcAbortBtn.Enable()
		go func() {
			// Yield for two Fyne render frames (~32 ms at 60 fps) before starting trials.
			// Without this, a fast MC run can post fyne.Do(Hide) to the event queue
			// before the Show() calls above have been rendered, making the progress bar
			// and Abort button appear to never show up (rare race condition).
			time.Sleep(32 * time.Millisecond)
			result, err := runMonteCarloTrials(mcCandidates, mcFitResult, mcNoiseSigma, numTrials, &mcAbortFlag, func(progress float64) {
				fyne.Do(func() {
					mcProgressBar.SetValue(progress)
				})
			})
			fyne.Do(func() {
				mcProgressBar.Hide()
				mcAbortBtn.Hide()
				mcBtn.Enable()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				lastMCResult = result
				mcBtn.Importance = widget.WarningImportance
				mcBtn.Refresh()
				msg := fmt.Sprintf("Monte Carlo results (%d trials):\n\n", result.numTrials)
				for i := 0; i < result.numEdges && i < len(mcFitResult.edgeTimes); i++ {
					absTime := mcFitResult.edgeTimes[i] + mcFitResult.bestShift
					ts := formatSecondsAsTimestamp(absTime)
					msg += fmt.Sprintf("  Edge %d: %s +/- %.4f sec (3 sigma)\n", i+1, ts, 3*result.edgeStds[i])
				}
				if result.numEdges == 2 {
					fitDuration := math.Abs(mcFitResult.edgeTimes[1] - mcFitResult.edgeTimes[0])
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					msg += fmt.Sprintf("\n  Duration: %.4f +/- %.4f sec (3 sigma)\n", fitDuration, 3*durationStd)
				}
				fmt.Print(msg)

				// Log Monte Carlo results
				logAction(fmt.Sprintf("Monte Carlo results (%d trials):", result.numTrials))
				for i := 0; i < result.numEdges; i++ {
					logAction(fmt.Sprintf("  Edge %d: mean=%.4f sec, 3 sigma=%.4f sec", i+1, result.edgeMeans[i], 3*result.edgeStds[i]))
				}
				if result.numEdges == 2 {
					durationMean := math.Abs(result.edgeMeans[1] - result.edgeMeans[0])
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					logAction(fmt.Sprintf("  Duration: mean=%.4f sec, 3 sigma=%.4f sec", durationMean, 3*durationStd))
				}

				// Final report: fit edge times (as timestamps) with MC uncertainty
				logAction("--- Final Report ---")
				logAction(fmt.Sprintf("  NCC=%.4f, path offset=%.3f km", mcFitResult.bestNCC, mcFitParams.PathPerpendicularOffsetKm))
				if mcFitResult.bestScale > 0 {
					logAction(fmt.Sprintf("  Percent drop: %.2f%%", mcFitResult.bestScale*100.0))
				}
				if lastCsvExposureSecs > 0 {
					logAction(fmt.Sprintf("  Camera exposure time: %.6f seconds", lastCsvExposureSecs))
				} else {
					logAction("  Camera exposure time: not set")
				}
				for i, et := range mcFitResult.edgeTimes {
					absTime := et + mcFitResult.bestShift
					ts := formatSecondsAsTimestamp(absTime)
					if i < result.numEdges {
						logAction(fmt.Sprintf("  Edge %d: %s +/- %.4f sec (3 sigma)", i+1, ts, 3*result.edgeStds[i]))
					} else {
						logAction(fmt.Sprintf("  Edge %d: %s", i+1, ts))
					}
				}
				if len(mcFitResult.edgeTimes) == 2 && result.numEdges == 2 {
					fitDuration := math.Abs((mcFitResult.edgeTimes[1] + mcFitResult.bestShift) - (mcFitResult.edgeTimes[0] + mcFitResult.bestShift))
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					logAction(fmt.Sprintf("  Duration: %.4f +/- %.4f sec (3 sigma)", fitDuration, 3*durationStd))
				}
				logAction("--- End Report ---")

				summaryLabel := widget.NewLabel(msg)
				summaryLabel.Wrapping = fyne.TextWrapWord

				var mcContainer *fyne.Container
				if mcShowDiagnosticsCheck.Checked {
					// Show individual trial edge times (max 100)
					trialsMsg := "Individual trial edge times:\n"
					numCompleted := 0
					if result.numEdges > 0 {
						numCompleted = len(result.edgeAll[0])
					}
					maxDisplay := numCompleted
					if maxDisplay > 100 {
						maxDisplay = 100
					}
					for t := 0; t < maxDisplay; t++ {
						trialsMsg += fmt.Sprintf("  Trial %3d:", t+1)
						for i := 0; i < result.numEdges; i++ {
							trialsMsg += fmt.Sprintf("  Edge %d=%.4f", i+1, result.edgeAll[i][t])
						}
						if result.numEdges == 2 {
							trialsMsg += fmt.Sprintf("  Dur=%.4f", math.Abs(result.edgeAll[1][t]-result.edgeAll[0][t]))
						}
						if t < len(result.pathOffsets) {
							trialsMsg += fmt.Sprintf("  Path=%.3f km", result.pathOffsets[t])
						}
						trialsMsg += "\n"
					}
					if numCompleted > 100 {
						trialsMsg += fmt.Sprintf("  ... (%d more trials not shown)\n", numCompleted-100)
					}
					fmt.Print(trialsMsg)
					trialsLabel := widget.NewLabel(trialsMsg)
					trialsLabel.TextStyle.Monospace = true
					trialsScroll := container.NewScroll(trialsLabel)
					trialsScroll.SetMinSize(fyne.NewSize(750, 300))
					mcContainer = container.NewVBox(summaryLabel, trialsScroll)

					// Show edge and duration histograms
					for i := 0; i < result.numEdges; i++ {
						if len(result.edgeAll[i]) < 2 {
							continue
						}
						histImg, err := createHistogramImage(
							result.edgeAll[i],
							fmt.Sprintf("Edge %d Times", i+1),
							"Time (seconds)",
							mcTitle,
							900, 500,
						)
						if err != nil {
							fmt.Printf("Failed to create Edge %d histogram: %v", i+1, err)
							continue
						}
						histWin := a.NewWindow(fmt.Sprintf("Monte Carlo — Edge %d Histogram", i+1))
						histCanvas := canvas.NewImageFromImage(histImg)
						histCanvas.FillMode = canvas.ImageFillContain
						histWin.SetContent(histCanvas)
						histWin.Resize(fyne.NewSize(950, 550))
						histWin.CenterOnScreen()
						histWin.Show()
					}

					// Show duration histogram if 2 edges
					if result.numEdges == 2 && len(result.edgeAll[0]) >= 2 {
						n := len(result.edgeAll[0])
						durations := make([]float64, n)
						for t := 0; t < n; t++ {
							durations[t] = math.Abs(result.edgeAll[1][t] - result.edgeAll[0][t])
						}
						histImg, err := createHistogramImage(
							durations,
							"Event Duration",
							"Duration (seconds)",
							mcTitle,
							900, 500,
						)
						if err != nil {
							fmt.Printf("Failed to create duration histogram: %v", err)
						} else {
							histWin := a.NewWindow("Monte Carlo — Duration Histogram")
							histCanvas := canvas.NewImageFromImage(histImg)
							histCanvas.FillMode = canvas.ImageFillContain
							histWin.SetContent(histCanvas)
							histWin.Resize(fyne.NewSize(950, 550))
							histWin.CenterOnScreen()
							histWin.Show()
						}
					}
				} else {
					mcSpacer := canvas.NewRectangle(color.Transparent)
					mcSpacer.SetMinSize(fyne.NewSize(750, 0))
					mcContainer = container.NewVBox(mcSpacer, summaryLabel)
				}
				dialog.ShowCustom("Monte Carlo Edge Time Uncertainty", "OK", mcContainer, w)
				// Create a fit overlay plot with ±3σ edge uncertainty lines, using the
				// scale-adjusted theoretical curve (bestScale from the post-fit scale search).
				if len(mcTargetTimes) > 0 && len(mcTargetValues) > 0 {
					mcScale := mcFitResult.bestScale
					if mcScale == 0 {
						mcScale = 1.0
					}
					mcScaledCurve := make([]timeIntensityPoint, len(mcFitResult.curve))
					for i, pt := range mcFitResult.curve {
						mcScaledCurve[i] = timeIntensityPoint{
							time:      pt.time,
							intensity: pt.intensity*mcScale + (1.0 - mcScale),
						}
					}
					mcScaledSampledVals := make([]float64, len(mcFitResult.sampledVals))
					for i, v := range mcFitResult.sampledVals {
						mcScaledSampledVals[i] = v*mcScale + (1.0 - mcScale)
					}
					mcOverlayImg, err := createOverlayPlotImage(mcScaledCurve, mcFitResult.bestShift, mcFitResult.edgeTimes, mcTargetTimes, mcTargetValues, mcFitResult.sampledTimes, mcScaledSampledVals, mcTitle, 1200, 500, result.edgeStds)
					if err != nil {
						fmt.Printf("Failed to create MC overlay plot: %v\n", err)
					} else {
						// Save to the results folder
						savePath := filepath.Join(appDir, "fitPlotMC.png")
						if resultsFolder != "" {
							savePath = filepath.Join(resultsFolder, "fitPlotMC.png")
						}
						var buf bytes.Buffer
						if err := png.Encode(&buf, mcOverlayImg); err != nil {
							fmt.Printf("Warning: could not encode fitPlotMC.png: %v\n", err)
						} else if err := os.WriteFile(savePath, buf.Bytes(), 0644); err != nil {
							fmt.Printf("Warning: could not save fitPlotMC.png: %v\n", err)
						}

						mcOverlayWin := a.NewWindow("Fit Result with Monte Carlo Edge Uncertainty (±3σ)")
						mcOverlayCanvas := canvas.NewImageFromImage(mcOverlayImg)
						mcOverlayCanvas.FillMode = canvas.ImageFillContain
						mcOverlayWin.SetContent(mcOverlayCanvas)
						mcOverlayWin.Resize(fyne.NewSize(1250, 550))
						mcOverlayWin.CenterOnScreen()
						mcOverlayWin.Show()
					}

					// Update main plot: overlay theoretical curve, edge lines, and ±3σ lines
					theoryPoints := make([]PlotPoint, len(mcScaledCurve))
					for i, pt := range mcScaledCurve {
						theoryPoints[i] = PlotPoint{
							X:     pt.time + mcFitResult.bestShift,
							Y:     pt.intensity,
							Index: -1,
						}
					}
					theorySeries = &PlotSeries{
						Points:   theoryPoints,
						Color:    color.RGBA{R: 255, G: 170, B: 170, A: 255},
						Name:     "Theoretical (fit)",
						LineOnly: true,
					}
					edgeXVals := make([]float64, len(mcFitResult.edgeTimes))
					for i, et := range mcFitResult.edgeTimes {
						edgeXVals[i] = et + mcFitResult.bestShift
					}
					lightCurvePlot.SetVerticalLines(edgeXVals, true)
					var sigmaXVals []float64
					for i, et := range mcFitResult.edgeTimes {
						if i < len(result.edgeStds) {
							edgeX := et + mcFitResult.bestShift
							sigma3 := 3.0 * result.edgeStds[i]
							sigmaXVals = append(sigmaXVals, edgeX-sigma3, edgeX+sigma3)
						}
					}
					lightCurvePlot.SetSigmaLines(sigmaXVals, len(sigmaXVals) > 0)
					lightCurvePlot.ShowBaselineLine = false
					savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
					rebuildPlot()
					lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
				}
			})
		}()
	})
	mcBtn.Importance = widget.HighImportance
	mcBtn.Disable()

	var nieAbortFlag atomic.Bool
	var nieAbortBtn *widget.Button
	nieAbortBtn = widget.NewButton("Abort NIE", func() {
		nieAbortFlag.Store(true)
		nieAbortBtn.Disable()
	})
	nieAbortBtn.Importance = widget.DangerImportance
	nieAbortBtn.Hide()

	var runNieBtn *widget.Button
	runNieBtn = widget.NewButton("Run NIE analysis", func() {
		if lastFitResult == nil {
			dialog.ShowError(fmt.Errorf("no fit result available â run a fit first"), w)
			return
		}
		if len(lastFitResult.edgeTimes) < 2 {
			dialog.ShowError(fmt.Errorf("NIE requires a two-edge fit result"), w)
			return
		}
		if noiseSigma == 0 {
			dialog.ShowError(fmt.Errorf("no noise sigma available â run Normalize Baseline first"), w)
			return
		}
		if len(lastFitTargetTimes) == 0 {
			dialog.ShowError(fmt.Errorf("no target light curve available â run a fit first"), w)
			return
		}
		mcTrials, err := strconv.Atoi(mcNumTrialsEntry.Text)
		if err != nil || mcTrials < 1 {
			dialog.ShowError(fmt.Errorf("number of trials must be a positive integer"), w)
			return
		}
		numTrials := mcTrials * 10
		nPoints := len(lastFitTargetTimes)

		// launchNIE starts the goroutine given a known windowWidth, eventDrop, and selection source.
		launchNIE := func(windowWidth int, eventDrop float64, manualSelection bool) {
			logAction(fmt.Sprintf("NIE: starting %d trials, nPoints=%d, windowWidth=%d, noiseSigma=%.6f", numTrials, nPoints, windowWidth, noiseSigma))
			mcProgressBar.SetValue(0)
			mcProgressBar.Show()
			runNieBtn.Disable()
			nieAbortFlag.Store(false)
			nieAbortBtn.Show()
			nieAbortBtn.Enable()
			go func() {
				minMeans, err := runNIETrials(numTrials, nPoints, windowWidth, noiseSigma, &nieAbortFlag, func(progress float64) {
					fyne.Do(func() {
						mcProgressBar.SetValue(progress)
					})
				})
				fyne.Do(func() {
					mcProgressBar.Hide()
					nieAbortBtn.Hide()
					runNieBtn.Enable()
					if err != nil {
						dialog.ShowError(err, w)
						return
					}
					histImg, nieMean, nieSigma, err := createNIEHistogramImage(minMeans, windowWidth, eventDrop, lastDiffractionTitle, 800, 500)
					if err != nil {
						dialog.ShowError(fmt.Errorf("failed to create NIE histogram: %v", err), w)
						return
					}
					logAction(fmt.Sprintf("NIE: %d trials completed, min-window-mean distribution: mean=%.6f, sigma=%.6f", len(minMeans), nieMean, nieSigma))
					nieWindowTitle := "Noise Induced Drop study — fit-derived"
					if manualSelection {
						nieWindowTitle = "Noise Induced Drop study — manual selection"
					}
					histWindow := a.NewWindow(nieWindowTitle)
					histCanvas := canvas.NewImageFromImage(histImg)
					histCanvas.FillMode = canvas.ImageFillOriginal
					histWindow.SetContent(container.NewScroll(histCanvas))
					histWindow.Resize(fyne.NewSize(850, 550))
					histWindow.CenterOnScreen()
					histWindow.Show()
					runNieBtn.Importance = widget.WarningImportance
					runNieBtn.Refresh()
				})
			}()
		}

		// Compute window width from event edges and event drop from the fit.
		windowWidth := 0
		edge1Abs := lastFitResult.edgeTimes[0] + lastFitResult.bestShift
		edge2Abs := lastFitResult.edgeTimes[1] + lastFitResult.bestShift
		if edge1Abs > edge2Abs {
			edge1Abs, edge2Abs = edge2Abs, edge1Abs
		}
		for _, t := range lastFitTargetTimes {
			if t >= edge1Abs && t <= edge2Abs {
				windowWidth++
			}
		}
		if windowWidth < 1 {
			dialog.ShowError(fmt.Errorf("no target samples found between event edges â check fit result"), w)
			return
		}
		// Event drop = 1 - bestScale, where bestScale is the amplitude scale factor
		// found by the post-fit scale search (scaledTLC = bestTLC*scale + (1-scale)).
		// bestScale==0 (zero value, search not run) maps naturally to eventDrop=1.0 (full drop).
		eventDrop := 1.0 - lastFitResult.bestScale
		logAction(fmt.Sprintf("NIE fit-derived: bestScale=%.4f, eventDrop=%.4f", lastFitResult.bestScale, eventDrop))
		launchNIE(windowWidth, eventDrop, false)
	})
	runNieBtn.Importance = widget.HighImportance
	runNieBtn.Disable()

	// buildSodisFill creates a sodisPreFill with an optional occultation override.
	buildSodisFill := func(occultationOverride string, onSave func()) {
		occTitle := lastDiffractionTitle
		if lastFitParams != nil && lastFitParams.Title != "" {
			occTitle = lastFitParams.Title
		}
		// Compute observer-corrected t0 using the persisted observer GPS location.
		var computedObserverT0 time.Time
		if lastLoadedOccelmntXml != "" && lastObserverLocationSet {
			_, obsT0, _, t0Err := ObserverT0CorrectionFromOWC(
				lastLoadedOccelmntXml,
				lastObserverLatDeg, lastObserverLonDeg, lastObserverAltMeters,
				0, 0, 0)
			if t0Err == nil {
				computedObserverT0 = obsT0
			}
		}
		// Read "Event Time (UT)" from the details file if present.
		var detailsEventTimeUT string
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
			if dirEntries, derr := os.ReadDir(obsDir); derr == nil {
				for _, entry := range dirEntries {
					if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "detail") {
						if fileData, rerr := os.ReadFile(filepath.Join(obsDir, entry.Name())); rerr == nil {
							reader := csv.NewReader(bytes.NewReader(fileData))
							reader.FieldsPerRecord = -1
							reader.TrimLeadingSpace = true
							for {
								record, rerr2 := reader.Read()
								if rerr2 != nil {
									break
								}
								if len(record) >= 2 && strings.TrimSpace(record[0]) == "Event Time (UT)" && strings.TrimSpace(record[1]) != "" {
									detailsEventTimeUT = strings.TrimSpace(record[1])
									break
								}
							}
						}
						break
					}
				}
			}
		}
		showSodisReportDialog(w, &sodisPreFill{
			fitResult:           lastFitResult,
			mcResult:            lastMCResult,
			fitParams:           lastFitParams,
			lcData:              loadedLightCurveData,
			occTitle:            occTitle,
			sitePath:            lastLoadedSitePath,
			occelmntXml:         lastLoadedOccelmntXml,
			noiseSigma:          noiseSigma,
			csvExposureSecs:     lastCsvExposureSecs,
			observerT0:          computedObserverT0,
			detailsEventTimeUT:  detailsEventTimeUT,
			vt:                  vizierTab,
			occultationOverride: occultationOverride,
		}, onSave)
	}

	var fillSodisBtn *widget.Button
	fillSodisBtn = widget.NewButton("Fill SODIS report", func() {
		buildSodisFill("", func() {
			fillSodisBtn.Importance = widget.WarningImportance
			fillSodisBtn.Refresh()
		})
	})
	fillSodisBtn.Importance = widget.HighImportance
	fillSodisBtn.Disable()

	var fillSodisNegBtn *widget.Button
	fillSodisNegBtn = widget.NewButton("Fill SODIS Negative", func() {
		buildSodisFill("NEGATIVE", func() {
			sodisNegativeReportSaved = true
			fillSodisNegBtn.Importance = widget.WarningImportance
			fillSodisNegBtn.Refresh()
		})
	})
	fillSodisNegBtn.Importance = widget.HighImportance

	enablePostFitButtons = func() {
		mcBtn.Enable()
		runNieBtn.Enable()
		fillSodisBtn.Enable()
	}

	resetFitButtons = func() {
		fitBtn.Importance = widget.HighImportance
		fitBtn.Refresh()
		mcBtn.Importance = widget.HighImportance
		mcBtn.Disable()
		runNieBtn.Importance = widget.HighImportance
		runNieBtn.Disable()
		fillSodisBtn.Importance = widget.HighImportance
		fillSodisBtn.Disable()
	}

	tab10Content := container.NewStack(tab10Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Fit"),
		widget.NewSeparator(),
		widget.NewLabel("1. Click two points to mark a baseline region (pair)"),
		widget.NewLabel("2. Repeat to add more baseline regions"),
		widget.NewLabel("3. Click on a marked point to remove that pair"),
		widget.NewSeparator(),
		container.NewHBox(calcBaselineMeanBtn),
		widget.NewSeparator(),
		searchRangeCard,
		container.NewHBox(fitBtn, fitAbortBtn, widget.NewButton("Help", showSearchRangeHelp)),
		widget.NewSeparator(),
		widget.NewLabel("Monte Carlo trials"),
		mcNumTrialsEntry,
		container.NewHBox(mcNarrowSearchCheck),
		container.NewHBox(mcBtn, mcAbortBtn, runNieBtn, nieAbortBtn, fillSodisBtn, fillSodisNegBtn),
		mcProgressBar,
		widget.NewSeparator(),
		fitStatusLabel,
		fitProgressBar,
	)))
	tab10 := container.NewTabItem("Fit", tab10Content)

	tabs := container.NewAppTabs(tab2, tab3, tab10, vizierTab.TabItem, tab5, tab6, tab7)

	// Apply dark tab backgrounds if dark mode was persisted
	if prefs.BoolWithFallback("darkMode", false) {
		applyTabBgTheme(true)
	}

	// Handle tab selection events
	tabs.OnSelected = func(tab *container.TabItem) {
		// Track whether Fit tab is active and handle multi-pair selection mode
		previousOnFitTab := onFitTab
		onFitTab = tab == tab10

		// Clear saved pairs and baseline line when leaving the Fit tab
		if previousOnFitTab && !onFitTab {
			lightCurvePlot.SelectedPairs = nil
			lightCurvePlot.ShowBaselineLine = false
			lightCurvePlot.Refresh()
		}

		// Set selection modes based on the current tab
		if tab == tab6 {
			// Flash tags tab: enable single select mode
			lightCurvePlot.SingleSelectMode = true
			lightCurvePlot.MultiPairSelectMode = false
			// Clear point 2 selection when entering the Flash tags tab
			lightCurvePlot.selectedSeries2 = -1
			lightCurvePlot.selectedIndex2 = -1
			lightCurvePlot.selectedPointDataIndex2 = -1
			lightCurvePlot.selectedSeriesName2 = ""
			lightCurvePlot.SelectedPoint2Valid = false
			lightCurvePlot.SelectedPoint2Frame = 0
			lightCurvePlot.SelectedPoint2Value = 0
			lightCurvePlot.Refresh()
		} else if tab == tab10 {
			// Fit tab: require exactly one light curve to be displayed
			if len(displayedCurves) != 1 {
				dialog.ShowError(fmt.Errorf(
					"The Fit page requires a CSV file to be loaded with exactly one light curve displayed.\n\n"+
						"Currently %d light curve(s) are selected.\n\n"+
						"Please load a CSV file and select a single light curve before opening the Fit page.",
					len(displayedCurves)), w)
				tabs.Select(tab3)
				return
			}

			// Fit tab: multi-pair mode for baseline selection.
			lightCurvePlot.SingleSelectMode = false
			lightCurvePlot.MultiPairSelectMode = true
			lightCurvePlot.Refresh()

			// Autofill search range defaults from the parameters file only when
			// the entries are still empty (first visit). This prevents overwriting
			// user-modified values and triggering button color resets on re-entry.
			if lastDiffractionParamsPath != "" &&
				strings.TrimSpace(searchInitialOffsetEntry.Text) == "" &&
				strings.TrimSpace(searchFinalOffsetEntry.Text) == "" {
				go func() {
					file, err := os.Open(lastDiffractionParamsPath)
					if err != nil {
						return
					}
					params, err := parseOccultationParameters(file)
					if closeErr := file.Close(); closeErr != nil {
						fmt.Printf("Failed to close parameters file: %v\n", closeErr)
					}
					if err != nil {
						return
					}
					finalVal := params.MainBody.MajorAxisKm / 2
					if params.PathToExternalImage != "" && finalVal == 0 {
						finalVal = params.FundamentalPlaneWidthKm / 2
					}
					numSteps := 0
					if params.FundamentalPlaneWidthNumPoints > 0 {
						stepSize := params.FundamentalPlaneWidthKm / float64(params.FundamentalPlaneWidthNumPoints)
						if stepSize > 0 {
							numSteps = int(math.Abs(finalVal)/stepSize) + 1
						}
					}
					ns := numSteps
					fv := finalVal
					fyne.Do(func() {
						suppressSearchRangeEnable = true
						searchInitialOffsetEntry.SetText("0.0")
						searchFinalOffsetEntry.SetText(strconv.FormatFloat(fv, 'f', -1, 64))
						if ns > 0 {
							searchNumStepsEntry.SetText(fmt.Sprintf("%d", ns))
						}
						suppressSearchRangeEnable = false
					})
				}()
			}
		} else {
			lightCurvePlot.SingleSelectMode = false
			lightCurvePlot.MultiPairSelectMode = false
		}

		// csv ops tab: open the file dialog on the first visit
		if tab == tab3 && csvOpsTabFirstOpen {
			csvOpsTabFirstOpen = false
			openCSVDialog()
		}

		// VizieR tab: check that exactly one light curve is selected
		if tab == vizierTab.TabItem {
			numDisplayed := len(displayedCurves)
			if numDisplayed != 1 {
				dialog.ShowError(fmt.Errorf("a single light curve must be selected for use by the VizieR export function.\n\nCurrently %d curves are selected.\n\nBe sure to set the Start Frame and End Frame values so as to trim the data points sent to VizieR to be no more than about 100 points surrounding the event (if possible).", numDisplayed), w)
				tabs.Select(tab3) // Switch to the.csv ops tab
			}
		}
	}

	// Select the OBS select tab on startup
	tabs.Select(tab3)

	// Helper function to run IOTAdiffraction with a given parameter file
	runIOTAdiffraction := func(paramFilePath string) {
		// If the parameters file has a non-empty path_to_qe_table_file, prefix it with CAMERA-QE/
		// and write a temporary modified copy for IOTAdiffraction to use.
		actualParamFile := paramFilePath
		var tempParamFile string
		if content, err := os.ReadFile(paramFilePath); err == nil {
			var params OccultationParameters
			if err := json5.Unmarshal(content, &params); err == nil && params.PathToQeTableFile != "" {
				if !strings.HasPrefix(params.PathToQeTableFile, "CAMERA-QE/") && !strings.HasPrefix(params.PathToQeTableFile, "CAMERA-QE\\") {
					prefixed := "CAMERA-QE/" + params.PathToQeTableFile
					// Replace the value in the raw file content to preserve JSON5 formatting
					modified := strings.Replace(string(content),
						fmt.Sprintf("%q", params.PathToQeTableFile),
						fmt.Sprintf("%q", prefixed), 1)
					tmpFile, err := os.CreateTemp(appDir, "iotadiff-*.occparams")
					if err == nil {
						if _, err := tmpFile.WriteString(modified); err == nil {
							tempParamFile = tmpFile.Name()
							actualParamFile = tempParamFile
							logAction(fmt.Sprintf("QE file path prefixed: %s -> %s", params.PathToQeTableFile, prefixed))
						}
						if cerr := tmpFile.Close(); cerr != nil {
							fmt.Printf("Warning: failed to close temp params file: %v\n", cerr)
						}
					}
				}
			}
		}

		// Build the path to IOTAdiffraction.exe using the app directory
		exePath := filepath.Join(appDir, "IOTAdiffraction.exe")

		// Check if the file exists
		if _, err := os.Stat(exePath); os.IsNotExist(err) {
			dialog.ShowInformation("File Not Found",
				"IOTAdiffraction.exe was not found in the application directory.\n\n"+
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
		showPlots := fmt.Sprintf("%v", prefs.BoolWithFallback("showIOTAPlots", true))
		cmd := exec.Command(exePath, actualParamFile, showPlots)
		cmd.Dir = appDir

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
			// Clean up the temporary parameter file if one was created
			if tempParamFile != "" {
				if rerr := os.Remove(tempParamFile); rerr != nil {
					fmt.Printf("Warning: could not remove temp params file: %v\n", rerr)
				}
			}
			if err != nil {
				appendOutput(fmt.Sprintf("\n[Error: %v]", err))
			} else {
				appendOutput("\n[Process completed successfully]")
				if enableShowIOTAPlots != nil {
					fyne.Do(func() { enableShowIOTAPlots() })
				}
			}
		}()
	}

	// useParamFile sets up a global state and runs IOTAdiffraction with the given .occparams file.
	// Extracted here (rather than inside btnIOTA) so it can also be triggered automatically
	// when the parameters dialog saves a new file.
	useParamFile := func(paramFilePath string) {
		logAction(fmt.Sprintf("Running IOTAdiffraction with parameters file: %s", paramFilePath))
		lastDiffractionParamsPath = paramFilePath
		prefs.SetString("lastDiffractionParamsPath", paramFilePath)
		// Extract title and embedded occelmnt XML from the parameters file
		lastDiffractionTitle = ""
		if f, err := os.Open(paramFilePath); err == nil {
			if p, err := parseOccultationParameters(f); err == nil {
				lastDiffractionTitle = p.Title
				if p.OccelmntXml != "" {
					lastLoadedOccelmntXml = p.OccelmntXml
					prefs.SetString("lastLoadedOccelmntXml", lastLoadedOccelmntXml)
					vizierTab.FillStarFromOccelmntXml(lastLoadedOccelmntXml)
				}
			}
			if err := f.Close(); err != nil {
				fmt.Printf("Warning: failed to close parameters file: %v\n", err)
			}
		}
		prefs.SetString("lastDiffractionTitle", lastDiffractionTitle)
		// Fill VizieR Number and Name entries from title (e.g. "(2731) Cucula" → "2731", "Cucula")
		if strings.HasPrefix(lastDiffractionTitle, "(") {
			if end := strings.Index(lastDiffractionTitle, ")"); end > 0 {
				if num := strings.TrimSpace(lastDiffractionTitle[1:end]); num != "" {
					vizierTab.AsteroidNumberEntry.SetText(num)
				}
				if name := strings.TrimSpace(lastDiffractionTitle[end+1:]); name != "" {
					vizierTab.AsteroidNameEntry.SetText(name)
				}
			}
		}
		runIOTAdiffraction(paramFilePath)
	}
	afterOccParamsSaved = useParamFile

	btnIOTA := widget.NewButton("Run IOTAdiffraction", func() {
		dialog.ShowInformation("Use the Process OWC button",
			"To run IOTAdiffraction, use the \"Process OWC\" button to create or edit\n"+
				"an occultation parameters file. IOTAdiffraction will run automatically\n"+
				"when you save the file.", w)
	})
	btnIOTA.Disable()
	resetIOTABtn = func() { btnIOTA.Disable() }
	btnOccultParams := widget.NewButton("Edit Occultation Parameters", func() {
		if loadedLightCurveData != nil && loadedLightCurveData.SourceFilePath != "" {
			obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
			if entries, err := os.ReadDir(obsDir); err == nil {
				for _, entry := range entries {
					if !entry.IsDir() && strings.ToLower(filepath.Ext(entry.Name())) == ".occparams" {
						lastLoadedParamsPath = filepath.Join(obsDir, entry.Name())
						showOccultationParametersDialog(w, false, nil, obsDir)
						return
					}
				}
			}
			dialog.ShowInformation("No .occparams file found",
				"No .occparams file was found in the current observation folder.\n\n"+
					"Use the \"Process occelmnt file\" button to create one.", w)
			return
		}
		showOccultationParametersDialog(w, true, nil, "")
	})
	var stopProcessBlink func()
	btnProcessOccelemnt := widget.NewButton("Process occelmnt file", func() {
		// Autoload the first file whose name starts with "occ" from the CSV directory
		autoXml := ""
		if loadedLightCurveData != nil {
			if srcPath := loadedLightCurveData.SourceFilePath; srcPath != "" {
				obsDir := filepath.Dir(srcPath)
				if entries, err := os.ReadDir(obsDir); err == nil {
					for _, entry := range entries {
						if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "occel") {
							fullPath := filepath.Join(obsDir, entry.Name())
							if data, rerr := os.ReadFile(fullPath); rerr == nil {
								autoXml = strings.TrimPrefix(string(data), "\xef\xbb\xbf")
								lastLoadedOccelmntXml = autoXml
								prefs.SetString("lastLoadedOccelmntXml", autoXml)
								vizierTab.FillStarFromOccelmntXml(autoXml)
								logAction(fmt.Sprintf("Auto-loaded occelmnt file: %s", fullPath))
							}
							break
						}
					}
				}
			}
		}
		btnIOTA.Enable()
		showProcessOccelemntDialog(w, vizierTab, autoXml)
	})
	btnProcessOccelemnt.Importance = widget.HighImportance
	btnProcessOccelemnt.Disable()
	// startProcessBlink starts blinking the button; safe to call multiple times.
	startProcessBlink := func() {
		if stopProcessBlink != nil {
			return // already blinking
		}
		blinkStop := make(chan struct{})
		stopProcessBlink = func() { close(blinkStop) }
		go func() {
			on := true
			for {
				select {
				case <-blinkStop:
					return
				case <-time.After(600 * time.Millisecond):
					on = !on
					fyne.Do(func() {
						if on {
							btnProcessOccelemnt.Importance = widget.HighImportance
						} else {
							btnProcessOccelemnt.Importance = widget.MediumImportance
						}
						btnProcessOccelemnt.Refresh()
					})
				}
			}
		}()
	}
	resetProcessOccelmntBtn = func() {
		btnProcessOccelemnt.Enable()
		btnProcessOccelemnt.Importance = widget.HighImportance
		btnProcessOccelemnt.Refresh()
		startProcessBlink()
	}
	// Wrap afterOccParamsSaved to stop blinking and change the Process occelmnt button to yellow
	origAfterSaved := afterOccParamsSaved
	afterOccParamsSaved = func(path string) {
		origAfterSaved(path)
		if stopProcessBlink != nil {
			stopProcessBlink()
			stopProcessBlink = nil
		}
		btnProcessOccelemnt.Importance = widget.WarningImportance
		btnProcessOccelemnt.Refresh()
		tabs.Select(tab10)
	}

	btnShowDetails := widget.NewButton("Show details file", func() {
		if loadedLightCurveData == nil || loadedLightCurveData.SourceFilePath == "" {
			dialog.ShowInformation("No observation folder", "Please load a CSV file first.", w)
			return
		}
		obsDir := filepath.Dir(loadedLightCurveData.SourceFilePath)
		// Find the first file whose name contains "detail"
		detailPath := ""
		if entries, err := os.ReadDir(obsDir); err == nil {
			for _, entry := range entries {
				if !entry.IsDir() && strings.Contains(strings.ToLower(entry.Name()), "detail") {
					detailPath = filepath.Join(obsDir, entry.Name())
					break
				}
			}
		}
		if detailPath == "" {
			dialog.ShowInformation("No details file", "No file containing \"detail\" in its name was found in the current observation folder.", w)
			return
		}
		// Read and parse as CSV
		fileData, rerr := os.ReadFile(detailPath)
		if rerr != nil {
			dialog.ShowError(fmt.Errorf("failed to read details file: %w", rerr), w)
			return
		}
		reader := csv.NewReader(bytes.NewReader(fileData))
		reader.FieldsPerRecord = -1
		reader.TrimLeadingSpace = true
		var rows [][]string
		for {
			record, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to parse details file: %w", err), w)
				return
			}
			rows = append(rows, record)
		}
		if len(rows) == 0 {
			dialog.ShowInformation("Details file", "The details file is empty.", w)
			return
		}
		numCols := 0
		for _, row := range rows {
			if len(row) > numCols {
				numCols = len(row)
			}
		}
		// Compute column widths from content
		colWidths := make([]float32, numCols)
		for _, row := range rows {
			for c, cell := range row {
				w := float32(len(cell))*7.5 + 20
				if w > colWidths[c] {
					colWidths[c] = w
				}
				if colWidths[c] < 60 {
					colWidths[c] = 60
				}
				if colWidths[c] > 300 {
					colWidths[c] = 300
				}
			}
		}
		table := widget.NewTable(
			func() (int, int) { return len(rows), numCols },
			func() fyne.CanvasObject {
				lbl := widget.NewLabel("")
				lbl.Wrapping = fyne.TextWrapOff
				return lbl
			},
			func(id widget.TableCellID, cell fyne.CanvasObject) {
				lbl := cell.(*widget.Label)
				if id.Row < len(rows) && id.Col < len(rows[id.Row]) {
					lbl.SetText(rows[id.Row][id.Col])
				} else {
					lbl.SetText("")
				}
				if id.Row == 0 {
					lbl.TextStyle = fyne.TextStyle{Bold: true}
				} else {
					lbl.TextStyle = fyne.TextStyle{}
				}
				lbl.Refresh()
			},
		)
		for col, cw := range colWidths {
			table.SetColumnWidth(col, cw)
		}
		table.OnSelected = func(id widget.TableCellID) {
			cellValue := ""
			if id.Row < len(rows) && id.Col < len(rows[id.Row]) {
				cellValue = rows[id.Row][id.Col]
			}
			w.Clipboard().SetContent(cellValue)
			dialog.ShowInformation("Copied to clipboard", fmt.Sprintf("%q", cellValue), w)
		}
		closeBtn := widget.NewButton("Close", nil)
		var dlg dialog.Dialog
		closeBtn.OnTapped = func() { dlg.Hide() }
		tableScroll := container.NewScroll(table)
		tableScroll.SetMinSize(fyne.NewSize(800, 400))
		content := container.NewBorder(nil, container.NewHBox(layout.NewSpacer(), closeBtn), nil, nil, tableScroll)
		dlg = dialog.NewCustomWithoutButtons("Details: "+filepath.Base(detailPath), content, w)
		dlg.Resize(fyne.NewSize(900, 520))
		dlg.Show()
	})
	btnShowIOTAPlots := widget.NewButton("Show IOTAdiffraction plots", func() {
		type plotInfo struct {
			path  string
			title string
		}
		plots := []plotInfo{
			{filepath.Join(appDir, "lightCurvePlot.png"), "Light Curve Plot"},
			{filepath.Join(appDir, "diffractionImageWithPath.png"), "Diffraction Image"},
			{filepath.Join(appDir, "camera_response.png"), "Camera Response"},
		}
		var images []fyne.CanvasObject
		for _, p := range plots {
			if _, err := os.Stat(p.path); err != nil {
				continue
			}
			img := canvas.NewImageFromFile(p.path)
			img.FillMode = canvas.ImageFillContain
			img.SetMinSize(fyne.NewSize(500, 400))
			label := widget.NewLabel(p.title)
			label.Alignment = fyne.TextAlignCenter
			images = append(images, container.NewBorder(nil, label, nil, nil, img))
		}
		if len(images) == 0 {
			dialog.ShowInformation("No plots found",
				"No IOTAdiffraction plot files were found in the application directory.", w)
			return
		}
		plotsWin := a.NewWindow("IOTAdiffraction Plots")
		grid := container.NewGridWithColumns(len(images))
		for _, img := range images {
			grid.Add(img)
		}
		plotsWin.SetContent(container.NewScroll(grid))
		plotsWin.Resize(fyne.NewSize(1600, 500))
		plotsWin.CenterOnScreen()
		plotsWin.Show()
	})

	btnShowIOTAPlots.Disable()
	enableShowIOTAPlots = func() { btnShowIOTAPlots.Enable() }

	buttons := container.NewHBox(btnProcessOccelemnt, btnOccultParams, btnIOTA, btnShowDetails, btnShowIOTAPlots)

	// Split tabs and plot area
	split := container.NewHSplit(tabs, plotArea)
	splitOffset := prefs.FloatWithFallback("splitOffset", 0.35)
	split.SetOffset(splitOffset)

	content := container.NewBorder(nil, buttons, nil, nil, split)

	w.SetContent(content)
	w.Resize(fyne.NewSize(float32(savedW), float32(savedH)))

	if firstRun {
		w.CenterOnScreen()
	}

	// Save window geometry and split position on close
	w.SetCloseIntercept(func() {
		doClose := func() {
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
		}

		if sodisReportSavedThisSession && !vizierDatWrittenThisSession && !sodisNegativeReportSaved {
			dialog.ShowConfirm("VizieR file not written",
				"A SODIS report was saved but a VizieR .dat file has not been written.\n\nDo you really want to exit?",
				func(confirmed bool) {
					if confirmed {
						doClose()
					}
				}, w)
			return
		}
		doClose()
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
