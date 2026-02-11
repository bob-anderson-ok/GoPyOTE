package main

import (
	"bufio"
	"bytes"
	"embed"
	"fmt"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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

//go:embed help_markdown/singlePointAnalysis.md
var singlePointAnalysisMarkdown embed.FS

//go:embed help_markdown/fitMarkdown.md
var fitExplanationMarkdown embed.FS

// Version information
const Version = "1.1.6"

// Track the last loaded parameters file path for use by Run IOTAdiffraction
var lastLoadedParamsPath string

// Track the parameters file used for the last IOTAdiffraction run (for startup display)
var lastDiffractionParamsPath string

// Title from the parameters file used for the last IOTAdiffraction run (for plot titles)
var lastDiffractionTitle string

// resultsFolder is the path to the -RESULTS folder created alongside the opened CSV file.
// Various outputs (fit plots, histograms, etc.) are written here.
var resultsFolder string

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

// showFileOpenWithRecents shows a dialog with recent folders, then opens the file dialog
func showFileOpenWithRecents(w fyne.Window, prefs fyne.Preferences, title string, filter storage.FileFilter, callback func(fyne.URIReadCloser, error)) {
	folders := getRecentFolders(prefs)

	// If no recent folders, show the file dialog directly
	if len(folders) == 0 {
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

	// Add the "Browse..." button at the top
	browseBtn := widget.NewButton("Open last used folder", func() {
		openAtLocation("")
	})
	browseBtn.Importance = widget.HighImportance
	roseBg := canvas.NewRectangle(color.RGBA{R: 255, G: 150, B: 170, A: 255})
	browseBtnContainer := container.NewStack(roseBg, browseBtn)
	browseBtnHalf := container.NewGridWrap(fyne.NewSize(450/2, browseBtn.MinSize().Height), browseBtnContainer)
	buttons = append(buttons, browseBtnHalf)

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

	content := container.NewVBox(buttons...)
	customDialog = dialog.NewCustom(title, "Cancel", content, w)
	customDialog.Resize(fyne.NewSize(900, 0))
	customDialog.Show()
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
	exposureTimeSecsEntry := widget.NewEntry()

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
				if params.ExposureTimeSecs != 0.0 {
					exposureTimeSecsEntry.SetText(strconv.FormatFloat(params.ExposureTimeSecs, 'f', -1, 64))
				} else {
					exposureTimeSecsEntry.SetText("")
				}
				loadedFileName = filepath.Base(lastLoadedParamsPath)
				fileNameLabel.SetText("File being displayed:  " + loadedFileName)
				logAction(fmt.Sprintf("Auto-loaded parameters file: %s", lastLoadedParamsPath))
			}
		}
	}

	// Collect all entries for dirty-checking on Cancel
	allEntries := []*widget.Entry{
		windowSizeEntry, titleEntry, fundamentalPlaneWidthKmEntry,
		fundamentalPlaneWidthNumPointsEntry, parallaxArcsecEntry, distanceAuEntry,
		pathToQeTableFileEntry, observationWavelengthNmEntry, dXKmPerSecEntry,
		dYKmPerSecEntry, pathPerpendicularOffsetKmEntry, percentMagDropEntry,
		starDiamOnPlaneMasEntry, limbDarkeningCoeffEntry, starClassEntry,
		mainBodyXCenterEntry, mainBodyYCenterEntry, mainBodyMajorAxisEntry,
		mainBodyMinorAxisEntry, mainBodyPaDegreesEntry,
		satelliteXCenterEntry, satelliteYCenterEntry, satelliteMajorAxisEntry,
		satelliteMinorAxisEntry, satellitePaDegreesEntry,
		pathToExternalImageEntry, exposureTimeSecsEntry,
	}
	// Snapshot initial values so we can detect edits
	initialValues := make([]string, len(allEntries))
	for i, e := range allEntries {
		initialValues[i] = e.Text
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
		&widget.FormItem{Text: "Exposure time (secs)", Widget: wrapEntry(exposureTimeSecsEntry)},
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
		dirty := false
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
		showFileOpenWithRecents(w, fyne.CurrentApp().Preferences(), "Select Parameters File", nil, func(reader fyne.URIReadCloser, err error) {
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
			if params.ExposureTimeSecs != 0.0 {
				exposureTimeSecsEntry.SetText(strconv.FormatFloat(params.ExposureTimeSecs, 'f', -1, 64))
			} else {
				exposureTimeSecsEntry.SetText("")
			}

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
		})
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
				ExposureTimeSecs:    parseFloat(exposureTimeSecsEntry.Text),
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

			logAction(fmt.Sprintf("Saved parameters file: %s", writer.URI().Path()))

			// Re-snapshot so saved state is considered clean
			for i, e := range allEntries {
				initialValues[i] = e.Text
			}

			// Close the parameters dialog after a successful save
			customDialog.Hide()
		}, w)
		fileDialog.SetFilter(nil) // Allow all files or set a specific filter
		if loadedFileName != "" {
			fileDialog.SetFileName(loadedFileName)
		}
		// Set the starting directory to the directory of the last loaded parameters file
		if lastLoadedParamsPath != "" {
			folderURI := storage.NewFileURI(filepath.Dir(lastLoadedParamsPath))
			listableURI, locErr := storage.ListerForURI(folderURI)
			if locErr == nil {
				fileDialog.SetLocation(listableURI)
			}
		}
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
	})

	buttons := container.NewHBox(loadBtn, saveBtn, layout.NewSpacer(), cancelBtn)
	bottomSection := container.NewVBox(fileNameLabel, buttons)
	content := container.NewBorder(nil, bottomSection, nil, nil, scrollContent)

	customDialog = dialog.NewCustomWithoutButtons("Edit/Enter Occultation Parameters", content, w)
	customDialog.Resize(fyne.NewSize(840, 750))
	customDialog.Show()
}

func main() {
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
		fyne.NewMenuItem("Single point analysis", func() {
			content, err := singlePointAnalysisMarkdown.ReadFile("help_markdown/singlePointAnalysis.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load singlePointAnalysis.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Single point analysis", string(content), &singlePointAnalysisMarkdown, w)
		}),
		fyne.NewMenuItem("Fit explanation", func() {
			content, err := fitExplanationMarkdown.ReadFile("help_markdown/fitMarkdown.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load fitMarkdown.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Fit explanation", string(content), &fitExplanationMarkdown, w)
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

	tab2Bg := makeTabBg(color.RGBA{R: 200, G: 200, B: 230, A: 255}, color.RGBA{R: 50, G: 50, B: 80, A: 255})
	tab2Content := container.NewStack(tab2Bg, container.NewPadded(container.NewVBox(prefixCheckboxes, widget.NewSeparator(), darkModeCheck, grayBgCheck)))
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
	frameRangeRow := container.NewHBox(
		widget.NewLabel("Start Frame:"),
		startFrameContainer,
		widget.NewLabel("End Frame:"),
		endFrameContainer,
	)

	// Track the current x-axis label for click callback
	currentXAxisLabel := "Time"

	// Callback for single point analysis (assigned later when tab9 is defined)
	var onSinglePointAnalysis func()
	var onSinglePointDropCalc func()
	var onSinglePointTab bool // Track whether the Single Point tab is active
	var onFitTab bool         // Track whether the Fit tab is active

	// Create the plot with an empty series (will be populated when CSV is loaded)
	var lightCurvePlot *LightCurvePlot
	lightCurvePlot = NewLightCurvePlot(nil, func(point PlotPoint) {
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

		// Trigger single point analysis if both points are selected (only on the Single Point tab)
		if onSinglePointTab && onSinglePointAnalysis != nil && lightCurvePlot.SelectedPoint1Valid && lightCurvePlot.SelectedPoint2Valid {
			onSinglePointAnalysis()
		}

		// Trigger single point drop calculation if in a single select mode with one point
		if onSinglePointDropCalc != nil && lightCurvePlot.SingleSelectMode && lightCurvePlot.SelectedPoint1Valid && !lightCurvePlot.SelectedPoint2Valid {
			onSinglePointDropCalc()
		}
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

	// Function to reset frame range (assigned later after entry boxes are defined)
	// Returns true if the frame range was zoomed and got reset, false if already at original values
	var resetFrameRange func() bool

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
	timestampTicksCheck := widget.NewCheck("use timestamps", func(checked bool) {
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
	timestampTicksCheck.Checked = true
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
		widget.NewButton("Reset", func() {
			// Check if the frame range was zoomed and reset it
			wasZoomed := false
			if resetFrameRange != nil {
				wasZoomed = resetFrameRange()
			}
			// Only reset X/Y bounds if the frame range was not zoomed
			if !wasZoomed {
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
			} else {
				logAction("Reset frame range to default")
			}
		}),
		timestampTicksCheck,
	)

	// Bottom section with frame range controls on the left and status label on the right
	plotBottomRow := container.NewBorder(
		nil,             // top
		nil,             // bottom
		frameRangeRow,   // left
		nil,             // right
		plotStatusLabel, // center (takes remaining space)
	)

	// Create the startup overlay showing diffraction image and parameters file info
	var startupOverlayCenter fyne.CanvasObject
	if lastDiffractionParamsPath != "" {
		diffImgPath := filepath.Join(appDir, "diffractionImage8bit.png")
		if _, err := os.Stat(diffImgPath); err == nil {
			diffImg := canvas.NewImageFromFile(diffImgPath)
			diffImg.FillMode = canvas.ImageFillContain
			startupOverlayCenter = diffImg
		}
	}
	paramsInfo := "No diffraction image has been generated yet"
	if lastDiffractionParamsPath != "" {
		paramsInfo = "Current diffraction image as built from: " + filepath.Base(lastDiffractionParamsPath)
		// Try to load the title from the parameters file
		if f, err := os.Open(lastDiffractionParamsPath); err == nil {
			if p, err := parseOccultationParameters(f); err == nil && p.Title != "" {
				paramsInfo = p.Title + "\n" + paramsInfo
			}
			if err := f.Close(); err != nil {
				fmt.Printf("Warning: failed to close parameters file: %v\n", err)
			}
		}
	}
	startupInfoLabel := widget.NewLabel(paramsInfo)
	startupInfoLabel.Alignment = fyne.TextAlignCenter
	startupInfoLabel.TextStyle = fyne.TextStyle{Bold: true}

	var startupOverlay *fyne.Container
	if startupOverlayCenter != nil {
		startupOverlay = container.NewBorder(nil, startupInfoLabel, nil, nil, startupOverlayCenter)
	} else {
		startupOverlay = container.NewCenter(startupInfoLabel)
	}

	plotCenter := container.NewStack(lightCurvePlot, startupOverlay)

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

	// Assign the resetFrameRange function now that entry boxes are defined
	// Returns true if the frame range was zoomed and got reset, false otherwise
	resetFrameRange = func() bool {
		if minFrameNum == 0 && maxFrameNum == 0 {
			return false // No data loaded
		}
		// Check if currently zoomed (frame range differs from the original)
		wasZoomed := frameRangeStart != minFrameNum || frameRangeEnd != maxFrameNum
		if wasZoomed {
			frameRangeStart = minFrameNum
			frameRangeEnd = maxFrameNum
			startFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeStart))
			endFrameEntry.SetText(fmt.Sprintf("%.0f", frameRangeEnd))
			rebuildPlot()
		}
		return wasZoomed
	}

	// Set the occultation title on the main plot from the last diffraction run
	lightCurvePlot.occultationTitle = lastDiffractionTitle

	// Set up a warning callback for the plot
	lightCurvePlot.SetOnWarning(func(message string) {
		dialog.ShowInformation("Warning", message, w)
	})

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

			// Save Y bounds before rebuild to preserve Y axis scaling during zoom
			savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
			rebuildPlot()
			// Restore Y bounds after rebuild
			lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		}
	})

	// Create VizieR tab early so it can be populated from RAVF headers during a file load
	vizierTab := NewVizieRTab()
	// Register vizier tab background for dark mode toggling
	tabBgs = append(tabBgs, tabBgEntry{vizierTab.TabBg, color.RGBA{R: 210, G: 220, B: 210, A: 255}, color.RGBA{R: 60, G: 70, B: 60, A: 255}})

	// Track if csv ops tab has been opened for the first time
	csvOpsTabFirstOpen := true

	// Function to open the CSV file dialog
	openCSVDialog := func() {
		showFileOpenWithRecents(w, prefs, "Select CSV Folder", storage.NewExtensionFileFilter([]string{".csv"}), func(reader fyne.URIReadCloser, err error) {
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

			// Create a -RESULTS folder in the application directory
			base := filepath.Base(filePath)
			ext := filepath.Ext(base)
			nameWithoutExt := base[:len(base)-len(ext)]
			resultsFolder = filepath.Join(appDir, nameWithoutExt+"-RESULTS")
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

			// Parse the CSV file
			data, err := parseLightCurveCSV(filePath)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to parse CSV: %w", err), w)
				return
			}

			loadedLightCurveData = data
			startupOverlay.Hide()

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
			}

			// Clear displayed curves and reset the plot
			for k := range displayedCurves {
				delete(displayedCurves, k)
			}
			lightCurvePlot.SetSeries(nil)
			smoothedSeries = nil         // Clear any previous smooth curve
			normalizationApplied = false // Reset normalization flag

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

			plotStatusLabel.SetText(fmt.Sprintf("Loaded %d light curves (%d shown) with %d data points. Click to toggle display.",
				len(data.Columns), len(displayedCurves), len(data.TimeValues)))

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
	tab3 := container.NewTabItem("Csv", tab3Content)

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
		startupOverlay.Hide()
		normalizationApplied = false
		smoothedSeries = nil

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
		startupOverlay.Hide()
		normalizationApplied = false
		smoothedSeries = nil

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

	// Set up a Zip button callback
	vizierTab.ZipBtn.OnTapped = func() {
		zipDatFiles(w, vizierTab.OutputFolderEntry.Text, vizierTab.StatusLabel)
	}

	// Set up Load from NA spreadsheet button callback
	vizierTab.LoadXlsxBtn.OnTapped = func() {
		vizierTab.FillFromNASpreadsheet(w)
	}

	// Tab 9: Single Point
	tab9Bg := makeTabBg(color.RGBA{R: 220, G: 210, B: 230, A: 255}, color.RGBA{R: 70, G: 60, B: 80, A: 255})
	singlePointStatusLabel := widget.NewLabel("Select two points on the light curve to define a baseline region")
	singlePointStatusLabel.Wrapping = fyne.TextWrapWord

	// State for single point analysis
	var singlePointAnalysisReady bool
	var singlePointPolyCoeffs []float64 // Polynomial coefficients [a0, a1, a2, a3]
	var singlePointStartIdx, singlePointEndIdx int
	var singlePointStdDev, singlePointMeanY float64
	var singlePointNumPoints int
	var singlePointFrame1, singlePointFrame2 float64

	// Function to calculate a drop for a single selected point
	var calculateSinglePointDrop func()

	// Function to perform single point analysis when two points are selected
	performSinglePointAnalysis := func() {
		// Check that both points are valid and on the same series
		if !lightCurvePlot.SelectedPoint1Valid || !lightCurvePlot.SelectedPoint2Valid {
			return
		}
		if lightCurvePlot.selectedSeriesName != lightCurvePlot.selectedSeriesName2 {
			singlePointStatusLabel.SetText("Both points must be on the same light curve")
			return
		}
		if loadedLightCurveData == nil {
			singlePointStatusLabel.SetText("No light curve data loaded")
			return
		}

		// Get the data indices for the selected points
		idx1 := lightCurvePlot.selectedPointDataIndex
		idx2 := lightCurvePlot.selectedPointDataIndex2

		// Ensure idx1 < idx2
		if idx1 > idx2 {
			idx1, idx2 = idx2, idx1
		}

		if idx1 < 0 || idx2 < 0 || idx1 >= idx2 {
			singlePointStatusLabel.SetText("Invalid point selection")
			return
		}

		// Find the column that matches the selected series
		var refColumn *LightCurveColumn
		for i := range loadedLightCurveData.Columns {
			if loadedLightCurveData.Columns[i].Name == lightCurvePlot.selectedSeriesName {
				refColumn = &loadedLightCurveData.Columns[i]
				break
			}
		}
		if refColumn == nil {
			singlePointStatusLabel.SetText("Could not find data for selected series")
			return
		}

		// Use the data indices directly
		startIdx := idx1
		endIdx := idx2

		// Get actual frame numbers for display
		frame1 := loadedLightCurveData.FrameNumbers[startIdx]
		frame2 := loadedLightCurveData.FrameNumbers[endIdx]

		// Extract Y values for the range
		ys := refColumn.Values[startIdx : endIdx+1]
		numPoints := len(ys)

		if numPoints < 4 {
			singlePointStatusLabel.SetText(fmt.Sprintf("Need at least 4 points for 3rd order polynomial (have %d)", numPoints))
			return
		}

		// Create X values (normalized to 0-1 range for numerical stability)
		xs := make([]float64, numPoints)
		for i := range xs {
			xs[i] = float64(i) / float64(numPoints-1)
		}

		// Fit 3rd order polynomial using the least squares technique
		// y = a0 + a1*x + a2*x^2 + a3*x^3
		// Build normal equations: (X'X)a = X'y
		degree := 3
		n := numPoints

		// Build X'X matrix (4x4 for degree 3)
		xtx := make([][]float64, degree+1)
		for i := range xtx {
			xtx[i] = make([]float64, degree+1)
		}

		// Build X'y vector
		xty := make([]float64, degree+1)

		for i := 0; i < n; i++ {
			xi := xs[i]
			yi := ys[i]
			xpow := 1.0
			for j := 0; j <= degree; j++ {
				xty[j] += xpow * yi
				xpow2 := 1.0
				for k := 0; k <= degree; k++ {
					xtx[j][k] += xpow * xpow2
					xpow2 *= xi
				}
				xpow *= xi
			}
		}

		// Solve using Gaussian elimination with partial pivoting
		coeffs := make([]float64, degree+1)
		aug := make([][]float64, degree+1)
		for i := range aug {
			aug[i] = make([]float64, degree+2)
			copy(aug[i], xtx[i])
			aug[i][degree+1] = xty[i]
		}

		for col := 0; col <= degree; col++ {
			// Find pivot
			maxRow := col
			for row := col + 1; row <= degree; row++ {
				if math.Abs(aug[row][col]) > math.Abs(aug[maxRow][col]) {
					maxRow = row
				}
			}
			aug[col], aug[maxRow] = aug[maxRow], aug[col]

			// Eliminate
			for row := col + 1; row <= degree; row++ {
				if aug[col][col] != 0 {
					factor := aug[row][col] / aug[col][col]
					for j := col; j <= degree+1; j++ {
						aug[row][j] -= factor * aug[col][j]
					}
				}
			}
		}

		// Back substitution
		for i := degree; i >= 0; i-- {
			coeffs[i] = aug[i][degree+1]
			for j := i + 1; j <= degree; j++ {
				coeffs[i] -= aug[i][j] * coeffs[j]
			}
			if aug[i][i] != 0 {
				coeffs[i] /= aug[i][i]
			}
		}

		// Calculate fitted values, residuals, and mean
		fittedYs := make([]float64, numPoints)
		var sumSqResiduals float64
		var sumY float64
		for i := 0; i < numPoints; i++ {
			xi := xs[i]
			fitted := coeffs[0] + coeffs[1]*xi + coeffs[2]*xi*xi + coeffs[3]*xi*xi*xi
			fittedYs[i] = fitted
			residual := ys[i] - fitted
			sumSqResiduals += residual * residual
			sumY += ys[i]
		}
		stdDev := math.Sqrt(sumSqResiduals / float64(numPoints))
		meanY := sumY / float64(numPoints)

		// Store analysis state for single point drop calculation
		singlePointPolyCoeffs = coeffs
		singlePointStartIdx = startIdx
		singlePointEndIdx = endIdx
		singlePointStdDev = stdDev
		singlePointMeanY = meanY
		singlePointNumPoints = numPoints
		singlePointFrame1 = frame1
		singlePointFrame2 = frame2
		singlePointAnalysisReady = true

		// Create polynomial fit series for display (only for the selected range)
		var polyPoints []PlotPoint
		for i, y := range fittedYs {
			polyPoints = append(polyPoints, PlotPoint{
				X:     0, // Will be set in rebuildPlot based on frame numbers or timestamps
				Y:     y,
				Index: startIdx + i, // Map to original data index
			})
		}

		smoothedSeries = &PlotSeries{
			Points: polyPoints,
			Color:  color.RGBA{R: 255, G: 0, B: 255, A: 255}, // Magenta for polynomial curve
			Name:   "3rd Order Poly",
		}

		// Clear selections and switch to single select mode
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
		lightCurvePlot.SingleSelectMode = true

		// Save Y bounds and rebuild the plot to show the polynomial curve
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

		// Display results
		singlePointStatusLabel.SetText(fmt.Sprintf(
			"Baseline Analysis Complete:\n\n"+
				"Frame range: %.0f to %.0f\n"+
				"Number of points: %d\n"+
				"Mean value: %.4f\n"+
				"Standard deviation: %.4f\n\n"+
				"Now click on a point to measure its drop.",
			frame1, frame2, numPoints, meanY, stdDev))

		logAction(fmt.Sprintf("Single Point Analysis: frames %.0f-%.0f, %d points, 3rd order poly, mean %.4f, stdDev %.4f",
			frame1, frame2, numPoints, meanY, stdDev))
	}

	// Function to calculate a drop for a single selected point
	calculateSinglePointDrop = func() {
		if !singlePointAnalysisReady {
			return
		}
		if !lightCurvePlot.SelectedPoint1Valid {
			return
		}

		// Get the selected point's data index
		pointIdx := lightCurvePlot.selectedPointDataIndex
		if pointIdx < 0 || loadedLightCurveData == nil {
			return
		}

		// Get the actual Y value of the selected point
		var actualY float64
		for _, col := range loadedLightCurveData.Columns {
			if col.Name == lightCurvePlot.selectedSeriesName {
				if pointIdx < len(col.Values) {
					actualY = col.Values[pointIdx]
				}
				break
			}
		}

		// Check if the point is within the baseline range
		if pointIdx < singlePointStartIdx || pointIdx > singlePointEndIdx {
			// Point is outside the baseline range - reject selection
			// Clear the selection
			lightCurvePlot.selectedSeries = -1
			lightCurvePlot.selectedIndex = -1
			lightCurvePlot.selectedPointDataIndex = -1
			lightCurvePlot.selectedSeriesName = ""
			lightCurvePlot.SelectedPoint1Valid = false
			lightCurvePlot.SelectedPoint1Frame = 0
			lightCurvePlot.SelectedPoint1Value = 0
			lightCurvePlot.Refresh()

			// Show guidance message
			dialog.ShowInformation("Outside Baseline Region",
				fmt.Sprintf("Please select a point within the baseline region (frames %.0f to %.0f).\n\n"+
					"Click Reset to define a new baseline region.",
					singlePointFrame1, singlePointFrame2), w)
			return
		}

		// Calculate the normalized x position for the polynomial
		// It was fit with x normalized to [0, 1] over the range
		normalizedX := float64(pointIdx-singlePointStartIdx) / float64(singlePointNumPoints-1)
		referenceY := singlePointPolyCoeffs[0] +
			singlePointPolyCoeffs[1]*normalizedX +
			singlePointPolyCoeffs[2]*normalizedX*normalizedX +
			singlePointPolyCoeffs[3]*normalizedX*normalizedX*normalizedX

		// Calculate drop (reference - actual, so positive = drop below reference)
		drop := referenceY - actualY
		dropRatio := drop / singlePointStdDev

		// Get frame number for display
		frameNum := loadedLightCurveData.FrameNumbers[pointIdx]

		// Display results
		singlePointStatusLabel.SetText(fmt.Sprintf(
			"Baseline (frames %.0f to %.0f):\n"+
				"  Mean: %.4f, Std Dev: %.4f\n\n"+
				"Selected Point (frame %.0f):\n"+
				"  Actual value: %.4f\n"+
				"  Reference value: %.4f\n"+
				"  Drop: %.4f\n"+
				"  Drop / Std Dev: %.2f",
			singlePointFrame1, singlePointFrame2, singlePointMeanY, singlePointStdDev,
			frameNum, actualY, referenceY, drop, dropRatio))

		logAction(fmt.Sprintf("Single Point Drop: frame %.0f, actual %.4f, reference %.4f, drop %.4f, ratio %.2f",
			frameNum, actualY, referenceY, drop, dropRatio))
	}

	// Reset button for single point analysis
	singlePointResetBtn := widget.NewButton("Reset", func() {
		// Clear analysis state
		singlePointAnalysisReady = false
		singlePointPolyCoeffs = nil

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

		// Switch back to two-point selection mode
		lightCurvePlot.SingleSelectMode = false

		// Clear the smoothed curve
		smoothedSeries = nil

		// Rebuild plot
		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY, savedMaxY)

		// Reset status label
		singlePointStatusLabel.SetText("Select two points on the light curve to define a baseline region")

		logAction("Single Point Analysis reset")
	})

	tab9Content := container.NewStack(tab9Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Single Point Analysis"),
		widget.NewSeparator(),
		widget.NewLabel("Instructions:"),
		widget.NewLabel("1. Select two points to define a baseline region"),
		widget.NewLabel("2. A 3rd order polynomial is fit and std dev calculated"),
		widget.NewLabel("3. Click on any point to measure its drop from baseline"),
		widget.NewSeparator(),
		singlePointResetBtn,
		widget.NewSeparator(),
		singlePointStatusLabel,
	)))
	tab9 := container.NewTabItem("Single Point", tab9Content)

	// Assign the single point analysis callbacks
	onSinglePointAnalysis = performSinglePointAnalysis
	onSinglePointDropCalc = calculateSinglePointDrop

	// Tab 10: Fit
	tab10Bg := makeTabBg(color.RGBA{R: 200, G: 220, B: 240, A: 255}, color.RGBA{R: 50, G: 70, B: 90, A: 255})

	// Status label for Fit tab
	fitStatusLabel := widget.NewLabel("Select pairs of points to define baseline regions")

	// Stored baseline noise for later use
	var extractedNoise []float64

	// Stored last fit result, params, candidate curves, and target data for Monte Carlo
	var lastFitResult *fitResult
	var lastFitParams *OccultationParameters
	var lastFitCandidates []*precomputedCurve
	var lastFitTargetTimes, lastFitTargetValues []float64

	// Calculate Baseline mean button: computes mean, extracts noise, scales to unity
	calcBaselineMeanBtn := widget.NewButton("Calculate Baseline mean", func() {
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
		extractedNoise = noise

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

		savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
		rebuildPlot()
		lightCurvePlot.SetYBounds(savedMinY/scaleFactor, savedMaxY/scaleFactor)

		// Show noise histogram if we have enough points
		if len(noise) >= 2 {
			histImg, noiseMean, noiseSigma, err := createNoiseHistogramImage(noise, lastDiffractionTitle, 800, 500)
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to create noise histogram: %v", err), w)
			} else {
				histWindow := a.NewWindow("Baseline Noise Histogram")
				histCanvas := canvas.NewImageFromImage(histImg)
				histCanvas.FillMode = canvas.ImageFillOriginal
				histWindow.SetContent(container.NewScroll(histCanvas))
				histWindow.Resize(fyne.NewSize(850, 550))
				histWindow.Show()
				logAction(fmt.Sprintf("Fit: Extracted baseline noise: %d points, mean=%.6f, sigma=%.6f", len(noise), noiseMean, noiseSigma))
			}
		}

		fitStatusLabel.SetText(fmt.Sprintf("Scaled to unity (baseline=%.4f, %d points) — noise: %d samples", mean, count, len(noise)))
	})

	// Path perpendicular offset override entry
	fitOffsetEntry := widget.NewEntry()
	fitOffsetEntry.SetPlaceHolder("from parameters file")
	fitOffsetLabel := widget.NewLabel("Path Perpendicular Offset (km)")

	// Search range for observation path offset
	searchInitialOffsetEntry := NewFocusLossEntry()
	searchInitialOffsetEntry.SetPlaceHolder("")
	searchFinalOffsetEntry := NewFocusLossEntry()
	searchFinalOffsetEntry.SetPlaceHolder("")
	searchNumStepsEntry := widget.NewEntry()
	searchNumStepsEntry.SetPlaceHolder("")

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
			// Draw the initial offset path
			path1 := &lightcurve.ObservationPath{
				DxKmPerSec:               params.DXKmPerSec,
				DyKmPerSec:               params.DYKmPerSec,
				PathOffsetFromCenterKm:   initVal,
				FundamentalPlaneWidthKm:  params.FundamentalPlaneWidthKm,
				FundamentalPlaneWidthPts: params.FundamentalPlaneWidthNumPoints,
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
				FundamentalPlaneWidthPts: params.FundamentalPlaneWidthNumPoints,
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
				searchPreviewWindow.Show()
			})
		}()
	}

	searchInitialOffsetEntry.OnSubmitted = func(_ string) { showSearchRangePreview() }
	searchFinalOffsetEntry.OnSubmitted = func(_ string) { showSearchRangePreview() }

	searchRangeForm := widget.NewForm(
		&widget.FormItem{Text: "Initial offset", Widget: searchInitialOffsetEntry},
		&widget.FormItem{Text: "Final offset", Widget: searchFinalOffsetEntry},
		&widget.FormItem{Text: "Number of steps", Widget: searchNumStepsEntry},
	)
	searchRangeCard := widget.NewCard("Search range for observation path offset", "", searchRangeForm)

	fitProgressBar := widget.NewProgressBar()
	fitProgressBar.Hide()

	// Fit button - checks preconditions and reports readiness
	var fitBtn *widget.Button
	fitBtn = widget.NewButton("Fit", func() {
		var issues []string

		// Check 1: Single curve selected
		if len(displayedCurves) != 1 {
			issues = append(issues, fmt.Sprintf("A single light curve must be selected (currently %d displayed)", len(displayedCurves)))
		}

		// Check 2: Scaled to unity
		if !lightCurvePlot.ShowBaselineLine || lightCurvePlot.BaselineValue != 1.0 {
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

			// Override path perpendicular offset if the user entered a value
			if offsetText := fitOffsetEntry.Text; offsetText != "" {
				offsetVal, err := strconv.ParseFloat(strings.TrimSpace(offsetText), 64)
				if err != nil {
					dialog.ShowError(fmt.Errorf("invalid Path Perpendicular Offset value: %v", err), w)
					return
				}
				params.PathPerpendicularOffsetKm = offsetVal
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
				go func() {
					fsr, err := runFitSearch(params, targetTimes, targetValues, initVal, finalVal, stepsVal, func(progress float64) {
						fyne.Do(func() {
							fitProgressBar.SetValue(progress)
						})
					})
					fyne.Do(func() {
						fitProgressBar.Hide()
						fitBtn.Enable()
						if err != nil {
							dialog.ShowError(err, w)
						} else {
							fr, err := displayFitSearchResult(a, w, params, fsr, targetTimes, targetValues)
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
								lastFitTargetTimes = targetTimes
								lastFitTargetValues = targetValues
							}
						}
					})
				}()
			} else {
				fr, pc, err := performFit(a, w, params, targetTimes, targetValues)
				if err != nil {
					dialog.ShowError(err, w)
				} else {
					lastFitResult = fr
					paramsCopy := *params
					lastFitParams = &paramsCopy
					lastFitCandidates = []*precomputedCurve{pc}
					lastFitTargetTimes = targetTimes
					lastFitTargetValues = targetValues
				}
			}
		}
	})

	// Monte Carlo UI elements
	mcShowTrialsCheck := widget.NewCheck("Show individual trial results", nil)
	mcShowTrialsCheck.Checked = false
	mcShowHistogramsCheck := widget.NewCheck("Show histograms", nil)
	mcShowHistogramsCheck.Checked = false

	mcNumTrialsEntry := widget.NewEntry()
	mcNumTrialsEntry.SetText("1000")
	mcNumTrialsEntry.SetPlaceHolder("number of trials")
	mcProgressBar := widget.NewProgressBar()
	mcProgressBar.Hide()

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
		if len(extractedNoise) == 0 {
			dialog.ShowError(fmt.Errorf("no baseline noise data — run Estimate Baseline first"), w)
			return
		}
		numTrials, err := strconv.Atoi(mcNumTrialsEntry.Text)
		if err != nil || numTrials < 1 {
			dialog.ShowError(fmt.Errorf("number of Monte Carlo trials must be a positive integer"), w)
			return
		}
		mcProgressBar.SetValue(0)
		mcProgressBar.Show()
		mcBtn.Disable()
		go func() {
			result, err := runMonteCarloTrials(lastFitCandidates, lastFitResult, extractedNoise, numTrials, func(progress float64) {
				fyne.Do(func() {
					mcProgressBar.SetValue(progress)
				})
			})
			fyne.Do(func() {
				mcProgressBar.Hide()
				mcBtn.Enable()
				if err != nil {
					dialog.ShowError(err, w)
					return
				}
				msg := fmt.Sprintf("Monte Carlo results (%d trials):\n\n", result.numTrials)
				for i := 0; i < result.numEdges; i++ {
					msg += fmt.Sprintf("  Edge %d: %.4f sec (3 sigma)\n", i+1, 3*result.edgeStds[i])
				}
				if result.numEdges == 2 {
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					msg += fmt.Sprintf("\n  Duration: %.4f sec (3 sigma)\n", 3*durationStd)
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
				logAction(fmt.Sprintf("  NCC=%.4f, path offset=%.3f km", lastFitResult.bestNCC, lastFitParams.PathPerpendicularOffsetKm))
				for i, et := range lastFitResult.edgeTimes {
					absTime := et + lastFitResult.bestShift
					ts := formatSecondsAsTimestamp(absTime)
					if i < result.numEdges {
						logAction(fmt.Sprintf("  Edge %d: %s +/- %.4f sec (3 sigma)", i+1, ts, 3*result.edgeStds[i]))
					} else {
						logAction(fmt.Sprintf("  Edge %d: %s", i+1, ts))
					}
				}
				if len(lastFitResult.edgeTimes) == 2 && result.numEdges == 2 {
					fitDuration := math.Abs((lastFitResult.edgeTimes[1] + lastFitResult.bestShift) - (lastFitResult.edgeTimes[0] + lastFitResult.bestShift))
					durationStd := math.Sqrt(result.edgeStds[0]*result.edgeStds[0] + result.edgeStds[1]*result.edgeStds[1])
					logAction(fmt.Sprintf("  Duration: %.4f +/- %.4f sec (3 sigma)", fitDuration, 3*durationStd))
				}
				logAction("--- End Report ---")

				summaryLabel := widget.NewLabel(msg)
				summaryLabel.Wrapping = fyne.TextWrapWord

				var mcContainer *fyne.Container
				if mcShowTrialsCheck.Checked {
					// Temporary: show individual trial edge times
					trialsMsg := "Individual trial edge times:\n"
					numCompleted := 0
					if result.numEdges > 0 {
						numCompleted = len(result.edgeAll[0])
					}
					for t := 0; t < numCompleted; t++ {
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
					fmt.Print(trialsMsg)
					trialsLabel := widget.NewLabel(trialsMsg)
					trialsLabel.TextStyle.Monospace = true
					trialsScroll := container.NewScroll(trialsLabel)
					trialsScroll.SetMinSize(fyne.NewSize(750, 300))
					mcContainer = container.NewVBox(summaryLabel, trialsScroll)
				} else {
					mcSpacer := canvas.NewRectangle(color.Transparent)
					mcSpacer.SetMinSize(fyne.NewSize(750, 0))
					mcContainer = container.NewVBox(mcSpacer, summaryLabel)
				}
				dialog.ShowCustom("Monte Carlo Edge Time Uncertainty", "OK", mcContainer, w)

				// Show histograms if the checkbox is checked
				if mcShowHistogramsCheck.Checked {
					for i := 0; i < result.numEdges; i++ {
						if len(result.edgeAll[i]) < 2 {
							continue
						}
						histImg, err := createHistogramImage(
							result.edgeAll[i],
							fmt.Sprintf("Edge %d Times", i+1),
							"Time (seconds)",
							lastDiffractionTitle,
							900, 500,
						)
						if err != nil {
							fmt.Printf("Failed to create Edge %d histogram: %v\n", i+1, err)
							continue
						}
						histWin := a.NewWindow(fmt.Sprintf("Monte Carlo — Edge %d Histogram", i+1))
						histCanvas := canvas.NewImageFromImage(histImg)
						histCanvas.FillMode = canvas.ImageFillContain
						histWin.SetContent(histCanvas)
						histWin.Resize(fyne.NewSize(950, 550))
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
							lastDiffractionTitle,
							900, 500,
						)
						if err != nil {
							fmt.Printf("Failed to create duration histogram: %v\n", err)
						} else {
							histWin := a.NewWindow("Monte Carlo — Duration Histogram")
							histCanvas := canvas.NewImageFromImage(histImg)
							histCanvas.FillMode = canvas.ImageFillContain
							histWin.SetContent(histCanvas)
							histWin.Resize(fyne.NewSize(950, 550))
							histWin.Show()
						}
					}
				}

				// Create a fit overlay plot with ±3σ edge uncertainty lines
				if len(lastFitTargetTimes) > 0 && len(lastFitTargetValues) > 0 {
					mcOverlayImg, err := createOverlayPlotImage(
						lastFitResult.curve, lastFitResult.bestShift, lastFitResult.edgeTimes,
						lastFitTargetTimes, lastFitTargetValues,
						lastFitResult.sampledTimes, lastFitResult.sampledVals,
						lastFitResult.bestNCC, lastDiffractionTitle,
						1200, 500, result.edgeStds,
					)
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
						mcOverlayWin.Show()
					}
				}
			})
		}()
	})

	tab10Content := container.NewStack(tab10Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Fit"),
		widget.NewSeparator(),
		widget.NewLabel("1. Click two points to mark a baseline region (pair)"),
		widget.NewLabel("2. Repeat to add more baseline regions"),
		widget.NewLabel("3. Click on a marked point to remove that pair"),
		widget.NewSeparator(),
		calcBaselineMeanBtn,
		widget.NewSeparator(),
		fitOffsetLabel,
		fitOffsetEntry,
		searchRangeCard,
		fitBtn,
		widget.NewSeparator(),
		widget.NewLabel("Monte Carlo trials"),
		mcNumTrialsEntry,
		container.NewHBox(mcShowTrialsCheck, mcShowHistogramsCheck),
		mcBtn,
		mcProgressBar,
		widget.NewSeparator(),
		fitStatusLabel,
		fitProgressBar,
	)))
	tab10 := container.NewTabItem("Fit", tab10Content)

	tabs := container.NewAppTabs(tab2, tab3, tab5, tab6, tab7, vizierTab.TabItem, tab9, tab10)

	// Apply dark tab backgrounds if dark mode was persisted
	if prefs.BoolWithFallback("darkMode", false) {
		applyTabBgTheme(true)
	}

	// Track the previously selected tab for cleanup
	var previousTab *container.TabItem

	// Handle tab selection events
	tabs.OnSelected = func(tab *container.TabItem) {
		// Clean up Single Point analysis when leaving tab9
		if previousTab == tab9 && tab != tab9 {
			// Clear analysis state
			singlePointAnalysisReady = false
			singlePointPolyCoeffs = nil

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

			// Clear the smoothed curve
			smoothedSeries = nil

			// Reset status label
			singlePointStatusLabel.SetText("Select two points on the light curve to define a baseline region")

			// Rebuild plot
			savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
			rebuildPlot()
			lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		}
		previousTab = tab

		// Track whether the Single Point tab is active (for point click callbacks)
		onSinglePointTab = tab == tab9

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
			// Fit tab: enable multi-pair selection mode
			lightCurvePlot.SingleSelectMode = false
			lightCurvePlot.MultiPairSelectMode = true
			lightCurvePlot.Refresh()
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

	// Helper function to run IOTAdiffraction with a given parameter file
	runIOTAdiffraction := func(paramFilePath string) {
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
		cmd := exec.Command(exePath, paramFilePath)
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
			if err != nil {
				appendOutput(fmt.Sprintf("\n[Error: %v]", err))
			} else {
				appendOutput("\n[Process completed successfully]")
			}
		}()
	}

	btnIOTA := widget.NewButton("Run IOTAdiffraction", func() {
		// Always ask the user to select a parameter file
		showFileOpenWithRecents(w, prefs, "Select Parameter File", nil, func(reader fyne.URIReadCloser, err error) {
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

			logAction(fmt.Sprintf("Running IOTAdiffraction with parameters file: %s", paramFilePath))
			lastDiffractionParamsPath = paramFilePath
			prefs.SetString("lastDiffractionParamsPath", paramFilePath)
			// Extract and save the title from the parameters file
			lastDiffractionTitle = ""
			if f, err := os.Open(paramFilePath); err == nil {
				if p, err := parseOccultationParameters(f); err == nil {
					lastDiffractionTitle = p.Title
				}
				if err := f.Close(); err != nil {
					fmt.Printf("Warning: failed to close parameters file: %v\n", err)
				}
			}
			prefs.SetString("lastDiffractionTitle", lastDiffractionTitle)
			runIOTAdiffraction(paramFilePath)
		})
	})
	btnOccultParams := widget.NewButton("Edit/Enter Occultation Parameters", func() {
		showOccultationParametersDialog(w)
	})
	buttons := container.NewHBox(btnIOTA, btnOccultParams)

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
