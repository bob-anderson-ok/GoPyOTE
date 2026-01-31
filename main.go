package main

import (
	"bufio"
	"embed"
	"fmt"
	"image/color"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/storage"
	"fyne.io/fyne/v2/widget"

	"github.com/KevinWang15/go-json5"
	"github.com/pconstantinou/savitzkygolay"
)

//go:embed BobTest.md help_images/diffractionImage8bit.png
var bobTestMarkdown embed.FS

//go:embed timestampAnalysis.md help_images/droppedFrameDemoPlot.png help_images/consecutiveOCRerrorDemo.png
var timestampAnalysisMarkdown embed.FS

// Version information
const Version = "1.0.47"

// Track the last loaded parameters file path for use by Run IOTAdiffraction
var lastLoadedParamsPath string

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
func main() {
	a := app.NewWithID("com.gopyote.app")
	w := a.NewWindow("GoPyOTE Version: " + Version)
	w.SetMaster() // Closing this window will quit the app and close all other windows

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
	helpMenu := fyne.NewMenu("Help Topics",
		fyne.NewMenuItem("Light curve normalization", func() {
			dialog.ShowInformation("Light Curve Normalization",
				"Light curve normalization helps correct for atmospheric effects like clouds.\n\n"+
					"To use:\n"+
					"1. Load a CSV file with multiple light curves\n"+
					"2. Check the box next to a comparison star to use as reference\n"+
					"3. The target star's brightness will be divided by the reference", w)
		}),
		fyne.NewMenuItem("Block integration", func() {
			dialog.ShowInformation("Block Integration",
				"Block integration averages consecutive data points to reduce noise.\n\n"+
					"To use:\n"+
					"1. Click on a point to select the start of a block\n"+
					"2. Click on another point in the same light curve to define block size\n"+
					"3. Go to the BlockInt tab and click 'Block Integrate'\n"+
					"4. Points are averaged in groups, with partial blocks at ends ignored\n\n"+
					"Note: Reload the CSV to restore original data.", w)
		}),
		fyne.NewMenuItem("Bob Test", func() {
			content, err := bobTestMarkdown.ReadFile("BobTest.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load BobTest.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Bob Test", string(content), &bobTestMarkdown, w)
		}),
		fyne.NewMenuItem("Dropped frame and OCR detection", func() {
			content, err := timestampAnalysisMarkdown.ReadFile("timestampAnalysis.md")
			if err != nil {
				dialog.ShowError(fmt.Errorf("failed to load timestampAnalysis.md: %w", err), w)
				return
			}
			ShowMarkdownDialogWithImages("Dropped frame and OCR detection", string(content), &timestampAnalysisMarkdown, w)
		}),
		fyne.NewMenuItemSeparator(),
		fyne.NewMenuItem("About", func() {
			aboutMarkdown := fmt.Sprintf(`# GoPyOTE

**Version %s**

A Go desktop application for astronomical occultation timing and analysis.

## Features

- **Light curve visualization** - Interactive plotting with zoom and pan
- **Timing analysis** - Automatic detection of cadence errors and dropped frames
- **Normalization** - Correct for atmospheric effects using reference stars
- **Block integration** - Reduce noise by averaging consecutive readings

## Credits

Developed for the occultation astronomy community.
`, Version)
			ShowMarkdownDialogWithImages("About GoPyOTE", aboutMarkdown, nil, w)
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

	// Bottom section with frame range controls on left and status label on right
	plotBottomRow := container.NewBorder(
		nil,             // top
		nil,             // bottom
		frameRangeRow,   // left
		nil,             // right
		plotStatusLabel, // center (takes remaining space)
	)

	plotArea := container.NewBorder(
		rangeControls,  // top
		plotBottomRow,  // bottom
		nil,            // left
		nil,            // right
		lightCurvePlot, // center
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

			// Save Y bounds before rebuild to preserve Y axis scaling during zoom
			savedMinY, savedMaxY := lightCurvePlot.GetYBounds()
			rebuildPlot()
			// Restore Y bounds after rebuild
			lightCurvePlot.SetYBounds(savedMinY, savedMaxY)
		}
	})

	// Function to open the CSV file dialog
	openCSVDialog := func() {
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
				len(data.Columns), len(lightCurveListData), len(data.TimeValues)))
		}, w)
		fileDialog.SetFilter(storage.NewExtensionFileFilter([]string{".csv"}))
		fileDialog.Resize(fyne.NewSize(1200, 800))
		fileDialog.Show()
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
	tab3 := container.NewTabItem(".csv ops", tab3Content)

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

	// Tab 5: Block integration
	tab5Bg := canvas.NewRectangle(color.RGBA{R: 200, G: 220, B: 200, A: 255})

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
			fmt.Sprintf("Original: %d points\nBlock size: %d\nResult: %d averaged blocks\n\nNote: Reload the CSV to restore original data.", numPoints, blockSize, numBlocks), w)
	})

	tab5Content := container.NewStack(tab5Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Block Integration"),
		widget.NewSeparator(),
		widget.NewLabel("Instructions:"),
		widget.NewLabel("1. Click on a point to select point 1"),
		widget.NewLabel("2. Click on another point in the same light curve"),
		widget.NewLabel("3. The two points define the block size"),
		widget.NewLabel("4. Click 'Block Integrate' to apply"),
		widget.NewSeparator(),
		blockIntegrateButton,
		widget.NewSeparator(),
		blockIntStatusLabel,
	)))
	tab5 := container.NewTabItem("BlockInt", tab5Content)

	// Tab 6: Flash tags
	tab6Bg := canvas.NewRectangle(color.RGBA{R: 220, G: 200, B: 220, A: 255})

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
	tab7Bg := canvas.NewRectangle(color.RGBA{R: 200, G: 200, B: 230, A: 255})

	// Status label for smoothing
	smoothStatusLabel := widget.NewLabel("Select two points to define window size, check a reference curve")

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

		// Check if a reference curve is checked
		if checkedCurveIndex < 0 {
			dialog.ShowError(fmt.Errorf("no reference curve selected - check the box next to a light curve in the list"), w)
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

		// Get the column index of the checked curve
		if checkedCurveIndex >= len(listIndexToColumnIndex) {
			dialog.ShowError(fmt.Errorf("invalid reference curve index"), w)
			return
		}
		refColIdx := listIndexToColumnIndex[checkedCurveIndex]
		refColumn := loadedLightCurveData.Columns[refColIdx]

		// Get the indices of the two selected points to determine window size
		series := lightCurvePlot.series[lightCurvePlot.selectedSeries]
		idx1 := series.Points[lightCurvePlot.selectedIndex].Index
		series2 := lightCurvePlot.series[lightCurvePlot.selectedSeries2]
		idx2 := series2.Points[lightCurvePlot.selectedIndex2].Index

		// Calculate window size
		windowSize := idx2 - idx1
		if windowSize < 0 {
			windowSize = -windowSize
		}
		windowSize++ // inclusive

		// Make window size odd if needed
		if windowSize%2 == 0 {
			windowSize++
		}

		// Minimum window size check
		if windowSize < 3 {
			windowSize = 3
		}

		logAction(fmt.Sprintf("Savitzky-Golay smoothing: window size = %d, reference curve = %s", windowSize, refColumn.Name))

		// Get Y values for the reference column
		ys := refColumn.Values
		numPoints := len(ys)

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
			dialog.ShowError(fmt.Errorf("no smoothed reference curve - click 'Smooth' first"), w)
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

		// Update status
		statusMsg := fmt.Sprintf("Normalized all light curves (reference mean = %.4f)", meanSmooth)
		smoothStatusLabel.SetText(statusMsg)
		logAction(statusMsg)

		dialog.ShowInformation("Normalization Complete",
			fmt.Sprintf("All light curves have been normalized.\n\nReference mean: %.4f\n\nNote: Reload the CSV to restore original data.", meanSmooth), w)
	})

	tab7Content := container.NewStack(tab7Bg, container.NewPadded(container.NewVBox(
		widget.NewLabel("Savitzky-Golay Smoothing & Normalization"),
		widget.NewSeparator(),
		widget.NewLabel("Smoothing Instructions:"),
		widget.NewLabel("1. Check the box next to a light curve to use as reference"),
		widget.NewLabel("2. Click on a point to select point 1"),
		widget.NewLabel("3. Click on another point to define window size"),
		widget.NewLabel("4. Click 'Smooth' to apply Savitzky-Golay filter"),
		widget.NewSeparator(),
		container.NewHBox(smoothButton, clearSmoothButton),
		widget.NewSeparator(),
		widget.NewLabel("Normalization (after smoothing):"),
		widget.NewLabel("5. Click 'Normalize' to apply smoothed reference to all curves"),
		widget.NewLabel("Formula: y_norm[i] = (mean_ref * y[i]) / smooth_ref[i]"),
		widget.NewSeparator(),
		normalizeButton,
		widget.NewSeparator(),
		smoothStatusLabel,
	)))
	tab7 := container.NewTabItem("Smooth", tab7Content)

	tabs := container.NewAppTabs(tab2, tab3, tab5, tab6, tab7, tab4)

	// Handle tab selection events
	tabs.OnSelected = func(tab *container.TabItem) {
		if tab == tab6 {
			// Flash tags tab: enable single select mode
			lightCurvePlot.SingleSelectMode = true
			// Clear point 2 selection when entering the Flash tags tab
			lightCurvePlot.selectedSeries2 = -1
			lightCurvePlot.selectedIndex2 = -1
			lightCurvePlot.selectedPointDataIndex2 = -1
			lightCurvePlot.selectedSeriesName2 = ""
			lightCurvePlot.SelectedPoint2Valid = false
			lightCurvePlot.SelectedPoint2Frame = 0
			lightCurvePlot.SelectedPoint2Value = 0
			lightCurvePlot.Refresh()
		} else {
			lightCurvePlot.SingleSelectMode = false
		}
	}

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
